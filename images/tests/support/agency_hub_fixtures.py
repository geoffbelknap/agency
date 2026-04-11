"""Helpers for loading vendored agency-hub connector fixtures in tests."""

from pathlib import Path

import yaml

from images.models.connector import ConnectorConfig


def load_agency_hub_connector(relative_path: str) -> ConnectorConfig:
    tests_root = Path(__file__).resolve().parents[1]
    path = tests_root / "support" / "agency_hub" / relative_path
    return ConnectorConfig.model_validate(yaml.safe_load(path.read_text()))
