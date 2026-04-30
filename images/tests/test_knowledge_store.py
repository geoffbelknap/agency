"""Tests for the knowledge graph store."""

import json

from services.knowledge.store import KnowledgeStore


class TestNodeCRUD:
    def test_add_node(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        node_id = store.add_node(
            label="ChefHub pricing",
            kind="concept",
            summary="Three-tier pricing model",
            properties={"tiers": ["free", "pro", "enterprise"]},
            source_type="llm",
            source_channels=["#platform-review"],
        )
        assert node_id is not None
        node = store.get_node(node_id)
        assert node["label"] == "ChefHub pricing"
        assert node["kind"] == "concept"
        assert node["summary"] == "Three-tier pricing model"
        assert json.loads(node["properties"])["tiers"][0] == "free"
        assert json.loads(node["source_channels"]) == ["#platform-review"]

    def test_update_node_summary(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        node_id = store.add_node(label="pricing", kind="concept", summary="old")
        store.update_node(node_id, summary="new summary")
        node = store.get_node(node_id)
        assert node["summary"] == "new summary"

    def test_get_missing_node(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        assert store.get_node("nonexistent") is None

    def test_find_node_by_label(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        store.add_node(label="auth module", kind="component", summary="Handles login")
        store.add_node(label="pricing model", kind="concept", summary="Three tiers")
        results = store.find_nodes("auth")
        assert len(results) == 1
        assert results[0]["label"] == "auth module"

    def test_find_nodes_by_kind(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        store.add_node(label="scout", kind="agent", summary="Integration agent")
        store.add_node(label="pricing", kind="concept", summary="Model")
        store.add_node(label="auditor", kind="agent", summary="Security agent")
        results = store.find_nodes_by_kind("agent")
        assert len(results) == 2


class TestEdgeCRUD:
    def test_add_edge(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        n1 = store.add_node(label="scout", kind="agent", summary="")
        n2 = store.add_node(label="pricing", kind="concept", summary="")
        edge_id = store.add_edge(
            source_id=n1,
            target_id=n2,
            relation="discussed",
            weight=0.8,
            source_channel="#general",
        )
        assert edge_id is not None
        edges = store.get_edges(n1)
        assert len(edges) == 1
        assert edges[0]["relation"] == "discussed"
        assert edges[0]["target_id"] == n2

    def test_get_edges_incoming(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        n1 = store.add_node(label="scout", kind="agent", summary="")
        n2 = store.add_node(label="pricing", kind="concept", summary="")
        store.add_edge(source_id=n1, target_id=n2, relation="proposed")
        incoming = store.get_edges(n2, direction="incoming")
        assert len(incoming) == 1
        assert incoming[0]["source_id"] == n1

    def test_get_edges_both_with_relation_filter(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        n1 = store.add_node(label="scout", kind="agent", summary="")
        n2 = store.add_node(label="pricing", kind="concept", summary="")
        n3 = store.add_node(label="tier", kind="concept", summary="")
        store.add_edge(source_id=n1, target_id=n2, relation="proposed")
        store.add_edge(source_id=n2, target_id=n3, relation="includes")
        edges = store.get_edges(n2, direction="both", relation="proposed")
        assert len(edges) == 1
        assert edges[0]["relation"] == "proposed"


class TestACLFiltering:
    def test_find_nodes_filtered_by_channel(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        store.add_node(
            label="secret project",
            kind="concept",
            summary="Top secret",
            source_channels=["#private-ops"],
        )
        store.add_node(
            label="public feature",
            kind="concept",
            summary="Open",
            source_channels=["#general"],
        )
        # Agent can only see #general
        results = store.find_nodes("feature", visible_channels=["#general"])
        assert len(results) == 1
        assert results[0]["label"] == "public feature"

    def test_structural_nodes_always_visible(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        store.add_node(label="scout", kind="agent", summary="", source_channels=[])
        results = store.find_nodes("scout", visible_channels=["#general"])
        assert len(results) == 1


class TestSubgraph:
    def test_get_subgraph(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        n1 = store.add_node(label="pricing", kind="concept", summary="Three tiers")
        n2 = store.add_node(label="scout", kind="agent", summary="")
        n3 = store.add_node(label="free tier", kind="concept", summary="No cost")
        store.add_edge(source_id=n2, target_id=n1, relation="proposed")
        store.add_edge(source_id=n1, target_id=n3, relation="includes")
        subgraph = store.get_subgraph(n1, max_hops=1)
        assert len(subgraph["nodes"]) == 3
        assert len(subgraph["edges"]) == 2


class TestExport:
    def test_export_jsonl(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        store.add_node(label="test", kind="concept", summary="A test node")
        lines = store.export_jsonl()
        assert len(lines) >= 1
        parsed = json.loads(lines[0])
        assert parsed["type"] == "node"
        assert parsed["label"] == "test"


class TestUpdateFTS:
    def test_update_label_updates_fts(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        node_id = store.add_node(label="old name", kind="concept", summary="A thing")
        store.update_node(node_id, label="new name")
        assert len(store.find_nodes("new")) == 1
        assert len(store.find_nodes("old")) == 0


class TestStats:
    def test_stats_counts(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        store.add_node(label="a", kind="agent", summary="")
        store.add_node(label="b", kind="concept", summary="")
        n1 = store.add_node(label="c", kind="agent", summary="")
        n2 = store.add_node(label="d", kind="concept", summary="")
        store.add_edge(source_id=n1, target_id=n2, relation="knows")
        stats = store.stats()
        assert stats["nodes"] == 4
        assert stats["edges"] == 1
        assert stats["kinds"]["agent"] == 2
        assert stats["kinds"]["concept"] == 2
