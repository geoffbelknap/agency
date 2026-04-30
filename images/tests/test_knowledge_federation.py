"""Tests for knowledge federation — cache mode, query cache, contribute buffer."""

import json
import os
from pathlib import Path
from unittest.mock import patch

import pytest
import pytest_asyncio

from services.knowledge.server import create_app
from services.knowledge.store import KnowledgeStore
from .conftest import PlatformClient


@pytest.fixture
def store(tmp_path):
    return KnowledgeStore(tmp_path / "data")


class TestQueryCache:
    def test_cache_query_result(self, store):
        """Cache a query result for later retrieval."""
        result = {"query": "test", "results": [{"id": "n1", "label": "Test"}]}
        store.cache_query("test", result)
        cached = store.get_cached_query("test")
        assert cached is not None
        assert cached["results"][0]["label"] == "Test"

    def test_cache_miss(self, store):
        """Uncached query returns None."""
        assert store.get_cached_query("unknown") is None

    def test_cache_expiry(self, store):
        """Expired cache entries return None."""
        result = {"query": "old", "results": []}
        store.cache_query("old", result, ttl_seconds=0)
        # TTL=0 means immediately expired
        cached = store.get_cached_query("old")
        assert cached is None


class TestContributeBuffer:
    def test_buffer_contribution(self, store):
        """Buffer a knowledge contribution when upstream is unavailable."""
        entry = store.buffer_contribution(
            label="Project X",
            kind="concept",
            summary="A new project",
        )
        assert entry["label"] == "Project X"

    def test_read_buffer(self, store):
        """Read buffered contributions in FIFO order."""
        store.buffer_contribution(label="A", kind="concept", summary="first")
        store.buffer_contribution(label="B", kind="concept", summary="second")
        entries = store.read_contribution_buffer()
        assert len(entries) == 2
        assert entries[0]["label"] == "A"
        assert entries[1]["label"] == "B"

    def test_remove_buffer_entry(self, store):
        """Remove a specific buffered contribution after drain."""
        e1 = store.buffer_contribution(label="A", kind="concept", summary="")
        store.buffer_contribution(label="B", kind="concept", summary="")
        store.remove_contribution(e1["id"])
        remaining = store.read_contribution_buffer()
        assert len(remaining) == 1
        assert remaining[0]["label"] == "B"

    def test_empty_buffer(self, store):
        """Empty buffer returns empty list."""
        assert store.read_contribution_buffer() == []


class TestKnowledgeCacheMode:
    @pytest.fixture
    def cache_app(self, tmp_path):
        with patch.dict(os.environ, {
            "KNOWLEDGE_MODE": "cache",
            "KNOWLEDGE_UPSTREAM": "http://manager:18092",
            "KNOWLEDGE_INGESTION": "false",
        }):
            app = create_app(data_dir=tmp_path / "data", enable_ingestion=False)
        return app

    @pytest_asyncio.fixture
    async def cache_client(self, cache_app, aiohttp_client):
        raw = await aiohttp_client(cache_app)
        return PlatformClient(raw)

    @pytest.mark.asyncio
    async def test_health_reports_cache_mode(self, cache_client):
        resp = await cache_client.get("/health")
        data = await resp.json()
        assert data["mode"] == "cache"

    @pytest.mark.asyncio
    async def test_query_upstream_failure_returns_empty(self, cache_client):
        """When upstream is unreachable and no cache, return empty results."""
        resp = await cache_client.post("/query", json={
            "query": "test topic",
        })
        assert resp.status == 200
        data = await resp.json()
        assert data["results"] == []

    @pytest.mark.asyncio
    async def test_ingest_nodes_buffers_when_upstream_down(self, cache_client):
        """Node ingestion is buffered when upstream is unreachable."""
        resp = await cache_client.post("/ingest/nodes", json={
            "nodes": [{"label": "Test Node", "kind": "concept", "summary": "test"}],
        })
        assert resp.status == 200
        data = await resp.json()
        assert data.get("buffered", 0) == 1


class TestIngestionEnvVar:
    def test_ingestion_disabled_by_env(self, tmp_path):
        """KNOWLEDGE_INGESTION=false disables ingestion loop."""
        with patch.dict(os.environ, {"KNOWLEDGE_INGESTION": "false"}):
            app = create_app(data_dir=tmp_path / "data", enable_ingestion=False)
        hook_names = [getattr(f, "__name__", "") for f in app.on_startup]
        assert "_start_ingestion_loop" not in hook_names

    def test_ingestion_enabled_by_default(self, tmp_path):
        """Ingestion is enabled by default (backward compat)."""
        with patch.dict(os.environ, {}, clear=False):
            os.environ.pop("KNOWLEDGE_INGESTION", None)
            os.environ.pop("KNOWLEDGE_MODE", None)
            app = create_app(data_dir=tmp_path / "data", enable_ingestion=True)
        hook_names = [getattr(f, "__name__", "") for f in app.on_startup]
        assert "_start_ingestion_loop" in hook_names
