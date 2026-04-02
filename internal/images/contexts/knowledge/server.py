"""Knowledge graph HTTP server.

aiohttp server exposing knowledge query and ingestion endpoints.
Runs on the mediation network as shared infrastructure.

Endpoints:
    GET  /health                  - Health check
    POST /query                   - Query knowledge (synthesized search)
    GET  /who-knows?topic=X       - Find agents who know about a topic
    GET  /changes?since=T         - What changed since timestamp
    GET  /context?subject=X       - Full context about a subject
    POST /ingest/nodes            - Ingest nodes (rule-based or LLM)
    POST /ingest/edges            - Ingest edges
    GET  /export?format=jsonl     - Export graph for centralization
    GET  /stats                   - Graph statistics
"""

import argparse
import asyncio
import logging
import os
from pathlib import Path

import httpx
import yaml
from aiohttp import web

from agency_core.images.knowledge.ingester import RuleIngester
from agency_core.images.knowledge.store import KnowledgeStore
from agency_core.images.knowledge.synthesizer import LLMSynthesizer

logger = logging.getLogger("agency.knowledge")


def publish_knowledge_update(comms_url: str, node_summary: str, metadata: dict) -> None:
    """Publish a knowledge update to the _knowledge-updates comms channel."""
    try:
        client = httpx.Client(timeout=5)
        client.post(
            f"{comms_url}/channels/_knowledge-updates/messages",
            json={
                "author": "_knowledge-service",
                "content": node_summary,
                "metadata": metadata,
            },
            headers={"X-Agency-Platform": "true"},
        )
    except Exception:
        logger.warning("Failed to publish knowledge update to comms")


def _run_ontology_migration(store: KnowledgeStore, data_dir: Path) -> None:
    """One-time migration from freeform kinds to ontology types."""
    marker = data_dir / ".ontology-migrated"
    ontology_path = Path(os.environ.get("AGENCY_ONTOLOGY_PATH", "/app/ontology.yaml"))

    if marker.exists() or not ontology_path.exists():
        return

    try:
        ontology = yaml.safe_load(ontology_path.read_text())
    except Exception as e:
        logger.warning("Cannot load ontology for migration: %s", e)
        return

    entity_types = set(ontology.get("entity_types", {}).keys())
    if not entity_types:
        return

    # Mapping from common freeform kinds to ontology types
    kind_map = {
        "agent": "system",
        "channel": None,  # platform metadata, not a knowledge entity
        "team": "team",
        "topic": "concept",
        "fact": "fact",
        "decision": "decision",
        "finding": "finding",
        "concept": "concept",
        "person": "person",
        "project": "project",
        "system": "system",
        "service": "service",
        "incident": "incident",
        "task": "task",
        "process": "process",
        "document": "document",
        "rule": "rule",
        "pattern": "pattern",
        "lesson": "lesson",
    }

    try:
        all_nodes = store._db.execute("SELECT id, kind FROM nodes").fetchall()
        remapped = 0
        unchanged = 0
        skipped = 0

        for row in all_nodes:
            node_id = row[0] if isinstance(row, (list, tuple)) else row["id"]
            old_kind = row[1] if isinstance(row, (list, tuple)) else row["kind"]

            if old_kind in entity_types:
                unchanged += 1
                continue

            new_kind = kind_map.get(old_kind)
            if new_kind is None:
                # Check if it's a channel or other platform metadata
                if old_kind in ("channel",):
                    skipped += 1
                    continue
                new_kind = "fact"  # safe fallback

            if new_kind != old_kind:
                store._db.execute(
                    "UPDATE nodes SET kind = ? WHERE id = ?",
                    (new_kind, node_id),
                )
                remapped += 1
            else:
                unchanged += 1

        store._db.commit()
        marker.write_text(f"migrated: {remapped} remapped, {unchanged} unchanged, {skipped} skipped\n")
        logger.info(
            "Ontology migration complete: %d remapped, %d unchanged, %d skipped",
            remapped, unchanged, skipped,
        )
    except Exception as e:
        logger.error("Ontology migration failed: %s", e)


def create_app(data_dir: Path | None = None, enable_ingestion: bool = False) -> web.Application:
    app = web.Application()
    store = KnowledgeStore(data_dir or Path("/data"))
    app["store"] = store

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
        from agency_core.images.knowledge.curator import Curator, CurationLoop
        curator = Curator(store, mode=curator_mode)
        app["curator"] = curator
        app.on_startup.append(_start_curation_loop)
        app.on_cleanup.append(_stop_curation_loop)

    if should_ingest and mode != "cache":
        comms_url = os.environ.get("AGENCY_COMMS_URL", "http://comms:8080")
        ingester = RuleIngester(store, curator=app.get("curator"))
        synthesizer = LLMSynthesizer(store, curator=app.get("curator"))
        app["ingester"] = ingester
        app["synthesizer"] = synthesizer
        app["comms_url"] = comms_url
        app.on_startup.append(_start_ingestion_loop)
        app.on_cleanup.append(_stop_ingestion_loop)

    app.router.add_get("/health", handle_health)
    app.router.add_post("/query", handle_query)
    app.router.add_get("/who-knows", handle_who_knows)
    app.router.add_get("/changes", handle_changes)
    app.router.add_get("/context", handle_context)
    app.router.add_get("/neighbors", handle_neighbors)
    app.router.add_get("/path", handle_path)
    app.router.add_post("/ingest/nodes", handle_ingest_nodes)
    app.router.add_post("/ingest/edges", handle_ingest_edges)
    app.router.add_get("/export", handle_export)
    app.router.add_get("/stats", handle_stats)
    app.router.add_get("/curation/flags", handle_curation_flags)
    app.router.add_post("/curation/restore", handle_curation_restore)
    app.router.add_post("/curation/unflag", handle_curation_unflag)
    app.router.add_get("/curation/log", handle_curation_log)
    app.router.add_post("/migrate-kind", handle_migrate_kind)
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
    hops = min(max(int(request.query.get("hops", "2")), 1), 3)
    if not subject:
        return web.json_response({"error": "subject required"}, status=400)
    nodes = store.find_nodes(subject, visible_channels=visible)
    if not nodes:
        return web.json_response({"nodes": [], "edges": []})
    subgraph = store.get_subgraph(
        nodes[0]["id"], max_hops=hops, visible_channels=visible
    )
    return web.json_response(subgraph)


