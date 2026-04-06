"""IngestionPipeline — orchestrates classify, extract, store, and synthesis.

Pipeline steps:
1. Classify content (SourceClassifier)
2. Find a matching extractor
3. Extract nodes and edges
4. Store nodes in the knowledge graph
5. Store edges (resolve labels to node IDs)
6. Optionally queue for LLM synthesis (MergeBuffer gate)
7. Return stats
"""

from __future__ import annotations

import logging
from typing import Optional

try:
    from ingestion.base import BaseExtractor, ExtractionResult
    from ingestion.classifier import SourceClassifier
    from ingestion.extractors.config import ConfigExtractor
    from ingestion.extractors.markdown import MarkdownExtractor
    from ingestion.extractors.structured import StructuredExtractor
    from ingestion.merge_buffer import MergeBuffer
except ImportError:
    from knowledge.ingestion.base import BaseExtractor, ExtractionResult
    from knowledge.ingestion.classifier import SourceClassifier
    from knowledge.ingestion.extractors.config import ConfigExtractor
    from knowledge.ingestion.extractors.markdown import MarkdownExtractor
    from knowledge.ingestion.extractors.structured import StructuredExtractor
    from knowledge.ingestion.merge_buffer import MergeBuffer

logger = logging.getLogger("agency.knowledge.ingestion.pipeline")


class IngestionPipeline:
    """Orchestrates the dual extraction pipeline: deterministic + optional LLM."""

    def __init__(self, store, synthesizer=None):
        """Initialise with a KnowledgeStore and optional LLMSynthesizer.

        Parameters
        ----------
        store:
            A :class:`KnowledgeStore` instance for persisting nodes and edges.
        synthesizer:
            Optional LLM synthesizer.  When present and content warrants it,
            the pipeline queues content for synthesis.
        """
        self.store = store
        self.synthesizer = synthesizer

        # Extractors in order of specificity (most specific first).
        self._extractors: list[BaseExtractor] = [
            ConfigExtractor(),
            MarkdownExtractor(),
            StructuredExtractor(),
        ]

    # ------------------------------------------------------------------
    # Public API
    # ------------------------------------------------------------------

    def ingest(
        self,
        content: str,
        filename: str = "",
        content_type: str = "",
        scope: Optional[dict] = None,
        source_principal: str = "",
    ) -> dict:
        """Ingest content through the dual extraction pipeline.

        Returns a stats dict with keys: ``nodes_created``, ``edges_created``,
        ``source_type``, ``extractor``, ``synthesis_triggered``,
        ``synthesis_skipped``.
        """
        # 1. Classify
        mime_type = SourceClassifier.classify(filename, content_type, content)

        # 2. Find extractor
        extractor = self._find_extractor(mime_type, filename)

        # 3. Extract
        result: ExtractionResult = extractor.extract(content, filename)

        # 4. Store nodes
        nodes_created = 0
        for node in result.nodes:
            try:
                self.store.add_node(
                    label=node["label"],
                    kind=node["kind"],
                    summary=node.get("summary", ""),
                    properties=node.get("properties"),
                    source_type="rule",
                    scope=scope,
                )
                nodes_created += 1
            except Exception:
                logger.warning(
                    "Failed to store node label=%s kind=%s",
                    node.get("label"),
                    node.get("kind"),
                    exc_info=True,
                )

        # 5. Store edges — resolve labels to node IDs
        edges_created = 0
        for edge in result.edges:
            try:
                source_nodes = self.store.find_nodes(edge["source_label"], limit=1)
                target_nodes = self.store.find_nodes(edge["target_label"], limit=1)
                if not source_nodes or not target_nodes:
                    logger.warning(
                        "Edge skipped — could not resolve labels: %s -> %s",
                        edge["source_label"],
                        edge["target_label"],
                    )
                    continue
                self.store.add_edge(
                    source_id=source_nodes[0]["id"],
                    target_id=target_nodes[0]["id"],
                    relation=edge["relation"],
                    provenance=result.default_provenance,
                )
                edges_created += 1
            except Exception:
                logger.warning(
                    "Failed to store edge %s -> %s",
                    edge.get("source_label"),
                    edge.get("target_label"),
                    exc_info=True,
                )

        # 6. MergeBuffer decision
        synthesis_triggered = False
        synthesis_skipped = False
        if MergeBuffer.should_synthesize(result):
            if self.synthesizer is not None:
                prepared = MergeBuffer.prepare_for_synthesis(result)
                try:
                    if hasattr(self.synthesizer, "queue"):
                        self.synthesizer.queue(prepared, filename=filename)
                    else:
                        self.synthesizer.synthesize(prepared, filename=filename)
                    synthesis_triggered = True
                except Exception:
                    logger.warning("Synthesis queueing failed", exc_info=True)
                    synthesis_skipped = True
            else:
                synthesis_skipped = True
        else:
            synthesis_skipped = True

        # 7. Return stats
        return {
            "nodes_created": nodes_created,
            "edges_created": edges_created,
            "source_type": result.source_type,
            "extractor": extractor.name,
            "synthesis_triggered": synthesis_triggered,
            "synthesis_skipped": synthesis_skipped,
        }

    # ------------------------------------------------------------------
    # Internal helpers
    # ------------------------------------------------------------------

    def _find_extractor(self, content_type: str, filename: str) -> BaseExtractor:
        """Return the first extractor that can handle the content type."""
        for ext in self._extractors:
            if ext.can_handle(content_type, filename):
                return ext
        # Should not happen (StructuredExtractor handles text/*), but be safe.
        logger.warning(
            "No extractor found for content_type=%s filename=%s; using structured",
            content_type,
            filename,
        )
        return self._extractors[-1]
