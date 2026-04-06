"""Markdown/text extractor for the knowledge graph ingestion pipeline.

Deterministically extracts structure from markdown and plain-text files:
- Headings become ``concept`` nodes.
- Parent-child heading relationships become ``part_of`` edges.
- Markdown links pointing to ``.md`` files become ``relates_to`` edges.
- URLs (bare and inside links) become ``url`` nodes.
- ``needs_synthesis`` is set when substantial prose is present.
"""

from __future__ import annotations

import re
from typing import Optional

from ingestion.base import BaseExtractor, ExtractionResult

# ---------------------------------------------------------------------------
# Regex patterns
# ---------------------------------------------------------------------------

_HEADING_RE = re.compile(r"^(#{1,6})\s+(.+)$", re.MULTILINE)
_MD_LINK_RE = re.compile(r"\[([^\]]+)\]\(([^)]+)\)")
_URL_RE = re.compile(r"https?://[^\s<>\"')\]]+")

# For prose detection: strip headings, links, and URLs, then measure length.
_STRIP_HEADINGS_RE = re.compile(r"^#{1,6}\s+.+$", re.MULTILINE)
_STRIP_LINKS_RE = re.compile(r"\[([^\]]+)\]\([^)]+\)")
_STRIP_URLS_RE = re.compile(r"https?://[^\s<>\"')\]]+")

_PROSE_THRESHOLD = 200  # characters of non-heading/link text to trigger synthesis


class MarkdownExtractor(BaseExtractor):
    """Extract graph structure from Markdown and plain-text content."""

    @property
    def name(self) -> str:  # noqa: D401
        return "markdown"

    def can_handle(self, content_type: str, filename: str = "") -> bool:
        return content_type in ("text/markdown", "text/plain")

    def extract(
        self,
        content: str,
        filename: str = "",
        metadata: Optional[dict] = None,
    ) -> ExtractionResult:
        nodes: list[dict] = []
        edges: list[dict] = []

        # -- Headings → concept nodes + hierarchy edges --------------------

        # Stack tracks (level, label) for parent lookup.
        heading_stack: list[tuple[int, str]] = []

        for match in _HEADING_RE.finditer(content):
            level = len(match.group(1))
            label = match.group(2).strip()

            nodes.append(
                {
                    "label": label,
                    "kind": "concept",
                    "summary": "",
                    "properties": {"level": level},
                }
            )

            # Pop stack until we find a heading with a strictly smaller level.
            while heading_stack and heading_stack[-1][0] >= level:
                heading_stack.pop()

            if heading_stack:
                parent_label = heading_stack[-1][1]
                edges.append(
                    {
                        "source_label": label,
                        "target_label": parent_label,
                        "relation": "part_of",
                    }
                )

            heading_stack.append((level, label))

        # -- Markdown links to .md files → relates_to edges ----------------

        # Collect the most recent heading label for edge source context.
        for match in _MD_LINK_RE.finditer(content):
            target = match.group(2)
            if target.endswith(".md"):
                # Use filename (or first heading) as source.
                source_label = filename or (
                    nodes[0]["label"] if nodes else "unknown"
                )
                edges.append(
                    {
                        "source_label": source_label,
                        "target_label": target,
                        "relation": "relates_to",
                    }
                )

        # -- URLs → url nodes (deduplicated) --------------------------------

        seen_urls: set[str] = set()
        for match in _URL_RE.finditer(content):
            url = match.group(0)
            if url not in seen_urls:
                seen_urls.add(url)
                nodes.append(
                    {
                        "label": url,
                        "kind": "url",
                        "summary": "",
                        "properties": {},
                    }
                )

        # -- Prose detection for needs_synthesis ----------------------------

        stripped = _STRIP_HEADINGS_RE.sub("", content)
        stripped = _STRIP_LINKS_RE.sub("", stripped)
        stripped = _STRIP_URLS_RE.sub("", stripped)
        stripped = stripped.strip()
        needs_synthesis = len(stripped) > _PROSE_THRESHOLD

        # -- Build result --------------------------------------------------

        result_metadata = dict(metadata) if metadata else {}
        if filename:
            result_metadata["source_file"] = filename

        return ExtractionResult(
            source_type="markdown",
            content_type="text/markdown",
            nodes=nodes,
            edges=edges,
            raw_content=content,
            needs_synthesis=needs_synthesis,
            metadata=result_metadata,
        )
