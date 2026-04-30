"""Tests for the Scope authorization model."""

import json

import pytest

try:
    from services.knowledge.scope import Scope
except ImportError:
    from services.knowledge.scope import Scope


class TestScopeCreation:
    def test_defaults(self):
        s = Scope()
        assert s.channels == []
        assert s.principals == []
        assert s.classification is None

    def test_with_values(self):
        s = Scope(channels=["#ops"], principals=["alice"], classification="internal")
        assert s.channels == ["#ops"]
        assert s.principals == ["alice"]
        assert s.classification == "internal"


class TestToDict:
    def test_without_classification(self):
        s = Scope(channels=["b", "a"], principals=["z", "m"])
        d = s.to_dict()
        assert d == {"channels": ["a", "b"], "principals": ["m", "z"]}
        assert "classification" not in d

    def test_with_classification(self):
        s = Scope(channels=["c1"], classification="secret")
        d = s.to_dict()
        assert d["classification"] == "secret"

    def test_empty_scope(self):
        d = Scope().to_dict()
        assert d == {"channels": [], "principals": []}


class TestFromDict:
    def test_full_roundtrip(self):
        original = Scope(channels=["#sec", "#ops"], principals=["bob"], classification="internal")
        restored = Scope.from_dict(original.to_dict())
        assert restored.channels == sorted(original.channels)
        assert restored.principals == original.principals
        assert restored.classification == original.classification

    def test_json_roundtrip(self):
        original = Scope(channels=["z", "a"], principals=["p1"], classification="top")
        restored = Scope.from_dict(json.loads(json.dumps(original.to_dict())))
        assert restored.to_dict() == original.to_dict()

    def test_missing_fields(self):
        s = Scope.from_dict({})
        assert s.channels == []
        assert s.principals == []
        assert s.classification is None

    def test_partial_dict(self):
        s = Scope.from_dict({"channels": ["#dm"]})
        assert s.channels == ["#dm"]
        assert s.principals == []


class TestFromSourceChannels:
    def test_creates_scope_from_channels(self):
        s = Scope.from_source_channels(["#alerts", "#general"])
        assert s.channels == ["#alerts", "#general"]
        assert s.principals == []
        assert s.classification is None

    def test_copies_list(self):
        original = ["#a"]
        s = Scope.from_source_channels(original)
        original.append("#b")
        assert s.channels == ["#a"]


class TestOverlaps:
    def test_empty_overlaps_everything(self):
        empty = Scope()
        nonempty = Scope(channels=["#ops"], principals=["alice"])
        assert empty.overlaps(nonempty)
        assert nonempty.overlaps(empty)

    def test_both_empty_overlap(self):
        assert Scope().overlaps(Scope())

    def test_channel_overlap(self):
        a = Scope(channels=["#ops", "#sec"])
        b = Scope(channels=["#sec", "#dev"])
        assert a.overlaps(b)

    def test_principal_overlap(self):
        a = Scope(principals=["alice", "bob"])
        b = Scope(principals=["bob", "carol"])
        assert a.overlaps(b)

    def test_no_overlap(self):
        a = Scope(channels=["#ops"], principals=["alice"])
        b = Scope(channels=["#dev"], principals=["bob"])
        assert not a.overlaps(b)

    def test_channel_overlap_only(self):
        a = Scope(channels=["#ops"], principals=["alice"])
        b = Scope(channels=["#ops"], principals=["bob"])
        assert a.overlaps(b)

    def test_principal_overlap_only(self):
        a = Scope(channels=["#ops"], principals=["alice"])
        b = Scope(channels=["#dev"], principals=["alice"])
        assert a.overlaps(b)

    def test_empty_channels_nonempty_principals_vs_nonempty_channels(self):
        """One scope has only principals, other has only channels -- no overlap."""
        a = Scope(principals=["alice"])
        b = Scope(channels=["#ops"])
        assert not a.overlaps(b)


