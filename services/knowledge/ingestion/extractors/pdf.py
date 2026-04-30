"""PDF extractor for the knowledge graph ingestion pipeline.

Extracts text content from PDF files using PyMuPDF (fitz).  Degrades
gracefully when PyMuPDF is not installed — returns an empty
ExtractionResult with ``needs_synthesis=True`` so the pipeline can
still forward the source for LLM-based processing.
"""

from __future__ import annotations

from typing import Optional

try:
    import fitz  # PyMuPDF

    _PYMUPDF_AVAILABLE = True
except ImportError:
    _PYMUPDF_AVAILABLE = False

from services.knowledge.ingestion.base import BaseExtractor, ExtractionResult


class PdfExtractor(BaseExtractor):
    """Extract text and structure from PDF documents.

    When PyMuPDF is installed, iterates pages and extracts text blocks.
    When unavailable, returns a fallback result that flags the content
    for LLM synthesis.
    """

    @property
    def name(self) -> str:  # noqa: D401
        return "pdf"

    @property
    def available(self) -> bool:
        """Whether the PyMuPDF backend is installed and importable."""
        return _PYMUPDF_AVAILABLE

    @property
    def default_provenance(self) -> str:  # noqa: D401
        return "EXTRACTED"

    def can_handle(self, content_type: str, filename: str = "") -> bool:
        return content_type == "application/pdf"

    def extract(
        self,
        content: str,
        filename: str = "",
        metadata: Optional[dict] = None,
    ) -> ExtractionResult:
        result_metadata = dict(metadata) if metadata else {}
        if filename:
            result_metadata["source_file"] = filename

        if not _PYMUPDF_AVAILABLE:
            result_metadata["note"] = "PyMuPDF unavailable — install with: pip install PyMuPDF"
            return ExtractionResult(
                source_type="pdf",
                content_type="application/pdf",
                nodes=[],
                edges=[],
                raw_content="",
                needs_synthesis=True,
                default_provenance=self.default_provenance,
                metadata=result_metadata,
            )

        # --- Full extraction path (requires PyMuPDF) -----------------------
        return self._extract_with_pymupdf(content, filename, result_metadata)

    def _extract_with_pymupdf(
        self,
        content: str,
        filename: str,
        result_metadata: dict,
    ) -> ExtractionResult:
        """Extract text from PDF bytes using PyMuPDF.

        *content* is expected to be raw PDF bytes (as a string or bytes).
        """
        nodes: list[dict] = []
        edges: list[dict] = []
        pages_text: list[str] = []

        try:
            # content may arrive as str or bytes; fitz needs bytes
            raw = content.encode("latin-1") if isinstance(content, str) else content
            doc = fitz.open(stream=raw, filetype="pdf")  # type: ignore[union-attr]

            for page_num, page in enumerate(doc, start=1):
                text = page.get_text()
                if text.strip():
                    pages_text.append(text)
                    nodes.append(
                        {
                            "label": f"page_{page_num}",
                            "kind": "document_section",
                            "summary": text[:200].strip(),
                            "properties": {"page": page_num},
                        }
                    )

            doc.close()
            result_metadata["page_count"] = len(pages_text)

        except Exception as exc:
            result_metadata["error"] = str(exc)
            return ExtractionResult(
                source_type="pdf",
                content_type="application/pdf",
                nodes=[],
                edges=[],
                raw_content="",
                needs_synthesis=True,
                default_provenance=self.default_provenance,
                metadata=result_metadata,
            )

        full_text = "\n\n".join(pages_text)

        return ExtractionResult(
            source_type="pdf",
            content_type="application/pdf",
            nodes=nodes,
            edges=edges,
            raw_content=full_text,
            needs_synthesis=bool(full_text.strip()),
            default_provenance=self.default_provenance,
            metadata=result_metadata,
        )
