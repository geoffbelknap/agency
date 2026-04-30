"""Knowledge graph HTTP server.

aiohttp server exposing knowledge query and ingestion endpoints.
Runs on the mediation network as shared infrastructure.

Endpoints:
    GET  /health                  - Health check
    POST /query                   - Query knowledge (synthesized search)
    GET  /who-knows?topic=X       - Find agents who know about a topic
    GET  /changes?since=T         - What changed since timestamp
    GET  /context?subject=X       - Full context about a subject
    GET  /org-context?agent=X    - Organizational context scoped to an agent
    POST /ingest                  - Universal content ingestion (auto-classify)
    POST /ingest/nodes            - Ingest nodes (rule-based or LLM)
    POST /ingest/edges            - Ingest edges
    GET  /export?format=jsonl     - Export graph for centralization
    GET  /stats                   - Graph statistics
    GET  /pending                 - List pending org-structural contributions
    POST /review/{pending_id}     - Approve or reject a pending contribution
    GET  /memory/proposals        - List memory proposals awaiting review
    POST /memory/proposals/{id}/review - Approve or reject a memory proposal
    GET  /principals              - List all principals (optional ?type= filter)
    POST /principals              - Register a principal ({type, name, metadata?})
    GET  /principals/{uuid}       - Resolve a principal UUID
    GET  /communities             - List all detected communities
    GET  /community/{id}          - Get members of a specific community
    GET  /hubs                    - Get top hub nodes (optional ?limit=N)
    POST /insight                 - Save an agent's synthesized insight
    POST /quarantine              - Quarantine nodes by agent ({agent, since?})
    POST /quarantine/release      - Release quarantined node(s) ({node_id} or {agent})
    GET  /quarantine              - List quarantined nodes (optional ?agent=)
    GET  /classification          - Return current classification config
"""

import argparse
import asyncio
import json
import logging
import os
from pathlib import Path

import httpx
import yaml
from aiohttp import web
from aiohttp.abc import AbstractAccessLogger


class _HealthFilterAccessLogger(AbstractAccessLogger):
    """Access logger that suppresses noisy /health probes."""

    def log(self, request, response, time):
        if request.path == "/health":
            return
        self.logger.info(
            '%s "%s %s" %s %.3fs',
            request.remote, request.method, request.path_qs,
            response.status, time,
        )

from typing import Optional
from services.knowledge.ingester import RuleIngester
from services.knowledge.principal_registry import PrincipalRegistry
from services.knowledge.classification import ClassificationConfig
from services.knowledge.store import KnowledgeStore
from services.knowledge.synthesizer import LLMSynthesizer
from services.knowledge.manager import KnowledgeManager, MemoryManager
from services.knowledge.gateway_client import GatewayClient

logger = logging.getLogger("agency.knowledge")


def publish_knowledge_update(gateway: "GatewayClient", node_summary: str, metadata: dict) -> None:
    """Publish a knowledge update event via the gateway event bus."""
    gateway.publish_knowledge_update(node_summary, metadata)


def _run_ontology_migration(store: KnowledgeStore, data_dir: Path) -> None:
    """One-time migration from freeform kinds to ontology types.

    Delegates to LLMSynthesizer.migrate_freeform_kinds() which uses the same
    _validate_kind() alias table used during live extraction — keeping migration
    and runtime validation consistent.
    """
    marker = data_dir / ".ontology-migrated"
    if marker.exists():
        return

    ontology_path = Path(os.environ.get("AGENCY_ONTOLOGY_PATH", "/app/ontology.yaml"))
    if not ontology_path.exists():
        return

    try:
        synth = LLMSynthesizer(store)
        if synth._ontology is None:
            return
        result = synth.migrate_freeform_kinds()
        logger.info(
            "Ontology migration: %d remapped, %d unchanged, %d total",
            result.get("remapped", 0),
            result.get("unchanged", 0),
            result.get("total", 0),
        )
    except Exception as e:
        logger.error("Ontology migration failed: %s", e)


