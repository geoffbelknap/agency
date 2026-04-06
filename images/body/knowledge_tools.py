"""Knowledge graph query tools for the body runtime.

Registers tools that let agents query the organizational knowledge graph
through the knowledge HTTP server.
"""

import json
import logging
import os
from pathlib import Path

import httpx
from typing import Optional

try:
    import yaml
except ImportError:
    yaml = None  # type: ignore[assignment]

logger = logging.getLogger("agency.body.knowledge_tools")
_http = httpx.Client(timeout=10)

# Ontology cache for kind validation
_ontology_cache: Optional[dict] = None
_ontology_mtime: float = 0.0


def _load_ontology() -> Optional[dict]:
    """Load the ontology from the mounted file for kind validation."""
    global _ontology_cache, _ontology_mtime
    ontology_path = Path(os.environ.get(
        "AGENCY_ONTOLOGY_PATH", "/agency/knowledge/ontology.yaml"
    ))
    if not ontology_path.exists() or yaml is None:
        return _ontology_cache

    try:
        current_mtime = ontology_path.stat().st_mtime
        if current_mtime != _ontology_mtime:
            data = yaml.safe_load(ontology_path.read_text())
            _ontology_cache = data
            _ontology_mtime = current_mtime
            logger.info("Loaded ontology v%s for kind validation", data.get("version", "?"))
    except Exception as e:
        logger.warning("Failed to load ontology: %s", e)
    return _ontology_cache


def _validate_kind(kind: str) -> str:
    """Validate and optionally correct a kind against the ontology.

    Returns the validated/corrected kind. Falls back to 'fact' for unknown kinds.
    """
    ontology = _load_ontology()
    if not ontology or "entity_types" not in ontology:
        return kind  # No ontology, accept as-is

    entity_types = ontology["entity_types"]
    lower = kind.lower()

    # Exact match
    if lower in entity_types:
        return lower

    # Common aliases
    aliases = {
        "agent": "system", "application": "system", "app": "software",
        "platform": "system", "database": "system", "repository": "system",
        "repo": "system", "topic": "concept", "idea": "concept",
        "observation": "finding", "discovery": "finding", "insight": "finding",
        "issue": "incident", "bug": "incident", "problem": "incident",
        "choice": "decision", "company": "organization", "org": "organization",
        "vendor": "organization", "department": "organization",
        "member": "person", "user": "person", "operator": "person",
        "customer": "person", "workflow": "process", "runbook": "process",
        "ticket": "task", "pr": "task", "meeting": "event",
        "deadline": "event", "release": "event", "milestone": "event",
        "fix": "resolution", "doc": "document", "spec": "document",
        "policy": "rule", "kpi": "metric", "sla": "metric",
        "link": "url", "file": "artifact", "api": "service",
        "term": "terminology", "concern": "risk", "note": "fact",
        "info": "fact", "information": "fact", "data": "fact",
        # Asset inventory types
        "package": "software", "library": "software",
        "firmware": "software", "binary": "software",
        "config": "config_item", "setting": "config_item", "parameter": "config_item",
        "behavior": "behavior_pattern", "pattern": "behavior_pattern",
    }
    if lower in aliases:
        mapped = aliases[lower]
        logger.info("Mapped kind '%s' to '%s'", kind, mapped)
        return mapped

    # Substring match
    for type_name in entity_types:
        if lower in type_name or type_name in lower:
            logger.info("Mapped kind '%s' to '%s' (substring match)", kind, type_name)
            return type_name

    # Fallback
    logger.info("Unknown kind '%s', stored as 'fact'", kind)
    return "fact"


