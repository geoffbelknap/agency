"""Config file extractor — YAML, JSON, TOML.

Deterministically extracts structure from configuration files:
- Top-level keys become ``config_item`` nodes.
- Nested keys produce ``part_of`` edges to their parent.
- String values that look like URLs produce ``url`` nodes.
- Always sets ``needs_synthesis=False`` — config structure is fully
  captured without LLM assistance.
"""

from __future__ import annotations

import json
import re
import tomllib
from typing import Optional

import yaml

from ingestion.base import BaseExtractor, ExtractionResult

_URL_RE = re.compile(r"^https?://\S+$")

_CONTENT_TYPES = frozenset({
    "application/yaml",
    "application/json",
    "application/toml",
})

_EXT_MAP = {
    ".yaml": "application/yaml",
    ".yml": "application/yaml",
    ".json": "application/json",
    ".toml": "application/toml",
}


class ConfigExtractor(BaseExtractor):
    """Extracts graph structure from YAML, JSON, and TOML config files."""

    @property
    def name(self) -> str:
        return "config"

    def can_handle(self, content_type: str, filename: str = "") -> bool:
        if content_type in _CONTENT_TYPES:
            return True
        if filename:
            for ext, _ in _EXT_MAP.items():
                if filename.endswith(ext):
                    return True
        return False

    def extract(
        self,
        content: str,
        filename: str = "",
        metadata: Optional[dict] = None,
    ) -> ExtractionResult:
        meta = dict(metadata or {})
        content_type = self._detect_format(content, filename, meta)

        try:
            data = self._parse(content, content_type)
        except Exception as exc:
            meta["error"] = str(exc)
            return ExtractionResult(
                source_type="config",
                content_type=content_type or "application/octet-stream",
                nodes=[],
                edges=[],
                raw_content=content,
                needs_synthesis=False,
                metadata=meta,
            )

        nodes: list[dict] = []
        edges: list[dict] = []

        if isinstance(data, dict):
            self._walk(data, prefix="", nodes=nodes, edges=edges)

        if filename:
            meta["source_file"] = filename

        return ExtractionResult(
            source_type="config",
            content_type=content_type or "application/octet-stream",
            nodes=nodes,
            edges=edges,
            raw_content=content,
            needs_synthesis=False,
            metadata=meta,
        )

    # ------------------------------------------------------------------
    # Internal helpers
    # ------------------------------------------------------------------

    @staticmethod
    def _detect_format(
        content: str, filename: str, meta: dict
    ) -> str:
        """Determine the config format from metadata or filename."""
        ct = meta.get("content_type", "")
        if ct in _CONTENT_TYPES:
            return ct
        if filename:
            for ext, mime in _EXT_MAP.items():
                if filename.endswith(ext):
                    return mime
        return ""

    @staticmethod
    def _parse(content: str, content_type: str) -> dict:
        """Parse *content* according to *content_type*."""
        if content_type == "application/json":
            return json.loads(content)
        if content_type == "application/toml":
            return tomllib.loads(content)
        # Default to YAML (covers application/yaml and fallback).
        return yaml.safe_load(content)

    def _walk(
        self,
        obj: dict,
        prefix: str,
        nodes: list[dict],
        edges: list[dict],
    ) -> None:
        """Recursively walk *obj*, emitting nodes and edges."""
        for key, value in obj.items():
            label = f"{prefix}.{key}" if prefix else key
            nodes.append({
                "label": label,
                "kind": "config_item",
                "summary": "",
                "properties": {},
            })
            if prefix:
                edges.append({
                    "source_label": label,
                    "target_label": prefix,
                    "relation": "part_of",
                })
            # Recurse into nested dicts.
            if isinstance(value, dict):
                self._walk(value, prefix=label, nodes=nodes, edges=edges)
            # Check scalar string values for URLs.
            elif isinstance(value, str) and _URL_RE.match(value):
                nodes.append({
                    "label": value,
                    "kind": "url",
                    "summary": "",
                    "properties": {"source_key": label},
                })
