"""Tests for HtmlExtractor."""

import os
import sys

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "knowledge"))
from services.knowledge.ingestion.extractors.html_extractor import HtmlExtractor


# ---------------------------------------------------------------------------
# can_handle
# ---------------------------------------------------------------------------


class TestHtmlExtractorCanHandle:
    """HtmlExtractor.can_handle for various content types."""

    def test_name_is_html(self):
        ext = HtmlExtractor()
        assert ext.name == "html"

    def test_handles_text_html(self):
        ext = HtmlExtractor()
        assert ext.can_handle("text/html") is True

    def test_rejects_text_plain(self):
        ext = HtmlExtractor()
        assert ext.can_handle("text/plain") is False

    def test_rejects_text_markdown(self):
        ext = HtmlExtractor()
        assert ext.can_handle("text/markdown") is False

    def test_rejects_application_json(self):
        ext = HtmlExtractor()
        assert ext.can_handle("application/json") is False


# ---------------------------------------------------------------------------
# Title extraction
# ---------------------------------------------------------------------------


class TestHtmlExtractorTitle:
    """<title> tag produces a document node."""

    def test_title_creates_document_node(self):
        ext = HtmlExtractor()
        result = ext.extract("<html><head><title>My Page</title></head><body></body></html>")
        docs = [n for n in result.nodes if n["kind"] == "document"]
        assert len(docs) == 1
        assert docs[0]["label"] == "My Page"

    def test_no_title_no_document_node(self):
        ext = HtmlExtractor()
        result = ext.extract("<html><body><p>Hello</p></body></html>")
        docs = [n for n in result.nodes if n["kind"] == "document"]
        assert len(docs) == 0


# ---------------------------------------------------------------------------
# Heading extraction
# ---------------------------------------------------------------------------


class TestHtmlExtractorHeadings:
    """<h1>-<h6> tags produce concept nodes with heading_level."""

    def test_h1_creates_concept_node(self):
        ext = HtmlExtractor()
        result = ext.extract("<h1>Introduction</h1>")
        concepts = [n for n in result.nodes if n["kind"] == "concept"]
        assert len(concepts) == 1
        assert concepts[0]["label"] == "Introduction"
        assert concepts[0]["properties"]["heading_level"] == 1

    def test_h3_heading_level(self):
        ext = HtmlExtractor()
        result = ext.extract("<h3>Details</h3>")
        concepts = [n for n in result.nodes if n["kind"] == "concept"]
        assert concepts[0]["properties"]["heading_level"] == 3

    def test_multiple_headings(self):
        ext = HtmlExtractor()
        html = "<h1>Top</h1><h2>Sub</h2><h3>Deep</h3>"
        result = ext.extract(html)
        concepts = [n for n in result.nodes if n["kind"] == "concept"]
        assert len(concepts) == 3

    def test_all_heading_levels(self):
        ext = HtmlExtractor()
        html = "".join(f"<h{i}>H{i}</h{i}>" for i in range(1, 7))
        result = ext.extract(html)
        concepts = [n for n in result.nodes if n["kind"] == "concept"]
        assert len(concepts) == 6
        for i, c in enumerate(concepts, 1):
            assert c["properties"]["heading_level"] == i


# ---------------------------------------------------------------------------
# Link extraction
# ---------------------------------------------------------------------------