def register_knowledge_tools(registry, knowledge_url: str, agent_name: str, active_mission: Optional[dict] = None) -> None:
    registry.register_tool(
        name="contribute_knowledge",
        description=(
            "Contribute a finding or piece of knowledge to the organizational "
            "knowledge graph so other agents can benefit from it. Use this after "
            "completing research, analysis, or any work that produces durable "
            "organizational value. The knowledge persists and is queryable by "
            "all agents."
        ),
        parameters={
            "type": "object",
            "properties": {
                "topic": {
                    "type": "string",
                    "description": "The topic or subject of the knowledge (e.g. 'React performance', 'Customer X requirements')",
                },
                "summary": {
                    "type": "string",
                    "description": "A clear, concise summary of what was found or learned",
                },
                "kind": {
                    "type": "string",
                    "description": (
                        "Entity type from the ontology. Common types: person, system, decision, "
                        "finding, fact, concept, project, task, incident, pattern, lesson, "
                        "preference, risk, process, requirement, goal, rule, metric, document, "
                        "artifact, narrative, priority, constraint, context, status, opinion, "
                        "assumption, terminology, event, change, schedule, resolution, workaround, "
                        "cause, team, organization, role, service, environment, configuration, "
                        "product, credential, stakeholder, contact, template, standard, location, "
                        "skill, quantity, url, tension"
                    ),
                },
                "properties": {
                    "type": "object",
                    "description": "Optional additional structured data (sources, links, dates, etc.)",
                },
            },
            "required": ["topic", "summary"],
        },
        handler=lambda args: _contribute_knowledge(knowledge_url, agent_name, args, active_mission=active_mission),
    )

    registry.register_tool(
        name="query_knowledge",
        description=(
            "Query the organizational knowledge graph for synthesized "
            "understanding about a topic. Returns what the organization "
            "knows, not just raw messages."
        ),
        parameters={
            "type": "object",
            "properties": {
                "query": {
                    "type": "string",
                    "description": "What you want to know about",
                },
            },
            "required": ["query"],
        },
        handler=lambda args: _query_knowledge(knowledge_url, agent_name, args),
    )

    registry.register_tool(
        name="who_knows_about",
        description=(
            "Find which agents or humans have knowledge about a topic, "
            "based on their observed discussions and work."
        ),
        parameters={
            "type": "object",
            "properties": {
                "topic": {
                    "type": "string",
                    "description": "Topic to find experts on",
                },
            },
            "required": ["topic"],
        },
        handler=lambda args: _who_knows_about(knowledge_url, agent_name, args),
    )

    registry.register_tool(
        name="what_changed_since",
        description=(
            "Get a summary of what changed in organizational knowledge "
            "since a timestamp. Useful for catching up after being halted."
        ),
        parameters={
            "type": "object",
            "properties": {
                "since": {
                    "type": "string",
                    "description": "ISO 8601 timestamp to get changes after",
                },
            },
            "required": ["since"],
        },
        handler=lambda args: _what_changed_since(knowledge_url, agent_name, args),
    )

    registry.register_tool(
        name="get_context",
        description=(
            "Get everything the organization knows about a subject -- "
            "related concepts, who is involved, decisions made, "
            "connected topics."
        ),
        parameters={
            "type": "object",
            "properties": {
                "subject": {
                    "type": "string",
                    "description": "Subject to get full context on",
                },
            },
            "required": ["subject"],
        },
        handler=lambda args: _get_context(knowledge_url, agent_name, args),
    )

    registry.register_tool(
        name="query_graph",
        description="Query the knowledge graph by entity ID, relationships, or property filters. Returns structured nodes and edges, not text search results.",
        parameters={
            "type": "object",
            "properties": {
                "pattern": {"type": "string", "enum": ["get_entity", "get_neighbors", "filter_entities", "find_similar", "get_community", "list_communities", "get_hubs"], "description": "Query pattern"},
                "id": {"type": "string", "description": "Node ID (for get_entity, get_neighbors, find_similar)"},
                "relation": {"type": "string", "description": "Edge relation type (for get_neighbors)"},
                "kind": {"type": "string", "description": "Entity kind (for filter_entities)"},
                "property": {"type": "string", "description": "Property name (for filter_entities)"},
                "value": {"type": "string", "description": "Property value (for filter_entities)"},
                "limit": {"type": "integer", "description": "Max results (for find_similar, default 10)"},
            },
            "required": ["pattern"],
        },
        handler=lambda args: _query_graph(knowledge_url, agent_name, args),
    )


def _contribute_knowledge(base_url: str, agent_name: str, args: dict, active_mission: Optional[dict] = None) -> str:
    try:
        raw_kind = args.get("kind", "finding")
        validated_kind = _validate_kind(raw_kind)
        node = {
            "label": args["topic"],
            "kind": validated_kind,
            "summary": args["summary"],
            "source_type": "agent",
            "properties": {
                **(args.get("properties") or {}),
                "contributed_by": agent_name,
            },
        }
        if validated_kind != raw_kind:
            node["properties"]["original_kind"] = raw_kind
        # Tag with mission_id when contributing during active mission work.
        if active_mission and active_mission.get("status") == "active":
            node["mission_id"] = active_mission.get("id")
        resp = _http.post(f"{base_url}/ingest/nodes", json={"nodes": [node]})
        if resp.status_code == 200:
            return json.dumps({"status": "ok", "message": f"Knowledge about '{args['topic']}' contributed to organizational graph"})
        return json.dumps({"error": f"Ingest returned {resp.status_code}: {resp.text}"})
    except Exception as e:
        return json.dumps({"error": f"Knowledge contribution failed: {e}"})


