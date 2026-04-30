"""Base types for the universal ingestion pipeline.

ExtractionResult — output of any deterministic extractor.
BaseExtractor   — ABC that all extractors implement.
"""

from __future__ import annotations

from abc import ABC, abstractmethod
from dataclasses import dataclass, field
from typing import Optional


@dataclass
class ExtractionResult:
    """Output of a single extraction pass.

    Holds deterministically extracted nodes and edges plus the raw content
    for optional LLM synthesis.  Two results can be merged to combine
    outputs from multiple extractors operating on the same source.
    """

    source_type: str
    """Extractor-assigned label, e.g. ``"markdown"``, ``"config"``."""

    content_type: str
    """MIME type of the original content."""

    nodes: list = field(default_factory=list)
    """Extracted nodes.  Each is a dict with *label*, *kind*, *summary*, *properties*."""

    edges: list = field(default_factory=list)
    """Extracted edges.  Each is a dict with *source_label*, *target_label*, *relation*."""

    raw_content: str = ""
    """Original content preserved for LLM synthesis."""

    needs_synthesis: bool = True
    """If ``True`` the MergeBuffer should forward this result to the LLM."""

    default_provenance: str = "EXTRACTED"
    """Provenance tag stamped on nodes/edges created from this result."""

    metadata: dict = field(default_factory=dict)
    """Arbitrary metadata carried through the pipeline."""

    # ------------------------------------------------------------------
    # Merge
    # ------------------------------------------------------------------

    def merge(self, other: ExtractionResult) -> ExtractionResult:
        """Combine *self* with *other*, returning a new result.

        * Nodes and edges are concatenated (no dedup — that is the store's job).
        * ``raw_content`` values are joined with a newline separator.
        * ``needs_synthesis`` is ``True`` if **either** result requires it.
        * ``metadata`` dicts are shallow-merged (right wins on key conflict).
        * ``source_type``, ``content_type``, and ``default_provenance`` come
          from the left operand.
        """
        merged_metadata = {**self.metadata, **other.metadata}
        return ExtractionResult(
            source_type=self.source_type,
            content_type=self.content_type,
            nodes=list(self.nodes) + list(other.nodes),
            edges=list(self.edges) + list(other.edges),
            raw_content="\n".join(filter(None, [self.raw_content, other.raw_content])),
            needs_synthesis=self.needs_synthesis or other.needs_synthesis,
            default_provenance=self.default_provenance,
            metadata=merged_metadata,
        )


class BaseExtractor(ABC):
    """Abstract base class for deterministic content extractors.

    Subclasses declare which content types they handle and implement
    the extraction logic that produces an :class:`ExtractionResult`.
    """

    @property
    @abstractmethod
    def name(self) -> str:
        """Short identifier for this extractor (e.g. ``"markdown"``)."""

    @abstractmethod
    def can_handle(self, content_type: str, filename: str = "") -> bool:
        """Return ``True`` if this extractor can process the given content.

        Parameters
        ----------
        content_type:
            MIME type string.
        filename:
            Optional filename hint for format detection.
        """

    @abstractmethod
    def extract(
        self,
        content: str,
        filename: str = "",
        metadata: Optional[dict] = None,
    ) -> ExtractionResult:
        """Extract nodes, edges, and metadata from *content*.

        Parameters
        ----------
        content:
            The raw text content to extract from.
        filename:
            Optional filename for context.
        metadata:
            Optional metadata dict passed through to the result.
        """
