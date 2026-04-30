"""Tests for ontology emergence scan — Curator.emergence_scan()."""

import json

import pytest

from services.knowledge.store import KnowledgeStore
from services.knowledge.curator import Curator


class TestEmergenceScanKindCandidates:
    def test_detects_novel_kind_above_threshold(self, tmp_path):
        """10 nodes of kind 'widget' across 3 channels → surfaces as candidate."""
        store = KnowledgeStore(tmp_path)
        channels = ["alpha", "beta", "gamma"]
        for i in range(10):
            ch = channels[i % 3]
            store.add_node(
                label=f"widget-{i}",
                kind="widget",
                source_channels=[ch],
            )
        curator = Curator(store, mode="active")
        result = curator.emergence_scan()
        assert result["kind_candidates"] >= 1
        candidates = store.find_nodes_by_kind("OntologyCandidate")
        assert any(
            json.loads(n["properties"]).get("value") == "widget"
            for n in candidates
        )

    def test_ignores_kinds_already_in_ontology(self, tmp_path):
        """Kinds present in loaded ontology are not surfaced."""
        store = KnowledgeStore(tmp_path)
        channels = ["alpha", "beta", "gamma"]
        for i in range(10):
            ch = channels[i % 3]
            store.add_node(
                label=f"tool-{i}",
                kind="tool",
                source_channels=[ch],
            )
        curator = Curator(store, mode="active")
        # Inject a minimal ontology that includes "tool"
        curator._ontology = {
            "entity_types": ["tool"],
            "relationship_types": [],
        }
        result = curator.emergence_scan()
        # "tool" should not be a candidate since it's in the ontology
        candidates = store.find_nodes_by_kind("OntologyCandidate")
        assert not any(
            json.loads(n["properties"]).get("value") == "tool"
            for n in candidates
        )

    def test_below_threshold_kind_not_surfaced(self, tmp_path):
        """Only 5 nodes (< threshold of 10) → not surfaced."""
        store = KnowledgeStore(tmp_path)
        channels = ["alpha", "beta", "gamma"]
        for i in range(5):
            ch = channels[i % 3]
            store.add_node(
                label=f"gadget-{i}",
                kind="gadget",
                source_channels=[ch],
            )
        curator = Curator(store, mode="active")
        result = curator.emergence_scan()
        candidates = store.find_nodes_by_kind("OntologyCandidate")
        assert not any(
            json.loads(n["properties"]).get("value") == "gadget"
            for n in candidates
        )

    def test_creates_ontology_candidate_node(self, tmp_path):
        """OntologyCandidate node has correct structure."""
        store = KnowledgeStore(tmp_path)
        channels = ["alpha", "beta", "gamma"]
        for i in range(10):
            ch = channels[i % 3]
            store.add_node(
                label=f"device-{i}",
                kind="device",
                source_channels=[ch],
            )
        curator = Curator(store, mode="active")
        curator.emergence_scan()
        candidates = store.find_nodes_by_kind("OntologyCandidate")
        device_candidates = [
            n for n in candidates
            if json.loads(n["properties"]).get("value") == "device"
        ]
        assert len(device_candidates) == 1
        node = device_candidates[0]
        assert node["kind"] == "OntologyCandidate"
        assert node["label"] == "candidate:device"
        assert node["source_type"] == "rule"
        props = json.loads(node["properties"])
        assert props["candidate_type"] == "kind"
        assert props["occurrence_count"] >= 10
        assert props["source_count"] >= 3
        assert props["status"] == "candidate"
        assert "first_seen" in props
        assert "last_updated" in props
        assert "example_labels" in props
        assert len(props["example_labels"]) <= 5

    def test_idempotent_no_duplicates(self, tmp_path):
        """Running emergence_scan twice produces only one OntologyCandidate per value."""
        store = KnowledgeStore(tmp_path)
        channels = ["alpha", "beta", "gamma"]
        for i in range(10):
            ch = channels[i % 3]
            store.add_node(
                label=f"sensor-{i}",
                kind="sensor",
                source_channels=[ch],
            )
        curator = Curator(store, mode="active")
        curator.emergence_scan()
        curator.emergence_scan()
        candidates = store.find_nodes_by_kind("OntologyCandidate")
        sensor_candidates = [
            n for n in candidates
            if json.loads(n["properties"]).get("value") == "sensor"
        ]
        assert len(sensor_candidates) == 1

    def test_json_each_counts_distinct_channels_correctly(self, tmp_path):
        """10 nodes all with source_channels=['alpha','beta'] = only 2 sources → below min_sources=3."""
        store = KnowledgeStore(tmp_path)
        for i in range(10):
            store.add_node(
                label=f"widget2-{i}",
                kind="widget2",
                source_channels=["alpha", "beta"],
            )
        curator = Curator(store, mode="active")
        result = curator.emergence_scan()
        candidates = store.find_nodes_by_kind("OntologyCandidate")
        assert not any(
            json.loads(n["properties"]).get("value") == "widget2"
            for n in candidates
        ), "Should not be a candidate — only 2 distinct source channels, below min_sources=3"


