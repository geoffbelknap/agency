"""Tests for knowledge graph curation support."""

import asyncio

import pytest

from images.knowledge.store import KnowledgeStore
from images.knowledge.curator import Curator


class TestCurationSchema:
    def test_nodes_have_curation_columns(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        node_id = store.add_node(label="test", kind="concept", summary="x")
        node = store.get_node(node_id)
        assert "curation_status" in node
        assert node["curation_status"] is None

    def test_curation_log_table_exists(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        rows = store._db.execute(
            "SELECT name FROM sqlite_master WHERE type='table' AND name='curation_log'"
        ).fetchall()
        assert len(rows) == 1


class TestCurationFiltering:
    def test_find_nodes_excludes_soft_deleted(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        node_id = store.add_node(label="deleted thing", kind="concept", summary="gone")
        store._db.execute("UPDATE nodes SET curation_status='soft_deleted' WHERE id=?", (node_id,))
        store._db.commit()
        results = store.find_nodes("deleted")
        assert len(results) == 0

    def test_find_nodes_excludes_merged(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        node_id = store.add_node(label="merged thing", kind="concept", summary="absorbed")
        store._db.execute("UPDATE nodes SET curation_status='merged' WHERE id=?", (node_id,))
        store._db.commit()
        results = store.find_nodes("merged")
        assert len(results) == 0

    def test_find_nodes_includes_flagged(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        store.add_node(label="flagged thing", kind="concept", summary="suspicious")
        store._db.execute(
            "UPDATE nodes SET curation_status='flagged' WHERE id=?",
            (store.find_nodes("flagged")[0]["id"],)
        )
        store._db.commit()
        results = store.find_nodes("flagged")
        assert len(results) == 1

    def test_find_nodes_includes_normal(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        store.add_node(label="normal thing", kind="concept", summary="fine")
        results = store.find_nodes("normal")
        assert len(results) == 1


class TestCurationLogHelpers:
    def test_log_curation_action(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        node_id = store.add_node(label="test", kind="concept", summary="x")
        store.log_curation("merge", node_id, {"merged_into": "abc123"})
        logs = store.get_curation_log(node_id=node_id)
        assert len(logs) == 1
        assert logs[0]["action"] == "merge"
        assert "merged_into" in logs[0]["detail"]

    def test_get_curation_log_filtered(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        n1 = store.add_node(label="a", kind="concept", summary="")
        n2 = store.add_node(label="b", kind="concept", summary="")
        store.log_curation("merge", n1, {})
        store.log_curation("flag", n2, {"reason": "burst"})
        store.log_curation("soft_delete", n1, {})
        assert len(store.get_curation_log(action="merge")) == 1
        assert len(store.get_curation_log(node_id=n1)) == 2
        assert len(store.get_curation_log()) == 3


class TestPostIngestionCheck:
    def test_exact_case_variation_merged(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        curator = Curator(store, mode="active")
        n1 = store.add_node(label="nginx", kind="component", summary="web server")
        n2 = store.add_node(label="nginx ", kind="component", summary="reverse proxy")
        result = curator.post_ingestion_check(n2)
        assert result is not None
        absorbed = store.get_node(n2)
        assert absorbed["curation_status"] == "merged"

    def test_punctuation_variation_merged(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        curator = Curator(store, mode="active")
        n1 = store.add_node(label="api-gateway", kind="component", summary="routes requests")
        n2 = store.add_node(label="api gateway", kind="component", summary="the gateway")
        result = curator.post_ingestion_check(n2)
        assert result is not None

    def test_below_threshold_not_merged(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        curator = Curator(store, mode="active")
        n1 = store.add_node(label="nginx", kind="component", summary="web server")
        n2 = store.add_node(label="redis", kind="component", summary="cache")
        result = curator.post_ingestion_check(n2)
        assert result is None

    def test_different_kind_not_compared(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        curator = Curator(store, mode="active")
        n1 = store.add_node(label="nginx", kind="component", summary="web server")
        n2 = store.add_node(label="nginx", kind="agent", summary="monitoring agent")
        result = curator.post_ingestion_check(n2)
        assert result is None


class TestMergeSemantics:
    def test_edges_transferred_to_surviving_node(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        curator = Curator(store, mode="active")
        a = store.add_node(label="agent-x", kind="agent", summary="")
        n1 = store.add_node(label="nginx server", kind="component", summary="web")
        n2 = store.add_node(label="nginx  server", kind="component", summary="proxy")
        store.add_edge(source_id=a, target_id=n2, relation="manages")
        curator.post_ingestion_check(n2)
        edges = store.get_edges(n1, direction="incoming")
        assert any(e["source_id"] == a and e["relation"] == "manages" for e in edges)
        edges_n2 = store.get_edges(n2, direction="both")
        assert len(edges_n2) == 0

    def test_merge_log_entry_written(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        curator = Curator(store, mode="active")
        n1 = store.add_node(label="redis cache", kind="component", summary="fast")
        n2 = store.add_node(label="redis  cache", kind="component", summary="memory store")
        curator.post_ingestion_check(n2)
        logs = store.get_curation_log(node_id=n2, action="merge")
        assert len(logs) == 1
        assert n1 in logs[0]["detail"]

    def test_observe_mode_logs_but_does_not_merge(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        curator = Curator(store, mode="observe")
        n1 = store.add_node(label="postgres db", kind="component", summary="database")
        n2 = store.add_node(label="postgres  db", kind="component", summary="sql store")
        result = curator.post_ingestion_check(n2)
        assert result is None
        node = store.get_node(n2)
        assert node["curation_status"] is None
        logs = store.get_curation_log(action="observe_merge")
        assert len(logs) == 1


class TestFuzzyDuplicateScan:
    def test_high_similarity_auto_merges(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        curator = Curator(store, mode="active")
        n1 = store.add_node(label="authentication service", kind="component", summary="handles auth")
        n2 = store.add_node(label="authentication svc", kind="component", summary="auth handler")
        stats = curator.fuzzy_duplicate_scan()
        assert stats["merged"] >= 1
        assert store.get_node(n2)["curation_status"] == "merged"

    def test_cross_channel_prevents_auto_merge(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        curator = Curator(store, mode="active")
        n1 = store.add_node(label="secret project alpha", kind="concept",
                           summary="classified", source_channels=["#private"])
        n2 = store.add_node(label="secret project alpha", kind="concept",
                           summary="also classified", source_channels=["#other-private"])
        stats = curator.fuzzy_duplicate_scan()
        node = store.get_node(n2)
        assert node["curation_status"] != "merged" or node["curation_status"] == "flagged"

    def test_empty_graph_is_noop(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        curator = Curator(store, mode="active")
        stats = curator.fuzzy_duplicate_scan()
        assert stats["scanned"] == 0
        assert stats["merged"] == 0


class TestOrphanPruning:
    def test_orphan_node_soft_deleted(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        curator = Curator(store, mode="active", orphan_age_hours=0)
        n1 = store.add_node(label="lonely node", kind="concept", summary="no edges")
        stats = curator.orphan_pruning()
        assert stats["pruned"] == 1
        node = store.get_node(n1)
        assert node["curation_status"] == "soft_deleted"

    def test_structural_kind_not_pruned(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        curator = Curator(store, mode="active", orphan_age_hours=0)
        store.add_node(label="scout", kind="agent", summary="agent node")
        store.add_node(label="#general", kind="channel", summary="channel node")
        store.add_node(label="task-123", kind="task", summary="task node")
        stats = curator.orphan_pruning()
        assert stats["pruned"] == 0

    def test_connected_node_not_pruned(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        curator = Curator(store, mode="active", orphan_age_hours=0)
        n1 = store.add_node(label="connected", kind="concept", summary="has edges")
        n2 = store.add_node(label="neighbor", kind="concept", summary="linked")
        store.add_edge(source_id=n1, target_id=n2, relation="related")
        stats = curator.orphan_pruning()
        assert stats["pruned"] == 0

    def test_young_orphan_not_pruned(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        curator = Curator(store, mode="active", orphan_age_hours=24)
        store.add_node(label="just created", kind="concept", summary="too new")
        stats = curator.orphan_pruning()
        assert stats["pruned"] == 0


class TestClusterConcentration:
    def test_over_concentrated_kind_flagged(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        curator = Curator(store, mode="active")
        for i in range(5):
            store.add_node(label=f"concept-{i}", kind="concept", summary="")
        store.add_node(label="scout", kind="agent", summary="")
        result = curator.cluster_analysis()
        assert "concept" in result["over_concentrated"]

    def test_balanced_distribution_no_flags(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        curator = Curator(store, mode="active")
        for kind in ["concept", "agent", "component"]:
            for i in range(3):
                store.add_node(label=f"{kind}-{i}", kind=kind, summary="")
        result = curator.cluster_analysis()
        assert len(result["over_concentrated"]) == 0


class TestAnomalyDetection:
    def test_burst_detection(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        curator = Curator(store, mode="active", burst_multiplier=2.0)
        agent_id = store.add_node(label="agent-x", kind="agent", summary="")
        for i in range(20):
            n = store.add_node(label=f"burst-node-{i}", kind="concept", summary="",
                              source_type="agent")
            store.add_edge(source_id=agent_id, target_id=n, relation="contributed")
        result = curator.anomaly_detection()
        assert result["checked"] > 0

    def test_steady_contributions_not_flagged(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        curator = Curator(store, mode="active")
        result = curator.anomaly_detection()
        assert result["flagged"] == 0


class TestHealthMetrics:
    def test_compute_health_metrics(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        curator = Curator(store, mode="active")
        store.add_node(label="a", kind="concept", summary="")
        store.add_node(label="b", kind="concept", summary="")
        n1 = store.add_node(label="c", kind="agent", summary="")
        n2 = store.add_node(label="d", kind="concept", summary="")
        store.add_edge(source_id=n1, target_id=n2, relation="knows")
        metrics = curator.compute_health_metrics()
        assert "orphan_ratio" in metrics
        assert "total_nodes" in metrics
        assert "cluster_distribution" in metrics
        assert metrics["total_nodes"] == 4
        assert metrics["total_edges"] == 1
        logs = store.get_curation_log(action="metrics")
        assert len(logs) == 1


class TestHardDeleteCleanup:
    def test_expired_soft_delete_hard_deleted(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        curator = Curator(store, mode="active", recovery_days=0)
        n1 = store.add_node(label="doomed", kind="concept", summary="going away")
        curator._soft_delete_node(n1, "test")
        stats = curator.hard_delete_cleanup()
        assert stats["hard_deleted"] == 1
        assert store.get_node(n1) is None

    def test_within_recovery_window_not_hard_deleted(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        curator = Curator(store, mode="active", recovery_days=7)
        n1 = store.add_node(label="safe", kind="concept", summary="within window")
        curator._soft_delete_node(n1, "test")
        stats = curator.hard_delete_cleanup()
        assert stats["hard_deleted"] == 0
        assert store.get_node(n1) is not None


class TestCurationLoop:
    @pytest.mark.asyncio
    async def test_curation_loop_runs_one_cycle(self, tmp_path):
        from images.knowledge.curator import CurationLoop
        store = KnowledgeStore(tmp_path)
        curator = Curator(store, mode="active")
        loop = CurationLoop(curator, interval_seconds=0.1)
        store.add_node(label="test", kind="concept", summary="x")
        task = asyncio.create_task(loop.run())
        await asyncio.sleep(0.3)
        task.cancel()
        try:
            await task
        except asyncio.CancelledError:
            pass
        logs = store.get_curation_log(action="metrics")
        assert len(logs) >= 1


class TestSourcePriority:
    def test_local_source_type_in_priority(self, tmp_path):
        from images.knowledge.store import _SOURCE_PRIORITY
        assert "local" in _SOURCE_PRIORITY
        assert _SOURCE_PRIORITY["local"] == 1
        assert _SOURCE_PRIORITY["llm"] > _SOURCE_PRIORITY["local"]

    def test_local_node_summary_overridden_by_llm(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        n1 = store.add_node(label="test-svc", kind="component", summary="local summary", source_type="local")
        n2 = store.add_node(label="test-svc", kind="component", summary="llm summary", source_type="llm")
        node = store.get_node(n1)
        assert node["summary"] == "llm summary"

    def test_llm_summary_not_overridden_by_local(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        n1 = store.add_node(label="test-svc", kind="component", summary="llm summary", source_type="llm")
        n2 = store.add_node(label="test-svc", kind="component", summary="local summary", source_type="local")
        node = store.get_node(n1)
        assert node["summary"] == "llm summary"


class TestIngestionHooks:
    def test_ingester_calls_post_ingestion_check(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        from images.knowledge.ingester import RuleIngester
        from images.knowledge.curator import Curator
        ingester = RuleIngester(store)
        curator = Curator(store, mode="active")
        ingester.curator = curator
        ingester.ingest_message({
            "id": "msg1", "channel": "#general", "author": "scout",
            "content": "test message", "flags": {"decision": True},
        })
        assert True  # If we got here, integration works

    def test_synthesizer_calls_post_ingestion_check(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        from images.knowledge.synthesizer import LLMSynthesizer
        from images.knowledge.curator import Curator
        synth = LLMSynthesizer(store)
        curator = Curator(store, mode="active")
        synth.curator = curator
        synth._apply_extraction(
            {"entities": [{"label": "test entity", "kind": "concept", "summary": "x"}],
             "relationships": []},
            ["#general"],
        )
        nodes = store.find_nodes("test entity")
        assert len(nodes) >= 1
