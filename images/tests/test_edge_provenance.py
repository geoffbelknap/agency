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

        store.migrate_edge_provenance()

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

        store.migrate_edge_provenance()
        assert store._db.execute(
            "SELECT provenance FROM edges WHERE id = ?", (e_id,)
        ).fetchone()["provenance"] == "EXTRACTED"

        # Manually set provenance to something else to prove migration won't overwrite
        store._db.execute("UPDATE edges SET provenance = 'INFERRED' WHERE id = ?", (e_id,))
        store._db.commit()

        store.migrate_edge_provenance()
        # Should remain INFERRED because the _provenance_migrated marker prevents re-migration
        assert store._db.execute(
            "SELECT provenance FROM edges WHERE id = ?", (e_id,)
        ).fetchone()["provenance"] == "INFERRED"
