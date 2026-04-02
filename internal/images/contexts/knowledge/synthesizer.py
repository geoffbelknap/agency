"""LLM-based knowledge synthesis pipeline.

Periodically batches unprocessed messages, sends them to the LLM
via the Anthropic Messages API for entity/relationship extraction,
and writes results to the knowledge graph.

Trigger conditions (whichever comes first):
  - 10 new messages since last synthesis
  - 1 hour since last synthesis
  - Max once per 5 minutes
"""

import json
import logging
import os
import time
from pathlib import Path

import httpx
import yaml

from agency_core.images.knowledge.store import KnowledgeStore

logger = logging.getLogger("agency.knowledge.synthesizer")

DEFAULT_MESSAGE_THRESHOLD = 10
DEFAULT_TIME_THRESHOLD_HOURS = 1
DEFAULT_MIN_INTERVAL_SECONDS = 300

EXTRACTION_PROMPT = """\
You are extracting knowledge from team conversations. Given the messages below, identify:

1. **Entities** -- things, concepts, components, people, decisions, problems, strategies, \
vulnerabilities, features, or anything meaningful to understanding the team's work.
2. **Relationships** -- how entities relate to each other and to the message authors.
3. **Summaries** -- what the team now understands about each entity.

Be consistent with existing entities listed below -- merge into them rather than creating duplicates.

## Entity Types
Use ONLY these entity types for the "kind" field:
{entity_types}

## Relationship Types
Use ONLY these relationship types for the "relation" field:
{relationship_types}

## Existing Entities
{existing_labels}

## Messages
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

Use only defined entity types for "kind" and defined relationship types for "relation". \
If something doesn't fit any defined type, use the closest match and note it in the summary. \
Do not invent new types.
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
        message_threshold: int | None = None,
        time_threshold_hours: int | None = None,
        min_interval_seconds: int | None = None,
        curator=None,
    ):
        self.store = store
        self.curator = curator
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
        self._last_synthesis = 0.0

        # Config
        synth_timeout = int(os.environ.get("KNOWLEDGE_SYNTH_TIMEOUT", "120"))
        self._local_model_enabled = os.environ.get(
            "KNOWLEDGE_LOCAL_MODEL_ENABLED", "false"
        ).lower() == "true"
        self._model_name = os.environ.get("KNOWLEDGE_LOCAL_MODEL", "qwen2.5:3b")
        self._fallback_enabled = os.environ.get(
            "KNOWLEDGE_SYNTH_FALLBACK", "true"
        ).lower() != "false"
        # Model alias — resolved by the gateway via routing.yaml
        self._fallback_model = os.environ.get(
            "KNOWLEDGE_SYNTH_MODEL", "claude-haiku"
        )
        # Gateway endpoint for LLM calls (replaces direct provider access)
        self._gateway_url = os.environ.get(
            "AGENCY_GATEWAY_URL", "http://host.docker.internal:8200"
        )
        self._gateway_token = os.environ.get("AGENCY_GATEWAY_TOKEN", "")
        self._admin_model_url = (
            "http://agency-infra-admin-model:11434/v1/chat/completions"
        )

        # HTTP clients: admin model (no proxy) and gateway (no proxy — reachable via host.docker.internal)
        self._http_admin = httpx.Client(timeout=synth_timeout)
        self._http_gateway = httpx.Client(timeout=synth_timeout)

        # Graduated trust — load persisted state or defaults
        self._validation_threshold = float(os.environ.get(
            "KNOWLEDGE_LOCAL_MODEL_VALIDATION_THRESHOLD", "0.70"
        ))
        self._validation_batch_count = int(os.environ.get(
            "KNOWLEDGE_LOCAL_MODEL_VALIDATION_BATCHES", "3"
        ))
        self._validation_state_file = Path(store.data_dir) / "validation_state.json"
        self._local_model_validated, self._validation_recalls, self._validation_batches_remaining = \
            self._load_validation_state()

        # Env override: operator can bypass validation entirely
        if os.environ.get("KNOWLEDGE_LOCAL_MODEL_VALIDATED", "false").lower() == "true":
            self._local_model_validated = True

        # Load ontology for typed extraction
        self._ontology = self._load_ontology()
        self._ontology_mtime: float = 0.0
        ontology_path = Path(os.environ.get("AGENCY_ONTOLOGY_PATH", "/app/ontology.yaml"))
        if ontology_path.exists():
            self._ontology_mtime = ontology_path.stat().st_mtime

    def record_message(self, msg_id: str, msg: dict | None = None) -> None:
        if msg_id not in self._pending_ids:
            self._pending_ids.add(msg_id)
            if msg:
                self._pending_messages.append(msg)

    def should_synthesize(self) -> bool:
        now = time.monotonic()
        if self._last_synthesis and now - self._last_synthesis < self.min_interval_seconds:
            return False
        if len(self._pending_ids) >= self.message_threshold:
            return True
        if self._last_synthesis and now - self._last_synthesis >= self.time_threshold_seconds:
            return True
        return False

    def synthesize(self, messages: list[dict], source_channels: list[str]) -> None:
        if not messages:
            return
        prompt = self._build_extraction_prompt(messages)
        source_type = "local"
        fallback_triggered = False
        fallback_reason = None
        model_used = self._model_name
        t0 = time.monotonic()

        response = None
        extraction = None

        if self._local_model_enabled:
            response = self._call_admin_model(prompt)
            if response:
                extraction = self._parse_response(response)
                if not extraction or not extraction.get("entities"):
                    fallback_reason = "empty_extraction" if extraction is not None else "parse_error"
                    response = None
                    extraction = None
            else:
                fallback_reason = "connection_refused"

            # Graduated trust: dual-run when not yet validated
            if (
                extraction is not None
                and not self._local_model_validated
                and self._validation_batches_remaining > 0
            ):
                reference_response = self._call_llm(prompt)
                if reference_response:
                    reference_extraction = self._parse_response(reference_response)
                    if reference_extraction:
                        local_entities = extraction.get("entities", [])
                        reference_entities = reference_extraction.get("entities", [])
                        recall = self._compute_recall(local_entities, reference_entities)
                        self._validation_recalls.append(recall)
                        self._validation_batches_remaining -= 1
                        logger.info(
                            "Graduated trust batch: recall=%.2f, remaining=%d",
                            recall, self._validation_batches_remaining,
                        )
                        if self._validation_batches_remaining <= 0:
                            avg_recall = (
                                sum(self._validation_recalls) / len(self._validation_recalls)
                                if self._validation_recalls else 0.0
                            )
                            if avg_recall >= self._validation_threshold:
                                self._local_model_validated = True
                                logger.info(
                                    "Local model validated: avg_recall=%.2f >= %.2f",
                                    avg_recall, self._validation_threshold,
                                )
                            else:
                                logger.warning(
                                    "Local model validation failed: avg_recall=%.2f < %.2f",
                                    avg_recall, self._validation_threshold,
                                )
                        self._save_validation_state()

        if extraction is None and (self._fallback_enabled or not self._local_model_enabled):
            fallback_triggered = self._local_model_enabled  # only a "fallback" if local was tried
            response = self._call_llm(prompt)
            if response:
                extraction = self._parse_response(response)
                source_type = "llm"
                model_used = self._fallback_model

        duration_ms = int((time.monotonic() - t0) * 1000)

        if extraction:
            self._apply_extraction(extraction, source_channels, source_type=source_type)
            logger.info(
                "Synthesis complete: %d entities, %d relationships (model=%s, fallback=%s)",
                len(extraction.get("entities", [])),
                len(extraction.get("relationships", [])),
                model_used,
                fallback_triggered,
            )

        self._log_synthesis({
            "model_attempted": self._model_name if self._local_model_enabled else self._fallback_model,
            "model_used": model_used,
            "fallback_triggered": fallback_triggered,
            "fallback_reason": fallback_reason,
            "entities_extracted": len(extraction.get("entities", [])) if extraction else 0,
            "relationships_extracted": len(extraction.get("relationships", [])) if extraction else 0,
            "source_type": source_type,
            "batch_size": len(messages),
            "duration_ms": duration_ms,
        })

        self._last_synthesis = time.monotonic()
        self._pending_messages.clear()
        self._pending_ids.clear()

    def _load_validation_state(self) -> tuple[bool, list[float], int]:
        """Load graduated trust state from disk. Reset if model changed."""
        try:
            if self._validation_state_file.exists():
                state = json.loads(self._validation_state_file.read_text())
                if state.get("model_name") != self._model_name:
                    logger.warning(
                        "Model changed from %s to %s, resetting validation",
                        state.get("model_name"), self._model_name,
                    )
                    return False, [], self._validation_batch_count
                return (
                    state.get("validated", False),
                    state.get("recalls", []),
                    max(0, self._validation_batch_count - len(state.get("recalls", []))),
                )
        except Exception as e:
            logger.warning("Failed to load validation state: %s", e)
        return False, [], self._validation_batch_count

    def _save_validation_state(self) -> None:
        """Persist graduated trust state to disk."""
        try:
            state = {
                "validated": self._local_model_validated,
                "model_name": self._model_name,
                "recalls": self._validation_recalls,
            }
            self._validation_state_file.write_text(json.dumps(state))
        except Exception as e:
            logger.warning("Failed to save validation state: %s", e)

    def _compute_recall(self, local_entities: list[dict], reference_entities: list[dict]) -> float:
        """Compute entity recall: fraction of Haiku entities found by local model."""
        if not reference_entities:
            return 1.0
        reference_labels = {e.get("label", "").lower() for e in reference_entities}
        local_labels = {e.get("label", "").lower() for e in local_entities}
        found = reference_labels & local_labels
        return len(found) / len(reference_labels)

    def _load_ontology(self) -> dict | None:
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

    def _pull_model(self) -> bool:
        """Trigger lazy model pull on admin model container."""
        pull_timeout = int(os.environ.get("KNOWLEDGE_SYNTH_PULL_TIMEOUT", "600"))
        pull_url = "http://agency-infra-admin-model:11434/api/pull"
        try:
            logger.info("Pulling model %s (this may take several minutes)...", self._model_name)
            with httpx.Client(timeout=pull_timeout) as client:
                resp = client.post(
                    pull_url,
                    json={"name": self._model_name},
                )
                resp.raise_for_status()
                logger.info("Model %s pull complete.", self._model_name)
                return True
        except Exception as e:
            logger.error("Model pull failed: %s", e)
            return False

    def _send_admin_request(self, prompt: str) -> str:
        """Send a request to the admin model and return the content string."""
        resp = self._http_admin.post(
            self._admin_model_url,
            json={
                "model": self._model_name,
                "messages": [{"role": "user", "content": prompt}],
                "max_tokens": 4096,
                "temperature": 0.1,
            },
            headers={"content-type": "application/json"},
        )
        resp.raise_for_status()
        data = resp.json()
        return data["choices"][0]["message"]["content"]

    def _call_admin_model(self, prompt: str) -> str | None:
        """Call the admin model on the mediation network (OpenAI-compatible)."""
        try:
            return self._send_admin_request(prompt)
        except Exception as e:
            error_str = str(e).lower()
            if "not found" in error_str or "404" in error_str:
                logger.info("Model not found, attempting lazy pull...")
                if self._pull_model():
                    try:
                        return self._send_admin_request(prompt)
                    except Exception as retry_e:
                        logger.warning("Admin model retry after pull failed: %s", retry_e)
            else:
                logger.warning("Admin model call failed: %s", e)
            return None

    def _call_llm(self, prompt: str) -> str | None:
        """Call LLM via gateway internal endpoint.

        The gateway handles model resolution, format translation,
        cost tracking, and provider proxying. We always send and
        receive OpenAI-compatible format.
        """
        try:
            resp = self._http_gateway.post(
                f"{self._gateway_url}/api/v1/internal/llm",
                json={
                    "model": self._fallback_model,
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
            logger.error("Synthesis LLM call failed (model=%s): %s", self._fallback_model, e)
            return None

    def _log_synthesis(self, entry: dict) -> None:
        """Log a structured synthesis audit record."""
        logger.info("synthesis_audit: %s", json.dumps(entry))

    def _parse_response(self, response: str) -> dict | None:
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

    def _apply_extraction(
        self, extraction: dict, source_channels: list[str], source_type: str = "llm"
    ) -> None:
        entities = extraction.get("entities", [])
        relationships = extraction.get("relationships", [])

        node_map: dict[str, str] = {}
        for entity in entities:
            label = entity.get("label", "")
            if not label:
                continue
            kind = entity.get("kind", "concept")
            summary = entity.get("summary", "")

            existing = self.store.find_nodes(label)
            matched = None
            for e in existing:
                if e["label"].lower() == label.lower():
                    matched = e
                    break

            if matched:
                if summary and len(summary) > len(matched.get("summary", "")):
                    self.store.update_node(matched["id"], summary=summary)
                    self._check_curation(matched["id"])
                node_map[label] = matched["id"]
            else:
                node_id = self.store.add_node(
                    label=label,
                    kind=kind,
                    summary=summary,
                    source_type=source_type,
                    source_channels=source_channels,
                )
                self._check_curation(node_id)
                node_map[label] = node_id

        for rel in relationships:
            source_label = rel.get("source", "")
            target_label = rel.get("target", "")
            relation = rel.get("relation", "related")

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
                    source_channel=source_channels[0] if source_channels else "",
                )
