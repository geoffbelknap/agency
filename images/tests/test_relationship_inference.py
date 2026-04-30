"""Tests for curator relationship inference (3 tiers)."""
import json
import os
import tempfile
import pytest

from services.knowledge.store import KnowledgeStore
from services.knowledge.curator import Curator


@pytest.fixture
def store_and_curator():
    """Create a temporary KnowledgeStore and Curator."""
    with tempfile.TemporaryDirectory() as tmpdir:
        store = KnowledgeStore(tmpdir)
        curator = Curator(store, mode="active", orphan_age_hours=9999)
        yield store, curator


class TestTier1ExplicitRules:
    """Tier 1: explicit property-match rules that auto-create edges."""

    def test_creates_edge_on_matching_property(self, store_and_curator):
        store, curator = store_and_curator

        store.add_node(label="gb-desktop", kind="Device", properties={"host_id": "abc-123"})
        store.add_node(label="Home Network", kind="network_segment", properties={"host_id": "abc-123"})

        rules_path = os.path.join(str(store.data_dir), "inference-rules.yaml")
        with open(rules_path, "w") as f:
            f.write("inference_rules:\n  - match_property: host_id\n    from_kinds: [Device]\n    to_kinds: [network_segment]\n    relation: ON_SEGMENT\n")

        stats = curator.relationship_inference()
        assert stats["tier1_created"] == 1

        device = store.find_nodes("gb-desktop")[0]
        edges = store.get_edges(device["id"], direction="outgoing")
        assert len(edges) == 1
        assert edges[0]["relation"] == "ON_SEGMENT"

    def test_skips_existing_edge(self, store_and_curator):
        store, curator = store_and_curator

        d_id = store.add_node(label="gb-desktop", kind="Device", properties={"host_id": "abc-123"})
        s_id = store.add_node(label="Home", kind="network_segment", properties={"host_id": "abc-123"})
        store.add_edge(source_id=d_id, target_id=s_id, relation="ON_SEGMENT")

        rules_path = os.path.join(str(store.data_dir), "inference-rules.yaml")
        with open(rules_path, "w") as f:
            f.write("inference_rules:\n  - match_property: host_id\n    from_kinds: [Device]\n    to_kinds: [network_segment]\n    relation: ON_SEGMENT\n")

        stats = curator.relationship_inference()
        assert stats["tier1_created"] == 0

    def test_cross_property_matching(self, store_and_curator):
        store, curator = store_and_curator

        store.add_node(label="104.9.124.68:blocked.com", kind="DNSQuery", properties={"client_ip": "104.9.124.68"})
        store.add_node(label="gb-desktop", kind="Device", properties={"ip_address": "104.9.124.68"})

        rules_path = os.path.join(str(store.data_dir), "inference-rules.yaml")
        with open(rules_path, "w") as f:
            f.write("inference_rules:\n  - match_property: client_ip\n    from_kinds: [DNSQuery]\n    to_kinds: [Device]\n    match_to_property: ip_address\n    relation: ORIGINATES_FROM\n")

        stats = curator.relationship_inference()
        assert stats["tier1_created"] == 1

    def test_label_matching(self, store_and_curator):
        store, curator = store_and_curator

        store.add_node(label="session checkpoint", kind="finding", properties={"contributed_by": "henrybot9000"})
        store.add_node(label="henrybot9000", kind="agent", properties={})

        rules_path = os.path.join(str(store.data_dir), "inference-rules.yaml")
        with open(rules_path, "w") as f:
            f.write("inference_rules:\n  - match_property: contributed_by\n    from_kinds: [finding]\n    to_kinds: [agent]\n    match_to_property: label\n    relation: CONTRIBUTED_BY\n")

        stats = curator.relationship_inference()
        assert stats["tier1_created"] == 1

    def test_no_rules_file_is_fine(self, store_and_curator):
        store, curator = store_and_curator
        stats = curator.relationship_inference()
        assert stats["tier1_created"] == 0


class TestTier2PropertyOverlap:
    """Tier 2: automatic property overlap detection."""

    def test_proposes_candidate_for_shared_properties(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            store = KnowledgeStore(tmpdir)
            curator = Curator(store, mode="active", orphan_age_hours=9999)

            # Orphan node with properties
            store.add_node(label="orphan-dns", kind="DNSQuery",
                           properties={"region": "us-west", "protocol": "UDP", "priority": "high"})

            # Connected node with overlapping properties
            d_id = store.add_node(label="my-device", kind="Device",
                                  properties={"region": "us-west", "protocol": "UDP"})
            s_id = store.add_node(label="Home", kind="network_segment", properties={})
            store.add_edge(source_id=d_id, target_id=s_id, relation="ON_SEGMENT")

            stats = curator.relationship_inference()
            assert stats["tier2_proposed"] >= 1

            candidates = store._db.execute(
                "SELECT * FROM nodes WHERE kind = 'RelationshipCandidate'"
            ).fetchall()
            assert len(candidates) >= 1

    def test_skips_single_property_overlap(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            store = KnowledgeStore(tmpdir)
            curator = Curator(store, mode="active", orphan_age_hours=9999)

            store.add_node(label="orphan", kind="DNSQuery",
                           properties={"region": "us-west"})
            d_id = store.add_node(label="device", kind="Device",
                                  properties={"region": "us-west"})
            s_id = store.add_node(label="net", kind="network_segment", properties={})
            store.add_edge(source_id=d_id, target_id=s_id, relation="ON")

            stats = curator.relationship_inference()
            assert stats["tier2_proposed"] == 0


class TestTier1ObserveMode:
    """Tier 1 in observe mode should log but not create edges."""

    def test_observe_mode_does_not_create_edges(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            store = KnowledgeStore(tmpdir)
            curator = Curator(store, mode="observe")

            store.add_node(label="dev", kind="Device", properties={"host_id": "abc"})
            store.add_node(label="seg", kind="network_segment", properties={"host_id": "abc"})

            rules_path = os.path.join(str(store.data_dir), "inference-rules.yaml")
            with open(rules_path, "w") as f:
                f.write("inference_rules:\n  - match_property: host_id\n    from_kinds: [Device]\n    to_kinds: [network_segment]\n    relation: ON_SEGMENT\n")

            stats = curator.relationship_inference()
            # Should count the match but not create the edge
            assert stats["tier1_created"] >= 1

            edges = store._db.execute("SELECT count(*) FROM edges").fetchone()[0]
            assert edges == 0
