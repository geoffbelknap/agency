"""Tests for graph intelligence columns and methods (community detection, hub scores)."""

import json
import os
import sys

import pytest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "knowledge"))

from images.knowledge.store import KnowledgeStore

try:
    import networkx
    _HAS_NETWORKX = True
except ImportError:
    _HAS_NETWORKX = False


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


# ---------------------------------------------------------------------------
# CommunityDetector tests
# ---------------------------------------------------------------------------

if _HAS_NETWORKX:
    from graph_intelligence import CommunityDetector


def _build_two_cluster_store(tmp_path):
    """Create a store with two clear 3-node clusters, weakly connected."""
    store = KnowledgeStore(tmp_path)
    # Cluster A: a1, a2, a3 (dense)
    a1 = store.add_node(label="a1", kind="concept", summary="A1")
    a2 = store.add_node(label="a2", kind="concept", summary="A2")
    a3 = store.add_node(label="a3", kind="concept", summary="A3")
    store.add_edge(a1, a2, "related", provenance="EXTRACTED")
    store.add_edge(a1, a3, "related", provenance="EXTRACTED")
    store.add_edge(a2, a3, "related", provenance="EXTRACTED")

    # Cluster B: b1, b2, b3 (dense)
    b1 = store.add_node(label="b1", kind="concept", summary="B1")
    b2 = store.add_node(label="b2", kind="concept", summary="B2")
    b3 = store.add_node(label="b3", kind="concept", summary="B3")
    store.add_edge(b1, b2, "related", provenance="INFERRED")
    store.add_edge(b1, b3, "related", provenance="INFERRED")
    store.add_edge(b2, b3, "related", provenance="INFERRED")

    # Weak bridge between clusters
    store.add_edge(a3, b1, "weak_link", provenance="EXTRACTED")
    return store