def create_app(data_dir: Optional[Path] = None, enable_ingestion: bool = False) -> web.Application:
    try:
        from logging_config import correlation_middleware
    except ImportError:
        from images.logging_config import correlation_middleware
    app = web.Application(middlewares=[correlation_middleware()])
    store = KnowledgeStore(data_dir or Path("/data"))
    app["store"] = store

    # Initialize principal registry from gateway snapshot
    snapshot_path = "/app/registry.json"
    if not os.path.exists(snapshot_path):
        snapshot_path = os.path.join(
            os.environ.get("AGENCY_HOME", "/data"), "registry.json"
        )
    principal_registry = PrincipalRegistry(snapshot_path=snapshot_path)
    app["principal_registry"] = principal_registry

    # Initialize classification config
    config_path = os.environ.get("CLASSIFICATION_CONFIG_PATH", "/app/classification.yaml")
    # fallback paths
    if not os.path.exists(config_path):
        config_path = os.path.join(os.environ.get("AGENCY_HOME", "/data"), "classification.yaml")
    classification_config = ClassificationConfig(config_path=config_path if os.path.exists(config_path) else None)
    app["classification_config"] = classification_config
    # Wire into store
    store.set_classification_config(classification_config)

    # Run one-time ontology migration
    _run_ontology_migration(store, data_dir or Path("/data"))

    mode = os.environ.get("KNOWLEDGE_MODE", "primary")
    app["mode"] = mode

    if mode == "cache":
        upstream = os.environ.get("KNOWLEDGE_UPSTREAM", "")
        app["upstream_url"] = upstream
        app["upstream_state"] = {"ok": False}
        app.on_startup.append(_start_knowledge_upstream_client)
        app.on_cleanup.append(_stop_knowledge_upstream_client)

    # Ingestion: check env var (defaults to enable_ingestion param for compat)
    ingestion_env = os.environ.get("KNOWLEDGE_INGESTION")
    should_ingest = enable_ingestion
    if ingestion_env is not None:
        should_ingest = ingestion_env.lower() in ("true", "1", "yes")

    # Curator: create before ingester/synthesizer so they can use it
    curator_mode = os.environ.get("KNOWLEDGE_CURATOR_MODE", "auto")
    if curator_mode != "disabled":
        from services.knowledge.curator import Curator, CurationLoop
        curator = Curator(store, mode=curator_mode)
        app["curator"] = curator
        app.on_startup.append(_start_curation_loop)
        app.on_cleanup.append(_stop_curation_loop)

    if should_ingest and mode != "cache":
        gateway_url = os.environ.get("AGENCY_GATEWAY_URL", "http://localhost:8200")
        gateway_token = os.environ.get("AGENCY_GATEWAY_TOKEN", "")
        gateway = GatewayClient(base_url=gateway_url, token=gateway_token)
        ingester = RuleIngester(store, curator=app.get("curator"))
        synthesizer = LLMSynthesizer(store, curator=app.get("curator"))
        memory_manager = MemoryManager(store)
        knowledge_manager = KnowledgeManager(store, memory_manager=memory_manager)
        app["ingester"] = ingester
        app["synthesizer"] = synthesizer
        app["memory_manager"] = memory_manager
        app["knowledge_manager"] = knowledge_manager
        app["gateway"] = gateway
        app.on_startup.append(_start_ingestion_loop)
        app.on_cleanup.append(_stop_ingestion_loop)

    # Universal ingestion pipeline (optional — depends on ingestion extras)
    try:
        from services.knowledge.ingestion.pipeline import IngestionPipeline
        synth = app.get("synthesizer")
        pipeline = IngestionPipeline(store=store, synthesizer=synth)
        app["pipeline"] = pipeline
    except ImportError:
        app["pipeline"] = None

    # Run schema migrations on startup
    app.on_startup.append(_run_schema_migrations)

    # Start embedding backfill as a background task
    app.on_startup.append(_start_backfill_task)
    app.on_cleanup.append(_stop_backfill_task)

    app.router.add_get("/health", handle_health)
    app.router.add_post("/query", handle_query)
    app.router.add_get("/who-knows", handle_who_knows)
    app.router.add_get("/changes", handle_changes)
    app.router.add_get("/context", handle_context)
    app.router.add_get("/org-context", handle_org_context)
    app.router.add_get("/neighbors", handle_neighbors)
    app.router.add_get("/path", handle_path)
    app.router.add_post("/ingest", handle_ingest_universal)
    app.router.add_post("/ingest/nodes", handle_ingest_nodes)
    app.router.add_post("/ingest/edges", handle_ingest_edges)
    app.router.add_get("/export", handle_export)
    app.router.add_get("/stats", handle_stats)
    app.router.add_get("/curation/flags", handle_curation_flags)
    app.router.add_post("/curation/restore", handle_curation_restore)
    app.router.add_post("/curation/unflag", handle_curation_unflag)
    app.router.add_get("/curation/log", handle_curation_log)
    app.router.add_post("/curation/run", handle_curation_run)
    app.router.add_post("/migrate-kind", handle_migrate_kind)
    app.router.add_get("/pending", handle_pending)
    app.router.add_post("/review/{pending_id}", handle_review)
    app.router.add_get("/memory", handle_memories)
    app.router.add_post("/memory/{memory_id}/actions", handle_memory_action)
    app.router.add_get("/memory/proposals", handle_memory_proposals)
    app.router.add_post("/memory/proposals/{proposal_id}/review", handle_memory_proposal_review)
    app.router.add_get("/graph/node/{node_id}", handle_graph_node)
    app.router.add_get("/graph/neighbors/{node_id}", handle_graph_neighbors)
    app.router.add_get("/graph/filter", handle_graph_filter)
    app.router.add_get("/graph/similar/{node_id}", handle_graph_similar)
    app.router.add_get("/ontology/candidates", handle_ontology_candidates)
    app.router.add_post("/ontology/promote", handle_ontology_promote)
    app.router.add_post("/ontology/reject", handle_ontology_reject)
    app.router.add_post("/ontology/restore", handle_ontology_restore)
    app.router.add_post("/delete-by-label", handle_delete_by_label)
    app.router.add_post("/delete-by-kind", handle_delete_by_kind)
    app.router.add_get("/principals", handle_principals_list)
    # POST /principals removed — registrations are gateway-only now
    app.router.add_get("/principals/{uuid}", handle_principals_resolve)
    app.router.add_get("/communities", handle_communities)
    app.router.add_get("/community/{id}", handle_community)
    app.router.add_get("/hubs", handle_hubs)
    app.router.add_post("/insight", handle_save_insight)
    app.router.add_post("/quarantine", handle_quarantine)
    app.router.add_post("/quarantine/release", handle_quarantine_release)
    app.router.add_get("/quarantine", handle_quarantine_list)
    app.router.add_get("/classification", handle_classification)

    async def _log_knowledge_shutdown(app: web.Application) -> None:
        logger.info("Knowledge server shutting down")

    app.on_shutdown.append(_log_knowledge_shutdown)
    return app


async def _start_knowledge_upstream_client(app):
    app["http"] = httpx.AsyncClient(timeout=httpx.Timeout(2.0, connect=2.0))


async def _stop_knowledge_upstream_client(app):
    client = app.get("http")
    if client:
        await client.aclose()


async def handle_health(request: web.Request) -> web.Response:
    mode = request.app.get("mode", "primary")
    resp = {"status": "ok", "mode": mode}
    if mode == "cache":
        state = request.app.get("upstream_state", {})
        resp["upstream_ok"] = state.get("ok", False)
    return web.json_response(resp)


async def handle_classification(request: web.Request) -> web.Response:
    """GET /classification — return current classification config."""
    config = request.app.get("classification_config")
    if not config:
        return web.json_response({"error": "classification not configured"}, status=503)
    return web.json_response(config.to_dict())


async def handle_query(request: web.Request) -> web.Response:
    store: KnowledgeStore = request.app["store"]

    if request.app.get("mode") == "cache":
        return await _cache_query(request, store)

    body = await request.json()
    query = body.get("query", "")
    visible = body.get("visible_channels")
    if not query:
        return web.json_response({"error": "query required"}, status=400)
    results = store.find_nodes(query, visible_channels=visible)
    # Enrich with connected edges for context
    for node in results:
        edges = store.get_edges(node["id"], direction="both")
        node["connections"] = len(edges)
    return web.json_response({"query": query, "results": results})


async def _cache_query(request, store):
    body = await request.json()
    query = body.get("query", "")
    if not query:
        return web.json_response({"error": "query required"}, status=400)

    visible = body.get("visible_channels")
    state = request.app.get("upstream_state", {})
    http = request.app["http"]
    upstream = request.app["upstream_url"]

    # Try upstream
    try:
        resp = await http.post(f"{upstream}/query", json=body)
        if resp.status_code == 200:
            result = resp.json()
            store.cache_query(query, result)
            state["ok"] = True
            return web.json_response(result)
    except Exception:
        state["ok"] = False

    # Try local cache
    cached = store.get_cached_query(query)
    if cached is not None:
        return web.json_response(cached)

    # Try local store directly
    results = store.find_nodes(query, visible_channels=visible)
    for node in results:
        edges = store.get_edges(node["id"], direction="both")
        node["connections"] = len(edges)
    return web.json_response({"query": query, "results": results})


async def handle_who_knows(request: web.Request) -> web.Response:
    store: KnowledgeStore = request.app["store"]

    if request.app.get("mode") == "cache":
        return await _cache_who_knows(request, store)

    topic = request.query.get("topic", "")
    visible_raw = request.query.get("visible_channels", "")
    visible = [c.strip() for c in visible_raw.split(",") if c.strip()] or None
    if not topic:
        return web.json_response({"error": "topic required"}, status=400)
    # Find topic nodes
    topic_nodes = store.find_nodes(topic, visible_channels=visible)
    # Find agents connected to those nodes
    agent_scores: dict[str, float] = {}
    for node in topic_nodes:
        edges = store.get_edges(node["id"], direction="incoming")
        for edge in edges:
            src = store.get_node(edge["source_id"])
            if src and src["kind"] == "agent":
                name = src["label"]
                agent_scores[name] = agent_scores.get(name, 0) + edge["weight"]
    agents = [
        {"label": name, "relevance": score}
        for name, score in sorted(agent_scores.items(), key=lambda x: -x[1])
    ]
    return web.json_response({"topic": topic, "agents": agents})


