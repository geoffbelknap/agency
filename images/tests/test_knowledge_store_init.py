from pathlib import Path
from unittest.mock import patch


def test_knowledge_store_init_does_not_eagerly_create_embedding_provider(tmp_path: Path):
    from services.knowledge.store import KnowledgeStore

    with patch("services.knowledge.embedding.create_provider", side_effect=AssertionError("should stay lazy")):
        KnowledgeStore(tmp_path)