async def _cache_context(request, store):
    subject = request.query.get("subject", "")
    if not subject:
        return web.json_response({"error": "subject required"}, status=400)
    visible_raw = request.query.get("visible_channels", "")
    visible = [c.strip() for c in visible_raw.split(",") if c.strip()] or None
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
    subgraph = store.get_subgraph(nodes[0]["id"], max_hops=2, visible_channels=visible)
    return web.json_response(subgraph)


async def handle_neighbors(request: web.Request) -> web.Response:
    store: KnowledgeStore = request.app["store"]
    node_id = request.query.get("node_id", "")
    direction = request.query.get("direction", "both")
    relation = request.query.get("relation") or None
    if not node_id:
        return web.json_response({"error": "node_id required"}, status=400)
    if direction not in ("outgoing", "incoming", "both"):
        return web.json_response({"error": "direction must be outgoing, incoming, or both"}, status=400)
    result = store.get_neighbors(node_id, direction=direction, relation=relation)
    return web.json_response(result)


async def handle_path(request: web.Request) -> web.Response:
    store: KnowledgeStore = request.app["store"]
    from_label = request.query.get("from", "")
    to_label = request.query.get("to", "")
    try:
        max_hops = int(request.query.get("max_hops", "4"))
    except ValueError:
        return web.json_response({"error": "max_hops must be an integer"}, status=400)
    if not from_label or not to_label:
        return web.json_response({"error": "from and to required"}, status=400)
    result = store.find_path(from_label, to_label, max_hops=max_hops)
    if result is None:
        return web.json_response({"error": "no path found", "from": from_label, "to": to_label}, status=404)
    return web.json_response(result)


async def handle_ingest_nodes(request: web.Request) -> web.Response:
    store: KnowledgeStore = request.app["store"]

    if request.app.get("mode") == "cache":
        return await _cache_ingest_nodes(request, store)

    body = await request.json()
    nodes = body.get("nodes", [])
    comms_url = request.app.get("comms_url", os.environ.get("AGENCY_COMMS_URL", "http://comms:18091"))
    count = 0
    for node in nodes:
        node_id = store.add_node(
            label=node["label"],
            kind=node["kind"],
            summary=node.get("summary", ""),
            properties=node.get("properties"),
            source_type=node.get("source_type", "rule"),
            source_channels=node.get("source_channels"),
        )
        node_summary = node.get("summary") or node["label"]
        publish_knowledge_update(
            comms_url=comms_url,
            node_summary=node_summary,
            metadata={
                "node_id": node_id,
                "kind": node["kind"],
                "topic": node["label"],
                "contributed_by": node.get("source_type", "rule"),
            },
        )
        count += 1
    return web.json_response({"ingested": count})


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


async def handle_migrate_kind(request: web.Request) -> web.Response:
    """Migrate all nodes from one kind to another."""
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


async def _start_curation_loop(app: web.Application) -> None:
    curator = app.get("curator")
    if curator:
        interval = int(os.environ.get("KNOWLEDGE_CURATOR_INTERVAL", "600"))
        from agency_core.images.knowledge.curator import CurationLoop
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
    comms_url: str = app["comms_url"]
    http = httpx.AsyncClient(timeout=10)
    last_timestamps: dict[str, str] = {}

    logger.info("Ingestion loop started (comms=%s)", comms_url)

    while True:
        try:
            # Poll comms for channels
            resp = await http.get(f"{comms_url}/channels")
            if resp.status_code == 200:
                channels = resp.json()
                for ch in channels:
                    ch_name = ch.get("name", "")
                    since = last_timestamps.get(ch_name, "1970-01-01T00:00:00Z")
                    # Get messages since last poll
                    msg_resp = await http.get(
                        f"{comms_url}/channels/{ch_name}/messages",
                        params={"since": since, "limit": "100"},
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

        except asyncio.CancelledError:
            raise
        except Exception as e:
            logger.debug("Ingestion poll error (comms may not be ready): %s", e)

        await asyncio.sleep(10)


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--data-dir", default="/data")
    parser.add_argument("--port", type=int, default=8080)
    args = parser.parse_args()

    ingestion_env = os.environ.get("KNOWLEDGE_INGESTION", "true")
    enable_ingestion = ingestion_env.lower() in ("true", "1", "yes")

    logging.basicConfig(level=logging.INFO)
    app = create_app(data_dir=Path(args.data_dir), enable_ingestion=enable_ingestion)
    web.run_app(app, port=args.port)