async def _cache_who_knows(request, store):
    topic = request.query.get("topic", "")
    if not topic:
        return web.json_response({"error": "topic required"}, status=400)
    visible_raw = request.query.get("visible_channels", "")
    visible = [c.strip() for c in visible_raw.split(",") if c.strip()] or None
    cache_key = f"who_knows:{topic}:{sorted(visible or [])}"
    state = request.app.get("upstream_state", {})
    http = request.app["http"]
    upstream = request.app["upstream_url"]
    try:
        resp = await http.get(f"{upstream}/who-knows", params=dict(request.query))
        if resp.status_code == 200:
            result = resp.json()
            store.cache_query(cache_key, result)
            state["ok"] = True
            return web.json_response(result)
    except Exception:
        state["ok"] = False
    cached = store.get_cached_query(cache_key)
    if cached:
        return web.json_response(cached)
    # Fall through to local (same logic as primary handler)
    topic_nodes = store.find_nodes(topic, visible_channels=visible)
    agent_scores: dict[str, float] = {}
    for node in topic_nodes:
        edges = store.get_edges(node["id"], direction="incoming")
        for edge in edges:
            src = store.get_node(edge["source_id"])
            if src and src["kind"] == "agent":
                name = src["label"]
                agent_scores[name] = agent_scores.get(name, 0) + edge["weight"]
    agents = [
        {"label": name, "relevance": score}
        for name, score in sorted(agent_scores.items(), key=lambda x: -x[1])
    ]
    return web.json_response({"topic": topic, "agents": agents})


async def handle_changes(request: web.Request) -> web.Response:
    store: KnowledgeStore = request.app["store"]

    if request.app.get("mode") == "cache":
        return await _cache_changes(request, store)

    since = request.query.get("since", "")
    visible_raw = request.query.get("visible_channels", "")
    visible = [c.strip() for c in visible_raw.split(",") if c.strip()] or None
    if not since:
        return web.json_response({"error": "since required"}, status=400)
    nodes = store._db.execute(
        "SELECT * FROM nodes WHERE updated_at >= ? ORDER BY updated_at",
        (since,),
    ).fetchall()
    nodes = [dict(r) for r in nodes]
    if visible:
        nodes = store._filter_by_channels(nodes, visible)
    edges = store._db.execute(
        "SELECT * FROM edges WHERE timestamp >= ? ORDER BY timestamp",
        (since,),
    ).fetchall()
    return web.json_response({
        "since": since,
        "nodes": nodes,
        "edges": [dict(r) for r in edges],
    })


async def _cache_changes(request, store):
    since = request.query.get("since", "")
    if not since:
        return web.json_response({"error": "since required"}, status=400)
    visible_raw = request.query.get("visible_channels", "")
    visible = [c.strip() for c in visible_raw.split(",") if c.strip()] or None
    cache_key = f"changes:{since}:{sorted(visible or [])}"
    state = request.app.get("upstream_state", {})
    http = request.app["http"]
    upstream = request.app["upstream_url"]
    try:
        resp = await http.get(f"{upstream}/changes", params=dict(request.query))
        if resp.status_code == 200:
            result = resp.json()
            store.cache_query(cache_key, result)
            state["ok"] = True
            return web.json_response(result)
    except Exception:
        state["ok"] = False
    cached = store.get_cached_query(cache_key)
    if cached:
        return web.json_response(cached)
    # Fall through to local
    nodes = store._db.execute(
        "SELECT * FROM nodes WHERE updated_at >= ? ORDER BY updated_at",
        (since,),
    ).fetchall()
    nodes = [dict(r) for r in nodes]
    if visible:
        nodes = store._filter_by_channels(nodes, visible)
    edges = store._db.execute(
        "SELECT * FROM edges WHERE timestamp >= ? ORDER BY timestamp",
        (since,),
    ).fetchall()
    return web.json_response({
        "since": since,
        "nodes": nodes,
        "edges": [dict(r) for r in edges],
    })


async def handle_context(request: web.Request) -> web.Response:
    store: KnowledgeStore = request.app["store"]

    if request.app.get("mode") == "cache":
        return await _cache_context(request, store)

    subject = request.query.get("subject", "")
    visible_raw = request.query.get("visible_channels", "")
    visible = [c.strip() for c in visible_raw.split(",") if c.strip()] or None
    principal_param = request.query.get("principal")
    principal = {"principals": [principal_param]} if principal_param else None
    hops = min(max(int(request.query.get("hops", "2")), 1), 3)
    if not subject:
        return web.json_response({"error": "subject required"}, status=400)
    nodes = store.find_nodes(subject, visible_channels=visible)
    if not nodes:
        return web.json_response({"nodes": [], "edges": []})
    subgraph = store.get_subgraph(
        nodes[0]["id"], max_hops=hops, visible_channels=visible, principal=principal
    )
    return web.json_response(subgraph)


async def _cache_context(request, store):
    subject = request.query.get("subject", "")
    if not subject:
        return web.json_response({"error": "subject required"}, status=400)
    visible_raw = request.query.get("visible_channels", "")
    visible = [c.strip() for c in visible_raw.split(",") if c.strip()] or None
    principal_param = request.query.get("principal")
    principal = {"principals": [principal_param]} if principal_param else None
    cache_key = f"context:{subject}:{sorted(visible or [])}"
    state = request.app.get("upstream_state", {})
    http = request.app["http"]
    upstream = request.app["upstream_url"]
    try:
        resp = await http.get(f"{upstream}/context", params=dict(request.query))
        if resp.status_code == 200:
            result = resp.json()
            store.cache_query(cache_key, result)
            state["ok"] = True
            return web.json_response(result)
    except Exception:
        state["ok"] = False
    cached = store.get_cached_query(cache_key)
    if cached:
        return web.json_response(cached)
    # Fall through to local
    nodes = store.find_nodes(subject, visible_channels=visible)
    if not nodes:
        return web.json_response({"nodes": [], "edges": []})
    subgraph = store.get_subgraph(nodes[0]["id"], max_hops=2, visible_channels=visible, principal=principal)
    return web.json_response(subgraph)


async def handle_org_context(request: web.Request) -> web.Response:
    store: KnowledgeStore = request.app["store"]
    agent = request.query.get("agent", "")
    if not agent:
        return web.json_response({"error": "agent parameter required"}, status=400)
    # Non-platform callers (agents) can only query their own org context.
    # The X-Agency-Agent header is set by the body runtime on its requests.
    if not _require_platform(request):
        caller = request.headers.get("X-Agency-Agent", "")
        if caller != agent:
            return web.json_response(
                {"error": "agents can only query their own org context"}, status=403,
            )
    result = store.get_org_context(agent)
    return web.json_response(result)


