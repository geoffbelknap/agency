"""Pydantic models for all Agency file schemas."""

from pathlib import Path

import yaml
from pydantic import ValidationError as PydanticValidationError

from agency_core.exceptions import ValidationError
from agency_core.models.agent import AgentConfig
from agency_core.models.constraints import ConstraintsConfig
from agency_core.models.policy import AgentPolicyConfig, PolicyConfig
from agency_core.models.principal import PrincipalsConfig
from agency_core.models.pack import PackConfig
from agency_core.models.connector import ConnectorConfig
from agency_core.models.mission import Mission
from agency_core.models.preset import PresetConfig
from agency_core.models.workspace import AgentWorkspaceConfig, WorkspaceConfig
from agency_core.models.hub import AgencyConfig, HubConfig, HubInstalledEntry, HubSource

# Map filenames to their schema models
SCHEMA_MAP = {
    "policy.yaml": None,  # determined by context (org vs agent)
    "principals.yaml": PrincipalsConfig,
    "agent.yaml": AgentConfig,
    "constraints.yaml": ConstraintsConfig,
    "preset.yaml": PresetConfig,
    "mission.yaml": Mission,
    "workspace.yaml": None,  # determined by context
    "pack.yaml": PackConfig,
    "connector.yaml": ConnectorConfig,
}


def _detect_schema(path: Path, data: dict):
    """Detect the appropriate schema for a file based on path and content."""
    name = path.name
    if name == "principals.yaml":
        return PrincipalsConfig
    if name == "agent.yaml":
        return AgentConfig
    if name == "constraints.yaml":
        return ConstraintsConfig
    if name == "preset.yaml":
        return PresetConfig
    if name == "mission.yaml":
        return Mission
    if name == "policy.yaml":
        # Use path context: files under agents/ are agent-level
        parts = path.resolve().parts
        if "agents" in parts:
            return AgentPolicyConfig
        # Fallback: agent-level policy has inherits_from; org-level has bundle
        if "bundle" in data:
            return PolicyConfig
        return AgentPolicyConfig
    if name == "pack.yaml":
        return PackConfig
    if name == "connector.yaml":
        return ConnectorConfig
    if name == "workspace.yaml":
        # Use path context: files under agents/ are agent workspace refs
        parts = path.resolve().parts
        if "agents" in parts:
            return AgentWorkspaceConfig
        # Fallback: workspace template has 'base'
        if "base" in data:
            return WorkspaceConfig
        return AgentWorkspaceConfig
    return None


def validate_file(path: Path) -> None:
    """Validate a YAML file against its schema. Raises ValidationError on failure."""
    path = Path(path)
    if not path.exists():
        raise ValidationError(f"File not found: {path}")
    if not path.suffix == ".yaml":
        return  # only validate YAML files

    try:
        with open(path) as f:
            data = yaml.safe_load(f)
    except yaml.YAMLError as e:
        # Extract line/column from PyYAML's problem_mark if available
        mark = getattr(e, "problem_mark", None)
        problem = getattr(e, "problem", str(e))
        if mark:
            raise ValidationError(
                f"YAML syntax error in {path} at line {mark.line + 1}, "
                f"column {mark.column + 1}: {problem}\n"
                f"  Hint: check indentation and special characters near that line"
            )
        raise ValidationError(f"YAML syntax error in {path}: {problem}")

    if data is None:
        raise ValidationError(f"Empty file: {path}")

    schema = _detect_schema(path, data)
    if schema is None:
        return  # no schema for this file, skip

    try:
        schema.model_validate(data)
    except PydanticValidationError as e:
        errors = []
        for err in e.errors():
            loc = " → ".join(str(x) for x in err["loc"])
            errors.append(f"  {loc}: {err['msg']}")
        error_detail = "\n".join(errors)
        hint = ""
        if any("required" in err["msg"].lower() for err in e.errors()):
            hint = "\n  Hint: check for missing required fields"
        if any("extra" in err["type"] for err in e.errors()):
            hint += "\n  Hint: check for misspelled field names"
        raise ValidationError(
            f"Validation failed for {path}:\n{error_detail}{hint}"
        )


__all__ = [
    "PolicyConfig",
    "AgentPolicyConfig",
    "PrincipalsConfig",
    "AgentConfig",
    "ConstraintsConfig",
    "PresetConfig",
    "Mission",
    "WorkspaceConfig",
    "AgentWorkspaceConfig",
    "ConnectorConfig",
    "AgencyConfig",
    "HubConfig",
    "HubInstalledEntry",
    "HubSource",
    "validate_file",
]
