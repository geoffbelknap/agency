"""Tests for the knowledge HTTP server."""

import json

import pytest

from images.knowledge.server import create_app
from .conftest import PlatformClient


@pytest.fixture
def client(tmp_path, aiohttp_client):
    async def _make():
        app = create_app(data_dir=tmp_path)
        raw = await aiohttp_client(app)
        return PlatformClient(raw)
    return _make()


class TestHealth:
    @pytest.mark.asyncio
    async def test_health(self, client):
        c = await client
        resp = await c.get("/health")
        assert resp.status == 200
        data = await resp.json()
        assert data["status"] == "ok"


class TestNodeIngestion:
    @pytest.mark.asyncio
    async def test_ingest_nodes(self, client):
        c = await client
        resp = await c.post("/ingest/nodes", json={
            "nodes": [
                {"label": "pricing", "kind": "concept", "summary": "Three tiers",
                 "source_type": "llm", "source_channels": ["#general"]},
            ]
        })
        assert resp.status == 200
        data = await resp.json()
        assert data["ingested"] == 1

    @pytest.mark.asyncio
    async def test_universal_ingest_markdown(self, client):
        c = await client
        resp = await c.post("/ingest", json={
            "content": "# Graph ingest probe\n\nUnique marker: universal ingest works\n",
            "filename": "probe.md",
            "content_type": "text/markdown",
        })
        assert resp.status == 200
        data = await resp.json()
        assert data["extractor"] == "markdown"
        assert data["nodes_created"] >= 1


class TestQuery:
    @pytest.mark.asyncio
    async def test_query_knowledge(self, client):
        c = await client
        # Ingest a node first
        await c.post("/ingest/nodes", json={
            "nodes": [
                {"label": "pricing model", "kind": "concept",
                 "summary": "Three-tier pricing: free, pro, enterprise",
                 "source_channels": ["#general"]},
            ]
        })
        resp = await c.post("/query", json={
            "query": "pricing",
            "visible_channels": ["#general"],
        })
        assert resp.status == 200
        data = await resp.json()
        assert len(data["results"]) >= 1
        assert "pricing" in data["results"][0]["label"]


class TestContext:
    @pytest.mark.asyncio
    async def test_get_context(self, client):
        c = await client
        await c.post("/ingest/nodes", json={
            "nodes": [
                {"label": "ChefHub", "kind": "project", "summary": "Recipe platform"},
            ]
        })
        resp = await c.get("/context", params={"subject": "ChefHub"})
        assert resp.status == 200
        data = await resp.json()
        assert len(data["nodes"]) >= 1


class TestWhoKnows:
    @pytest.mark.asyncio
    async def test_who_knows(self, client):
        c = await client
        await c.post("/ingest/nodes", json={
            "nodes": [
                {"label": "scout", "kind": "agent", "summary": ""},
                {"label": "pricing", "kind": "concept", "summary": "Tiers"},
            ]
        })
        await c.post("/ingest/edges", json={
            "edges": [
                {"source_label": "scout", "target_label": "pricing",
                 "relation": "discussed", "source_channel": "#general"},
            ]
        })
        resp = await c.get("/who-knows", params={
            "topic": "pricing",
            "visible_channels": "#general",
        })
        assert resp.status == 200
        data = await resp.json()
        assert any(a["label"] == "scout" for a in data["agents"])


class TestChanges:
    @pytest.mark.asyncio
    async def test_changes_since(self, client):
        c = await client
        await c.post("/ingest/nodes", json={
            "nodes": [
                {"label": "new thing", "kind": "concept", "summary": "Just added"},
            ]
        })
        resp = await c.get("/changes", params={"since": "2020-01-01T00:00:00Z"})
        assert resp.status == 200
        data = await resp.json()
        assert len(data["nodes"]) >= 1


class TestExport:
    @pytest.mark.asyncio
    async def test_export(self, client):
        c = await client
        await c.post("/ingest/nodes", json={
            "nodes": [
                {"label": "test", "kind": "concept", "summary": "A node"},
            ]
        })
        resp = await c.get("/export", params={"format": "jsonl"})
        assert resp.status == 200
        text = await resp.text()
        lines = [l for l in text.strip().split("\n") if l]
        assert len(lines) >= 1
        parsed = json.loads(lines[0])
        assert parsed["type"] == "node"