class TestHtmlExtractorLinks:
    """<a href> tags produce url nodes."""

    def test_link_creates_url_node(self):
        ext = HtmlExtractor()
        result = ext.extract('<a href="https://example.com">Example</a>')
        urls = [n for n in result.nodes if n["kind"] == "url"]
        assert len(urls) == 1
        assert urls[0]["label"] == "https://example.com"

    def test_multiple_links(self):
        ext = HtmlExtractor()
        html = '<a href="https://a.com">A</a><a href="https://b.com">B</a>'
        result = ext.extract(html)
        urls = [n for n in result.nodes if n["kind"] == "url"]
        assert len(urls) == 2

    def test_anchor_only_link_skipped(self):
        ext = HtmlExtractor()
        result = ext.extract('<a href="#section">Jump</a>')
        urls = [n for n in result.nodes if n["kind"] == "url"]
        assert len(urls) == 0

    def test_javascript_link_skipped(self):
        ext = HtmlExtractor()
        result = ext.extract('<a href="javascript:void(0)">Click</a>')
        urls = [n for n in result.nodes if n["kind"] == "url"]
        assert len(urls) == 0

    def test_relative_link_kept(self):
        ext = HtmlExtractor()
        result = ext.extract('<a href="/about">About</a>')
        urls = [n for n in result.nodes if n["kind"] == "url"]
        assert len(urls) == 1
        assert urls[0]["label"] == "/about"

    def test_duplicate_links_deduplicated(self):
        ext = HtmlExtractor()
        html = '<a href="https://a.com">A</a><a href="https://a.com">A again</a>'
        result = ext.extract(html)
        urls = [n for n in result.nodes if n["kind"] == "url"]
        assert len(urls) == 1


# ---------------------------------------------------------------------------
# Meta description
# ---------------------------------------------------------------------------


class TestHtmlExtractorMeta:
    """<meta name="..." content="..."> stored in metadata."""

    def test_meta_description_in_metadata(self):
        ext = HtmlExtractor()
        html = '<html><head><meta name="description" content="A test page"></head><body></body></html>'
        result = ext.extract(html)
        assert result.metadata.get("description") == "A test page"

    def test_meta_author_in_metadata(self):
        ext = HtmlExtractor()
        html = '<meta name="author" content="Alice">'
        result = ext.extract(html)
        assert result.metadata.get("author") == "Alice"

    def test_meta_without_name_ignored(self):
        ext = HtmlExtractor()
        html = '<meta charset="utf-8">'
        result = ext.extract(html)
        # charset meta has no name attribute, so nothing added to metadata
        assert "charset" not in result.metadata


# ---------------------------------------------------------------------------
# Synthesis threshold
# ---------------------------------------------------------------------------


class TestHtmlExtractorSynthesis:
    """needs_synthesis based on total text content length."""

    def test_long_content_needs_synthesis(self):
        ext = HtmlExtractor()
        html = "<p>" + "This is substantial content. " * 20 + "</p>"
        result = ext.extract(html)
        assert result.needs_synthesis is True

    def test_short_content_no_synthesis(self):
        ext = HtmlExtractor()
        html = "<p>Short.</p>"
        result = ext.extract(html)
        assert result.needs_synthesis is False

    def test_empty_html_no_synthesis(self):
        ext = HtmlExtractor()
        result = ext.extract("<html><body></body></html>")
        assert result.needs_synthesis is False


# ---------------------------------------------------------------------------
# Result properties
# ---------------------------------------------------------------------------


class TestHtmlExtractorResult:
    """General result properties."""

    def test_source_type_is_html(self):
        ext = HtmlExtractor()
        result = ext.extract("<p>test</p>")
        assert result.source_type == "html"

    def test_content_type_is_text_html(self):
        ext = HtmlExtractor()
        result = ext.extract("<p>test</p>")
        assert result.content_type == "text/html"

    def test_default_provenance(self):
        ext = HtmlExtractor()
        result = ext.extract("<p>test</p>")
        assert result.default_provenance == "EXTRACTED"

    def test_raw_content_preserved(self):
        ext = HtmlExtractor()
        html = "<h1>Hello</h1><p>World</p>"
        result = ext.extract(html)
        assert result.raw_content == html

    def test_metadata_passed_through(self):
        ext = HtmlExtractor()
        result = ext.extract("<p>test</p>", metadata={"custom": "val"})
        assert result.metadata["custom"] == "val"

    def test_filename_in_metadata(self):
        ext = HtmlExtractor()
        result = ext.extract("<p>test</p>", filename="index.html")
        assert result.metadata["source_file"] == "index.html"