class TestIntersection:
    def test_partial_overlap(self):
        a = Scope(channels=["#ops", "#sec"], principals=["alice", "bob"])
        b = Scope(channels=["#sec", "#dev"], principals=["bob", "carol"])
        result = a.intersection(b)
        assert result.channels == ["#sec"]
        assert result.principals == ["bob"]

    def test_no_overlap(self):
        a = Scope(channels=["#ops"], principals=["alice"])
        b = Scope(channels=["#dev"], principals=["bob"])
        result = a.intersection(b)
        assert result.channels == []
        assert result.principals == []

    def test_result_is_sorted(self):
        a = Scope(channels=["z", "a", "m"], principals=["z", "a"])
        b = Scope(channels=["m", "z"], principals=["a", "z"])
        result = a.intersection(b)
        assert result.channels == ["m", "z"]
        assert result.principals == ["a", "z"]

    def test_classification_not_carried(self):
        a = Scope(channels=["#x"], classification="secret")
        b = Scope(channels=["#x"], classification="public")
        result = a.intersection(b)
        assert result.classification is None


class TestIsNarrowerThan:
    def test_subset(self):
        narrow = Scope(channels=["#ops"], principals=["alice"])
        wide = Scope(channels=["#ops", "#sec"], principals=["alice", "bob"])
        assert narrow.is_narrower_than(wide)

    def test_equal_scopes(self):
        a = Scope(channels=["#ops"], principals=["alice"])
        b = Scope(channels=["#ops"], principals=["alice"])
        assert a.is_narrower_than(b)

    def test_not_narrower(self):
        wide = Scope(channels=["#ops", "#sec"], principals=["alice", "bob"])
        narrow = Scope(channels=["#ops"], principals=["alice"])
        assert not wide.is_narrower_than(narrow)

    def test_empty_is_narrower_than_anything(self):
        assert Scope().is_narrower_than(Scope(channels=["#ops"]))

    def test_empty_is_narrower_than_empty(self):
        assert Scope().is_narrower_than(Scope())

    def test_partial_subset_fails(self):
        """Channels are subset but principals are not."""
        a = Scope(channels=["#ops"], principals=["alice", "bob"])
        b = Scope(channels=["#ops", "#sec"], principals=["alice"])
        assert not a.is_narrower_than(b)


# --- Store integration tests for scope column ---

from services.knowledge.store import KnowledgeStore


