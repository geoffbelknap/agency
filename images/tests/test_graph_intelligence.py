"""Tests for graph intelligence columns and methods (community detection, hub scores)."""

import os
import sys

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "knowledge"))

from images.knowledge.store import KnowledgeStore


class TestCommunityColumns:
    def test_community_id_column_exists(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        node_id = store.add_node(label="test", kind="concept", summary="")
        node = store.get_node(node_id)
        assert "community_id" in node
        assert node["community_id"] is None

    def test_community_cohesion_column_exists(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        node_id = store.add_node(label="test", kind="concept", summary="")
        node = store.get_node(node_id)
        assert "community_cohesion" in node
        assert node["community_cohesion"] is None


class TestHubColumns:
    def test_hub_score_column_exists(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        node_id = store.add_node(label="test", kind="concept", summary="")
        node = store.get_node(node_id)
        assert "hub_score" in node
        assert node["hub_score"] is None

    def test_hub_type_column_exists(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        node_id = store.add_node(label="test", kind="concept", summary="")
        node = store.get_node(node_id)
        assert "hub_type" in node
        assert node["hub_type"] is None


class TestUpdateCommunity:
    def test_update_community_sets_values(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        node_id = store.add_node(label="node-a", kind="concept", summary="A")
        store.update_community(node_id, community_id="comm-1", cohesion=0.85)
        node = store.get_node(node_id)
        assert node["community_id"] == "comm-1"
        assert node["community_cohesion"] == 0.85

    def test_update_community_overwrites(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        node_id = store.add_node(label="node-a", kind="concept", summary="A")
        store.update_community(node_id, community_id="comm-1", cohesion=0.85)
        store.update_community(node_id, community_id="comm-2", cohesion=0.92)
        node = store.get_node(node_id)
        assert node["community_id"] == "comm-2"
        assert node["community_cohesion"] == 0.92


class TestUpdateHub:
    def test_update_hub_sets_values(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        node_id = store.add_node(label="hub-node", kind="concept", summary="Central")
        store.update_hub(node_id, hub_score=12.5, hub_type="bridge")
        node = store.get_node(node_id)
        assert node["hub_score"] == 12.5
        assert node["hub_type"] == "bridge"

    def test_update_hub_overwrites(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        node_id = store.add_node(label="hub-node", kind="concept", summary="Central")
        store.update_hub(node_id, hub_score=12.5, hub_type="bridge")
        store.update_hub(node_id, hub_score=25.0, hub_type="authority")
        node = store.get_node(node_id)
        assert node["hub_score"] == 25.0
        assert node["hub_type"] == "authority"


class TestClearCommunities:
    def test_clear_communities_resets_all(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        n1 = store.add_node(label="a", kind="concept", summary="")
        n2 = store.add_node(label="b", kind="concept", summary="")
        store.update_community(n1, community_id="comm-1", cohesion=0.8)
        store.update_community(n2, community_id="comm-2", cohesion=0.9)
        store.clear_communities()
        assert store.get_node(n1)["community_id"] is None
        assert store.get_node(n1)["community_cohesion"] is None
        assert store.get_node(n2)["community_id"] is None
        assert store.get_node(n2)["community_cohesion"] is None


class TestClearHubs:
    def test_clear_hubs_resets_all(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        n1 = store.add_node(label="a", kind="concept", summary="")
        n2 = store.add_node(label="b", kind="concept", summary="")
        store.update_hub(n1, hub_score=10.0, hub_type="bridge")
        store.update_hub(n2, hub_score=20.0, hub_type="authority")
        store.clear_hubs()
        assert store.get_node(n1)["hub_score"] is None
        assert store.get_node(n1)["hub_type"] is None
        assert store.get_node(n2)["hub_score"] is None
        assert store.get_node(n2)["hub_type"] is None


class TestGetCommunityMembers:
    def test_returns_correct_members(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        n1 = store.add_node(label="a", kind="concept", summary="")
        n2 = store.add_node(label="b", kind="concept", summary="")
        n3 = store.add_node(label="c", kind="concept", summary="")
        store.update_community(n1, community_id="comm-1", cohesion=0.8)
        store.update_community(n2, community_id="comm-1", cohesion=0.9)
        store.update_community(n3, community_id="comm-2", cohesion=0.7)
        members = store.get_community_members("comm-1")
        assert len(members) == 2
        labels = {m["label"] for m in members}
        assert labels == {"a", "b"}

    def test_excludes_curated_nodes(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        n1 = store.add_node(label="active", kind="concept", summary="")
        n2 = store.add_node(label="merged", kind="concept", summary="")
        store.update_community(n1, community_id="comm-1", cohesion=0.8)
        store.update_community(n2, community_id="comm-1", cohesion=0.9)
        # Mark n2 as merged (not active)
        store._db.execute(
            "UPDATE nodes SET curation_status = 'merged' WHERE id = ?", (n2,)
        )
        store._db.commit()
        members = store.get_community_members("comm-1")
        assert len(members) == 1
        assert members[0]["label"] == "active"

    def test_respects_limit(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        for i in range(5):
            nid = store.add_node(label=f"n{i}", kind="concept", summary="")
            store.update_community(nid, community_id="comm-1", cohesion=0.8)
        members = store.get_community_members("comm-1", limit=3)
        assert len(members) == 3


class TestGetHubs:
    def test_returns_ordered_by_score(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        n1 = store.add_node(label="low", kind="concept", summary="")
        n2 = store.add_node(label="high", kind="concept", summary="")
        n3 = store.add_node(label="mid", kind="concept", summary="")
        n4 = store.add_node(label="none", kind="concept", summary="")
        store.update_hub(n1, hub_score=5.0, hub_type="bridge")
        store.update_hub(n2, hub_score=25.0, hub_type="authority")
        store.update_hub(n3, hub_score=15.0, hub_type="bridge")
        # n4 has no hub_score — should not appear
        hubs = store.get_hubs()
        assert len(hubs) == 3
        assert hubs[0]["label"] == "high"
        assert hubs[1]["label"] == "mid"
        assert hubs[2]["label"] == "low"

    def test_respects_limit(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        for i in range(5):
            nid = store.add_node(label=f"hub{i}", kind="concept", summary="")
            store.update_hub(nid, hub_score=float(i), hub_type="bridge")
        hubs = store.get_hubs(limit=2)
        assert len(hubs) == 2
        # Highest scores first
        assert hubs[0]["hub_score"] == 4.0
        assert hubs[1]["hub_score"] == 3.0