async def handle_neighbors(request: web.Request) -> web.Response:
    store: KnowledgeStore = request.app["store"]
    node_id = request.query.get("node_id", "")
    direction = request.query.get("direction", "both")
    relation = request.query.get("relation") or None
    principal_param = request.query.get("principal")
    principal = {"principals": [principal_param]} if principal_param else None
    if not node_id:
        return web.json_response({"error": "node_id required"}, status=400)
    if direction not in ("outgoing", "incoming", "both"):
        return web.json_response({"error": "direction must be outgoing, incoming, or both"}, status=400)
    result = store.get_neighbors(node_id, direction=direction, relation=relation, principal=principal)
    return web.json_response(result)


async def handle_path(request: web.Request) -> web.Response:
    store: KnowledgeStore = request.app["store"]
    from_label = request.query.get("from", "")
    to_label = request.query.get("to", "")
    principal_param = request.query.get("principal")
    principal = {"principals": [principal_param]} if principal_param else None
    try:
        max_hops = int(request.query.get("max_hops", "4"))
    except ValueError:
        return web.json_response({"error": "max_hops must be an integer"}, status=400)
    if not from_label or not to_label:
        return web.json_response({"error": "from and to required"}, status=400)
    result = store.find_path(from_label, to_label, max_hops=max_hops, principal=principal)
    if result is None:
        return web.json_response({"error": "no path found", "from": from_label, "to": to_label}, status=404)
    return web.json_response(result)


async def handle_ingest_universal(request: web.Request) -> web.Response:
    """POST /ingest — universal content ingestion.

    Accepts raw content with metadata and routes it through the
    IngestionPipeline (classify → extract → store → optional synthesis).

    Body: {
        "content": "...",          # Required — the raw content to ingest
        "filename": "...",         # Optional — filename hint for classification
        "content_type": "...",     # Optional — MIME type hint
        "scope": {...},            # Optional — authorization scope metadata
        "source_principal": "..."  # Optional — principal that produced the content
    }
    """
    pipeline = request.app.get("pipeline")
    if pipeline is None:
        return web.json_response(
            {"error": "Ingestion pipeline not available"},
            status=503,
        )

    try:
        body = await request.json()
    except Exception:
        return web.json_response({"error": "Invalid JSON body"}, status=400)

    content = body.get("content", "")
    if not content or not content.strip():
        return web.json_response({"error": "content is required and must be non-empty"}, status=400)

    filename = body.get("filename", "")
    content_type = body.get("content_type", "")
    scope = body.get("scope")
    source_principal = body.get("source_principal", "")

    try:
        # The knowledge store uses a shared SQLite connection, so ingestion
        # must run on the server thread instead of a worker executor.
        stats = pipeline.ingest(
            content,
            filename=filename,
            content_type=content_type,
            scope=scope,
            source_principal=source_principal,
        )
    except Exception:
        logger.exception("Ingestion pipeline error")
        return web.json_response({"error": "Ingestion failed"}, status=500)

    return web.json_response(stats)


async def handle_ingest_nodes(request: web.Request) -> web.Response:
    store: KnowledgeStore = request.app["store"]

    if request.app.get("mode") == "cache":
        return await _cache_ingest_nodes(request, store)

    body = await request.json()
    nodes = body.get("nodes", [])
    gateway = request.app.get("gateway")
    if gateway is None:
        gateway_url = os.environ.get("AGENCY_GATEWAY_URL", "http://localhost:8200")
        gateway_token = os.environ.get("AGENCY_GATEWAY_TOKEN", "")
        gateway = GatewayClient(base_url=gateway_url, token=gateway_token)
    count = 0
    pending_review = 0
    for node in nodes:
        kind = node["kind"]
        # Gate org-structural contributions: hold for operator review instead
        # of committing directly. Prevents compromised agents from injecting
        # false team/leadership data that would propagate via /org-context.
        if store.is_org_structural(kind):
            store.submit_pending(
                label=node["label"],
                kind=kind,
                summary=node.get("summary", ""),
                properties=node.get("properties"),
                source_agent=node.get("source_type", ""),
            )
            pending_review += 1
            continue
        node_id = store.add_node(
            label=node["label"],
            kind=kind,
            summary=node.get("summary", ""),
            properties=node.get("properties"),
            source_type=node.get("source_type", "rule"),
            source_channels=node.get("source_channels"),
        )
        # Only publish channel updates for meaningful findings, not raw telemetry.
        # DNS queries, network connections, and device inventory are too noisy.
        _SILENT_KINDS = {"dns_query", "network_connection", "device", "sensor", "ip_address", "domain", "memory_proposal"}
        if kind.lower() not in _SILENT_KINDS:
            node_summary = node.get("summary") or node["label"]
            publish_knowledge_update(
                gateway=gateway,
                node_summary=node_summary,
                metadata={
                    "node_id": node_id,
                    "kind": kind,
                    "topic": node["label"],
                    "contributed_by": node.get("source_type", "rule"),
                },
            )
        count += 1
    manager = request.app.get("knowledge_manager")
    memory_processed = None
    if manager is not None:
        memory_processed = manager.process_cycle().get("memory")
    response = {"ingested": count, "pending_review": pending_review}
    if memory_processed:
        response["memory_processed"] = memory_processed
    return web.json_response(response)


async def _cache_ingest_nodes(request, store):
    body = await request.json()
    nodes = body.get("nodes", [])
    state = request.app.get("upstream_state", {})
    http = request.app["http"]
    upstream = request.app["upstream_url"]

    # Try upstream first
    try:
        resp = await http.post(f"{upstream}/ingest/nodes", json=body)
        if resp.status_code == 200:
            state["ok"] = True
            return web.json_response(resp.json())
    except Exception:
        state["ok"] = False

    # Buffer locally
    count = 0
    for node in nodes:
        store.buffer_contribution(
            label=node["label"],
            kind=node["kind"],
            summary=node.get("summary", ""),
            properties=node.get("properties"),
            source_type=node.get("source_type", "rule"),
            source_channels=node.get("source_channels"),
        )
        count += 1
    return web.json_response({"buffered": count})


async def handle_ingest_edges(request: web.Request) -> web.Response:
    store: KnowledgeStore = request.app["store"]
    body = await request.json()
    edges = body.get("edges", [])
    count = 0
    for edge in edges:
        # Resolve node IDs from labels if needed
        source_id = edge.get("source_id")
        target_id = edge.get("target_id")
        if not source_id and "source_label" in edge:
            nodes = store.find_nodes(edge["source_label"])
            if nodes:
                source_id = nodes[0]["id"]
        if not target_id and "target_label" in edge:
            nodes = store.find_nodes(edge["target_label"])
            if nodes:
                target_id = nodes[0]["id"]
        if source_id and target_id:
            store.add_edge(
                source_id=source_id,
                target_id=target_id,
                relation=edge.get("relation", "related"),
                weight=edge.get("weight", 1.0),
                source_channel=edge.get("source_channel", ""),
                provenance_id=edge.get("provenance_id", ""),
            )
            count += 1
    return web.json_response({"ingested": count})