class TestNodeHasScopeColumn:
    def test_node_has_scope_column(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        node_id = store.add_node(label="test", kind="concept", summary="x")
        node = store.get_node(node_id)
        assert "scope" in node
        scope_data = json.loads(node["scope"])
        assert "channels" in scope_data
        assert "principals" in scope_data


class TestEdgeHasScopeColumn:
    def test_edge_has_scope_column(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        n1 = store.add_node(label="a", kind="concept")
        n2 = store.add_node(label="b", kind="concept")
        edge_id = store.add_edge(n1, n2, "related")
        edges = store.get_edges(n1, direction="outgoing")
        assert len(edges) == 1
        assert "scope" in edges[0]


class TestAddNodeWithScope:
    def test_add_node_with_scope(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        scope_dict = {"channels": ["#ops", "#sec"], "principals": ["alice"]}
        node_id = store.add_node(
            label="scoped fact",
            kind="concept",
            summary="visible to ops/sec",
            scope=scope_dict,
        )
        node = store.get_node(node_id)
        stored_scope = json.loads(node["scope"])
        assert sorted(stored_scope["channels"]) == ["#ops", "#sec"]
        assert stored_scope["principals"] == ["alice"]


class TestAddNodeScopeDefaultsFromSourceChannels:
    def test_auto_build_scope_from_source_channels(self, tmp_path):
        """When scope is not provided but source_channels is, scope is auto-built."""
        store = KnowledgeStore(tmp_path)
        node_id = store.add_node(
            label="channel fact",
            kind="concept",
            source_channels=["#alerts", "#general"],
        )
        node = store.get_node(node_id)
        stored_scope = json.loads(node["scope"])
        assert sorted(stored_scope["channels"]) == ["#alerts", "#general"]
        assert stored_scope["principals"] == []

    def test_explicit_scope_overrides_source_channels(self, tmp_path):
        """When scope is provided, source_channels does not affect scope."""
        store = KnowledgeStore(tmp_path)
        node_id = store.add_node(
            label="override fact",
            kind="concept",
            source_channels=["#alerts"],
            scope={"channels": ["#ops"], "principals": ["bob"]},
        )
        node = store.get_node(node_id)
        stored_scope = json.loads(node["scope"])
        assert stored_scope["channels"] == ["#ops"]
        assert stored_scope["principals"] == ["bob"]


class TestAddNodeScopeMergeOnDedup:
    def test_merge_unions_scope(self, tmp_path):
        """When deduplicating, scope channels and principals are unioned."""
        store = KnowledgeStore(tmp_path)
        store.add_node(
            label="shared",
            kind="concept",
            scope={"channels": ["#ops"], "principals": ["alice"]},
        )
        store.add_node(
            label="shared",
            kind="concept",
            scope={"channels": ["#sec"], "principals": ["bob"]},
        )
        nodes = store.find_nodes("shared")
        assert len(nodes) == 1
        stored_scope = json.loads(nodes[0]["scope"])
        assert sorted(stored_scope["channels"]) == ["#ops", "#sec"]
        assert sorted(stored_scope["principals"]) == ["alice", "bob"]


class TestFindNodesFiltersByPrincipalScope:
    def test_filters_by_principal_scope(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        store.add_node(
            label="ops alert",
            kind="concept",
            summary="ops visible",
            scope={"channels": ["#ops"], "principals": ["alice"]},
        )
        store.add_node(
            label="sec alert",
            kind="concept",
            summary="sec visible",
            scope={"channels": ["#sec"], "principals": ["bob"]},
        )
        # alice can see ops but not sec
        results = store.find_nodes("alert", principal={"channels": ["#ops"], "principals": ["alice"]})
        labels = [r["label"] for r in results]
        assert "ops alert" in labels
        assert "sec alert" not in labels

    def test_empty_scope_nodes_visible_to_everyone(self, tmp_path):
        """Nodes with empty scope are visible to any principal (backward compat)."""
        store = KnowledgeStore(tmp_path)
        store.add_node(label="public fact", kind="concept", summary="no scope")
        results = store.find_nodes("public", principal={"channels": ["#ops"], "principals": ["alice"]})
        assert len(results) == 1
        assert results[0]["label"] == "public fact"

    def test_structural_nodes_always_visible(self, tmp_path):
        """Structural nodes (agent, channel, task) bypass scope filtering."""
        store = KnowledgeStore(tmp_path)
        store.add_node(
            label="my-agent",
            kind="agent",
            scope={"channels": ["#ops"], "principals": ["alice"]},
        )
        results = store.find_nodes("my-agent", principal={"channels": ["#sec"], "principals": ["bob"]})
        assert len(results) == 1
        assert results[0]["label"] == "my-agent"


class TestMigrateScopeFromSourceChannels:
    def test_migrate_populates_scope(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        # Insert a node with source_channels but empty scope (simulate legacy)
        node_id = store.add_node(
            label="legacy node",
            kind="concept",
            source_channels=["#alerts", "#ops"],
        )
        # Manually clear the scope to simulate pre-migration state
        store._db.execute("UPDATE nodes SET scope = '{}' WHERE id = ?", (node_id,))
        store._db.commit()

        result = store.migrate_node_scopes()
        assert result["migrated"] == 1

        node = store.get_node(node_id)
        stored_scope = json.loads(node["scope"])
        assert sorted(stored_scope["channels"]) == ["#alerts", "#ops"]
        assert stored_scope["principals"] == []

    def test_skips_nodes_without_source_channels(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        store.add_node(label="no channels", kind="concept")
        # Clear scope
        store._db.execute("UPDATE nodes SET scope = '{}' WHERE id IS NOT NULL")
        store._db.commit()

        result = store.migrate_node_scopes()
        assert result["migrated"] == 0


class TestMigrateScopeIsIdempotent:
    def test_second_run_does_not_duplicate(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        node_id = store.add_node(
            label="legacy node",
            kind="concept",
            source_channels=["#alerts"],
        )
        # Clear scope to simulate pre-migration
        store._db.execute("UPDATE nodes SET scope = '{}' WHERE id = ?", (node_id,))
        store._db.commit()

        result1 = store.migrate_node_scopes()
        assert result1["migrated"] == 1

        result2 = store.migrate_node_scopes()
        assert result2["migrated"] == 0

        # Verify scope is still correct after second run
        node = store.get_node(node_id)
        stored_scope = json.loads(node["scope"])
        assert stored_scope["channels"] == ["#alerts"]


class TestGetSubgraphScopeEnforcement:
    def test_subgraph_respects_scope(self, tmp_path):
        """get_subgraph with principal should not cross scope boundaries."""
        store = KnowledgeStore(tmp_path)
        # In-scope cluster
        a = store.add_node("a-node", "fact", "in scope", scope={"principals": ["agent:x"]})
        b = store.add_node("b-node", "fact", "in scope", scope={"principals": ["agent:x"]})
        store.add_edge(a, b, "relates_to", provenance="EXTRACTED")
        # Out-of-scope node connected to b
        c = store.add_node("c-node", "fact", "out of scope", scope={"principals": ["agent:y"]})
        store.add_edge(b, c, "relates_to", provenance="EXTRACTED")
        # Further node behind c
        d = store.add_node("d-node", "fact", "also out", scope={"principals": ["agent:y"]})
        store.add_edge(c, d, "relates_to", provenance="EXTRACTED")
        store._db.commit()

        result = store.get_subgraph(a, max_hops=3, principal={"principals": ["agent:x"]})
        node_ids = {n["id"] for n in result["nodes"]}
        assert a in node_ids  # starting node
        assert b in node_ids  # in scope
        assert c not in node_ids  # out of scope — boundary
        assert d not in node_ids  # behind boundary

    def test_subgraph_without_principal_returns_all(self, tmp_path):
        """Without principal param, all nodes reachable."""
        store = KnowledgeStore(tmp_path)
        a = store.add_node("a", "fact", "node a", scope={"principals": ["agent:x"]})
        b = store.add_node("b", "fact", "node b", scope={"principals": ["agent:y"]})
        store.add_edge(a, b, "relates_to", provenance="EXTRACTED")
        store._db.commit()

        result = store.get_subgraph(a, max_hops=1)
        assert len(result["nodes"]) == 2

    def test_subgraph_structural_nodes_always_visible(self, tmp_path):
        """Structural nodes (agent, channel) visible regardless of scope."""
        store = KnowledgeStore(tmp_path)
        a = store.add_node("a", "fact", "in scope", scope={"principals": ["agent:x"]})
        agent_node = store.add_node("some-agent", "agent", "an agent", scope={"principals": ["agent:y"]})
        store.add_edge(a, agent_node, "member_of", provenance="EXTRACTED")
        store._db.commit()

        result = store.get_subgraph(a, max_hops=1, principal={"principals": ["agent:x"]})
        labels = {n["label"] for n in result["nodes"]}
        assert "some-agent" in labels  # structural, always visible


class TestGetNeighborsScopeEnforcement:
    def test_neighbors_filtered_by_scope(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        center = store.add_node("center", "fact", "hub", scope={"principals": ["agent:x"]})
        visible = store.add_node("visible", "fact", "in scope", scope={"principals": ["agent:x"]})
        hidden = store.add_node("hidden", "fact", "out of scope", scope={"principals": ["agent:y"]})
        store.add_edge(center, visible, "relates_to", provenance="EXTRACTED")
        store.add_edge(center, hidden, "relates_to", provenance="EXTRACTED")
        store._db.commit()

        result = store.get_neighbors(center, principal={"principals": ["agent:x"]})
        labels = {n["label"] for n in result["neighbors"]}
        assert "visible" in labels
        assert "hidden" not in labels

    def test_neighbors_subgraph_filtered(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        center = store.add_node("center", "fact", "hub", scope={"principals": ["agent:x"]})
        visible = store.add_node("vis", "fact", "ok", scope={"principals": ["agent:x"]})
        hidden = store.add_node("hid", "fact", "nope", scope={"principals": ["agent:y"]})
        store.add_edge(center, visible, "relates_to", provenance="EXTRACTED")
        store.add_edge(center, hidden, "relates_to", provenance="EXTRACTED")
        store._db.commit()

        result = store.get_neighbors_subgraph(center, principal={"principals": ["agent:x"]})
        labels = {n["label"] for n in result["nodes"]}
        assert "vis" in labels
        assert "hid" not in labels


class TestFindPathScopeEnforcement:
    def test_path_blocked_by_scope_boundary(self, tmp_path):
        """If the only path crosses a scope boundary, return None."""
        store = KnowledgeStore(tmp_path)
        a = store.add_node("start", "fact", "A", scope={"principals": ["agent:x"]})
        b = store.add_node("middle", "fact", "B", scope={"principals": ["agent:y"]})  # out of scope
        c = store.add_node("end", "fact", "C", scope={"principals": ["agent:x"]})
        store.add_edge(a, b, "relates_to", provenance="EXTRACTED")
        store.add_edge(b, c, "relates_to", provenance="EXTRACTED")
        store._db.commit()

        result = store.find_path("start", "end", principal={"principals": ["agent:x"]})
        assert result is None  # no in-scope path

    def test_path_uses_in_scope_route(self, tmp_path):
        """Path finding should use in-scope nodes even if longer."""
        store = KnowledgeStore(tmp_path)
        a = store.add_node("start", "fact", "A", scope={"principals": ["agent:x"]})
        shortcut = store.add_node("shortcut", "fact", "X", scope={"principals": ["agent:y"]})
        via1 = store.add_node("via1", "fact", "V1", scope={"principals": ["agent:x"]})
        via2 = store.add_node("via2", "fact", "V2", scope={"principals": ["agent:x"]})
        end = store.add_node("end", "fact", "E", scope={"principals": ["agent:x"]})
        # Short path: a -> shortcut -> end (crosses scope)
        store.add_edge(a, shortcut, "relates_to", provenance="EXTRACTED")
        store.add_edge(shortcut, end, "relates_to", provenance="EXTRACTED")
        # Long path: a -> via1 -> via2 -> end (all in scope)
        store.add_edge(a, via1, "relates_to", provenance="EXTRACTED")
        store.add_edge(via1, via2, "relates_to", provenance="EXTRACTED")
        store.add_edge(via2, end, "relates_to", provenance="EXTRACTED")
        store._db.commit()

        result = store.find_path("start", "end", principal={"principals": ["agent:x"]})
        assert result is not None
        path_labels = [n["label"] for n in result["nodes"]]
        assert "shortcut" not in path_labels
        assert "via1" in path_labels

    def test_path_without_principal_crosses_all(self, tmp_path):
        """Without principal, path crosses scope boundaries."""
        store = KnowledgeStore(tmp_path)
        a = store.add_node("start", "fact", "A", scope={"principals": ["agent:x"]})
        b = store.add_node("middle", "fact", "B", scope={"principals": ["agent:y"]})
        c = store.add_node("end", "fact", "C", scope={"principals": ["agent:x"]})
        store.add_edge(a, b, "relates_to", provenance="EXTRACTED")
        store.add_edge(b, c, "relates_to", provenance="EXTRACTED")
        store._db.commit()

        result = store.find_path("start", "end")
        assert result is not None


class TestEdgeScopeInheritance:
    def test_edge_scope_narrower_than_source_accepted(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        src = store.add_node("source", "fact", "wide scope", scope={"channels": ["a", "b"], "principals": ["op:1"]})
        tgt = store.add_node("target", "fact", "target")
        # Edge scope is a subset — should be accepted
        eid = store.add_edge(src, tgt, "relates_to", scope={"channels": ["a"]})
        assert eid is not None

    def test_edge_scope_wider_than_source_rejected(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        src = store.add_node("source", "fact", "narrow scope", scope={"channels": ["a"]})
        tgt = store.add_node("target", "fact", "target")
        with pytest.raises(ValueError, match="wider"):
            store.add_edge(src, tgt, "relates_to", scope={"channels": ["a", "b", "c"]})

    def test_edge_no_explicit_scope_accepted(self, tmp_path):
        """Edges without explicit scope are always accepted."""
        store = KnowledgeStore(tmp_path)
        src = store.add_node("source", "fact", "scope", scope={"channels": ["a"]})
        tgt = store.add_node("target", "fact", "target")
        eid = store.add_edge(src, tgt, "relates_to")
        assert eid is not None