class TestEmergenceScanRelationCandidates:
    def test_relation_candidates_detected_from_edges(self, tmp_path):
        """10 edges with relation 'links_to' across 3 channels → surfaces as candidate."""
        store = KnowledgeStore(tmp_path)
        # Create source nodes
        source_ids = []
        target_ids = []
        channels = ["alpha", "beta", "gamma"]
        for i in range(10):
            sid = store.add_node(label=f"src-{i}", kind="concept", source_channels=[channels[i % 3]])
            tid = store.add_node(label=f"tgt-{i}", kind="concept", source_channels=[channels[i % 3]])
            source_ids.append(sid)
            target_ids.append(tid)
        # Add 10 edges with the same relation across 3 channels
        for i in range(10):
            store.add_edge(
                source_ids[i],
                target_ids[i],
                relation="links_to",
                source_channel=channels[i % 3],
            )
        curator = Curator(store, mode="active")
        result = curator.emergence_scan()
        assert result["relation_candidates"] >= 1
        candidates = store.find_nodes_by_kind("OntologyCandidate")
        assert any(
            json.loads(n["properties"]).get("value") == "links_to"
            for n in candidates
        )

    def test_relation_candidate_has_correct_structure(self, tmp_path):
        """OntologyCandidate for a relation has candidate_type='relation'."""
        store = KnowledgeStore(tmp_path)
        channels = ["alpha", "beta", "gamma"]
        nodes = []
        for i in range(20):
            nid = store.add_node(label=f"n-{i}", kind="concept")
            nodes.append(nid)
        for i in range(10):
            store.add_edge(
                nodes[i],
                nodes[i + 10],
                relation="relates_to",
                source_channel=channels[i % 3],
            )
        curator = Curator(store, mode="active")
        curator.emergence_scan()
        candidates = store.find_nodes_by_kind("OntologyCandidate")
        rel_candidates = [
            n for n in candidates
            if json.loads(n["properties"]).get("value") == "relates_to"
        ]
        assert len(rel_candidates) == 1
        props = json.loads(rel_candidates[0]["properties"])
        assert props["candidate_type"] == "relation"
        assert props["status"] == "candidate"


