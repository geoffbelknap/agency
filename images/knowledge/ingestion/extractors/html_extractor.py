"""HTML extractor for the knowledge graph ingestion pipeline.

Deterministically extracts structure from HTML content using stdlib
``html.parser.HTMLParser`` — no external dependencies.

- ``<title>`` → ``document`` node
- ``<h1>``–``<h6>`` → ``concept`` nodes with ``heading_level`` property
- ``<a href="...">`` → ``url`` nodes (skips anchor-only ``#`` and ``javascript:`` links)
- ``<meta name="..." content="...">`` → stored in metadata dict
- Text content → ``raw_content`` for synthesis decision
- ``needs_synthesis=True`` when total text content exceeds 200 characters
"""

from __future__ import annotations

from html.parser import HTMLParser
from typing import Optional

try:
    from ingestion.base import BaseExtractor, ExtractionResult
except ImportError:
    from knowledge.ingestion.base import BaseExtractor, ExtractionResult

_PROSE_THRESHOLD = 200
_HEADING_TAGS = frozenset(f"h{i}" for i in range(1, 7))


class _HtmlContentParser(HTMLParser):
    """Single-pass HTML parser that collects nodes, metadata, and text."""

    def __init__(self) -> None:
        super().__init__()
        self.nodes: list[dict] = []
        self.meta: dict[str, str] = {}
        self.text_parts: list[str] = []
        self.seen_urls: set[str] = set()

        # State tracking for title and heading text capture.
        self._in_title = False
        self._title_parts: list[str] = []
        self._in_heading: int | None = None  # heading level or None
        self._heading_parts: list[str] = []

    # ------------------------------------------------------------------
    # Parser callbacks
    # ------------------------------------------------------------------

    def handle_starttag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        tag_lower = tag.lower()

        if tag_lower == "title":
            self._in_title = True
            self._title_parts = []

        elif tag_lower in _HEADING_TAGS:
            self._in_heading = int(tag_lower[1])
            self._heading_parts = []

        elif tag_lower == "a":
            href = dict(attrs).get("href", "")
            if href:
                self._process_link(href)

        elif tag_lower == "meta":
            attr_dict = dict(attrs)
            name = attr_dict.get("name")
            content = attr_dict.get("content")
            if name and content is not None:
                self.meta[name] = content

    def handle_endtag(self, tag: str) -> None:
        tag_lower = tag.lower()

        if tag_lower == "title" and self._in_title:
            self._in_title = False
            title_text = "".join(self._title_parts).strip()
            if title_text:
                self.nodes.append(
                    {
                        "label": title_text,
                        "kind": "document",
                        "summary": "",
                        "properties": {},
                    }
                )

        elif tag_lower in _HEADING_TAGS and self._in_heading is not None:
            level = self._in_heading
            self._in_heading = None
            heading_text = "".join(self._heading_parts).strip()
            if heading_text:
                self.nodes.append(
                    {
                        "label": heading_text,
                        "kind": "concept",
                        "summary": "",
                        "properties": {"heading_level": level},
                    }
                )

    def handle_data(self, data: str) -> None:
        self.text_parts.append(data)

        if self._in_title:
            self._title_parts.append(data)
        if self._in_heading is not None:
            self._heading_parts.append(data)

    # ------------------------------------------------------------------
    # Helpers
    # ------------------------------------------------------------------

    def _process_link(self, href: str) -> None:
        """Add a url node for *href*, skipping anchors and javascript."""
        if href.startswith("#"):
            return
        if href.lower().startswith("javascript:"):
            return
        if href in self.seen_urls:
            return
        self.seen_urls.add(href)
        self.nodes.append(
            {
                "label": href,
                "kind": "url",
                "summary": "",
                "properties": {},
            }
        )


class HtmlExtractor(BaseExtractor):
    """Extract graph structure from HTML content."""

    @property
    def name(self) -> str:  # noqa: D401
        return "html"

    def can_handle(self, content_type: str, filename: str = "") -> bool:
        return content_type == "text/html"

    def extract(
        self,
        content: str,
        filename: str = "",
        metadata: Optional[dict] = None,
    ) -> ExtractionResult:
        parser = _HtmlContentParser()
        parser.feed(content)

        # Synthesis decision based on total visible text length.
        total_text = "".join(parser.text_parts).strip()
        needs_synthesis = len(total_text) > _PROSE_THRESHOLD

        # Build metadata.
        result_metadata = dict(metadata) if metadata else {}
        result_metadata.update(parser.meta)
        if filename:
            result_metadata["source_file"] = filename

        return ExtractionResult(
            source_type="html",
            content_type="text/html",
            nodes=parser.nodes,
            edges=[],
            raw_content=content,
            needs_synthesis=needs_synthesis,
            default_provenance="EXTRACTED",
            metadata=result_metadata,
        )
