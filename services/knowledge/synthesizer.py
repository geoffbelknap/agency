"""LLM-based knowledge synthesis pipeline.

Periodically batches unprocessed messages, sends them to the LLM
via the gateway's internal LLM endpoint for entity/relationship
extraction, and writes results to the knowledge graph.

The gateway resolves models via routing.yaml — operators configure
which provider and model to use for synthesis there.

Default trigger conditions favor low/intermittent usage:
  - event mode: synthesize after each new message/content item
  - max once per minute

Set AGENCY_SYNTH_MODE=batch, AGENCY_SYNTH_MSG_THRESHOLD, and
AGENCY_SYNTH_MIN_INTERVAL_SECS to batch more aggressively if budget pressure
appears.
"""

import json
import logging
import os
import time
from pathlib import Path

import httpx
import yaml

from typing import Optional
from services.knowledge.store import KnowledgeStore

logger = logging.getLogger("agency.knowledge.synthesizer")

DEFAULT_SYNTH_MODE = "event"
DEFAULT_MESSAGE_THRESHOLD = 1
DEFAULT_TIME_THRESHOLD_HOURS = 1
DEFAULT_MIN_INTERVAL_SECONDS = 60

MAX_SYNTHESIS_BATCH = 25

EXTRACTION_PROMPT = """\
You are extracting durable organizational knowledge from team conversations.

## What to extract
- People, teams, systems, decisions, incidents, risks, processes, documents — \
anything meaningful to understanding the team's work a week from now.
- Relationships between entities, with correct direction.
- Concise summaries capturing what the team now knows about each entity.

## What to skip
- Greetings, small talk, one-off questions ("what's the weather", "solve 2+2").
- Transient requests that won't matter in a week (ad-hoc lookups, ephemeral tasks).
- If a message is purely conversational with no durable knowledge, skip it entirely.

## Entity type guidance
Use ONLY these entity types for the "kind" field:
{entity_types}

When choosing a type, note these distinctions:
- **decision** = a choice already made (with rationale). **project** = ongoing work. Don't use project for decisions.
- **assumption** = believed true but NOT confirmed. **fact** = verified. **cause** = confirmed root cause.
- **incident** = something that went wrong. **risk** = something that MIGHT go wrong.

## Relationship type guidance
Use ONLY these relationship types for the "relation" field:
{relationship_types}

**Relationship direction matters.** The "source" acts on the "target":
- "Sarah manages the SRE team" → source: "Sarah Kim", target: "SRE team", relation: "manages"
- "The outage was caused by a missing index" → source: "missing index", target: "outage", relation: "caused"
- "Alex reports to Sarah" → source: "Alex Chen", target: "Sarah Kim", relation: "escalate_to"
- "The project depends on the audit" → source: "project", target: "audit", relation: "depends_on"

## Existing Entities
Merge into these rather than creating duplicates:
{existing_labels}

## Messages
{messages}

Output valid JSON:
{{
  "entities": [
    {{"label": "...", "kind": "...", "summary": "..."}}
  ],
  "relationships": [
    {{"source": "...", "target": "...", "relation": "..."}}
  ]
}}

Every entity should have at least one relationship. \
Use only defined types. If something doesn't fit, use the closest match and note it in the summary.
"""

EXTRACTION_PROMPT_FREEFORM = """\
You are extracting knowledge from team conversations. Given the messages below, identify:

1. **Entities** -- things, concepts, components, people, decisions, problems, strategies, \
vulnerabilities, features, or anything meaningful to understanding the team's work.
2. **Relationships** -- how entities relate to each other and to the message authors.
3. **Summaries** -- what the team now understands about each entity.

Use whatever entity labels and relationship types fit the domain. Be consistent with \
existing entities listed below -- merge into them rather than creating duplicates.

Existing entities in the knowledge graph:
{existing_labels}

Messages to analyze:
{messages}

Output valid JSON with this structure:
{{
  "entities": [
    {{"label": "...", "kind": "...", "summary": "..."}}
  ],
  "relationships": [
    {{"source": "...", "target": "...", "relation": "..."}}
  ]
}}

Use whatever "kind" and "relation" values make sense for the domain. \
Be consistent within this batch but do not constrain yourself to a fixed vocabulary.
"""


