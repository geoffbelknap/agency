"""Named policy registry — reusable policy templates.

Operators define named policies in ~/.agency/policies/ as YAML files.
Departments, teams, and agents can reference them via `extends: <name>`
in their policy.yaml. The registry resolves the template and merges
parameters and rules.

Named policies can only restrict relative to the platform defaults —
they follow the same loosening rules as any other policy level.
"""

from pathlib import Path

import yaml

from typing import Optional
from images.tests.support.policy.defaults import HARD_FLOORS, PARAMETER_DEFAULTS, is_hard_floor, is_loosening


class PolicyRegistryError(Exception):
    """Error in named policy operations."""


class PolicyRegistry:
    """Manage named policy templates."""

    def __init__(self, agency_home: Optional[Path] = None):
        self.home = agency_home or Path.home() / ".agency"
        self.policies_dir = self.home / "policies"

    def list_policies(self) -> list[dict]:
        """List all named policies with metadata."""
        if not self.policies_dir.exists():
            return []

        policies = []
        for f in sorted(self.policies_dir.glob("*.yaml")):
            try:
                with open(f) as fh:
                    data = yaml.safe_load(fh) or {}
                policies.append({
                    "name": f.stem,
                    "description": data.get("description", ""),
                    "parameters": data.get("parameters", {}),
                    "rules": data.get("rules", []) + data.get("additions", []),
                    "file": str(f),
                })
            except (yaml.YAMLError, OSError):
                policies.append({
                    "name": f.stem,
                    "description": "(error loading)",
                    "parameters": {},
                    "rules": [],
                    "file": str(f),
                })
        return policies

    def get_policy(self, name: str) -> dict:
        """Load a named policy by name.

        Raises PolicyRegistryError if not found or invalid.
        """
        policy_file = self.policies_dir / f"{name}.yaml"
        if not policy_file.exists():
            raise PolicyRegistryError(f"Named policy '{name}' not found")

        try:
            with open(policy_file) as f:
                data = yaml.safe_load(f) or {}
        except yaml.YAMLError as e:
            raise PolicyRegistryError(f"Invalid YAML in policy '{name}': {e}")

        return data

    def validate_policy(self, name: str) -> list[str]:
        """Validate a named policy template.

        Returns list of issues. Empty = valid.
        """
        issues = []
        try:
            data = self.get_policy(name)
        except PolicyRegistryError as e:
            return [str(e)]

        params = data.get("parameters", {})
        for key, value in params.items():
            # Check hard floors
            if is_hard_floor(key):
                if value != HARD_FLOORS[key]:
                    issues.append(
                        f"Parameter '{key}' is a hard floor "
                        f"(must be '{HARD_FLOORS[key]}')"
                    )
                continue

            # Check against platform defaults for loosening
            spec = PARAMETER_DEFAULTS.get(key)
            if spec is None:
                issues.append(f"Unknown parameter '{key}'")
                continue

            default_value = spec["value"]
            if is_loosening(key, default_value, value):
                issues.append(
                    f"Parameter '{key}' value '{value}' loosens "
                    f"platform default '{default_value}'"
                )

        return issues

    def create_policy(
        self,
        name: str,
        description: str = "",
        parameters: Optional[dict] = None,
        rules: Optional[list] = None,
    ) -> Path:
        """Create a named policy template.

        Validates before saving. Raises PolicyRegistryError on issues.
        """
        self.policies_dir.mkdir(parents=True, exist_ok=True)

        policy_file = self.policies_dir / f"{name}.yaml"
        if policy_file.exists():
            raise PolicyRegistryError(f"Named policy '{name}' already exists")

        data = {
            "description": description or f"Named policy: {name}",
            "parameters": parameters or {},
            "rules": rules or [],
        }

        # Write then validate
        policy_file.write_text(yaml.dump(data, default_flow_style=False))

        issues = self.validate_policy(name)
        if issues:
            policy_file.unlink()
            raise PolicyRegistryError(
                f"Invalid policy: {'; '.join(issues)}"
            )

        return policy_file

    def delete_policy(self, name: str) -> None:
        """Delete a named policy template."""
        policy_file = self.policies_dir / f"{name}.yaml"
        if not policy_file.exists():
            raise PolicyRegistryError(f"Named policy '{name}' not found")
        policy_file.unlink()

    def resolve_extends(self, policy_data: dict) -> dict:
        """Resolve `extends` references in a policy.

        If policy_data has an `extends` key referencing a named policy,
        merge the template's parameters and rules as the base, then
        overlay the policy's own parameters and rules on top.

        Returns merged policy data. The original is not modified.
        """
        extends = policy_data.get("extends")
        if not extends:
            return policy_data

        try:
            template = self.get_policy(extends)
        except PolicyRegistryError:
            return policy_data

        # Merge: template is base, policy_data overlays
        merged_params = dict(template.get("parameters", {}))
        merged_params.update(policy_data.get("parameters", {}))

        merged_rules = list(template.get("rules", []) + template.get("additions", []))
        merged_rules.extend(policy_data.get("rules", []) + policy_data.get("additions", []))

        result = dict(policy_data)
        result["parameters"] = merged_params
        result["rules"] = merged_rules
        # Remove additions to avoid double-counting
        result.pop("additions", None)

        return result
