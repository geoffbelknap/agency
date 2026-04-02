"""Tests for file schema validation."""

import pytest
from pathlib import Path

import yaml

from images.exceptions import ValidationError
from images.models import validate_file
from images.models.agent import AgentConfig


def test_validate_valid_org_yaml(tmp_path):
    f = tmp_path / "org.yaml"
    f.write_text(yaml.dump({
        "version": "0.1",
        "name": "Test Agency",
        "operator": "testuser",
        "created": "2026-01-01T00:00:00Z",
        "deployment_mode": "standalone",
    }))
    validate_file(f)  # should not raise


def test_validate_invalid_org_yaml(tmp_path):
    """org.yaml has no registered schema, so validate_file skips it.

    Use agent.yaml (which has a schema) to test that invalid data raises.
    """
    f = tmp_path / "agent.yaml"
    f.write_text(yaml.dump({
        "version": "0.1",
        # missing required fields: name, role, body, workspace
    }))
    with pytest.raises(ValidationError, match="Validation failed"):
        validate_file(f)


def test_validate_valid_agent_yaml(tmp_path):
    f = tmp_path / "agent.yaml"
    f.write_text(yaml.dump({
        "version": "0.1",
        "name": "dev-assistant",
        "role": "assistant",
        "tier": "standard",
        "type": "standard",
        "body": {"runtime": "body", "version": ">=1.0"},
        "workspace": {"ref": "ubuntu-default"},
    }))
    validate_file(f)  # should not raise


def test_validate_invalid_agent_name():
    with pytest.raises(Exception):
        AgentConfig(
            name="BAD NAME",
            role="assistant",
            body={"runtime": "body", "version": ">=1.0"},
            workspace={"ref": "ubuntu-default"},
        )


def test_validate_invalid_tier(tmp_path):
    f = tmp_path / "agent.yaml"
    f.write_text(yaml.dump({
        "version": "0.1",
        "name": "dev-assistant",
        "role": "assistant",
        "tier": "high",  # invalid
        "type": "standard",
        "body": {"runtime": "body", "version": ">=1.0"},
        "workspace": {"ref": "ubuntu-default"},
    }))
    with pytest.raises(ValidationError, match="Validation failed"):
        validate_file(f)


def test_validate_valid_constraints_yaml(tmp_path):
    f = tmp_path / "constraints.yaml"
    f.write_text(yaml.dump({
        "version": "0.1",
        "agent": "dev-assistant",
        "identity": {"role": "assistant", "purpose": "General purpose"},
        "hard_limits": [
            {"rule": "never delete files", "reason": "safety"},
        ],
    }))
    validate_file(f)  # should not raise


def test_validate_missing_file(tmp_path):
    f = tmp_path / "nonexistent.yaml"
    with pytest.raises(ValidationError, match="File not found"):
        validate_file(f)


def test_validate_empty_file(tmp_path):
    f = tmp_path / "org.yaml"
    f.write_text("")
    with pytest.raises(ValidationError, match="Empty file"):
        validate_file(f)


def test_validate_invalid_yaml_syntax(tmp_path):
    f = tmp_path / "org.yaml"
    f.write_text("{{bad yaml: [")
    with pytest.raises(ValidationError, match="YAML syntax error"):
        validate_file(f)


def test_validate_skips_non_yaml(tmp_path):
    f = tmp_path / "identity.md"
    f.write_text("# Hello")
    validate_file(f)  # should not raise, skips non-yaml


def test_validate_workspace_template(tmp_path):
    f = tmp_path / "workspace.yaml"
    f.write_text(yaml.dump({
        "name": "ubuntu-default",
        "version": "1.0",
        "base": {
            "image": "ubuntu:24.04",
            "user": "agent",
            "filesystem": "readonly-root",
        },
    }))
    validate_file(f)  # should not raise


def test_validate_agent_workspace_ref(tmp_path):
    f = tmp_path / "workspace.yaml"
    f.write_text(yaml.dump({
        "version": "0.1",
        "agent": "dev-assistant",
        "workspace_ref": "ubuntu-default",
    }))
    validate_file(f)  # should not raise
