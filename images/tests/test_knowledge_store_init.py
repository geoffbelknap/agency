from pathlib import Path
from unittest.mock import patch


def test_knowledge_store_init_does_not_eagerly_create_embedding_provider(tmp_path: Path):
    try:
        from services.knowledge.store import KnowledgeStore
        patch_target = "knowledge.embedding.create_provider"
    except ImportError:
        from services.knowledge.store import KnowledgeStore
        patch_target = "services.knowledge.embedding.create_provider"

    with patch(patch_target, side_effect=AssertionError("should stay lazy")):
        KnowledgeStore(tmp_path)
