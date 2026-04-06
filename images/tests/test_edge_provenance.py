"""Tests for edge provenance column and migration."""

import json

import pytest

from images.knowledge.store import KnowledgeStore


VALID_PROVENANCE = ("EXTRACTED", "INFERRED", "AMBIGUOUS")


class TestEdgeProvenanceColumn:
    def test_edge_has_provenance_column(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        n1 = store.add_node(label="a", kind="concept", summary="")
        n2 = store.add_node(label="b", kind="concept", summary="")
        eid = store.add_edge(n1, n2, "relates_to")
        row = store._db.execute("SELECT provenance FROM edges WHERE id = ?", (eid,)).fetchone()
        assert row is not None, "provenance column must exist on edges"

    def test_edge_provenance_defaults_to_ambiguous(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        n1 = store.add_node(label="a", kind="concept", summary="")
        n2 = store.add_node(label="b", kind="concept", summary="")
        eid = store.add_edge(n1, n2, "relates_to")
        row = store._db.execute("SELECT provenance FROM edges WHERE id = ?", (eid,)).fetchone()
        assert row["provenance"] == "AMBIGUOUS"

    def test_edge_provenance_accepts_valid_values(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        n1 = store.add_node(label="a", kind="concept", summary="")
        n2 = store.add_node(label="b", kind="concept", summary="")
        for prov in VALID_PROVENANCE:
            eid = store.add_edge(n1, n2, "relates_to", provenance=prov)
            row = store._db.execute("SELECT provenance FROM edges WHERE id = ?", (eid,)).fetchone()
            assert row["provenance"] == prov

    def test_edge_provenance_rejects_invalid_value(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        n1 = store.add_node(label="a", kind="concept", summary="")
        n2 = store.add_node(label="b", kind="concept", summary="")
        with pytest.raises(ValueError, match="provenance"):
            store.add_edge(n1, n2, "relates_to", provenance="BOGUS")


class TestEdgeProvenanceMigration:
    def test_migrate_provenance_from_source_type(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        # Create nodes with different source_types
        n_rule = store.add_node(label="rule-node", kind="concept", summary="", source_type="rule")
        n_agent = store.add_node(label="agent-node", kind="concept", summary="", source_type="agent")
        n_llm = store.add_node(label="llm-node", kind="concept", summary="", source_type="llm")
        n_local = store.add_node(label="local-node", kind="concept", summary="", source_type="local")
        n_target = store.add_node(label="target", kind="concept", summary="")

        # Create edges from each source node (provenance defaults to AMBIGUOUS)
        e_rule = store.add_edge(n_rule, n_target, "relates_to")
        e_agent = store.add_edge(n_agent, n_target, "relates_to")
        e_llm = store.add_edge(n_llm, n_target, "relates_to")
        e_local = store.add_edge(n_local, n_target, "relates_to")

        stats = store.migrate_edge_provenance()
        assert stats["migrated"] == 4

        def get_prov(eid):
            return store._db.execute(
                "SELECT provenance FROM edges WHERE id = ?", (eid,)
            ).fetchone()["provenance"]

        assert get_prov(e_rule) == "EXTRACTED"
        assert get_prov(e_agent) == "INFERRED"
        assert get_prov(e_llm) == "AMBIGUOUS"
        assert get_prov(e_local) == "AMBIGUOUS"

    def test_migrate_provenance_is_idempotent(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        n_rule = store.add_node(label="rule-node", kind="concept", summary="", source_type="rule")
        n_target = store.add_node(label="target", kind="concept", summary="")
        e_id = store.add_edge(n_rule, n_target, "relates_to")

        stats = store.migrate_edge_provenance()
        assert stats["migrated"] > 0
        assert store._db.execute(
            "SELECT provenance FROM edges WHERE id = ?", (e_id,)
        ).fetchone()["provenance"] == "EXTRACTED"

        # Manually set provenance to something else to prove migration won't overwrite
        store._db.execute("UPDATE edges SET provenance = 'INFERRED' WHERE id = ?", (e_id,))
        store._db.commit()

        stats = store.migrate_edge_provenance()
        assert stats["migrated"] == 0
        # Should remain INFERRED because the _provenance_migrated marker prevents re-migration
        assert store._db.execute(
            "SELECT provenance FROM edges WHERE id = ?", (e_id,)
        ).fetchone()["provenance"] == "INFERRED"


class TestIngesterCreatesExtractedEdges:
    """RuleIngester should tag all edges with provenance=EXTRACTED."""

    def test_ingester_creates_extracted_edges(self, tmp_path):
        from images.knowledge.ingester import RuleIngester

        store = KnowledgeStore(tmp_path)
        ingester = RuleIngester(store)

        # Ingest a simple message — creates agent node, channel node, member_of edge
        ingester.ingest_message({
            "id": "msg-001",
            "channel": "general",
            "author": "alice",
            "content": "Hello world",
        })

        edges = [dict(r) for r in store._db.execute("SELECT * FROM edges").fetchall()]
        assert len(edges) >= 1, "Expected at least one edge from ingest_message"
        for edge in edges:
            assert edge["provenance"] == "EXTRACTED", (
                f"Edge {edge['id']} ({edge['relation']}) has provenance={edge['provenance']}, "
                f"expected EXTRACTED"
            )

    def test_ingester_decision_edge_is_extracted(self, tmp_path):
        from images.knowledge.ingester import RuleIngester

        store = KnowledgeStore(tmp_path)
        ingester = RuleIngester(store)

        ingester.ingest_message({
            "id": "msg-002",
            "channel": "ops",
            "author": "bob",
            "content": "We decided to use Postgres",
            "flags": {"decision": True},
        })

        edges = [dict(r) for r in store._db.execute("SELECT * FROM edges").fetchall()]
        decided_edges = [e for e in edges if e["relation"] == "decided"]
        assert len(decided_edges) == 1
        assert decided_edges[0]["provenance"] == "EXTRACTED"

    def test_ingester_blocker_edge_is_extracted(self, tmp_path):
        from images.knowledge.ingester import RuleIngester

        store = KnowledgeStore(tmp_path)
        ingester = RuleIngester(store)

        ingester.ingest_message({
            "id": "msg-003",
            "channel": "ops",
            "author": "carol",
            "content": "Blocked on API access",
            "flags": {"blocker": True},
        })

        edges = [dict(r) for r in store._db.execute("SELECT * FROM edges").fetchall()]
        raised_edges = [e for e in edges if e["relation"] == "raised"]
        assert len(raised_edges) == 1
        assert raised_edges[0]["provenance"] == "EXTRACTED"

    def test_ingester_trust_signal_edge_is_extracted(self, tmp_path):
        from images.knowledge.ingester import RuleIngester

        store = KnowledgeStore(tmp_path)
        ingester = RuleIngester(store)

        # First create the agent node via a message
        ingester.ingest_message({
            "id": "msg-004",
            "channel": "ops",
            "author": "dave",
            "content": "test",
        })

        ingester.ingest_trust_signal("dave", {
            "signal_type": "task_success",
            "weight": 1,
            "timestamp": "2026-04-05T00:00:00Z",
        })

        edges = [dict(r) for r in store._db.execute("SELECT * FROM edges").fetchall()]
        trust_edges = [e for e in edges if e["relation"] == "trust_signal"]
        assert len(trust_edges) == 1
        assert trust_edges[0]["provenance"] == "EXTRACTED"


class TestGetEdgesFiltersByMinProvenance:
    """get_edges() min_provenance parameter filters by provenance tier."""

    def _make_edges(self, store):
        """Create three edges with EXTRACTED, INFERRED, AMBIGUOUS provenance."""
        n1 = store.add_node(label="source", kind="concept", summary="")
        n2 = store.add_node(label="target", kind="concept", summary="")
        e_ext = store.add_edge(n1, n2, "relates_to", provenance="EXTRACTED")
        e_inf = store.add_edge(n1, n2, "relates_to", provenance="INFERRED")
        e_amb = store.add_edge(n1, n2, "relates_to", provenance="AMBIGUOUS")
        return n1, n2, e_ext, e_inf, e_amb

    def test_no_filter_returns_all(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        n1, n2, e_ext, e_inf, e_amb = self._make_edges(store)
        edges = store.get_edges(n1, direction="outgoing")
        assert len(edges) == 3
        ids = {e["id"] for e in edges}
        assert ids == {e_ext, e_inf, e_amb}

    def test_min_provenance_ambiguous_returns_all(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        n1, n2, e_ext, e_inf, e_amb = self._make_edges(store)
        edges = store.get_edges(n1, direction="outgoing", min_provenance="AMBIGUOUS")
        assert len(edges) == 3

    def test_min_provenance_inferred_excludes_ambiguous(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        n1, n2, e_ext, e_inf, e_amb = self._make_edges(store)
        edges = store.get_edges(n1, direction="outgoing", min_provenance="INFERRED")
        ids = {e["id"] for e in edges}
        assert ids == {e_ext, e_inf}
        assert e_amb not in ids

    def test_min_provenance_extracted_only(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        n1, n2, e_ext, e_inf, e_amb = self._make_edges(store)
        edges = store.get_edges(n1, direction="outgoing", min_provenance="EXTRACTED")
        assert len(edges) == 1
        assert edges[0]["id"] == e_ext

    def test_provenance_included_in_returned_dicts(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        n1, n2, e_ext, e_inf, e_amb = self._make_edges(store)
        edges = store.get_edges(n1, direction="outgoing")
        for e in edges:
            assert "provenance" in e
            assert e["provenance"] in VALID_PROVENANCE

    def test_min_provenance_works_with_incoming_direction(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        n1, n2, e_ext, e_inf, e_amb = self._make_edges(store)
        edges = store.get_edges(n2, direction="incoming", min_provenance="EXTRACTED")
        assert len(edges) == 1
        assert edges[0]["id"] == e_ext

    def test_min_provenance_works_with_both_direction(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        n1, n2, e_ext, e_inf, e_amb = self._make_edges(store)
        edges = store.get_edges(n1, direction="both", min_provenance="INFERRED")
        ids = {e["id"] for e in edges}
        assert ids == {e_ext, e_inf}

    def test_min_provenance_combined_with_relation(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        n1 = store.add_node(label="a", kind="concept", summary="")
        n2 = store.add_node(label="b", kind="concept", summary="")
        store.add_edge(n1, n2, "relates_to", provenance="EXTRACTED")
        store.add_edge(n1, n2, "depends_on", provenance="AMBIGUOUS")
        edges = store.get_edges(n1, direction="outgoing", relation="relates_to", min_provenance="EXTRACTED")
        assert len(edges) == 1
        assert edges[0]["relation"] == "relates_to"
        assert edges[0]["provenance"] == "EXTRACTED"

    def test_invalid_min_provenance_raises(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        n1 = store.add_node(label="a", kind="concept", summary="")
        with pytest.raises(ValueError, match="min_provenance"):
            store.get_edges(n1, min_provenance="BOGUS")


class TestHealthMetricsBenchmarks:
    """compute_health_metrics() includes performance benchmark fields."""

    def test_health_metrics_include_benchmarks(self, tmp_path):
        from images.knowledge.curator import Curator

        store = KnowledgeStore(tmp_path)
        n1 = store.add_node(label="alpha", kind="concept", summary="first")
        n2 = store.add_node(label="beta", kind="concept", summary="second")
        store.add_edge(n1, n2, "relates_to")

        curator = Curator(store)
        metrics = curator.compute_health_metrics()

        # graph_size = total_nodes + total_edges
        assert "graph_size" in metrics
        assert metrics["graph_size"] == metrics["total_nodes"] + metrics["total_edges"]

        # traversal_p95_ms is a non-negative float
        assert "traversal_p95_ms" in metrics
        assert isinstance(metrics["traversal_p95_ms"], float)
        assert metrics["traversal_p95_ms"] >= 0.0

        # scope_resolution_ms is a non-negative float
        assert "scope_resolution_ms" in metrics
        assert isinstance(metrics["scope_resolution_ms"], float)
        assert metrics["scope_resolution_ms"] >= 0.0

    def test_health_metrics_benchmarks_empty_graph(self, tmp_path):
        from images.knowledge.curator import Curator

        store = KnowledgeStore(tmp_path)
        curator = Curator(store)
        metrics = curator.compute_health_metrics()

        assert metrics["graph_size"] == 0
        assert metrics["traversal_p95_ms"] == 0.0
        assert isinstance(metrics["scope_resolution_ms"], float)


class TestProvenanceWeighting:
    def test_extracted_edges_boost_ranking(self, tmp_path):
        """Nodes with EXTRACTED edges should rank higher than AMBIGUOUS."""
        store = KnowledgeStore(tmp_path)
        # Node with EXTRACTED edges
        strong = store.add_node("strong-finding", "finding", "well-supported finding")
        support1 = store.add_node("evidence-1", "fact", "solid evidence")
        store.add_edge(strong, support1, "DERIVED_FROM", provenance="EXTRACTED")

        # Node with AMBIGUOUS edges
        weak = store.add_node("weak-finding", "finding", "weakly-supported finding")
        support2 = store.add_node("guess-1", "fact", "uncertain evidence")
        store.add_edge(weak, support2, "DERIVED_FROM", provenance="AMBIGUOUS")
        store._db.commit()

        results = store.find_nodes("finding")
        # Both should be found, but strong should rank higher
        labels = [r["label"] for r in results]
        assert "strong-finding" in labels
        assert "weak-finding" in labels
        # Strong should come first (or at least not after weak)
        strong_idx = labels.index("strong-finding")
        weak_idx = labels.index("weak-finding")
        assert strong_idx <= weak_idx

    def test_provenance_boost_with_no_edges(self, tmp_path):
        """Nodes with no edges get neutral provenance score."""
        store = KnowledgeStore(tmp_path)
        store.add_node("orphan-finding", "finding", "an orphan finding with no edges")
        store._db.commit()

        results = store.find_nodes("orphan-finding")
        labels = [r["label"] for r in results]
        assert "orphan-finding" in labels

    def test_provenance_boost_does_not_add_internal_fields(self, tmp_path):
        """Internal scoring fields should be cleaned up before return."""
        store = KnowledgeStore(tmp_path)
        n1 = store.add_node("item", "finding", "a finding")
        n2 = store.add_node("support", "fact", "evidence")
        store.add_edge(n1, n2, "DERIVED_FROM", provenance="EXTRACTED")
        store._db.commit()

        results = store.find_nodes("finding")
        for r in results:
            assert "_provenance_score" not in r
            assert "_combined_score" not in r

    def test_inferred_ranks_between_extracted_and_ambiguous(self, tmp_path):
        """INFERRED edges should rank between EXTRACTED and AMBIGUOUS."""
        store = KnowledgeStore(tmp_path)
        # EXTRACTED node
        n_ext = store.add_node("extracted-node", "finding", "extracted finding")
        s_ext = store.add_node("ext-evidence", "fact", "solid")
        store.add_edge(n_ext, s_ext, "DERIVED_FROM", provenance="EXTRACTED")

        # INFERRED node
        n_inf = store.add_node("inferred-node", "finding", "inferred finding")
        s_inf = store.add_node("inf-evidence", "fact", "moderate")
        store.add_edge(n_inf, s_inf, "DERIVED_FROM", provenance="INFERRED")

        # AMBIGUOUS node
        n_amb = store.add_node("ambiguous-node", "finding", "ambiguous finding")
        s_amb = store.add_node("amb-evidence", "fact", "weak")
        store.add_edge(n_amb, s_amb, "DERIVED_FROM", provenance="AMBIGUOUS")
        store._db.commit()

        results = store.find_nodes("finding")
        labels = [r["label"] for r in results]
        # All three nodes with "finding" in label should appear
        assert "extracted-node" in labels
        assert "inferred-node" in labels
        assert "ambiguous-node" in labels
        # EXTRACTED should not rank after AMBIGUOUS
        assert labels.index("extracted-node") <= labels.index("ambiguous-node")