@pytest.mark.skipif(not _HAS_NETWORKX, reason="networkx not installed")
class TestCommunityDetector:
    def test_detects_two_communities(self, tmp_path):
        store = _build_two_cluster_store(tmp_path)
        detector = CommunityDetector(store)
        stats = detector.detect()
        assert stats["communities_found"] >= 2

    def test_community_nodes_created(self, tmp_path):
        store = _build_two_cluster_store(tmp_path)
        detector = CommunityDetector(store)
        detector.detect()
        communities = store.list_communities()
        assert len(communities) >= 2
        for c in communities:
            assert c["kind"] == "Community"

    def test_nodes_assigned_community_id(self, tmp_path):
        store = _build_two_cluster_store(tmp_path)
        detector = CommunityDetector(store)
        detector.detect()
        # Check that concept nodes got community_id assigned
        rows = store._db.execute(
            "SELECT * FROM nodes WHERE kind = 'concept' AND community_id IS NOT NULL"
        ).fetchall()
        assert len(rows) == 6  # all 6 concept nodes

    def test_cohesion_between_0_and_1(self, tmp_path):
        store = _build_two_cluster_store(tmp_path)
        detector = CommunityDetector(store)
        detector.detect()
        communities = store.list_communities()
        for c in communities:
            props = json.loads(c["properties"]) if isinstance(c["properties"], str) else c["properties"]
            assert 0.0 <= props["cohesion"] <= 1.0

    def test_ambiguous_edges_excluded(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        n1 = store.add_node(label="x1", kind="concept", summary="X1")
        n2 = store.add_node(label="x2", kind="concept", summary="X2")
        # Only AMBIGUOUS edges — should not appear in NetworkX graph
        store.add_edge(n1, n2, "related", provenance="AMBIGUOUS")
        detector = CommunityDetector(store)
        G = detector._build_graph()
        assert G.number_of_edges() == 0

    def test_structural_nodes_excluded(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        store.add_node(label="my-agent", kind="agent", summary="An agent")
        store.add_node(label="my-channel", kind="channel", summary="A channel")
        store.add_node(label="my-task", kind="task", summary="A task")
        store.add_node(label="real-concept", kind="concept", summary="Real")
        detector = CommunityDetector(store)
        G = detector._build_graph()
        assert len(G.nodes()) == 1  # only real-concept

    def test_oversized_community_gets_split(self, tmp_path):
        """A single large clique that exceeds max_community_fraction should be recursively split."""
        store = KnowledgeStore(tmp_path)
        # Create 20 fully-connected nodes — a single community that is 100% of graph
        nodes = []
        for i in range(20):
            nid = store.add_node(label=f"n{i}", kind="concept", summary=f"Node {i}")
            nodes.append(nid)
        for i in range(len(nodes)):
            for j in range(i + 1, len(nodes)):
                store.add_edge(nodes[i], nodes[j], "related", provenance="EXTRACTED")
        detector = CommunityDetector(store, max_community_fraction=0.25, resolution=1.0)
        stats = detector.detect()
        # With recursive splitting, we should get more than 1 community
        assert stats["communities_found"] >= 2

    def test_empty_graph_returns_zero(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        detector = CommunityDetector(store)
        stats = detector.detect()
        assert stats["communities_found"] == 0
        assert stats["nodes_assigned"] == 0


# ---------------------------------------------------------------------------
# HubDetector tests
# ---------------------------------------------------------------------------

if _HAS_NETWORKX:
    from graph_intelligence import HubDetector


@pytest.mark.skipif(not _HAS_NETWORKX, reason="networkx not installed")
class TestHubDetectorBasic:
    """Hub detected for a node connected to many others."""

    def test_hub_detected(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        hub_id = store.add_node(label="central-concept", kind="concept", summary="The hub")
        for i in range(8):
            nid = store.add_node(label=f"spoke-{i}", kind="concept", summary=f"Spoke {i}")
            store.add_edge(hub_id, nid, relation="related_to")

        detector = HubDetector(store)
        result = detector.detect()

        assert result["hubs_found"] >= 1
        hubs = store.get_hubs()
        hub_labels = {h["label"] for h in hubs}
        assert "central-concept" in hub_labels

    def test_hub_score_and_type(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        hub_id = store.add_node(label="central-concept", kind="concept", summary="The hub")
        for i in range(8):
            nid = store.add_node(label=f"spoke-{i}", kind="concept", summary=f"Spoke {i}")
            store.add_edge(hub_id, nid, relation="related_to")

        detector = HubDetector(store)
        detector.detect()

        hubs = store.get_hubs()
        top = [h for h in hubs if h["label"] == "central-concept"][0]
        assert top["hub_score"] > 0
        assert top["hub_type"] == "hub"


@pytest.mark.skipif(not _HAS_NETWORKX, reason="networkx not installed")
class TestHubDetectorStructuralExclusion:
    """Structural kinds must NOT be flagged as hubs."""

    def test_structural_nodes_excluded(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        # Create an agent node with many connections — should be excluded
        agent_id = store.add_node(label="my-agent", kind="agent", summary="An agent")
        for i in range(10):
            nid = store.add_node(label=f"node-{i}", kind="concept", summary=f"Node {i}")
            store.add_edge(agent_id, nid, relation="uses")

        detector = HubDetector(store)
        detector.detect()

        hubs = store.get_hubs()
        hub_labels = {h["label"] for h in hubs}
        assert "my-agent" not in hub_labels


@pytest.mark.skipif(not _HAS_NETWORKX, reason="networkx not installed")
class TestHubDetectorBridge:
    """Bridge detection: node connecting separate clusters."""

    def test_bridge_detected(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        # Cluster A: fully connected
        a_nodes = []
        for i in range(4):
            nid = store.add_node(label=f"cluster-a-{i}", kind="concept", summary=f"A{i}")
            a_nodes.append(nid)
        for i in range(len(a_nodes)):
            for j in range(i + 1, len(a_nodes)):
                store.add_edge(a_nodes[i], a_nodes[j], relation="related_to")

        # Cluster B: fully connected
        b_nodes = []
        for i in range(4):
            nid = store.add_node(label=f"cluster-b-{i}", kind="concept", summary=f"B{i}")
            b_nodes.append(nid)
        for i in range(len(b_nodes)):
            for j in range(i + 1, len(b_nodes)):
                store.add_edge(b_nodes[i], b_nodes[j], relation="related_to")

        # Bridge node connecting both clusters
        bridge_id = store.add_node(label="the-bridge", kind="concept", summary="Bridges A and B")
        store.add_edge(bridge_id, a_nodes[0], relation="related_to")
        store.add_edge(bridge_id, b_nodes[0], relation="related_to")

        # Assign different communities so bridge detection can see cross-community neighbors
        for nid in a_nodes:
            store.update_community(nid, community_id="comm-a", cohesion=0.9)
        for nid in b_nodes:
            store.update_community(nid, community_id="comm-b", cohesion=0.9)

        detector = HubDetector(store)
        result = detector.detect()

        assert result["bridges_found"] >= 1
        hubs = store.get_hubs()
        bridges = [h for h in hubs if h["hub_type"] == "bridge"]
        bridge_labels = {b["label"] for b in bridges}
        assert "the-bridge" in bridge_labels


@pytest.mark.skipif(not _HAS_NETWORKX, reason="networkx not installed")
class TestHubDetectorEmptyGraph:
    """Empty graph returns zeros."""

    def test_empty_graph(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        detector = HubDetector(store)
        result = detector.detect()
        assert result["hubs_found"] == 0
        assert result["bridges_found"] == 0


# ---------------------------------------------------------------------------
# Curator integration tests
# ---------------------------------------------------------------------------

if _HAS_NETWORKX:
    from images.knowledge.curator import Curator


@pytest.mark.skipif(not _HAS_NETWORKX, reason="networkx not installed")
class TestCuratorCommunityDetection:
    """Community detection callable from curator."""

    def test_community_detection_callable_from_curator(self, tmp_path):
        store = _build_two_cluster_store(tmp_path)
        curator = Curator(store, mode="active")
        stats = curator.community_detection()
        assert "communities_found" in stats
        assert "nodes_assigned" in stats
        assert stats["communities_found"] >= 2

    def test_hub_detection_callable_from_curator(self, tmp_path):
        store = _build_two_cluster_store(tmp_path)
        curator = Curator(store, mode="active")
        # Run community detection first so hub/bridge detection has community_id data
        curator.community_detection()
        stats = curator.hub_detection()
        assert "hubs_found" in stats
        assert "bridges_found" in stats

    def test_community_detection_ms_in_metrics(self, tmp_path):
        store = _build_two_cluster_store(tmp_path)
        curator = Curator(store, mode="active")
        curator.community_detection()
        metrics = curator.compute_health_metrics()
        assert "community_detection_ms" in metrics
        assert isinstance(metrics["community_detection_ms"], float)
        assert metrics["community_detection_ms"] >= 0.0