async def handle_export(request: web.Request) -> web.Response:
    store: KnowledgeStore = request.app["store"]
    since = request.query.get("since")
    fmt = request.query.get("format", "jsonl")

    if fmt == "cypher":
        text = store.export_cypher(since=since)
        return web.Response(text=text, content_type="text/plain")

    if fmt == "dot":
        text = store.export_dot(since=since)
        return web.Response(text=text, content_type="text/plain")

    if fmt == "json":
        import json as _json
        lines = store.export_jsonl(since=since)
        entries = [_json.loads(line) for line in lines if line.strip()]
        return web.json_response(entries)

    # default: jsonl
    lines = store.export_jsonl(since=since)
    return web.Response(
        text="\n".join(lines) + "\n" if lines else "",
        content_type="application/x-ndjson",
    )


async def handle_stats(request: web.Request) -> web.Response:
    store: KnowledgeStore = request.app["store"]
    data = store.stats()
    # Include curation metrics if available
    logs = store.get_curation_log(action="metrics", limit=1)
    if logs:
        import json as _json
        detail = logs[0].get("detail", "{}")
        if isinstance(detail, str):
            detail = _json.loads(detail)
        data["curation"] = detail
    return web.json_response(data)


async def handle_curation_flags(request: web.Request) -> web.Response:
    store: KnowledgeStore = request.app["store"]
    kind = request.query.get("kind")
    since = request.query.get("since")
    sql = "SELECT * FROM nodes WHERE curation_status = 'flagged'"
    params: list = []
    if kind:
        sql += " AND kind = ?"
        params.append(kind)
    if since:
        sql += " AND curation_at >= ?"
        params.append(since)
    sql += " ORDER BY curation_at DESC"
    rows = store._db.execute(sql, params).fetchall()
    flagged = [dict(r) for r in rows]
    return web.json_response({"flagged": flagged})


async def handle_curation_restore(request: web.Request) -> web.Response:
    store: KnowledgeStore = request.app["store"]
    body = await request.json()
    node_id = body.get("node_id", "")
    if not node_id:
        return web.json_response({"error": "node_id required"}, status=400)
    # Check if node was hard-deleted
    hard_deleted = store.get_curation_log(node_id=node_id, action="hard_delete")
    if hard_deleted:
        return web.json_response({"error": "node was hard-deleted"}, status=410)
    # get_node filters out merged/soft_deleted, so query raw
    row = store._db.execute("SELECT * FROM nodes WHERE id = ?", (node_id,)).fetchone()
    if not row:
        return web.json_response({"error": "node not found"}, status=404)
    node = dict(row)
    if node.get("curation_status") is None:
        return web.json_response({"error": "node is already in normal status"}, status=404)
    store._db.execute(
        "UPDATE nodes SET curation_status = NULL, curation_reason = NULL, curation_at = NULL WHERE id = ?",
        (node_id,),
    )
    store._db.commit()
    store.log_curation("restore", node_id, {"restored_from": node.get("curation_status")})
    return web.json_response({"restored": node_id})


async def handle_curation_unflag(request: web.Request) -> web.Response:
    store: KnowledgeStore = request.app["store"]
    body = await request.json()
    node_id = body.get("node_id", "")
    if not node_id:
        return web.json_response({"error": "node_id required"}, status=400)
    node = store.get_node(node_id)
    if not node or node.get("curation_status") != "flagged":
        return web.json_response({"error": "node not flagged"}, status=404)
    store._db.execute(
        "UPDATE nodes SET curation_status = NULL, curation_reason = NULL, curation_at = NULL WHERE id = ?",
        (node_id,),
    )
    store._db.commit()
    store.log_curation("unflag", node_id, {"previous_reason": node.get("curation_reason")})
    return web.json_response({"unflagged": node_id})


async def handle_curation_log(request: web.Request) -> web.Response:
    store: KnowledgeStore = request.app["store"]
    node_id = request.query.get("node_id")
    action = request.query.get("action")
    since = request.query.get("since")
    limit = int(request.query.get("limit", "100"))
    offset = int(request.query.get("offset", "0"))
    entries = store.get_curation_log(
        node_id=node_id, action=action, since=since, limit=limit, offset=offset,
    )
    return web.json_response({"entries": entries})


async def handle_curation_run(request: web.Request) -> web.Response:
    """POST /curation/run — manually trigger a full curation cycle.
    Returns per-operation results."""
    curator = request.app.get("curator")
    if curator is None:
        return web.json_response({"error": "curator not initialized"}, status=500)

    operations = [
        ("fuzzy_duplicate_scan", curator.fuzzy_duplicate_scan),
        ("orphan_pruning", curator.orphan_pruning),
        ("cluster_analysis", curator.cluster_analysis),
        ("anomaly_detection", curator.anomaly_detection),
        ("emergence_scan", curator.emergence_scan),
        ("relationship_inference", curator.relationship_inference),
    ]
    results = {"status": "completed"}
    for name, op in operations:
        try:
            results[name] = op()
        except Exception as e:
            results[name] = {"error": str(e)}
    return web.json_response(results)


def _require_platform(request: web.Request) -> bool:
    """Check X-Agency-Platform header. Only the gateway sets this."""
    return request.headers.get("X-Agency-Platform") == "true"


async def handle_pending(request: web.Request) -> web.Response:
    """GET /pending — list all org-structural contributions awaiting review."""
    if not _require_platform(request):
        return web.json_response({"error": "platform access required"}, status=403)
    store: KnowledgeStore = request.app["store"]
    items = store.list_pending()
    return web.json_response({"items": items})


async def handle_review(request: web.Request) -> web.Response:
    """POST /review/{pending_id} — approve or reject a pending contribution.

    Body: {"action": "approve" | "reject"}
    """
    if not _require_platform(request):
        return web.json_response({"error": "platform access required"}, status=403)
    store: KnowledgeStore = request.app["store"]
    pending_id = request.match_info["pending_id"]
    try:
        body = await request.json()
    except Exception:
        return web.json_response({"error": "JSON body required"}, status=400)
    action = body.get("action", "")
    if action not in ("approve", "reject"):
        return web.json_response({"error": "action must be 'approve' or 'reject'"}, status=400)
    found = store.review_pending(pending_id, action)
    if not found:
        return web.json_response({"error": "pending contribution not found"}, status=404)
    return web.json_response({"pending_id": pending_id, "action": action})


async def handle_memory_proposals(request: web.Request) -> web.Response:
    """GET /memory/proposals — list durable-memory proposals for operator review."""
    if not _require_platform(request):
        return web.json_response({"error": "platform access required"}, status=403)
    store: KnowledgeStore = request.app["store"]
    status = request.query.get("status", "needs_review")
    if status not in ("pending_review", "needs_review", "approved", "rejected"):
        return web.json_response({"error": "invalid proposal status"}, status=400)
    try:
        limit = int(request.query.get("limit", "100"))
    except ValueError:
        return web.json_response({"error": "limit must be an integer"}, status=400)
    limit = max(1, min(limit, 250))
    return web.json_response({"items": store.list_memory_proposals(status=status, limit=limit)})


