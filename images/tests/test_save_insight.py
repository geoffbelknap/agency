"""Tests for KnowledgeStore.save_insight() — query feedback loop."""

import json
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from images.knowledge.store import KnowledgeStore


class TestSaveInsightValidation:
    def test_rejects_invalid_confidence(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        n1 = store.add_node(label="src", kind="concept", summary="source node")
        with pytest.raises(ValueError, match="confidence"):
            store.save_insight("some insight", [n1], confidence="extreme")

    def test_rejects_empty_source_node_ids(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        with pytest.raises(ValueError, match="source_node_ids"):
            store.save_insight("some insight", [], confidence="high")

    def test_rejects_nonexistent_source_node(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        n1 = store.add_node(label="real", kind="concept", summary="exists")
        with pytest.raises(ValueError, match="not found"):
            store.save_insight("insight", [n1, "nonexistent_id"], confidence="high")


class TestSaveInsightNodeCreation:
    def test_creates_finding_node(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        n1 = store.add_node(label="src", kind="concept", summary="source")
        result = store.save_insight("This is an insight", [n1], confidence="high")
        node = store.get_node(result["node_id"])
        assert node is not None
        assert node["kind"] == "finding"

    def test_node_summary_is_full_insight(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        n1 = store.add_node(label="src", kind="concept", summary="source")
        insight_text = "A detailed analysis of the situation"
        result = store.save_insight(insight_text, [n1], confidence="medium")
        node = store.get_node(result["node_id"])
        assert node["summary"] == insight_text

    def test_label_truncated_to_100_chars(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        n1 = store.add_node(label="src", kind="concept", summary="source")
        long_insight = "x" * 200
        result = store.save_insight(long_insight, [n1], confidence="low")
        node = store.get_node(result["node_id"])
        assert len(node["label"]) == 100

    def test_source_type_is_agent(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        n1 = store.add_node(label="src", kind="concept", summary="source")
        result = store.save_insight("insight", [n1], confidence="high")
        node = store.get_node(result["node_id"])
        assert node["source_type"] == "agent"


class TestSaveInsightProperties:
    def test_confidence_stored(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        n1 = store.add_node(label="src", kind="concept", summary="source")
        result = store.save_insight("insight", [n1], confidence="high")
        node = store.get_node(result["node_id"])
        props = json.loads(node["properties"])
        assert props["confidence"] == "high"

    def test_agent_name_stored(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        n1 = store.add_node(label="src", kind="concept", summary="source")
        result = store.save_insight("insight", [n1], confidence="medium", agent_name="analyst-1")
        node = store.get_node(result["node_id"])
        props = json.loads(node["properties"])
        assert props["contributed_by"] == "analyst-1"

    def test_tags_stored(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        n1 = store.add_node(label="src", kind="concept", summary="source")
        result = store.save_insight(
            "insight", [n1], confidence="high", tags=["threat-intel", "ioc"]
        )
        node = store.get_node(result["node_id"])
        props = json.loads(node["properties"])
        assert props["tags"] == ["threat-intel", "ioc"]

    def test_no_tags_key_when_none(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        n1 = store.add_node(label="src", kind="concept", summary="source")
        result = store.save_insight("insight", [n1], confidence="high")
        node = store.get_node(result["node_id"])
        props = json.loads(node["properties"])
        assert "tags" not in props

    def test_source_count_stored(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        n1 = store.add_node(label="a", kind="concept", summary="source a")
        n2 = store.add_node(label="b", kind="concept", summary="source b")
        result = store.save_insight("insight", [n1, n2], confidence="high")
        node = store.get_node(result["node_id"])
        props = json.loads(node["properties"])
        assert props["source_count"] == 2

    def test_insight_type_stored(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        n1 = store.add_node(label="src", kind="concept", summary="source")
        result = store.save_insight("insight", [n1], confidence="high")
        node = store.get_node(result["node_id"])
        props = json.loads(node["properties"])
        assert props["insight_type"] == "agent_synthesis"


class TestSaveInsightEdges:
    def test_creates_derived_from_edges(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        n1 = store.add_node(label="a", kind="concept", summary="source a")
        n2 = store.add_node(label="b", kind="concept", summary="source b")
        result = store.save_insight("combined insight", [n1, n2], confidence="high")
        assert result["edges_created"] == 2

        edges = store.get_edges(result["node_id"], direction="outgoing", relation="DERIVED_FROM")
        target_ids = {e["target_id"] for e in edges}
        assert target_ids == {n1, n2}

    def test_edges_have_inferred_provenance(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        n1 = store.add_node(label="src", kind="concept", summary="source")
        result = store.save_insight("insight", [n1], confidence="high")
        edges = store.get_edges(result["node_id"], direction="outgoing", relation="DERIVED_FROM")
        assert len(edges) == 1
        assert edges[0]["provenance"] == "INFERRED"


class TestSaveInsightScope:
    def test_scope_is_intersection_of_sources(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        n1 = store.add_node(
            label="a", kind="concept", summary="",
            scope={"channels": ["alpha", "beta"], "principals": ["p1", "p2"]},
        )
        n2 = store.add_node(
            label="b", kind="concept", summary="",
            scope={"channels": ["beta", "gamma"], "principals": ["p2", "p3"]},
        )
        result = store.save_insight("insight", [n1, n2], confidence="high")
        node = store.get_node(result["node_id"])
        scope = json.loads(node["scope"])
        assert sorted(scope["channels"]) == ["beta"]
        assert sorted(scope["principals"]) == ["p2"]

    def test_scope_intersection_three_sources(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        n1 = store.add_node(
            label="a", kind="concept", summary="",
            scope={"channels": ["x", "y", "z"], "principals": ["p1"]},
        )
        n2 = store.add_node(
            label="b", kind="concept", summary="",
            scope={"channels": ["y", "z"], "principals": ["p1", "p2"]},
        )
        n3 = store.add_node(
            label="c", kind="concept", summary="",
            scope={"channels": ["z", "w"], "principals": ["p1", "p3"]},
        )
        result = store.save_insight("insight", [n1, n2, n3], confidence="low")
        node = store.get_node(result["node_id"])
        scope = json.loads(node["scope"])
        assert scope["channels"] == ["z"]
        assert scope["principals"] == ["p1"]

    def test_empty_scope_sources_produce_empty_scope(self, tmp_path):
        """Sources with no scope produce an unrestricted insight scope."""
        store = KnowledgeStore(tmp_path)
        n1 = store.add_node(label="a", kind="concept", summary="")
        n2 = store.add_node(label="b", kind="concept", summary="")
        result = store.save_insight("insight", [n1, n2], confidence="high")
        node = store.get_node(result["node_id"])
        scope = json.loads(node["scope"])
        assert scope["channels"] == []
        assert scope["principals"] == []


class TestSaveInsightChaining:
    def test_insight_references_previous_insight(self, tmp_path):
        """An insight can use another insight as a source (chaining)."""
        store = KnowledgeStore(tmp_path)
        n1 = store.add_node(label="raw-fact", kind="concept", summary="original data")
        first = store.save_insight("first insight", [n1], confidence="high")
        second = store.save_insight(
            "meta insight", [first["node_id"]], confidence="medium"
        )
        node = store.get_node(second["node_id"])
        assert node["kind"] == "finding"
        edges = store.get_edges(second["node_id"], direction="outgoing", relation="DERIVED_FROM")
        assert len(edges) == 1
        assert edges[0]["target_id"] == first["node_id"]
