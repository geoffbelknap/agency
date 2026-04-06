"""Tests for ExtractionResult and BaseExtractor base types."""

import pytest
from knowledge.ingestion.base import ExtractionResult, BaseExtractor


# ---------------------------------------------------------------------------
# ExtractionResult
# ---------------------------------------------------------------------------


class TestExtractionResultDefaults:
    """Empty / default ExtractionResult behaviour."""

    def test_empty_result_has_no_nodes(self):
        r = ExtractionResult(source_type="test", content_type="text/plain")
        assert r.nodes == []

    def test_empty_result_has_no_edges(self):
        r = ExtractionResult(source_type="test", content_type="text/plain")
        assert r.edges == []

    def test_empty_result_raw_content_is_empty(self):
        r = ExtractionResult(source_type="test", content_type="text/plain")
        assert r.raw_content == ""

    def test_empty_result_needs_synthesis_true(self):
        r = ExtractionResult(source_type="test", content_type="text/plain")
        assert r.needs_synthesis is True

    def test_default_provenance(self):
        r = ExtractionResult(source_type="test", content_type="text/plain")
        assert r.default_provenance == "EXTRACTED"

    def test_default_metadata_is_empty_dict(self):
        r = ExtractionResult(source_type="test", content_type="text/plain")
        assert r.metadata == {}

    def test_default_metadata_not_shared_between_instances(self):
        a = ExtractionResult(source_type="a", content_type="text/plain")
        b = ExtractionResult(source_type="b", content_type="text/plain")
        a.metadata["key"] = "val"
        assert "key" not in b.metadata


class TestExtractionResultWithData:
    """ExtractionResult populated with nodes and edges."""

    def _make_result(self):
        return ExtractionResult(
            source_type="markdown",
            content_type="text/markdown",
            nodes=[
                {"label": "Agency", "kind": "project", "summary": "AI agent platform", "properties": {}},
                {"label": "ASK", "kind": "framework", "summary": "Security tenets", "properties": {}},
            ],
            edges=[
                {"source_label": "Agency", "target_label": "ASK", "relation": "implements"},
            ],
            raw_content="# Agency\nImplements ASK.",
            needs_synthesis=False,
            default_provenance="MANUAL",
            metadata={"filename": "README.md"},
        )

    def test_node_count(self):
        r = self._make_result()
        assert len(r.nodes) == 2

    def test_edge_count(self):
        r = self._make_result()
        assert len(r.edges) == 1

    def test_source_type(self):
        r = self._make_result()
        assert r.source_type == "markdown"

    def test_content_type(self):
        r = self._make_result()
        assert r.content_type == "text/markdown"

    def test_custom_provenance(self):
        r = self._make_result()
        assert r.default_provenance == "MANUAL"

    def test_needs_synthesis_false(self):
        r = self._make_result()
        assert r.needs_synthesis is False

    def test_metadata_filename(self):
        r = self._make_result()
        assert r.metadata["filename"] == "README.md"


class TestExtractionResultMerge:
    """Merging two ExtractionResults."""

    def _left(self):
        return ExtractionResult(
            source_type="markdown",
            content_type="text/markdown",
            nodes=[{"label": "A", "kind": "entity", "summary": "", "properties": {}}],
            edges=[{"source_label": "A", "target_label": "B", "relation": "links"}],
            raw_content="left content",
            needs_synthesis=False,
            metadata={"origin": "left"},
        )

    def _right(self):
        return ExtractionResult(
            source_type="config",
            content_type="application/yaml",
            nodes=[{"label": "B", "kind": "entity", "summary": "", "properties": {}}],
            edges=[],
            raw_content="right content",
            needs_synthesis=True,
            metadata={"origin": "right"},
        )

    def test_merge_combines_nodes(self):
        merged = self._left().merge(self._right())
        assert len(merged.nodes) == 2

    def test_merge_combines_edges(self):
        merged = self._left().merge(self._right())
        assert len(merged.edges) == 1

    def test_merge_concatenates_raw_content(self):
        merged = self._left().merge(self._right())
        assert "left content" in merged.raw_content
        assert "right content" in merged.raw_content

    def test_merge_needs_synthesis_if_either_does(self):
        merged = self._left().merge(self._right())
        assert merged.needs_synthesis is True

    def test_merge_both_false_stays_false(self):
        left = self._left()
        right = self._right()
        right.needs_synthesis = False
        merged = left.merge(right)
        assert merged.needs_synthesis is False

    def test_merge_keeps_left_source_type(self):
        merged = self._left().merge(self._right())
        assert merged.source_type == "markdown"

    def test_merge_combines_metadata(self):
        merged = self._left().merge(self._right())
        assert merged.metadata["origin"] == "right"  # right overwrites left

    def test_merge_does_not_mutate_originals(self):
        left = self._left()
        right = self._right()
        left.merge(right)
        assert len(left.nodes) == 1
        assert len(right.nodes) == 1


# ---------------------------------------------------------------------------
# BaseExtractor ABC
# ---------------------------------------------------------------------------


class DummyExtractor(BaseExtractor):
    """Concrete test implementation of BaseExtractor."""

    @property
    def name(self) -> str:
        return "dummy"

    def can_handle(self, content_type: str, filename: str = "") -> bool:
        return content_type == "text/plain"

    def extract(self, content: str, filename: str = "", metadata: dict | None = None) -> ExtractionResult:
        return ExtractionResult(
            source_type="plain",
            content_type="text/plain",
            nodes=[{"label": "test", "kind": "entity", "summary": content[:50], "properties": {}}],
            raw_content=content,
        )


class TestBaseExtractor:
    """Verify BaseExtractor contract via DummyExtractor."""

    def test_name_property(self):
        ext = DummyExtractor()
        assert ext.name == "dummy"

    def test_can_handle_matching(self):
        ext = DummyExtractor()
        assert ext.can_handle("text/plain") is True

    def test_can_handle_non_matching(self):
        ext = DummyExtractor()
        assert ext.can_handle("application/json") is False

    def test_extract_returns_extraction_result(self):
        ext = DummyExtractor()
        result = ext.extract("hello world")
        assert isinstance(result, ExtractionResult)

    def test_extract_populates_nodes(self):
        ext = DummyExtractor()
        result = ext.extract("hello world")
        assert len(result.nodes) == 1
        assert result.nodes[0]["label"] == "test"

    def test_cannot_instantiate_abc_directly(self):
        with pytest.raises(TypeError):
            BaseExtractor()