async def handle_memories(request: web.Request) -> web.Response:
    """GET /memory — list promoted durable memories."""
    if not _require_platform(request):
        return web.json_response({"error": "platform access required"}, status=403)
    store: KnowledgeStore = request.app["store"]
    memory_type = request.query.get("type", "")
    if memory_type and memory_type not in ("semantic", "episodic", "procedural"):
        return web.json_response({"error": "invalid memory type"}, status=400)
    agent = request.query.get("agent", "")
    try:
        limit = int(request.query.get("limit", "100"))
    except ValueError:
        return web.json_response({"error": "limit must be an integer"}, status=400)
    return web.json_response({
        "items": store.list_approved_memories(memory_type=memory_type, agent=agent, limit=limit),
    })


async def handle_memory_action(request: web.Request) -> web.Response:
    """POST /memory/{memory_id}/actions — apply an operator action to durable memory."""
    if not _require_platform(request):
        return web.json_response({"error": "platform access required"}, status=403)
    memory_id = request.match_info["memory_id"]
    try:
        body = await request.json()
    except Exception:
        return web.json_response({"error": "JSON body required"}, status=400)
    action = body.get("action", "")
    if action != "revoke":
        return web.json_response({"error": "action must be 'revoke'"}, status=400)
    store: KnowledgeStore = request.app["store"]
    if not store.revoke_memory(memory_id, str(body.get("reason", ""))):
        return web.json_response({"error": "memory not found"}, status=404)
    return web.json_response({"id": memory_id, "action": action})


async def handle_memory_proposal_review(request: web.Request) -> web.Response:
    """POST /memory/proposals/{proposal_id}/review — approve or reject a memory proposal."""
    if not _require_platform(request):
        return web.json_response({"error": "platform access required"}, status=403)
    proposal_id = request.match_info["proposal_id"]
    try:
        body = await request.json()
    except Exception:
        return web.json_response({"error": "JSON body required"}, status=400)
    action = body.get("action", "")
    if action not in ("approve", "reject"):
        return web.json_response({"error": "action must be 'approve' or 'reject'"}, status=400)
    manager = request.app.get("memory_manager") or MemoryManager(request.app["store"])
    result = manager.review_proposal(proposal_id, action, str(body.get("reason", "")))
    if result is None:
        return web.json_response({"error": "memory proposal not found"}, status=404)
    return web.json_response(result)


async def handle_graph_node(request: web.Request) -> web.Response:
    store: KnowledgeStore = request.app["store"]
    node_id = request.match_info["node_id"]
    node = store.get_node(node_id)
    if not node:
        return web.json_response({"error": "not found"}, status=404)
    return web.json_response({"nodes": [node], "edges": []})


async def handle_graph_neighbors(request: web.Request) -> web.Response:
    store: KnowledgeStore = request.app["store"]
    node_id = request.match_info["node_id"]
    relation = request.query.get("relation")
    principal_param = request.query.get("principal")
    principal = {"principals": [principal_param]} if principal_param else None
    min_provenance = request.query.get("min_provenance")
    result = store.get_neighbors_subgraph(node_id, relation=relation, principal=principal, min_provenance=min_provenance)
    return web.json_response(result)


async def handle_graph_filter(request: web.Request) -> web.Response:
    store: KnowledgeStore = request.app["store"]
    kind = request.query.get("kind", "")
    prop = request.query.get("property", "")
    value = request.query.get("value", "")
    if not kind or not prop or not value:
        return web.json_response({"error": "kind, property, and value required"}, status=400)
    nodes = store.filter_nodes_by_property(kind, prop, value)
    node_ids = {n["id"] for n in nodes}
    edges = []
    for n in nodes:
        for e in store.get_edges(n["id"], direction="both"):
            if e["source_id"] in node_ids and e["target_id"] in node_ids and e not in edges:
                edges.append(e)
    return web.json_response({"nodes": nodes, "edges": edges})


async def handle_migrate_kind(request: web.Request) -> web.Response:
    """Migrate all nodes from one kind to another."""
    if not _require_platform(request):
        return web.json_response({"error": "platform access required"}, status=403)
    store: KnowledgeStore = request.app["store"]
    body = await request.json()
    from_kind = body.get("from", "")
    to_kind = body.get("to", "")
    if not from_kind or not to_kind:
        return web.json_response({"error": "from and to required"}, status=400)

    rows = store._db.execute(
        "SELECT id FROM nodes WHERE kind = ?", (from_kind,)
    ).fetchall()
    count = len(rows)
    for row in rows:
        node_id = row[0] if isinstance(row, (list, tuple)) else row["id"]
        store._db.execute(
            "UPDATE nodes SET kind = ? WHERE id = ?", (to_kind, node_id)
        )
    store._db.commit()
    logger.info("Migrated %d nodes from '%s' to '%s'", count, from_kind, to_kind)
    return web.json_response({
        "migrated": count,
        "from": from_kind,
        "to": to_kind,
    })


async def handle_graph_similar(request: web.Request) -> web.Response:
    """GET /graph/similar/{node_id} — find nodes similar to a given node via vector search."""
    store: KnowledgeStore = request.app["store"]
    node_id = request.match_info["node_id"]
    node = store.get_node(node_id)
    if not node:
        return web.json_response({"error": "not found"}, status=404)
    limit = int(request.query.get("limit", "10"))
    similar = store.find_similar(node_id, limit=limit)
    return web.json_response({"nodes": similar, "edges": []})


async def handle_ontology_candidates(request: web.Request) -> web.Response:
    """GET /ontology/candidates — list OntologyCandidate nodes with status=candidate."""
    store: KnowledgeStore = request.app["store"]
    rows = store._db.execute(
        "SELECT * FROM nodes WHERE kind = 'OntologyCandidate'"
    ).fetchall()
    candidates = []
    for row in rows:
        node = dict(row)
        props = json.loads(node.get("properties") or "{}")
        if props.get("status") == "candidate":
            node["properties"] = props
            candidates.append(node)
    return web.json_response({"candidates": candidates})


async def handle_ontology_promote(request: web.Request) -> web.Response:
    """POST /ontology/promote — promote an OntologyCandidate to the ontology."""
    store: KnowledgeStore = request.app["store"]
    try:
        body = await request.json()
    except Exception:
        return web.json_response({"error": "JSON body required"}, status=400)
    node_id = body.get("node_id", "")
    if not node_id:
        return web.json_response({"error": "node_id required"}, status=400)
    row = store._db.execute(
        "SELECT * FROM nodes WHERE id = ? AND kind = 'OntologyCandidate'", (node_id,)
    ).fetchone()
    if not row:
        return web.json_response({"error": "OntologyCandidate not found"}, status=404)
    node = dict(row)
    props = json.loads(node.get("properties") or "{}")
    props["status"] = "promoted"
    store._db.execute(
        "UPDATE nodes SET properties = ? WHERE id = ?",
        (json.dumps(props), node_id),
    )
    store._db.commit()
    store.log_curation("ontology_promote", node_id, {
        "value": props.get("value"),
        "occurrence_count": props.get("occurrence_count"),
    })
    return web.json_response({"promoted": node_id, "value": props.get("value")})


