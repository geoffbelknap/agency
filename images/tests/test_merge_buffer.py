"""Tests for MergeBuffer — synthesis gate in the ingestion pipeline."""

import os
import sys
import pytest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "knowledge"))
from ingestion.base import ExtractionResult
from ingestion.merge_buffer import MergeBuffer


# ---------------------------------------------------------------------------
# should_synthesize
# ---------------------------------------------------------------------------


class TestShouldSynthesize:
    """MergeBuffer.should_synthesize gating logic."""

    def test_config_result_skips_synthesis(self):
        """Config extraction (needs_synthesis=False) should not synthesize."""
        result = ExtractionResult(
            source_type="config",
            content_type="application/x-yaml",
            nodes=[{"label": "db", "kind": "service"}],
            needs_synthesis=False,
            raw_content="database:\n  host: localhost\n  port: 5432",
        )
        assert MergeBuffer.should_synthesize(result) is False

    def test_markdown_with_prose_synthesizes(self):
        """Markdown with enough prose (needs_synthesis=True) should synthesize."""
        result = ExtractionResult(
            source_type="markdown",
            content_type="text/markdown",
            needs_synthesis=True,
            raw_content="x" * 100,
        )
        assert MergeBuffer.should_synthesize(result) is True

    def test_empty_extraction_with_content_synthesizes(self):
        """No nodes extracted but has content -> still worth synthesizing."""
        result = ExtractionResult(
            source_type="markdown",
            content_type="text/markdown",
            nodes=[],
            needs_synthesis=True,
            raw_content="This document describes the architecture of the system " * 5,
        )
        assert MergeBuffer.should_synthesize(result) is True

    def test_rich_extraction_no_synthesis(self):
        """Many nodes extracted, needs_synthesis=False -> skip."""
        result = ExtractionResult(
            source_type="config",
            content_type="application/json",
            nodes=[{"label": f"node_{i}", "kind": "entity"} for i in range(20)],
            needs_synthesis=False,
            raw_content="x" * 200,
        )
        assert MergeBuffer.should_synthesize(result) is False

    def test_short_content_skips_synthesis(self):
        """needs_synthesis=True but content below MIN_SYNTHESIS_CONTENT -> skip."""
        result = ExtractionResult(
            source_type="markdown",
            content_type="text/markdown",
            needs_synthesis=True,
            raw_content="too short",
        )
        assert MergeBuffer.should_synthesize(result) is False

    def test_exactly_at_threshold(self):
        """Content exactly at MIN_SYNTHESIS_CONTENT chars should synthesize."""
        result = ExtractionResult(
            source_type="markdown",
            content_type="text/markdown",
            needs_synthesis=True,
            raw_content="x" * MergeBuffer.MIN_SYNTHESIS_CONTENT,
        )
        assert MergeBuffer.should_synthesize(result) is True

    def test_one_below_threshold(self):
        """Content one char below threshold should not synthesize."""
        result = ExtractionResult(
            source_type="markdown",
            content_type="text/markdown",
            needs_synthesis=True,
            raw_content="x" * (MergeBuffer.MIN_SYNTHESIS_CONTENT - 1),
        )
        assert MergeBuffer.should_synthesize(result) is False


# ---------------------------------------------------------------------------
# prepare_for_synthesis
# ---------------------------------------------------------------------------


class TestPrepareForSynthesis:
    """MergeBuffer.prepare_for_synthesis content preparation."""

    def test_returns_content(self):
        """Should return the raw_content from the extraction result."""
        result = ExtractionResult(
            source_type="markdown",
            content_type="text/markdown",
            raw_content="Here is some content for synthesis.",
        )
        assert MergeBuffer.prepare_for_synthesis(result) == "Here is some content for synthesis."

    def test_truncates_long_content(self):
        """Content over 8000 chars should be truncated to 8000."""
        long_content = "a" * 10000
        result = ExtractionResult(
            source_type="markdown",
            content_type="text/markdown",
            raw_content=long_content,
        )
        prepared = MergeBuffer.prepare_for_synthesis(result)
        assert len(prepared) == 8000

    def test_preserves_short_content(self):
        """Content under 8000 chars should be returned as-is."""
        content = "b" * 5000
        result = ExtractionResult(
            source_type="markdown",
            content_type="text/markdown",
            raw_content=content,
        )
        prepared = MergeBuffer.prepare_for_synthesis(result)
        assert prepared == content
        assert len(prepared) == 5000
