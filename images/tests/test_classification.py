"""Tests for classification-based access control."""

import json
import sys
import os

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", ".."))

from images.knowledge.classification import ClassificationConfig, DEFAULT_CONFIG
from images.knowledge.store import KnowledgeStore


class TestClassificationConfig:
    def test_loads_default_config(self):
        config = ClassificationConfig()
        assert config.tiers is not None
        assert "public" in config.tiers
        assert "internal" in config.tiers
        assert "restricted" in config.tiers
        assert "confidential" in config.tiers

    def test_get_tier_scope_public(self):
        config = ClassificationConfig()
        scope = config.get_tier_scope("public")
        assert scope == {}

    def test_get_tier_scope_internal(self):
        config = ClassificationConfig()
        scope = config.get_tier_scope("internal")
        assert scope == {"principals": ["role:internal"]}

    def test_get_tier_scope_restricted(self):
        config = ClassificationConfig()
        scope = config.get_tier_scope("restricted")
        assert scope == {"principals": ["role:restricted"]}

    def test_get_tier_scope_confidential(self):
        config = ClassificationConfig()
        scope = config.get_tier_scope("confidential")
        assert scope == {"principals": ["role:confidential"]}

    def test_unknown_classification_defaults_to_internal(self):
        config = ClassificationConfig()
        scope = config.get_tier_scope("top_secret")
        assert scope == {"principals": ["role:internal"]}

    def test_merge_public_adds_nothing(self):
        config = ClassificationConfig()
        original = {"principals": ["agent:scout"]}
        merged = config.merge_classification_scope(original, "public")
        assert merged == {"principals": ["agent:scout"]}

    def test_merge_adds_tier_principals(self):
        config = ClassificationConfig()
        original = {"principals": ["agent:scout"]}
        merged = config.merge_classification_scope(original, "restricted")
        assert "role:restricted" in merged["principals"]
        assert "agent:scout" in merged["principals"]

    def test_merge_unions_existing_principals(self):
        config = ClassificationConfig()
        original = {"principals": ["agent:scout", "role:restricted"]}
        merged = config.merge_classification_scope(original, "restricted")
        # Should union, not duplicate
        assert merged["principals"].count("role:restricted") == 1
        assert "agent:scout" in merged["principals"]

    def test_merge_with_no_classification_returns_original(self):
        config = ClassificationConfig()
        original = {"principals": ["agent:scout"]}
        merged = config.merge_classification_scope(original, None)
        assert merged == original

    def test_merge_with_empty_scope_dict(self):
        config = ClassificationConfig()
        merged = config.merge_classification_scope({}, "restricted")
        assert merged == {"principals": ["role:restricted"]}

    def test_merge_with_none_scope_dict(self):
        config = ClassificationConfig()
        merged = config.merge_classification_scope(None, "restricted")
        assert merged == {"principals": ["role:restricted"]}

    def test_merge_unions_channels(self):
        """Tier scope with channels should union with existing channels."""
        config = ClassificationConfig()
        # Override config to have channels in a tier
        config._config["tiers"]["restricted"]["scope"]["channels"] = ["#sec-ops"]
        original = {"channels": ["#general"], "principals": []}
        merged = config.merge_classification_scope(original, "restricted")
        assert "#sec-ops" in merged["channels"]
        assert "#general" in merged["channels"]

    def test_loads_from_yaml_file(self, tmp_path):
        import yaml
        custom = {
            "version": 1,
            "tiers": {
                "public": {"description": "Open", "scope": {}},
                "secret": {"description": "Secret stuff", "scope": {"principals": ["role:secret"]}},
            },
        }
        config_path = tmp_path / "classification.yaml"
        config_path.write_text(yaml.dump(custom))
        config = ClassificationConfig(config_path=str(config_path))
        assert "secret" in config.tiers
        assert config.get_tier_scope("secret") == {"principals": ["role:secret"]}

    def test_to_dict(self):
        config = ClassificationConfig()
        d = config.to_dict()
        assert d["version"] == 1
        assert "tiers" in d


class TestClassificationStoreIntegration:
    def test_classified_node_excluded_for_unauthorized_principal(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        config = ClassificationConfig()
        store.set_classification_config(config)

        # Add a restricted node
        store.add_node(
            label="secret report",
            kind="document",
            summary="Confidential findings",
            scope={"classification": "restricted", "principals": [], "channels": []},
        )

        # Search as unauthorized principal (no role:restricted)
        results = store.find_nodes(
            "secret report",
            principal={"principals": ["agent:scout"], "channels": []},
        )
        assert len(results) == 0

    def test_classified_node_visible_to_authorized_principal(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        config = ClassificationConfig()
        store.set_classification_config(config)

        store.add_node(
            label="secret report",
            kind="document",
            summary="Confidential findings",
            scope={"classification": "restricted", "principals": [], "channels": []},
        )

        # Search as authorized principal (has role:restricted)
        results = store.find_nodes(
            "secret report",
            principal={"principals": ["role:restricted"], "channels": []},
        )
        assert len(results) == 1
        assert results[0]["label"] == "secret report"

    def test_node_without_classification_unaffected(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        config = ClassificationConfig()
        store.set_classification_config(config)

        store.add_node(
            label="public doc",
            kind="document",
            summary="Open information",
            scope={"principals": [], "channels": []},
        )

        # Any principal can see unclassified nodes (empty scope overlaps all)
        results = store.find_nodes(
            "public doc",
            principal={"principals": ["agent:scout"], "channels": []},
        )
        assert len(results) == 1