async def handle_ontology_reject(request: web.Request) -> web.Response:
    """POST /ontology/reject — reject an OntologyCandidate."""
    store: KnowledgeStore = request.app["store"]
    try:
        body = await request.json()
    except Exception:
        return web.json_response({"error": "JSON body required"}, status=400)
    node_id = body.get("node_id", "")
    if not node_id:
        return web.json_response({"error": "node_id required"}, status=400)
    row = store._db.execute(
        "SELECT * FROM nodes WHERE id = ? AND kind = 'OntologyCandidate'", (node_id,)
    ).fetchone()
    if not row:
        return web.json_response({"error": "OntologyCandidate not found"}, status=404)
    node = dict(row)
    props = json.loads(node.get("properties") or "{}")
    props["status"] = "rejected"
    props["rejection_count_at"] = props.get("occurrence_count")
    store._db.execute(
        "UPDATE nodes SET properties = ? WHERE id = ?",
        (json.dumps(props), node_id),
    )
    store._db.commit()
    store.log_curation("ontology_reject", node_id, {
        "value": props.get("value"),
        "occurrence_count": props.get("occurrence_count"),
    })
    return web.json_response({"rejected": node_id, "value": props.get("value")})


async def handle_ontology_restore(request: web.Request) -> web.Response:
    """POST /ontology/restore — restore a promoted or rejected OntologyCandidate."""
    store: KnowledgeStore = request.app["store"]
    try:
        body = await request.json()
    except Exception:
        return web.json_response({"error": "JSON body required"}, status=400)
    node_id = body.get("node_id", "")
    if not node_id:
        return web.json_response({"error": "node_id required"}, status=400)
    row = store._db.execute(
        "SELECT * FROM nodes WHERE id = ? AND kind = 'OntologyCandidate'", (node_id,)
    ).fetchone()
    if not row:
        return web.json_response({"error": "OntologyCandidate not found"}, status=404)
    node = dict(row)
    props = json.loads(node.get("properties") or "{}")
    previous_status = props.get("status")
    if previous_status not in {"promoted", "rejected"}:
        return web.json_response({"error": "OntologyCandidate is not restorable"}, status=409)
    props["status"] = "candidate"
    store._db.execute(
        "UPDATE nodes SET properties = ? WHERE id = ?",
        (json.dumps(props), node_id),
    )
    store._db.commit()
    store.log_curation("ontology_restore", node_id, {
        "value": props.get("value"),
        "restored_from": previous_status,
    })
    return web.json_response({"restored": node_id, "value": props.get("value")})


async def handle_delete_by_label(request: web.Request) -> web.Response:
    """POST /delete-by-label — soft-delete a cached_result node by label.

    Body: {"label": "cache:agent:hash", "kind": "cached_result"}
    Used by the body runtime to evict stale cache entries after task failure.
    """
    store: KnowledgeStore = request.app["store"]
    try:
        body = await request.json()
    except Exception:
        return web.json_response({"error": "JSON body required"}, status=400)
    label = body.get("label", "")
    kind = body.get("kind", "")
    if not label or not kind:
        return web.json_response({"error": "label and kind required"}, status=400)
    count = store.soft_delete_by_label(label, kind)
    return web.json_response({"deleted": count, "label": label, "kind": kind})


async def handle_delete_by_kind(request: web.Request) -> web.Response:
    """POST /delete-by-kind — soft-delete all nodes of a kind matching a property filter.

    Body: {"kind": "cached_result", "filter": {"agent": "my-agent"}}
    Used by the gateway to clear all cached results for an agent.
    """
    store: KnowledgeStore = request.app["store"]
    try:
        body = await request.json()
    except Exception:
        return web.json_response({"error": "JSON body required"}, status=400)
    kind = body.get("kind", "")
    filt = body.get("filter", {})
    if not kind:
        return web.json_response({"error": "kind required"}, status=400)
    if not filt or not isinstance(filt, dict):
        return web.json_response({"error": "filter with at least one property required"}, status=400)
    total = 0
    for prop, value in filt.items():
        total += store.soft_delete_by_kind_and_property(kind, prop, value)
    return web.json_response({"deleted": total, "kind": kind})


async def handle_principals_list(request: web.Request) -> web.Response:
    """GET /principals — list all principals, optional ?type= filter."""
    registry: PrincipalRegistry = request.app["principal_registry"]
    ptype = request.query.get("type")
    if ptype:
        if ptype not in PrincipalRegistry.VALID_TYPES:
            return web.json_response(
                {"error": f"invalid type '{ptype}', must be one of: {', '.join(PrincipalRegistry.VALID_TYPES)}"},
                status=400,
            )
        principals = registry.list_by_type(ptype)
    else:
        principals = registry.list_all()
    return web.json_response({"principals": principals})


async def handle_principals_register(request: web.Request) -> web.Response:
    """POST /principals — no longer supported.

    Principal registration is now handled by the gateway API.
    """
    return web.json_response(
        {"error": "Principal registration moved to gateway API. Use POST /api/v1/principals on the gateway."},
        status=410,
    )


async def handle_principals_resolve(request: web.Request) -> web.Response:
    """GET /principals/{uuid} — resolve a principal UUID."""
    registry: PrincipalRegistry = request.app["principal_registry"]
    principal_uuid = request.match_info["uuid"]
    principal = registry.resolve(principal_uuid)
    if not principal:
        return web.json_response({"error": "principal not found"}, status=404)
    return web.json_response(principal)


async def handle_communities(request: web.Request) -> web.Response:
    """GET /communities — list all Community nodes."""
    store: KnowledgeStore = request.app["store"]
    communities = store.list_communities()
    return web.json_response({"communities": communities})


async def handle_community(request: web.Request) -> web.Response:
    """GET /community/{id} — get members of a specific community."""
    store: KnowledgeStore = request.app["store"]
    community_id = request.match_info["id"]
    members = store.get_community_members(community_id)
    return web.json_response({"community_id": community_id, "members": members})


async def handle_hubs(request: web.Request) -> web.Response:
    """GET /hubs — get top hub nodes, optional ?limit=N."""
    store: KnowledgeStore = request.app["store"]
    limit = int(request.query.get("limit", "20"))
    hubs = store.get_hubs(limit=limit)
    return web.json_response({"hubs": hubs})


async def handle_save_insight(request: web.Request) -> web.Response:
    """POST /insight — save an agent's synthesized insight."""
    store: KnowledgeStore = request.app["store"]

    try:
        body = await request.json()
    except Exception:
        return web.json_response({"error": "Invalid JSON body"}, status=400)

    insight = body.get("insight", "")
    source_nodes = body.get("source_nodes", [])

    if not insight or not insight.strip():
        return web.json_response({"error": "insight is required and must be non-empty"}, status=400)
    if not source_nodes:
        return web.json_response({"error": "source_nodes is required and must be non-empty"}, status=400)

    confidence = body.get("confidence", "medium")
    tags = body.get("tags")
    agent_name = body.get("agent_name", "")

    try:
        loop = asyncio.get_event_loop()
        result = await loop.run_in_executor(
            None,
            lambda: store.save_insight(
                insight=insight,
                source_node_ids=source_nodes,
                confidence=confidence,
                tags=tags,
                agent_name=agent_name,
            ),
        )
    except ValueError as e:
        return web.json_response({"error": str(e)}, status=400)

    return web.json_response(result)


