"""Tests for provenance-based quarantine on the knowledge store."""

import json
import sys
import os

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", ".."))

from images.knowledge.store import KnowledgeStore


def _make_store(tmp_path):
    return KnowledgeStore(tmp_path)


def _add_agent_node(store, label, agent_name, kind="concept"):
    """Helper: add a node with contributed_by in properties."""
    return store.add_node(
        label=label,
        kind=kind,
        summary=f"Summary for {label}",
        properties={"contributed_by": agent_name},
        source_type="agent",
    )


class TestQuarantineByAgent:
    def test_marks_correct_nodes(self, tmp_path):
        store = _make_store(tmp_path)
        n1 = _add_agent_node(store, "finding-1", "scout")
        n2 = _add_agent_node(store, "finding-2", "scout")
        n3 = _add_agent_node(store, "finding-3", "auditor")

        result = store.quarantine_by_agent("scout")
        assert result == {"quarantined": 2}

        # Verify curation_status set
        node1 = store.get_node(n1)
        assert node1["curation_status"] == "quarantined"
        node2 = store.get_node(n2)
        assert node2["curation_status"] == "quarantined"
        # Unrelated agent not affected
        node3 = store.get_node(n3)
        assert node3["curation_status"] is None

    def test_quarantined_excluded_from_find_nodes(self, tmp_path):
        store = _make_store(tmp_path)
        _add_agent_node(store, "vuln-report", "scout")
        _add_agent_node(store, "vuln-analysis", "auditor")

        store.quarantine_by_agent("scout")
        results = store.find_nodes("vuln")
        labels = [r["label"] for r in results]
        assert "vuln-report" not in labels
        assert "vuln-analysis" in labels

    def test_since_limits_scope(self, tmp_path):
        store = _make_store(tmp_path)
        # Add node with an old timestamp by directly updating created_at
        n_old = _add_agent_node(store, "old-finding", "scout")
        store._db.execute(
            "UPDATE nodes SET created_at = ? WHERE id = ?",
            ("2020-01-01T00:00:00Z", n_old),
        )
        store._db.commit()

        n_new = _add_agent_node(store, "new-finding", "scout")

        result = store.quarantine_by_agent("scout", since="2025-01-01T00:00:00Z")
        assert result == {"quarantined": 1}

        # Old node untouched, new node quarantined
        assert store.get_node(n_old)["curation_status"] is None
        assert store.get_node(n_new)["curation_status"] == "quarantined"

    def test_no_matches_returns_zero(self, tmp_path):
        store = _make_store(tmp_path)
        _add_agent_node(store, "finding", "auditor")
        result = store.quarantine_by_agent("nonexistent")
        assert result == {"quarantined": 0}


class TestQuarantineReleaseNode:
    def test_release_individual_node(self, tmp_path):
        store = _make_store(tmp_path)
        n1 = _add_agent_node(store, "finding-1", "scout")
        store.quarantine_by_agent("scout")

        assert store.get_node(n1)["curation_status"] == "quarantined"

        store.quarantine_release_node(n1)
        node = store.get_node(n1)
        assert node["curation_status"] is None
        assert node["curation_reason"] is None
        assert node["curation_at"] is None


class TestQuarantineReleaseAgent:
    def test_release_by_agent(self, tmp_path):
        store = _make_store(tmp_path)
        n1 = _add_agent_node(store, "finding-1", "scout")
        n2 = _add_agent_node(store, "finding-2", "scout")
        n3 = _add_agent_node(store, "finding-3", "auditor")

        store.quarantine_by_agent("scout")
        store.quarantine_by_agent("auditor")

        result = store.quarantine_release_agent("scout")
        assert result == {"released": 2}

        assert store.get_node(n1)["curation_status"] is None
        assert store.get_node(n2)["curation_status"] is None
        # auditor still quarantined
        assert store.get_node(n3)["curation_status"] == "quarantined"


class TestEdgeQuarantineExclusion:
    def test_edges_touching_quarantined_excluded(self, tmp_path):
        store = _make_store(tmp_path)
        n1 = _add_agent_node(store, "agent-node", "scout")
        n2 = _add_agent_node(store, "clean-node", "auditor")
        n3 = _add_agent_node(store, "another-clean", "auditor")

        store.add_edge(source_id=n1, target_id=n2, relation="relates_to")
        store.add_edge(source_id=n2, target_id=n3, relation="supports")

        # Before quarantine: n2 has edges to both n1 and n3
        edges_before = store.get_edges(n2, direction="both")
        assert len(edges_before) == 2

        store.quarantine_by_agent("scout")

        # After quarantine: edge touching n1 should be excluded
        edges_out_n2 = store.get_edges(n2, direction="both")
        assert len(edges_out_n2) == 1
        assert edges_out_n2[0]["relation"] == "supports"

        # Edges from quarantined node also excluded
        edges_n1 = store.get_edges(n1, direction="outgoing")
        assert len(edges_n1) == 0


class TestListQuarantined:
    def test_list_all(self, tmp_path):
        store = _make_store(tmp_path)
        _add_agent_node(store, "finding-1", "scout")
        _add_agent_node(store, "finding-2", "scout")
        _add_agent_node(store, "finding-3", "auditor")

        store.quarantine_by_agent("scout")
        store.quarantine_by_agent("auditor")

        quarantined = store.list_quarantined()
        assert len(quarantined) == 3

    def test_list_by_agent(self, tmp_path):
        store = _make_store(tmp_path)
        _add_agent_node(store, "finding-1", "scout")
        _add_agent_node(store, "finding-2", "auditor")

        store.quarantine_by_agent("scout")
        store.quarantine_by_agent("auditor")

        quarantined = store.list_quarantined(agent="scout")
        assert len(quarantined) == 1
        assert quarantined[0]["label"] == "finding-1"

    def test_list_empty(self, tmp_path):
        store = _make_store(tmp_path)
        _add_agent_node(store, "finding", "scout")
        assert store.list_quarantined() == []


class TestCurationLog:
    def test_quarantine_logged(self, tmp_path):
        store = _make_store(tmp_path)
        n1 = _add_agent_node(store, "finding-1", "scout")
        store.quarantine_by_agent("scout")

        logs = store.get_curation_log(action="quarantine_by_agent")
        assert len(logs) >= 1
        node_ids = [log["node_id"] for log in logs]
        assert n1 in node_ids

    def test_release_logged(self, tmp_path):
        store = _make_store(tmp_path)
        n1 = _add_agent_node(store, "finding-1", "scout")
        store.quarantine_by_agent("scout")
        store.quarantine_release_node(n1)

        logs = store.get_curation_log(node_id=n1, action="quarantine_release")
        assert len(logs) == 1

    def test_release_agent_logged(self, tmp_path):
        store = _make_store(tmp_path)
        n1 = _add_agent_node(store, "finding-1", "scout")
        n2 = _add_agent_node(store, "finding-2", "scout")
        store.quarantine_by_agent("scout")
        store.quarantine_release_agent("scout")

        logs = store.get_curation_log(action="quarantine_release_agent")
        node_ids = [log["node_id"] for log in logs]
        assert n1 in node_ids
        assert n2 in node_ids
