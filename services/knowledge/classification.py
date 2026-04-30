"""Classification-based access control for the knowledge graph.

Maps classification tiers (public/internal/restricted/confidential) to
scope rules. Loaded from ~/.agency/knowledge/classification.yaml.
"""
import os
import json
import logging
import yaml

logger = logging.getLogger(__name__)

DEFAULT_CONFIG = {
    "version": 1,
    "tiers": {
        "public": {"description": "No access restrictions", "scope": {}},
        "internal": {"description": "Any registered principal", "scope": {"principals": ["role:internal"]}},
        "restricted": {"description": "Limited access", "scope": {"principals": ["role:restricted"]}},
        "confidential": {"description": "Need-to-know only", "scope": {"principals": ["role:confidential"]}},
    }
}


class ClassificationConfig:
    def __init__(self, config_path=None):
        self._config = dict(DEFAULT_CONFIG)
        self._path = config_path
        if config_path and os.path.exists(config_path):
            self.reload()

    def reload(self):
        if self._path and os.path.exists(self._path):
            with open(self._path) as f:
                self._config = yaml.safe_load(f) or DEFAULT_CONFIG
            logger.info("Loaded classification config from %s", self._path)

    def get_tier_scope(self, classification):
        """Get the scope dict for a classification tier.
        Returns empty dict for public/unknown-defaults-to-internal."""
        tiers = self._config.get("tiers", {})
        tier = tiers.get(classification)
        if tier is None:
            logger.warning("Unknown classification '%s', defaulting to 'internal'", classification)
            tier = tiers.get("internal", {"scope": {}})
        return tier.get("scope", {})

    def merge_classification_scope(self, scope_dict, classification):
        """Merge tier scope into a node's scope dict.
        Adds tier principals/channels to existing scope. Returns merged dict."""
        if not classification:
            return scope_dict

        tier_scope = self.get_tier_scope(classification)
        if not tier_scope:
            return scope_dict  # public — no restrictions to add

        merged = dict(scope_dict) if scope_dict else {}
        # Union principals
        existing_principals = set(merged.get("principals", []))
        tier_principals = set(tier_scope.get("principals", []))
        if tier_principals:
            merged["principals"] = sorted(existing_principals | tier_principals)
        # Union channels
        existing_channels = set(merged.get("channels", []))
        tier_channels = set(tier_scope.get("channels", []))
        if tier_channels:
            merged["channels"] = sorted(existing_channels | tier_channels)
        return merged

    @property
    def tiers(self):
        return self._config.get("tiers", {})

    def to_dict(self):
        return dict(self._config)