async def _run_schema_migrations(app: web.Application) -> None:
    """Run store schema migrations on startup."""
    store: KnowledgeStore = app["store"]
    try:
        result = store.migrate_edge_provenance()
        if result.get("migrated", 0) > 0:
            logger.info("Edge provenance migration: %s", result)
    except Exception as e:
        logger.warning("Edge provenance migration failed: %s", e)
    try:
        result = store.migrate_node_scopes()
        if result.get("migrated", 0) > 0:
            logger.info("Node scopes migration: %s", result)
    except Exception as e:
        logger.warning("Node scopes migration failed: %s", e)


async def _start_curation_loop(app: web.Application) -> None:
    curator = app.get("curator")
    if curator:
        interval = int(os.environ.get("KNOWLEDGE_CURATOR_INTERVAL", "600"))
        from services.knowledge.curator import CurationLoop
        loop = CurationLoop(curator, interval_seconds=interval)
        app["_curation_task"] = asyncio.ensure_future(loop.run())


async def _stop_curation_loop(app: web.Application) -> None:
    task = app.get("_curation_task")
    if task:
        task.cancel()
        try:
            await task
        except asyncio.CancelledError:
            pass


async def _start_backfill_task(app: web.Application) -> None:
    """Run embedding backfill in a background thread (blocking I/O)."""
    store: KnowledgeStore = app["store"]

    async def _run_backfill() -> None:
        try:
            loop = asyncio.get_event_loop()
            count = await loop.run_in_executor(None, store.backfill_embeddings)
            if count > 0:
                logger.info("Embedding backfill completed: %d nodes", count)
        except Exception as e:
            logger.warning("Embedding backfill failed: %s", e)

    app["_backfill_task"] = asyncio.ensure_future(_run_backfill())


async def _stop_backfill_task(app: web.Application) -> None:
    task = app.get("_backfill_task")
    if task:
        task.cancel()
        try:
            await task
        except asyncio.CancelledError:
            pass


async def _start_ingestion_loop(app: web.Application) -> None:
    app["_ingestion_task"] = asyncio.ensure_future(_ingestion_loop(app))


async def _stop_ingestion_loop(app: web.Application) -> None:
    task = app.get("_ingestion_task")
    if task:
        task.cancel()
        try:
            await task
        except asyncio.CancelledError:
            pass


async def _ingestion_loop(app: web.Application) -> None:
    ingester: RuleIngester = app["ingester"]
    synthesizer: LLMSynthesizer = app["synthesizer"]
    gateway: GatewayClient = app["gateway"]
    gateway_url = gateway.base_url
    headers = gateway._headers()
    http = httpx.AsyncClient(timeout=10)
    last_timestamps: dict[str, str] = {}

    logger.info("Ingestion loop started (gateway=%s)", gateway_url)

    while True:
        try:
            # Poll comms channels via gateway
            resp = await http.get(f"{gateway_url}/api/v1/comms/channels", headers=headers)
            if resp.status_code == 200:
                channels = resp.json()
                for ch in channels:
                    ch_name = ch.get("name", "")
                    since = last_timestamps.get(ch_name, "1970-01-01T00:00:00Z")
                    # Get messages since last poll
                    msg_resp = await http.get(
                        f"{gateway_url}/api/v1/comms/channels/{ch_name}/messages",
                        params={"since": since, "limit": "100"},
                        headers=headers,
                    )
                    if msg_resp.status_code == 200:
                        messages = msg_resp.json()
                        for msg in messages:
                            ingester.ingest_message(msg)
                            synthesizer.record_message(msg.get("id", ""), msg)
                            ts = msg.get("timestamp", "")
                            if ts > last_timestamps.get(ch_name, ""):
                                last_timestamps[ch_name] = ts

                # Check if synthesis is needed
                if synthesizer.should_synthesize():
                    all_channels = list(last_timestamps.keys())
                    synthesizer.synthesize(
                        synthesizer._pending_messages, all_channels
                    )
                manager = app.get("knowledge_manager")
                if manager is not None:
                    manager.process_cycle()

        except asyncio.CancelledError:
            raise
        except Exception as e:
            logger.debug("Ingestion poll error (comms may not be ready): %s", e)

        await asyncio.sleep(10)


async def handle_quarantine(request: web.Request) -> web.Response:
    """POST /quarantine — quarantine all nodes contributed by an agent.

    Body: {"agent": "name", "since": "optional-ISO-timestamp"}
    """
    store: KnowledgeStore = request.app["store"]
    try:
        body = await request.json()
    except Exception:
        return web.json_response({"error": "Invalid JSON body"}, status=400)

    agent = body.get("agent", "")
    if not agent:
        return web.json_response({"error": "agent is required"}, status=400)

    since = body.get("since")
    try:
        result = store.quarantine_by_agent(agent, since=since)
    except Exception:
        logger.exception("Quarantine failed for agent=%s", agent)
        return web.json_response({"error": "quarantine failed"}, status=500)

    return web.json_response(result)


async def handle_quarantine_release(request: web.Request) -> web.Response:
    """POST /quarantine/release — release quarantined node(s).

    Body: {"node_id": "id"} to release a single node,
          or {"agent": "name"} to release all quarantined nodes for an agent.
    """
    store: KnowledgeStore = request.app["store"]
    try:
        body = await request.json()
    except Exception:
        return web.json_response({"error": "Invalid JSON body"}, status=400)

    node_id = body.get("node_id")
    agent = body.get("agent")

    if not node_id and not agent:
        return web.json_response({"error": "node_id or agent is required"}, status=400)

    try:
        if node_id:
            store.quarantine_release_node(node_id)
            return web.json_response({"released_node": node_id})
        else:
            result = store.quarantine_release_agent(agent)
            return web.json_response(result)
    except Exception:
        logger.exception("Quarantine release failed")
        return web.json_response({"error": "release failed"}, status=500)


async def handle_quarantine_list(request: web.Request) -> web.Response:
    """GET /quarantine — list quarantined nodes, optionally filtered by agent."""
    store: KnowledgeStore = request.app["store"]
    agent = request.query.get("agent")
    try:
        nodes = store.list_quarantined(agent=agent)
    except Exception:
        logger.exception("Quarantine list failed")
        return web.json_response({"error": "list failed"}, status=500)

    return web.json_response({"quarantined": nodes})


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--data-dir", default="/data")
    parser.add_argument("--port", type=int, default=8080)
    args = parser.parse_args()

    ingestion_env = os.environ.get("KNOWLEDGE_INGESTION", "true")
    enable_ingestion = ingestion_env.lower() in ("true", "1", "yes")

    # Logging configured automatically by sitecustomize.py via AGENCY_COMPONENT env var.
    app = create_app(data_dir=Path(args.data_dir), enable_ingestion=enable_ingestion)
    logger.info("Starting knowledge server on port %d", args.port)
    web.run_app(app, port=args.port, access_log_class=_HealthFilterAccessLogger)
    logger.info("Knowledge server stopped")