def _query_knowledge(base_url: str, agent_name: str, args: dict) -> str:
    try:
        resp = _http.post(
            f"{base_url}/query",
            json={"query": args["query"], "agent": agent_name},
        )
        return resp.text
    except Exception as e:
        return json.dumps({"error": f"Knowledge query failed: {e}"})


def _who_knows_about(base_url: str, agent_name: str, args: dict) -> str:
    try:
        resp = _http.get(
            f"{base_url}/who-knows",
            params={"topic": args["topic"]},
        )
        return resp.text
    except Exception as e:
        return json.dumps({"error": f"Who-knows query failed: {e}"})


def _what_changed_since(base_url: str, agent_name: str, args: dict) -> str:
    try:
        resp = _http.get(
            f"{base_url}/changes",
            params={"since": args["since"]},
        )
        return resp.text
    except Exception as e:
        return json.dumps({"error": f"Changes query failed: {e}"})


def _get_context(base_url: str, agent_name: str, args: dict) -> str:
    try:
        resp = _http.get(
            f"{base_url}/context",
            params={"subject": args["subject"]},
        )
        return resp.text
    except Exception as e:
        return json.dumps({"error": f"Context query failed: {e}"})


def _query_graph(base_url: str, agent_name: str, args: dict) -> str:
    """Structured knowledge graph query — by entity, neighbors, or property filter."""
    pattern = args.get("pattern")
    if not pattern:
        return json.dumps({"error": "pattern is required (get_entity, get_neighbors, filter_entities)"})
    try:
        if pattern == "get_entity":
            node_id = args.get("id")
            if not node_id:
                return json.dumps({"error": "id is required for get_entity"})
            resp = _http.get(f"{base_url}/graph/node/{node_id}", params={"agent": agent_name})
        elif pattern == "get_neighbors":
            node_id = args.get("id")
            if not node_id:
                return json.dumps({"error": "id is required for get_neighbors"})
            params = {"agent": agent_name}
            if args.get("relation"):
                params["relation"] = args["relation"]
            resp = _http.get(f"{base_url}/graph/neighbors/{node_id}", params=params)
        elif pattern == "filter_entities":
            kind = args.get("kind")
            prop = args.get("property")
            value = args.get("value")
            if not all([kind, prop, value]):
                return json.dumps({"error": "kind, property, and value required for filter_entities"})
            resp = _http.get(f"{base_url}/graph/filter", params={"kind": kind, "property": prop, "value": value, "agent": agent_name})
        elif pattern == "find_similar":
            node_id = args.get("id")
            if not node_id:
                return json.dumps({"error": "id is required for find_similar"})
            params: dict = {"agent": agent_name}
            if args.get("limit"):
                params["limit"] = str(args["limit"])
            resp = _http.get(f"{base_url}/graph/similar/{node_id}", params=params)
        elif pattern == "get_community":
            node_id = args.get("id")
            if not node_id:
                return json.dumps({"error": "id is required for get_community"})
            # First get the node to find its community_id
            node_resp = _http.get(f"{base_url}/graph/node/{node_id}", params={"agent": agent_name})
            try:
                node_data = node_resp.json()
            except Exception:
                return json.dumps({"error": f"Failed to parse node response: {node_resp.text}"})
            community_id = node_data.get("community_id")
            if not community_id:
                return json.dumps({"error": f"Node {node_id} has no community_id", "node": node_data})
            resp = _http.get(f"{base_url}/community/{community_id}")
        elif pattern == "list_communities":
            resp = _http.get(f"{base_url}/communities")
        elif pattern == "get_hubs":
            hub_params = {}
            if args.get("limit"):
                hub_params["limit"] = str(args["limit"])
            resp = _http.get(f"{base_url}/hubs", params=hub_params)
        else:
            return json.dumps({"error": f"unknown pattern: {pattern}"})
        return resp.text
    except Exception as e:
        return json.dumps({"error": f"Graph query failed: {e}"})