class TestEmergenceScanRejectedCandidates:
    def test_rejected_candidates_not_resurfaced_below_double_threshold(self, tmp_path):
        """Rejected candidate is not re-surfaced unless occurrence_count > 2 * rejection_count_at."""
        store = KnowledgeStore(tmp_path)
        channels = ["alpha", "beta", "gamma"]
        for i in range(10):
            ch = channels[i % 3]
            store.add_node(
                label=f"artifact-{i}",
                kind="artifact",
                source_channels=[ch],
            )
        curator = Curator(store, mode="active")
        # First scan — creates the candidate
        curator.emergence_scan()
        # Manually set the candidate to rejected with rejection_count_at=10
        candidates = store.find_nodes_by_kind("OntologyCandidate")
        artifact_candidate = next(
            n for n in candidates
            if json.loads(n["properties"]).get("value") == "artifact"
        )
        props = json.loads(artifact_candidate["properties"])
        props["status"] = "rejected"
        props["rejection_count_at"] = 10
        store._db.execute(
            "UPDATE nodes SET properties=? WHERE id=?",
            (json.dumps(props), artifact_candidate["id"]),
        )
        store._db.commit()
        # Second scan — occurrence_count is still 10, which is not > 2*10=20, so not re-surfaced
        result = curator.emergence_scan()
        candidates_after = store.find_nodes_by_kind("OntologyCandidate")
        artifact_after = next(
            n for n in candidates_after
            if json.loads(n["properties"]).get("value") == "artifact"
        )
        props_after = json.loads(artifact_after["properties"])
        # Should still be rejected (not reset to candidate)
        assert props_after["status"] == "rejected"

    def test_rejected_candidate_resurfaced_when_count_doubles(self, tmp_path):
        """Rejected candidate IS re-surfaced when occurrence_count > 2 * rejection_count_at."""
        store = KnowledgeStore(tmp_path)
        channels = ["alpha", "beta", "gamma"]
        # Add 10 nodes to trigger the candidate initially
        for i in range(10):
            ch = channels[i % 3]
            store.add_node(
                label=f"blueprint-{i}",
                kind="blueprint",
                source_channels=[ch],
            )
        curator = Curator(store, mode="active")
        curator.emergence_scan()
        # Mark as rejected with rejection_count_at=10
        candidates = store.find_nodes_by_kind("OntologyCandidate")
        bp_candidate = next(
            n for n in candidates
            if json.loads(n["properties"]).get("value") == "blueprint"
        )
        props = json.loads(bp_candidate["properties"])
        props["status"] = "rejected"
        props["rejection_count_at"] = 5  # rejection happened at count=5
        store._db.execute(
            "UPDATE nodes SET properties=? WHERE id=?",
            (json.dumps(props), bp_candidate["id"]),
        )
        store._db.commit()
        # Current occurrence_count is 10, rejection_count_at is 5
        # 10 > 2 * 5 = 10 is False (need strictly greater)
        # Add one more node to make it 11, which > 10
        store.add_node(
            label="blueprint-extra",
            kind="blueprint",
            source_channels=["alpha"],
        )
        result = curator.emergence_scan()
        candidates_after = store.find_nodes_by_kind("OntologyCandidate")
        bp_after = next(
            n for n in candidates_after
            if json.loads(n["properties"]).get("value") == "blueprint"
        )
        props_after = json.loads(bp_after["properties"])
        # Should be re-surfaced as candidate
        assert props_after["status"] == "candidate"


class TestOntologyCandidateExclusion:
    def test_find_nodes_excludes_candidates(self, tmp_path):
        """OntologyCandidate nodes never appear in find_nodes results."""
        store = KnowledgeStore(tmp_path)
        store.add_node("real-device", "device", "A real device", {}, "agent", ["ch-1"])
        store.add_node("candidate:device", "OntologyCandidate", "", {"status": "candidate"}, "rule")
        results = store.find_nodes("device")
        labels = [r["label"] for r in results]
        assert "real-device" in labels
        assert "candidate:device" not in labels


class TestOntologyPromoteReject:
    def test_reject_sets_status_and_logs(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        store.add_node("candidate:device", "OntologyCandidate", "", {
            "candidate_type": "kind", "value": "device",
            "occurrence_count": 15, "source_count": 4, "status": "candidate",
        }, "rule")
        node = store._db.execute(
            "SELECT id, properties FROM nodes WHERE label = 'candidate:device'"
        ).fetchone()
        props = json.loads(node["properties"])
        props["status"] = "rejected"
        props["rejection_count_at"] = props["occurrence_count"]
        store._db.execute("UPDATE nodes SET properties = ? WHERE id = ?",
                          (json.dumps(props), node["id"]))
        store._db.commit()
        store.log_curation("ontology_reject", node["id"], {"value": "device", "occurrence_count": 15})
        updated = store._db.execute("SELECT properties FROM nodes WHERE label = 'candidate:device'").fetchone()
        assert json.loads(updated["properties"])["status"] == "rejected"
        log = store.get_curation_log(action="ontology_reject")
        assert len(log) >= 1
