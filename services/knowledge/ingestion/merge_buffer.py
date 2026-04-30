"""MergeBuffer — synthesis gate for the ingestion pipeline.

Decides whether LLM synthesis should run after deterministic extraction.
If the extractor set ``needs_synthesis=False`` (e.g., config files), skip.
Otherwise, check if there is enough raw content to be worth synthesizing.
"""

from __future__ import annotations

from .base import ExtractionResult

_MAX_SYNTHESIS_CHARS = 8000


class MergeBuffer:
    """Gate between deterministic extraction and LLM synthesis."""

    MIN_SYNTHESIS_CONTENT = 50  # chars minimum

    @classmethod
    def should_synthesize(cls, result: ExtractionResult) -> bool:
        """Return ``False`` if *needs_synthesis* is False or content is too short."""
        if not result.needs_synthesis:
            return False
        return len(result.raw_content) >= cls.MIN_SYNTHESIS_CONTENT

    @classmethod
    def prepare_for_synthesis(cls, result: ExtractionResult) -> str:
        """Return raw content, truncated to ~8000 chars for token efficiency."""
        return result.raw_content[:_MAX_SYNTHESIS_CHARS]
