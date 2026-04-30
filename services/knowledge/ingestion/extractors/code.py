"""Code file extractor for the knowledge graph ingestion pipeline.

Deterministically extracts structure from source code files using regex:
- Functions become ``function`` nodes.
- Classes / structs / interfaces become ``system`` nodes.
- Imports are stored in metadata.
- ``needs_synthesis`` is set when comment/docstring volume exceeds a threshold.

Supported languages: Python, Go, JavaScript, TypeScript.
Other code types from SourceClassifier._CODE_TYPES are accepted (can_handle
returns True) but produce only comment-based synthesis detection with no
structural extraction.
"""

from __future__ import annotations

import re
from typing import Optional

from services.knowledge.ingestion.base import BaseExtractor, ExtractionResult

# ---------------------------------------------------------------------------
# Code content types (mirrors SourceClassifier._CODE_TYPES)
# ---------------------------------------------------------------------------

_CODE_TYPES: frozenset[str] = frozenset({
    "text/x-python",
    "text/x-go",
    "text/javascript",
    "text/typescript",
    "text/x-java",
    "text/x-rust",
    "text/x-ruby",
    "text/x-c",
    "text/x-c++",
    "text/x-shellscript",
})

# ---------------------------------------------------------------------------
# Language detection from content type
# ---------------------------------------------------------------------------

_LANG_MAP: dict[str, str] = {
    "text/x-python": "python",
    "text/x-go": "go",
    "text/javascript": "javascript",
    "text/typescript": "typescript",
    "text/x-java": "java",
    "text/x-rust": "rust",
    "text/x-ruby": "ruby",
    "text/x-c": "c",
    "text/x-c++": "c++",
    "text/x-shellscript": "shell",
}

# ---------------------------------------------------------------------------
# Regex patterns per language
# ---------------------------------------------------------------------------

# Python
_PY_FUNC_RE = re.compile(r"^def\s+(\w+)\s*\(", re.MULTILINE)
_PY_CLASS_RE = re.compile(r"^class\s+(\w+)", re.MULTILINE)
_PY_IMPORT_RE = re.compile(r"^import\s+(\S+)", re.MULTILINE)
_PY_FROM_RE = re.compile(r"^from\s+(\S+)\s+import", re.MULTILINE)

# Go
_GO_FUNC_RE = re.compile(r"^func\s+(?:\([^)]*\)\s+)?(\w+)\s*\(", re.MULTILINE)
_GO_STRUCT_RE = re.compile(r"^type\s+(\w+)\s+struct", re.MULTILINE)
_GO_IFACE_RE = re.compile(r"^type\s+(\w+)\s+interface", re.MULTILINE)

# JavaScript / TypeScript
_JS_FUNC_RE = re.compile(r"^(?:export\s+)?function\s+(\w+)", re.MULTILINE)
_JS_ARROW_RE = re.compile(
    r"^(?:export\s+)?(?:const|let|var)\s+(\w+)\s*=\s*(?:\([^)]*\)\s*=>|function)",
    re.MULTILINE,
)
_JS_CLASS_RE = re.compile(r"^(?:export\s+)?class\s+(\w+)", re.MULTILINE)

# Comment / docstring detection (language-agnostic approximation)
_LINE_COMMENT_RE = re.compile(r"(?://|#)\s*(.+)$", re.MULTILINE)
_BLOCK_COMMENT_RE = re.compile(r'/\*.*?\*/', re.DOTALL)
_TRIPLE_QUOTE_RE = re.compile(r'""".*?"""|\'\'\'.*?\'\'\'', re.DOTALL)

_COMMENT_THRESHOLD = 200  # chars of comments/docstrings to trigger synthesis


class CodeExtractor(BaseExtractor):
    """Extract graph structure from source code files."""

    @property
    def name(self) -> str:  # noqa: D401
        return "code"

    def can_handle(self, content_type: str, filename: str = "") -> bool:
        return content_type in _CODE_TYPES

    def extract(
        self,
        content: str,
        filename: str = "",
        metadata: Optional[dict] = None,
    ) -> ExtractionResult:
        nodes: list[dict] = []
        edges: list[dict] = []
        result_metadata = dict(metadata) if metadata else {}

        content_type = result_metadata.get("content_type", "")
        language = _LANG_MAP.get(content_type, "unknown")

        if filename:
            result_metadata["source_file"] = filename

        # Precompute line start offsets for line_number calculation.
        line_starts = [0]
        for i, ch in enumerate(content):
            if ch == "\n":
                line_starts.append(i + 1)

        def _line_number(match_start: int) -> int:
            """Return 1-based line number for the given string offset."""
            lo, hi = 0, len(line_starts) - 1
            while lo < hi:
                mid = (lo + hi + 1) // 2
                if line_starts[mid] <= match_start:
                    lo = mid
                else:
                    hi = mid - 1
            return lo + 1

        def _add_node(label: str, kind: str, match_start: int) -> None:
            nodes.append({
                "label": label,
                "kind": kind,
                "summary": "",
                "properties": {
                    "language": language,
                    "source_file": filename,
                    "line_number": _line_number(match_start),
                },
            })

        # -- Language-specific extraction -----------------------------------

        if content_type == "text/x-python":
            for m in _PY_FUNC_RE.finditer(content):
                _add_node(m.group(1), "function", m.start())
            for m in _PY_CLASS_RE.finditer(content):
                _add_node(m.group(1), "system", m.start())
            # Imports
            imports: list[str] = []
            for m in _PY_IMPORT_RE.finditer(content):
                imports.append(m.group(1))
            for m in _PY_FROM_RE.finditer(content):
                imports.append(m.group(1))
            if imports:
                result_metadata["imports"] = imports

        elif content_type == "text/x-go":
            for m in _GO_FUNC_RE.finditer(content):
                _add_node(m.group(1), "function", m.start())
            for m in _GO_STRUCT_RE.finditer(content):
                _add_node(m.group(1), "system", m.start())
            for m in _GO_IFACE_RE.finditer(content):
                _add_node(m.group(1), "system", m.start())

        elif content_type in ("text/javascript", "text/typescript"):
            for m in _JS_FUNC_RE.finditer(content):
                _add_node(m.group(1), "function", m.start())
            for m in _JS_ARROW_RE.finditer(content):
                _add_node(m.group(1), "function", m.start())
            for m in _JS_CLASS_RE.finditer(content):
                _add_node(m.group(1), "system", m.start())

        # -- Comment / docstring volume for needs_synthesis -----------------

        comment_chars = 0
        for m in _LINE_COMMENT_RE.finditer(content):
            comment_chars += len(m.group(1))
        for m in _BLOCK_COMMENT_RE.finditer(content):
            comment_chars += len(m.group(0))
        for m in _TRIPLE_QUOTE_RE.finditer(content):
            comment_chars += len(m.group(0))

        needs_synthesis = comment_chars > _COMMENT_THRESHOLD

        return ExtractionResult(
            source_type="code",
            content_type=content_type,
            nodes=nodes,
            edges=edges,
            raw_content=content,
            needs_synthesis=needs_synthesis,
            default_provenance="EXTRACTED",
            metadata=result_metadata,
        )