class LLMSynthesizer:
    def __init__(
        self,
        store: KnowledgeStore,
        message_threshold: Optional[int] = None,
        time_threshold_hours: Optional[int] = None,
        min_interval_seconds: Optional[int] = None,
        curator=None,
    ):
        self.store = store
        self.curator = curator
        self.mode = os.environ.get("AGENCY_SYNTH_MODE", DEFAULT_SYNTH_MODE).lower()
        if self.mode not in ("event", "batch"):
            self.mode = DEFAULT_SYNTH_MODE
        self.message_threshold = message_threshold or int(
            os.environ.get("AGENCY_SYNTH_MSG_THRESHOLD", str(DEFAULT_MESSAGE_THRESHOLD))
        )
        time_hours = time_threshold_hours or int(
            os.environ.get("AGENCY_SYNTH_TIME_THRESHOLD_HOURS", str(DEFAULT_TIME_THRESHOLD_HOURS))
        )
        self.time_threshold_seconds = time_hours * 3600
        self.min_interval_seconds = min_interval_seconds or int(
            os.environ.get("AGENCY_SYNTH_MIN_INTERVAL_SECS", str(DEFAULT_MIN_INTERVAL_SECONDS))
        )
        self._pending_messages: list[dict] = []
        self._pending_ids: set[str] = set()
        self._pending_content: list[dict] = []
        self._last_synthesis = 0.0

        # Config
        synth_timeout = int(os.environ.get("KNOWLEDGE_SYNTH_TIMEOUT", "120"))
        # Model alias — resolved by the gateway via routing.yaml
        self._model = os.environ.get("KNOWLEDGE_SYNTH_MODEL", "fast")
        # Gateway endpoint for LLM calls. Infra normally injects this; fall
        # back to the local mediation proxy when runtime-specific wiring is absent.
        raw_gateway_url = os.environ.get(
            "AGENCY_GATEWAY_URL", "http://localhost:8200"
        )
        self._gateway_token = os.environ.get("AGENCY_GATEWAY_TOKEN", "")

        # Gateway HTTP client: Unix socket when explicitly configured, otherwise TCP.
        if raw_gateway_url.startswith("http+unix://"):
            sock_path = raw_gateway_url.replace("http+unix://", "")
            self._gateway_url = "http://localhost:8200"
            self._http_gateway = httpx.Client(
                timeout=synth_timeout,
                transport=httpx.HTTPTransport(uds=sock_path),
            )
        else:
            self._gateway_url = raw_gateway_url
            self._http_gateway = httpx.Client(timeout=synth_timeout)

        # Load ontology for typed extraction
        self._ontology = self._load_ontology()
        self._ontology_mtime: float = 0.0
        ontology_path = Path(os.environ.get("AGENCY_ONTOLOGY_PATH", "/app/ontology.yaml"))
        if ontology_path.exists():
            self._ontology_mtime = ontology_path.stat().st_mtime

    def record_message(self, msg_id: str, msg: Optional[dict] = None) -> None:
        if msg_id not in self._pending_ids:
            self._pending_ids.add(msg_id)
            if msg:
                self._pending_messages.append(msg)

    def add_content_for_synthesis(self, content: str, scope: dict = None) -> None:
        """Queue raw content for knowledge extraction.

        Args:
            content: Raw text to extract knowledge from.
            scope: Optional scope dict (e.g. source_channels, source_type)
                   passed through to _apply_extraction.
        """
        self._pending_content.append({"content": content, "scope": scope})

    def has_pending_content(self) -> bool:
        """True if there is raw content queued for synthesis."""
        return bool(self._pending_content)

    def should_synthesize(self) -> bool:
        now = time.monotonic()
        if self._last_synthesis and now - self._last_synthesis < self.min_interval_seconds:
            return False
        if self.mode == "event" and (self._pending_ids or self._pending_content):
            return True
        if len(self._pending_ids) >= self.message_threshold:
            return True
        if self._pending_content:
            return True
        if self._last_synthesis and now - self._last_synthesis >= self.time_threshold_seconds:
            return True
        return False

    def synthesize(self, messages: list[dict], source_channels: list[str]) -> None:
        if not messages:
            return
        # Cap batch size to avoid diluting LLM attention on large backlogs.
        # Remaining messages stay in _pending for the next cycle.
        batch = messages[:MAX_SYNTHESIS_BATCH]
        if len(messages) > MAX_SYNTHESIS_BATCH:
            logger.info(
                "Batch capped at %d messages (%d remaining for next cycle)",
                MAX_SYNTHESIS_BATCH, len(messages) - MAX_SYNTHESIS_BATCH,
            )
            # Keep overflow in pending for next synthesis
            self._pending_messages = messages[MAX_SYNTHESIS_BATCH:]
            # Don't clear pending_ids — they'll be cleared when remaining are processed
        prompt = self._build_extraction_prompt(batch)
        t0 = time.monotonic()

        response = self._call_llm(prompt)
        extraction = self._parse_response(response) if response else None

        duration_ms = int((time.monotonic() - t0) * 1000)

        if extraction:
            self._apply_extraction(extraction, source_channels, source_type="llm")
            logger.info(
                "Synthesis complete: %d entities, %d relationships (model=%s)",
                len(extraction.get("entities", [])),
                len(extraction.get("relationships", [])),
                self._model,
            )

        self._log_synthesis({
            "model": self._model,
            "entities_extracted": len(extraction.get("entities", [])) if extraction else 0,
            "relationships_extracted": len(extraction.get("relationships", [])) if extraction else 0,
            "batch_size": len(batch),
            "duration_ms": duration_ms,
        })

        self._last_synthesis = time.monotonic()
        self._pending_messages.clear()
        self._pending_ids.clear()

    def synthesize_content(self) -> None:
        """Process pending raw content through the extraction pipeline.

        Each content item is formatted into an extraction prompt, sent to the LLM,
        and the results are applied to the knowledge graph. Called from the synthesis
        loop when has_pending_content() is True.
        """
        if not self._pending_content:
            return

        batch = list(self._pending_content)
        self._pending_content.clear()

        for item in batch:
            content = item["content"]
            scope = item.get("scope") or {}
            source_channels = scope.get("source_channels", ["ingestion"])
            source_type = scope.get("source_type", "content")

            prompt = self._build_content_extraction_prompt(content)
            t0 = time.monotonic()

            response = self._call_llm(prompt)
            extraction = self._parse_response(response) if response else None

            duration_ms = int((time.monotonic() - t0) * 1000)

            if extraction:
                self._apply_extraction(extraction, source_channels, source_type=source_type)
                logger.info(
                    "Content synthesis complete: %d entities, %d relationships (model=%s)",
                    len(extraction.get("entities", [])),
                    len(extraction.get("relationships", [])),
                    self._model,
                )

            self._log_synthesis({
                "model": self._model,
                "entities_extracted": len(extraction.get("entities", [])) if extraction else 0,
                "relationships_extracted": len(extraction.get("relationships", [])) if extraction else 0,
                "content_length": len(content),
                "source_type": source_type,
                "duration_ms": duration_ms,
            })

        self._last_synthesis = time.monotonic()

    def _build_content_extraction_prompt(self, content: str) -> str:
        """Build an extraction prompt for raw content (not comms messages).

        Uses the same ontology-aware prompt structure as message extraction,
        but formats the content directly instead of as authored messages.
        """
        self._maybe_reload_ontology()

        stats = self.store.stats()
        existing = []
        for kind, count in stats.get("kinds", {}).items():
            nodes = self.store.find_nodes_by_kind(kind, limit=20)
            for n in nodes:
                existing.append(f"- {n['label']} ({kind})")
        existing_text = "\n".join(existing[:50]) if existing else "(none yet)"

        # Truncate very large content to avoid blowing context
        truncated = content[:10000]

        if self._ontology:
            return EXTRACTION_PROMPT.format(
                entity_types=self._format_entity_types(),
                relationship_types=self._format_relationship_types(),
                existing_labels=existing_text,
                messages=truncated,
            )
        return EXTRACTION_PROMPT_FREEFORM.format(
            existing_labels=existing_text,
            messages=truncated,
        )

    def _load_ontology(self) -> Optional[dict]:
        """Load ontology from mounted file. Re-reads on each synthesis cycle if mtime changed."""
        ontology_path = Path(os.environ.get("AGENCY_ONTOLOGY_PATH", "/app/ontology.yaml"))
        if not ontology_path.exists():
            logger.info("No ontology file found at %s, using freeform extraction", ontology_path)
            return None
        try:
            data = yaml.safe_load(ontology_path.read_text())
            logger.info(
                "Loaded ontology v%s: %d entity types, %d relationship types",
                data.get("version", "?"),
                len(data.get("entity_types", {})),
                len(data.get("relationship_types", {})),
            )
            return data
        except Exception as e:
            logger.warning("Failed to load ontology: %s", e)
            return None

    def _maybe_reload_ontology(self) -> None:
        """Reload ontology if the file has been modified (hot-reload support)."""
        ontology_path = Path(os.environ.get("AGENCY_ONTOLOGY_PATH", "/app/ontology.yaml"))
        if not ontology_path.exists():
            return
        try:
            current_mtime = ontology_path.stat().st_mtime
            if current_mtime != self._ontology_mtime:
                logger.info("Ontology file changed, reloading")
                self._ontology = self._load_ontology()
                self._ontology_mtime = current_mtime
        except Exception:
            pass

    def _format_entity_types(self) -> str:
        """Format entity types from ontology for the extraction prompt."""
        if not self._ontology or "entity_types" not in self._ontology:
            return ""
        lines = []
        for name, info in sorted(self._ontology["entity_types"].items()):
            desc = info.get("description", "") if isinstance(info, dict) else str(info)
            lines.append(f"- **{name}**: {desc}")
        return "\n".join(lines)

    def _format_relationship_types(self) -> str:
        """Format relationship types from ontology for the extraction prompt."""
        if not self._ontology or "relationship_types" not in self._ontology:
            return ""
        lines = []
        for name, info in sorted(self._ontology["relationship_types"].items()):
            desc = info.get("description", "") if isinstance(info, dict) else str(info)
            lines.append(f"- **{name}**: {desc}")
        return "\n".join(lines)

    def _build_extraction_prompt(self, messages: list[dict]) -> str:
        # Reload ontology if file changed
        self._maybe_reload_ontology()

        stats = self.store.stats()
        existing = []
        for kind, count in stats.get("kinds", {}).items():
            nodes = self.store.find_nodes_by_kind(kind, limit=20)
            for n in nodes:
                existing.append(f"- {n['label']} ({kind})")
        existing_text = "\n".join(existing[:50]) if existing else "(none yet)"

        msg_text = ""
        for msg in messages[:100]:
            author = msg.get("author", "unknown")
            content = msg.get("content", "")[:500]
            channel = msg.get("channel", "")
            msg_text += f"[{author} in #{channel}]: {content}\n"

        # Use typed prompt if ontology is loaded, freeform otherwise
        if self._ontology:
            return EXTRACTION_PROMPT.format(
                entity_types=self._format_entity_types(),
                relationship_types=self._format_relationship_types(),
                existing_labels=existing_text,
                messages=msg_text,
            )
        return EXTRACTION_PROMPT_FREEFORM.format(
            existing_labels=existing_text,
            messages=msg_text,
        )

    def _call_llm(self, prompt: str) -> Optional[str]:
        """Call LLM via gateway internal endpoint.

        The gateway handles model resolution, format translation,
        cost tracking, and provider proxying via routing.yaml.
        """
        try:
            resp = self._http_gateway.post(
                f"{self._gateway_url}/api/v1/infra/internal/llm",
                json={
                    "model": self._model,
                    "messages": [{"role": "user", "content": prompt}],
                    "max_tokens": 4096,
                },
                headers={
                    "X-Agency-Token": self._gateway_token,
                    "X-Agency-Caller": "knowledge-synthesizer",
                    "Content-Type": "application/json",
                },
            )
            if resp.status_code == 429:
                logger.warning("Infrastructure LLM budget exhausted, skipping synthesis")
                return None
            resp.raise_for_status()
            data = resp.json()
            return data["choices"][0]["message"]["content"]
        except Exception as e:
            logger.error("Synthesis LLM call failed (model=%s): %s", self._model, e)
            return None

    def _log_synthesis(self, entry: dict) -> None:
        """Log a structured synthesis audit record."""
        logger.info("synthesis_audit: %s", json.dumps(entry))

    def _parse_response(self, response: str) -> Optional[dict]:
        text = response.strip()
        if "```json" in text:
            text = text.split("```json")[1].split("```")[0].strip()
        elif "```" in text:
            text = text.split("```")[1].split("```")[0].strip()
        try:
            return json.loads(text)
        except json.JSONDecodeError:
            logger.warning("Failed to parse LLM extraction response: %s", text[:500])
            return None

    def _check_curation(self, node_id: str) -> None:
        """Run post-ingestion curator check if curator is available."""
        if self.curator is None:
            return
        try:
            self.curator.post_ingestion_check(node_id)
        except Exception as e:
            logger.warning("Curator post-ingestion check failed for %s: %s", node_id, e)

    def _validate_kind(self, kind: str) -> str:
        """Validate an entity kind against the ontology. Returns the validated kind."""
        if not self._ontology or not kind:
            return kind or "fact"

        entity_types = self._ontology.get("entity_types", {})
        lower = kind.lower()

        # Exact match
        if lower in entity_types:
            return lower

        # Common aliases (mirrors Go ValidateNode)
        aliases = {
            "agent": "system", "application": "system", "app": "software",
            "platform": "system", "database": "system", "repository": "system",
            "repo": "system", "topic": "concept", "idea": "concept",
            "notion": "concept", "observation": "finding", "discovery": "finding",
            "insight": "finding", "issue": "incident", "bug": "incident",
            "problem": "incident", "choice": "decision",
            "resolution_decision": "decision", "company": "organization",
            "org": "organization", "vendor": "organization",
            "department": "organization", "member": "person", "user": "person",
            "operator": "person", "customer": "person", "workflow": "process",
            "runbook": "process", "sop": "process", "ticket": "task",
            "pr": "task", "pull_request": "task", "meeting": "event",
            "deadline": "event", "release": "event", "milestone": "event",
            "fix": "resolution", "patch": "resolution", "hotfix": "resolution",
            "hack": "workaround", "temp_fix": "workaround", "doc": "document",
            "spec": "document", "report": "document", "wiki": "document",
            "policy": "rule", "kpi": "metric", "sla": "metric",
            "link": "url", "reference": "url", "file": "artifact",
            "dashboard": "artifact", "api": "service", "endpoint": "service",
            "term": "terminology", "jargon": "terminology", "concern": "risk",
            "threat": "risk", "note": "fact", "info": "fact",
            "information": "fact", "data": "fact", "component": "system",
            # Asset inventory types
            "package": "software", "library": "software",
            "firmware": "software", "binary": "software",
            "config": "config_item", "setting": "config_item", "parameter": "config_item",
            "behavior": "behavior_pattern", "pattern": "behavior_pattern",
        }
        if lower in aliases:
            mapped = aliases[lower]
            logger.info("Mapped entity kind '%s' to '%s'", kind, mapped)
            return mapped

        # Substring match against defined types
        for type_name in entity_types:
            if lower in type_name or type_name in lower:
                logger.info("Fuzzy-matched entity kind '%s' to '%s'", kind, type_name)
                return type_name

        # Fallback
        logger.warning("Unknown entity kind '%s', falling back to 'fact'", kind)
        return "fact"

    def _validate_relation(self, relation: str) -> str:
        """Validate a relationship type against the ontology. Returns the validated relation."""
        if not self._ontology or not relation:
            return relation or "relates_to"

        rel_types = self._ontology.get("relationship_types", {})
        lower = relation.lower()

        # Exact match
        if lower in rel_types:
            return lower

        # Check inverses
        for name, info in rel_types.items():
            if isinstance(info, dict) and info.get("inverse", "").lower() == lower:
                return lower  # Inverse is a valid label

        # Common aliases
        aliases = {
            "related": "relates_to", "related_to": "relates_to",
            "has": "owns", "belongs_to": "part_of", "includes": "contains",
            "member_of": "part_of", "led_by": "managed_by",
            "affects": "relates_to", "impacts": "relates_to",
            "requires": "depends_on", "needed_by": "depended_on_by",
            "fixed_by": "resolved_by", "reports_to": "escalate_to",
            # Asset inventory relations
            "runs": "has_software", "installed": "has_software",
            "configured_with": "has_config", "shows": "exhibited",
            "resembles": "similar_to", "before": "preceded",
            "talks_to": "communicates_with", "connects_to": "communicates_with",
        }
        if lower in aliases:
            mapped = aliases[lower]
            logger.info("Mapped relationship '%s' to '%s'", relation, mapped)
            return mapped

        # Substring match
        for type_name in rel_types:
            if lower in type_name or type_name in lower:
                logger.info("Fuzzy-matched relationship '%s' to '%s'", relation, type_name)
                return type_name

        # Fallback
        logger.warning("Unknown relationship type '%s', falling back to 'relates_to'", relation)
        return "relates_to"

    def _apply_extraction(
        self, extraction: dict, source_channels: list[str], source_type: str = "llm"
    ) -> None:
        entities = extraction.get("entities", [])
        relationships = extraction.get("relationships", [])

        # Ontology version for forensics
        ontology_version = self._ontology.get("version") if self._ontology else None

        node_map: dict[str, str] = {}
        for entity in entities:
            label = entity.get("label", "")
            if not label:
                continue
            kind = self._validate_kind(entity.get("kind", "concept"))
            summary = entity.get("summary", "")

            # Build properties with ontology version stamp
            properties = {}
            if ontology_version is not None:
                properties["_ontology_version"] = ontology_version

            existing = self.store.find_nodes(label)
            matched = None
            for e in existing:
                if e["label"].lower() == label.lower():
                    matched = e
                    break

            if matched:
                if summary and len(summary) > len(matched.get("summary", "")):
                    self.store.update_node(matched["id"], summary=summary, properties=properties)
                    self._check_curation(matched["id"])
                node_map[label] = matched["id"]
            else:
                node_id = self.store.add_node(
                    label=label,
                    kind=kind,
                    summary=summary,
                    properties=properties,
                    source_type=source_type,
                    source_channels=source_channels,
                )
                self._check_curation(node_id)
                node_map[label] = node_id

        for rel in relationships:
            source_label = rel.get("source", "")
            target_label = rel.get("target", "")
            relation = self._validate_relation(rel.get("relation", "related"))

            source_id = node_map.get(source_label)
            target_id = node_map.get(target_label)

            if not source_id:
                found = self.store.find_nodes(source_label)
                for f in found:
                    if f["label"].lower() == source_label.lower():
                        source_id = f["id"]
                        break
            if not target_id:
                found = self.store.find_nodes(target_label)
                for f in found:
                    if f["label"].lower() == target_label.lower():
                        target_id = f["id"]
                        break

            if source_id and target_id:
                self.store.add_edge(
                    source_id=source_id,
                    target_id=target_id,
                    relation=relation,
                    provenance="AMBIGUOUS",
                    source_channel=source_channels[0] if source_channels else "",
                )

    def migrate_freeform_kinds(self) -> dict:
        """One-time migration of freeform kind values to ontology types.

        Returns a summary dict with counts of remapped and unchanged nodes.
        """
        if not self._ontology:
            logger.info("No ontology loaded, skipping freeform migration")
            return {"remapped": 0, "unchanged": 0, "total": 0}

        marker_path = Path(self.store.data_dir) / ".ontology-migrated"
        if marker_path.exists():
            logger.info("Ontology migration already completed, skipping")
            return {"remapped": 0, "unchanged": 0, "total": 0, "skipped": True}

        stats = self.store.stats()
        remapped = 0
        unchanged = 0
        total = 0

        for kind, count in stats.get("kinds", {}).items():
            nodes = self.store.find_nodes_by_kind(kind, limit=10000)
            for node in nodes:
                total += 1
                validated = self._validate_kind(kind)
                if validated != kind:
                    # Merge migration metadata into existing properties
                    existing_props = json.loads(node.get("properties", "{}"))
                    existing_props["_migrated"] = True
                    existing_props["_original_kind"] = kind
                    self.store.update_node(
                        node["id"],
                        kind=validated,
                        properties=existing_props,
                    )
                    remapped += 1
                else:
                    unchanged += 1

        logger.info(
            "Ontology migration complete: %d total, %d remapped, %d unchanged",
            total, remapped, unchanged,
        )
        marker_path.write_text(f"migrated={total} remapped={remapped} unchanged={unchanged}\n")

        return {"remapped": remapped, "unchanged": unchanged, "total": total}
