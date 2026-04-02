"""Tests for asset inventory ontology types."""
from unittest.mock import patch


class TestKnowledgeToolsAliases:
    def test_software_accepted(self):
        from images.body.knowledge_tools import _validate_kind
        with patch("images.body.knowledge_tools._load_ontology", return_value={
            "entity_types": {"software": {}, "config_item": {}, "behavior_pattern": {}, "fact": {}, "system": {}}
        }):
            assert _validate_kind("software") == "software"

    def test_app_maps_to_software(self):
        from images.body.knowledge_tools import _validate_kind
        with patch("images.body.knowledge_tools._load_ontology", return_value={
            "entity_types": {"software": {}, "system": {}, "fact": {}}
        }):
            assert _validate_kind("app") == "software"

    def test_config_maps_to_config_item(self):
        from images.body.knowledge_tools import _validate_kind
        with patch("images.body.knowledge_tools._load_ontology", return_value={
            "entity_types": {"config_item": {}, "fact": {}}
        }):
            assert _validate_kind("config") == "config_item"

    def test_behavior_maps_to_behavior_pattern(self):
        from images.body.knowledge_tools import _validate_kind
        with patch("images.body.knowledge_tools._load_ontology", return_value={
            "entity_types": {"behavior_pattern": {}, "fact": {}}
        }):
            assert _validate_kind("behavior") == "behavior_pattern"

    def test_package_maps_to_software(self):
        from images.body.knowledge_tools import _validate_kind
        with patch("images.body.knowledge_tools._load_ontology", return_value={
            "entity_types": {"software": {}, "fact": {}}
        }):
            assert _validate_kind("package") == "software"

    def test_application_still_maps_to_system(self):
        """Backward compat: 'application' remains mapped to 'system'."""
        from images.body.knowledge_tools import _validate_kind
        with patch("images.body.knowledge_tools._load_ontology", return_value={
            "entity_types": {"system": {}, "software": {}, "fact": {}}
        }):
            assert _validate_kind("application") == "system"
