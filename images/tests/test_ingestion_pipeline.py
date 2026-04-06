"""Tests for the IngestionPipeline orchestrator."""

from __future__ import annotations

from unittest.mock import MagicMock

import pytest

from knowledge.ingestion.pipeline import IngestionPipeline


# ---------------------------------------------------------------------------
# Fixtures
# ---------------------------------------------------------------------------

@pytest.fixture()
def mock_store():
    """Return a MagicMock with the KnowledgeStore interface."""
    store = MagicMock()
    store.add_node.return_value = "node-id-1"
    # find_nodes returns a list of dicts; default: match found.
    store.find_nodes.return_value = [{"id": "node-id-1", "label": "x"}]
    return store


@pytest.fixture()
def pipeline(mock_store):
    return IngestionPipeline(store=mock_store)


@pytest.fixture()
def pipeline_with_synthesizer(mock_store):
    synth = MagicMock()
    return IngestionPipeline(store=mock_store, synthesizer=synth), synth


# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------

class TestIngestConfigFile:
    """Ingest a YAML config file -- nodes created, synthesis skipped."""

    def test_nodes_created(self, pipeline, mock_store):
        content = "server:\n  host: localhost\n  port: 8080\n"
        stats = pipeline.ingest(content, filename="config.yaml")

        assert stats["nodes_created"] > 0
        assert mock_store.add_node.called
        assert stats["source_type"] == "config"
        assert stats["extractor"] == "config"

    def test_synthesis_skipped(self, pipeline):
        content = "key: value\n"
        stats = pipeline.ingest(content, filename="settings.yaml")

        assert stats["synthesis_triggered"] is False
        assert stats["synthesis_skipped"] is True


class TestIngestMarkdown:
    """Ingest a markdown file -- heading nodes created."""

    def test_heading_nodes(self, pipeline, mock_store):
        content = "# Top\n\nSome text.\n\n## Sub\n"
        stats = pipeline.ingest(content, filename="doc.md")

        assert stats["nodes_created"] >= 2
        assert stats["source_type"] == "markdown"
        assert stats["extractor"] == "markdown"

    def test_edges_created(self, pipeline, mock_store):
        content = "# Top\n\n## Sub\n"
        stats = pipeline.ingest(content, filename="doc.md")

        # Sub -> Top edge (part_of)
        assert stats["edges_created"] >= 1


class TestIngestWithScope:
    """Nodes carry the scope when provided."""

    def test_scope_passed_to_store(self, pipeline, mock_store):
        scope = {"channels": ["#general"], "principals": ["alice"]}
        content = "title: hello\n"
        pipeline.ingest(content, filename="data.yaml", scope=scope)

        # Every add_node call should have received the scope.
        for call in mock_store.add_node.call_args_list:
            assert call.kwargs.get("scope") == scope or call[1].get("scope") == scope


class TestIngestExplicitContentType:
    """Explicit content_type overrides filename-based detection."""

    def test_explicit_content_type(self, pipeline, mock_store):
        content = '{"a": 1}'
        stats = pipeline.ingest(
            content, filename="data.txt", content_type="application/json"
        )
        # ConfigExtractor handles application/json.
        assert stats["extractor"] == "config"


class TestIngestUnknownFallback:
    """Unknown text type falls back to structured extractor."""

    def test_fallback(self, pipeline, mock_store):
        content = "Some random text with 10.0.0.1 in it."
        stats = pipeline.ingest(content, filename="output.log", content_type="text/x-log")

        assert stats["extractor"] == "structured"


class TestStatsDict:
    """Return dict has all expected keys."""

    def test_all_keys(self, pipeline):
        content = "key: value\n"
        stats = pipeline.ingest(content, filename="f.yaml")

        expected_keys = {
            "nodes_created",
            "edges_created",
            "source_type",
            "extractor",
            "synthesis_triggered",
            "synthesis_skipped",
        }
        assert expected_keys <= set(stats.keys())


class TestSynthesisTriggered:
    """When synthesizer is provided and content warrants it, synthesis is triggered."""

    def test_synthesis_queued(self, pipeline_with_synthesizer, mock_store):
        pipe, synth = pipeline_with_synthesizer
        # Markdown with enough prose triggers synthesis.
        content = "# Heading\n\n" + "word " * 100 + "\n"
        stats = pipe.ingest(content, filename="doc.md")

        assert stats["synthesis_triggered"] is True
        assert stats["synthesis_skipped"] is False
        assert synth.queue.called or synth.synthesize.called


class TestEdgeResolutionFailure:
    """When edge label resolution fails, the edge is skipped."""

    def test_edge_skipped_on_missing_node(self, mock_store):
        # find_nodes returns empty for the second call (target not found).
        mock_store.find_nodes.side_effect = [
            [{"id": "id-1"}],  # source found
            [],                 # target not found
        ]
        pipe = IngestionPipeline(store=mock_store)
        content = "# Top\n\n## Sub\n"
        stats = pipe.ingest(content, filename="doc.md")

        # Edge should be skipped, not cause an error.
        assert isinstance(stats["edges_created"], int)
