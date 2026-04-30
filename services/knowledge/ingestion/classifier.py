"""SourceClassifier — determines content type for knowledge ingestion routing.

Priority order: explicit content_type > URL detection > extension > content sniffing > default text/plain.
"""

from __future__ import annotations

import os
import re

# Extension → MIME type mapping.
_EXTENSION_MAP: dict[str, str] = {
    ".md": "text/markdown",
    ".txt": "text/plain",
    ".yaml": "text/yaml",
    ".yml": "text/yaml",
    ".json": "application/json",
    ".toml": "text/toml",
    ".py": "text/x-python",
    ".go": "text/x-go",
    ".js": "text/javascript",
    ".ts": "text/typescript",
    ".java": "text/x-java",
    ".rs": "text/x-rust",
    ".rb": "text/x-ruby",
    ".c": "text/x-c",
    ".cpp": "text/x-c++",
    ".h": "text/x-c",
    ".html": "text/html",
    ".pdf": "application/pdf",
    ".png": "image/png",
    ".jpg": "image/jpeg",
    ".jpeg": "image/jpeg",
    ".sh": "text/x-shellscript",
}

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

_CONFIG_TYPES: frozenset[str] = frozenset({
    "text/yaml",
    "application/json",
    "text/toml",
})

_DEFAULT_TYPE = "text/plain"


class SourceClassifier:
    """Classifies source content for routing to the correct extractor."""

    @staticmethod
    def classify(
        filename: str = "",
        content_type: str = "",
        content: str = "",
    ) -> str:
        """Return the MIME type for the given source.

        Priority: explicit content_type > URL > extension > content sniffing > default.
        """
        # 1. Explicit content_type wins.
        if content_type:
            return content_type

        # 2. URL detection.
        if filename and (filename.startswith("http://") or filename.startswith("https://")):
            return "text/html"

        # 3. Extension-based detection.
        if filename:
            _, ext = os.path.splitext(filename)
            ext = ext.lower()
            if ext in _EXTENSION_MAP:
                return _EXTENSION_MAP[ext]

        # 4. Content sniffing.
        if content:
            stripped = content.lstrip()
            # HTML detection.
            if stripped.startswith("<!DOCTYPE") or stripped.startswith("<html"):
                return "text/html"
            # JSON detection.
            if stripped.startswith("{") or stripped.startswith("["):
                return "application/json"
            # YAML detection: frontmatter or key: value pattern.
            if stripped.startswith("---"):
                return "text/yaml"
            if re.match(r"^[a-zA-Z_][\w]*:\s", stripped):
                return "text/yaml"

        # 5. Default.
        return _DEFAULT_TYPE

    @staticmethod
    def is_code(content_type: str) -> bool:
        """Return True if the content type represents source code."""
        return content_type in _CODE_TYPES

    @staticmethod
    def is_config(content_type: str) -> bool:
        """Return True if the content type represents a configuration format."""
        return content_type in _CONFIG_TYPES

    @staticmethod
    def is_image(content_type: str) -> bool:
        """Return True if the content type represents an image."""
        return content_type.startswith("image/")
