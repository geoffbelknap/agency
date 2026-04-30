"""Tests for query_graph knowledge store methods and endpoints."""
import json
import pytest
from unittest.mock import MagicMock, patch


class TestFilterNodesByProperty:
    def test_finds_matching_node(self, tmp_path):
        from services.knowledge.store import KnowledgeStore
        store = KnowledgeStore(tmp_path)
        node_id = store.add_node("server-1", "Device", "A server", {"ip": "192.168.1.5"}, "agent")
        results = store.filter_nodes_by_property("Device", "ip", "192.168.1.5")
        assert len(results) == 1
        assert results[0]["id"] == node_id

    def test_no_match_returns_empty(self, tmp_path):
        from services.knowledge.store import KnowledgeStore
        store = KnowledgeStore(tmp_path)
        store.add_node("server-1", "Device", "A server", {"ip": "192.168.1.5"}, "agent")
        results = store.filter_nodes_by_property("Device", "ip", "10.0.0.1")
        assert results == []

    def test_respects_kind_filter(self, tmp_path):
        from services.knowledge.store import KnowledgeStore
        store = KnowledgeStore(tmp_path)
        store.add_node("server-1", "Device", "A server", {"ip": "192.168.1.5"}, "agent")
        store.add_node("alert-1", "Alert", "An alert", {"ip": "192.168.1.5"}, "agent")
        results = store.filter_nodes_by_property("Device", "ip", "192.168.1.5")
        assert len(results) == 1
        assert results[0]["kind"] == "Device"

    def test_limit_cap(self, tmp_path):
        from services.knowledge.store import KnowledgeStore
        store = KnowledgeStore(tmp_path)
        for i in range(60):
            store.add_node(f"dev-{i}", "Device", f"Device {i}", {"status": "active"}, "agent")
        results = store.filter_nodes_by_property("Device", "status", "active", limit=50)
        assert len(results) == 50


class TestGetNeighborsSubgraph:
    def test_returns_neighbors(self, tmp_path):
        from services.knowledge.store import KnowledgeStore
        store = KnowledgeStore(tmp_path)
        n1 = store.add_node("server-1", "Device", "Server 1", {}, "agent")
        n2 = store.add_node("alert-1", "Alert", "Alert 1", {}, "agent")
        store.add_edge(n1, n2, "TRIGGERED")
        result = store.get_neighbors_subgraph(n1)
        assert len(result["nodes"]) == 1
        assert result["nodes"][0]["id"] == n2

    def test_filters_by_relation(self, tmp_path):
        from services.knowledge.store import KnowledgeStore
        store = KnowledgeStore(tmp_path)
        n1 = store.add_node("server-1", "Device", "", {}, "agent")
        n2 = store.add_node("alert-1", "Alert", "", {}, "agent")
        n3 = store.add_node("user-1", "Person", "", {}, "agent")
        store.add_edge(n1, n2, "TRIGGERED")
        store.add_edge(n1, n3, "OWNED_BY")
        result = store.get_neighbors_subgraph(n1, relation="TRIGGERED")
        assert len(result["nodes"]) == 1
        assert result["nodes"][0]["id"] == n2

    def test_caps_at_50(self, tmp_path):
        from services.knowledge.store import KnowledgeStore
        store = KnowledgeStore(tmp_path)
        center = store.add_node("center", "Hub", "", {}, "agent")
        for i in range(60):
            nid = store.add_node(f"leaf-{i}", "Leaf", "", {}, "agent")
            store.add_edge(center, nid, "CONNECTED")
        result = store.get_neighbors_subgraph(center)
        assert len(result["nodes"]) <= 50


class TestQueryGraphTool:
    def test_get_entity_dispatch(self):
        from images.body.knowledge_tools import _query_graph
        with patch("images.body.knowledge_tools._http") as mock_http:
            mock_resp = MagicMock()
            mock_resp.text = '{"nodes": [{"id": "abc"}], "edges": []}'
            mock_http.get.return_value = mock_resp
            result = _query_graph("http://enforcer:8081/mediation/knowledge", "test-agent", {"pattern": "get_entity", "id": "abc"})
            assert "abc" in result

    def test_missing_pattern_returns_error(self):
        from images.body.knowledge_tools import _query_graph
        result = _query_graph("http://enforcer:8081/mediation/knowledge", "test-agent", {})
        assert "error" in result.lower()

    def test_missing_id_for_get_entity(self):
        from images.body.knowledge_tools import _query_graph
        result = _query_graph("http://enforcer:8081/mediation/knowledge", "test-agent", {"pattern": "get_entity"})
        assert "error" in result.lower()
