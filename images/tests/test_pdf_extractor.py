"""Tests for PdfExtractor — graceful degradation when PyMuPDF is unavailable."""

import os
import sys

import pytest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "knowledge"))
from services.knowledge.ingestion.base import ExtractionResult, BaseExtractor
from services.knowledge.ingestion.extractors.pdf import PdfExtractor


# ---------------------------------------------------------------------------
# PdfExtractor identity
# ---------------------------------------------------------------------------


class TestPdfExtractorIdentity:
    """Name, default provenance, and isinstance checks."""

    def test_name_is_pdf(self):
        ext = PdfExtractor()
        assert ext.name == "pdf"

    def test_is_base_extractor(self):
        ext = PdfExtractor()
        assert isinstance(ext, BaseExtractor)

    def test_default_provenance(self):
        ext = PdfExtractor()
        assert ext.default_provenance == "EXTRACTED"


# ---------------------------------------------------------------------------
# can_handle
# ---------------------------------------------------------------------------


class TestPdfExtractorCanHandle:
    """PdfExtractor.can_handle for various content types."""

    def test_handles_application_pdf(self):
        ext = PdfExtractor()
        assert ext.can_handle("application/pdf") is True

    def test_rejects_text_plain(self):
        ext = PdfExtractor()
        assert ext.can_handle("text/plain") is False

    def test_rejects_text_markdown(self):
        ext = PdfExtractor()
        assert ext.can_handle("text/markdown") is False

    def test_rejects_text_html(self):
        ext = PdfExtractor()
        assert ext.can_handle("text/html") is False

    def test_rejects_application_json(self):
        ext = PdfExtractor()
        assert ext.can_handle("application/json") is False

    def test_rejects_application_yaml(self):
        ext = PdfExtractor()
        assert ext.can_handle("application/yaml") is False

    def test_rejects_image_png(self):
        ext = PdfExtractor()
        assert ext.can_handle("image/png") is False

    def test_filename_ignored(self):
        ext = PdfExtractor()
        # Only MIME type matters, not filename
        assert ext.can_handle("text/plain", "report.pdf") is False
        assert ext.can_handle("application/pdf", "report.pdf") is True


# ---------------------------------------------------------------------------
# Availability (PyMuPDF not installed in test env)
# ---------------------------------------------------------------------------


class TestPdfExtractorAvailability:
    """available property reflects PyMuPDF install status."""

    def test_available_returns_false_without_pymupdf(self):
        ext = PdfExtractor()
        assert ext.available is False


# ---------------------------------------------------------------------------
# Graceful fallback when PyMuPDF unavailable
# ---------------------------------------------------------------------------


class TestPdfExtractorFallback:
    """extract() returns a graceful fallback when PyMuPDF is not installed."""

    def test_returns_extraction_result(self):
        ext = PdfExtractor()
        result = ext.extract("fake pdf content")
        assert isinstance(result, ExtractionResult)

    def test_empty_nodes(self):
        ext = PdfExtractor()
        result = ext.extract("fake pdf content")
        assert result.nodes == []

    def test_empty_edges(self):
        ext = PdfExtractor()
        result = ext.extract("fake pdf content")
        assert result.edges == []

    def test_needs_synthesis_true(self):
        ext = PdfExtractor()
        result = ext.extract("fake pdf content")
        assert result.needs_synthesis is True

    def test_source_type_is_pdf(self):
        ext = PdfExtractor()
        result = ext.extract("fake pdf content")
        assert result.source_type == "pdf"

    def test_content_type_is_application_pdf(self):
        ext = PdfExtractor()
        result = ext.extract("fake pdf content")
        assert result.content_type == "application/pdf"

    def test_metadata_has_unavailability_note(self):
        ext = PdfExtractor()
        result = ext.extract("fake pdf content")
        assert "note" in result.metadata
        assert "PyMuPDF" in result.metadata["note"]
        assert "unavailable" in result.metadata["note"].lower()

    def test_raw_content_empty_when_unavailable(self):
        ext = PdfExtractor()
        result = ext.extract("fake pdf content")
        assert result.raw_content == ""

    def test_default_provenance_on_result(self):
        ext = PdfExtractor()
        result = ext.extract("fake pdf content")
        assert result.default_provenance == "EXTRACTED"

    def test_metadata_passed_through(self):
        ext = PdfExtractor()
        result = ext.extract("fake pdf content", metadata={"tool": "upload"})
        assert result.metadata["tool"] == "upload"
        assert "note" in result.metadata

    def test_filename_in_metadata(self):
        ext = PdfExtractor()
        result = ext.extract("fake pdf content", filename="report.pdf")
        assert result.metadata.get("source_file") == "report.pdf"