@pytest.fixture
def curator_client(tmp_path, aiohttp_client):
    async def _make():
        app = create_app(data_dir=tmp_path)
        raw = await aiohttp_client(app)
        return PlatformClient(raw)
    return _make()


class TestCurationEndpoints:
    @pytest.mark.asyncio
    async def test_get_flags_empty(self, curator_client):
        c = await curator_client
        resp = await c.get("/curation/flags")
        assert resp.status == 200
        data = await resp.json()
        assert data["flagged"] == []

    @pytest.mark.asyncio
    async def test_get_flags_returns_flagged_nodes(self, curator_client):
        c = await curator_client
        await c.post("/ingest/nodes", json={
            "nodes": [{"label": "suspicious", "kind": "concept", "summary": "maybe bad"}]
        })
        store = c.app["store"]
        nodes = store.find_nodes("suspicious")
        store._db.execute(
            "UPDATE nodes SET curation_status='flagged', curation_reason='test' WHERE id=?",
            (nodes[0]["id"],)
        )
        store._db.commit()
        resp = await c.get("/curation/flags")
        assert resp.status == 200
        data = await resp.json()
        assert len(data["flagged"]) == 1

    @pytest.mark.asyncio
    async def test_restore_soft_deleted_node(self, curator_client):
        c = await curator_client
        await c.post("/ingest/nodes", json={
            "nodes": [{"label": "deleted node", "kind": "concept", "summary": "was deleted"}]
        })
        store = c.app["store"]
        nodes = store.find_nodes("deleted")
        node_id = nodes[0]["id"]
        store._db.execute(
            "UPDATE nodes SET curation_status='soft_deleted', curation_at=datetime('now') WHERE id=?",
            (node_id,)
        )
        store._db.commit()
        resp = await c.post("/curation/restore", json={"node_id": node_id})
        assert resp.status == 200
        node = store.get_node(node_id)
        assert node["curation_status"] is None
        logs = store.get_curation_log(node_id=node_id, action="restore")
        assert len(logs) == 1

    @pytest.mark.asyncio
    async def test_restore_hard_deleted_returns_410(self, curator_client):
        c = await curator_client
        store = c.app["store"]
        store.log_curation("hard_delete", "gone-node-id", {"label": "gone", "kind": "concept"})
        resp = await c.post("/curation/restore", json={"node_id": "gone-node-id"})
        assert resp.status == 410

    @pytest.mark.asyncio
    async def test_unflag_node(self, curator_client):
        c = await curator_client
        await c.post("/ingest/nodes", json={
            "nodes": [{"label": "flagged node", "kind": "concept", "summary": "flagged"}]
        })
        store = c.app["store"]
        nodes = store.find_nodes("flagged")
        node_id = nodes[0]["id"]
        store._db.execute(
            "UPDATE nodes SET curation_status='flagged', curation_reason='test' WHERE id=?",
            (node_id,)
        )
        store._db.commit()
        resp = await c.post("/curation/unflag", json={"node_id": node_id})
        assert resp.status == 200
        node = store.get_node(node_id)
        assert node["curation_status"] is None
        logs = store.get_curation_log(node_id=node_id, action="unflag")
        assert len(logs) == 1

    @pytest.mark.asyncio
    async def test_curation_log_endpoint(self, curator_client):
        c = await curator_client
        resp = await c.get("/curation/log")
        assert resp.status == 200
        data = await resp.json()
        assert "entries" in data

    @pytest.mark.asyncio
    async def test_stats_includes_curation(self, curator_client):
        c = await curator_client
        store = c.app["store"]
        from images.knowledge.curator import Curator
        curator = Curator(store, mode="active")
        store.add_node(label="test", kind="concept", summary="x")
        curator.compute_health_metrics()
        resp = await c.get("/stats")
        assert resp.status == 200
        data = await resp.json()
        assert "curation" in data
