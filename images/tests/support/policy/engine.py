"""Policy computation engine.

Computes effective policy for an agent by walking the full inheritance
chain and validating all exceptions.

Resolution order (most specific wins, but can only restrict):
  1. Platform defaults (always present, cannot be removed)
  2. Org policy (org/policy.yaml)
  3. Department policy (departments/<name>/policy.yaml) — if exists
  4. Team policy (teams/<name>/policy.yaml) — if exists
  5. Agent policy (agents/<name>/policy.yaml) — if exists

Rules:
  - Lower levels can only restrict, never expand
  - Hard floors cannot be modified at any level
  - Exceptions require both keys (delegation grant + exercise)
  - Grant expiry immediately invalidates child exceptions
"""

from dataclasses import dataclass, field
from datetime import datetime, timezone
from pathlib import Path

import yaml

from typing import Optional
from images.exceptions import AgencyError
from images.tests.support.policy.defaults import (
    DEFAULT_RULES,
    HARD_FLOORS,
    PARAMETER_DEFAULTS,
    is_hard_floor,
    is_loosening,
)
from images.tests.support.policy.registry import PolicyRegistry


class PolicyViolation(AgencyError):
    """Policy chain invalid — cannot start agent."""

    def __init__(self, file: str, issue: str, detail: str = "", fix: str = ""):
        self.violation_file = file
        self.issue = issue
        self.detail = detail
        message = f"POLICY VIOLATION:\n  File: {file}\n  Issue: {issue}"
        if detail:
            message += f"\n  {detail}"
        super().__init__(message, fix=fix)


@dataclass
class PolicyStep:
    """A single step in the policy resolution chain."""
    level: str  # "platform", "org", "department", "team", "agent"
    file: Optional[str]  # Path to the policy file, or None for platform
    status: str  # "ok", "missing", "violation"
    detail: str = ""


@dataclass
class ExceptionInfo:
    """Info about a policy exception."""
    exception_id: str
    grant_ref: str
    parameter: str
    granted_value: str
    status: str  # "active", "expired", "invalid"
    detail: str = ""


@dataclass
class ValidationResult:
    """Result of policy chain validation."""
    valid: bool
    steps: list[PolicyStep] = field(default_factory=list)
    violations: list[PolicyViolation] = field(default_factory=list)
    hard_floors_ok: bool = True
    parameters_ok: bool = True
    additions_ok: bool = True
    exceptions: list[ExceptionInfo] = field(default_factory=list)
    active_exceptions: int = 0
    expired_exceptions: int = 0


@dataclass
class EffectivePolicy:
    """The computed effective policy for an agent — sealed and immutable."""
    agent: str
    parameters: dict = field(default_factory=dict)
    rules: list[dict] = field(default_factory=list)
    hard_floors: dict = field(default_factory=dict)
    exceptions: list[ExceptionInfo] = field(default_factory=list)
    chain: list[PolicyStep] = field(default_factory=list)
    sealed: bool = False

    def seal(self):
        self.sealed = True


class PolicyEngine:
    """Compute and validate effective policy for agents."""

    def __init__(self, agency_home: Optional[Path] = None):
        self.home = agency_home or Path.home() / ".agency"
        self._registry = PolicyRegistry(self.home)

    def compute(self, agent_name: str) -> EffectivePolicy:
        """Walk policy chain, validate, return sealed effective policy.

        Raises PolicyViolation if any tenet is broken.
        """
        result = self.validate_chain(agent_name)
        if not result.valid:
            raise result.violations[0]

        policy = EffectivePolicy(
            agent=agent_name,
            parameters=self._resolved_parameters(agent_name),
            rules=self._resolved_rules(agent_name),
            hard_floors=dict(HARD_FLOORS),
            exceptions=result.exceptions,
            chain=result.steps,
        )
        policy.seal()
        return policy

    def validate_chain(self, agent_name: str) -> ValidationResult:
        """Validate the full policy chain without computing.

        Returns a detailed report suitable for `agency policy check`.
        """
        result = ValidationResult(valid=True)

        # Step 1: Platform defaults (always present)
        result.steps.append(PolicyStep(
            level="platform",
            file=None,
            status="ok",
            detail="platform defaults",
        ))

        # Step 2: Org policy
        org_policy_file = self.home / "policy.yaml"
        org_policy = self._load_and_validate_level(
            org_policy_file, "org", result
        )

        # Step 3: Department policy (if referenced)
        agent_dir = self.home / "agents" / agent_name
        agent_policy_file = agent_dir / "policy.yaml"
        dept_name = self._get_department(agent_name)
        dept_policy = None
        # Track the most specific parent for loosening checks
        effective_parent = org_policy
        if dept_name:
            dept_file = self.home / "departments" / dept_name / "policy.yaml"
            dept_policy = self._load_and_validate_level(
                dept_file, "department", result, parent_params=org_policy
            )
            if dept_policy:
                effective_parent = dept_policy
        else:
            result.steps.append(PolicyStep(
                level="department", file=None, status="missing",
                detail="no department policy",
            ))

        # Step 4: Team policy (if referenced)
        team_name = self._get_team(agent_name)
        team_policy = None
        if team_name:
            team_file = self.home / "teams" / team_name / "policy.yaml"
            team_policy = self._load_and_validate_level(
                team_file, "team", result, parent_params=effective_parent
            )
            if team_policy:
                effective_parent = team_policy
        else:
            result.steps.append(PolicyStep(
                level="team", file=None, status="missing",
                detail="no team policy",
            ))

        # Step 5: Agent policy
        if agent_policy_file.exists():
            self._load_and_validate_level(
                agent_policy_file, "agent", result, parent_params=effective_parent
            )
        else:
            result.steps.append(PolicyStep(
                level="agent", file=str(agent_policy_file), status="missing",
                detail="no agent policy",
            ))

        # Validate exceptions
        self._validate_exceptions(agent_name, result)

        # Check hard floors across entire chain
        self._validate_hard_floors(agent_name, result)

        return result

    def check_exception(self, agent_name: str, exception_id: str) -> ExceptionInfo:
        """Validate a specific exception — both keys present and valid."""
        agent_dir = self.home / "agents" / agent_name
        policy_file = agent_dir / "policy.yaml"

        if not policy_file.exists():
            return ExceptionInfo(
                exception_id=exception_id,
                grant_ref="",
                parameter="",
                granted_value="",
                status="invalid",
                detail="No agent policy file found",
            )

        with open(policy_file) as f:
            policy = yaml.safe_load(f) or {}

        exceptions = policy.get("exceptions", [])
        for exc in exceptions:
            if exc.get("exception_id") == exception_id:
                return self._validate_single_exception(exc)

        return ExceptionInfo(
            exception_id=exception_id,
            grant_ref="",
            parameter="",
            granted_value="",
            status="invalid",
            detail=f"Exception '{exception_id}' not found",
        )

    def _load_and_validate_level(
        self, file: Path, level: str, result: ValidationResult,
        parent_params: Optional[dict] = None,
    ) -> Optional[dict]:
        """Load a policy file and validate it against rules."""
        if not file.exists():
            result.steps.append(PolicyStep(
                level=level, file=str(file), status="missing",
                detail=f"no {level} policy",
            ))
            return None

        try:
            with open(file) as f:
                policy = yaml.safe_load(f) or {}
        except yaml.YAMLError as e:
            result.valid = False
            step = PolicyStep(
                level=level, file=str(file), status="violation",
                detail=f"Invalid YAML: {e}",
            )
            result.steps.append(step)
            result.violations.append(PolicyViolation(
                str(file), "Invalid YAML syntax", str(e),
                fix=f"Fix YAML syntax in {file}",
            ))
            return None

        # Check for parameter loosening
        params = policy.get("parameters", {})
        if parent_params and isinstance(parent_params, dict):
            parent_p = parent_params.get("parameters", {})
            for key, value in params.items():
                if key in parent_p and is_loosening(key, parent_p[key], value):
                    result.valid = False
                    violation = PolicyViolation(
                        str(file),
                        f"parameter '{key}' loosened",
                        f"Parent sets: \"{parent_p[key]}\"\n  {level.title()} sets: \"{value}\" <- INVALID - lower level cannot loosen",
                        fix=f"Remove '{key}' from {file}, or set to \"{parent_p[key]}\" or more restrictive",
                    )
                    result.steps.append(PolicyStep(
                        level=level, file=str(file), status="violation",
                        detail=violation.issue,
                    ))
                    result.violations.append(violation)
                    return policy

        # Check hard floors aren't modified
        for key in HARD_FLOORS:
            if key in params:
                floor_val = HARD_FLOORS[key]
                if params[key] != floor_val:
                    result.valid = False
                    violation = PolicyViolation(
                        str(file),
                        f"hard floor '{key}' modified",
                        f"Platform default: \"{floor_val}\"\n  {level.title()} sets: \"{params[key]}\" <- INVALID - hard floor cannot be changed",
                        fix=f"Remove '{key}' from {file} - hard floors cannot be modified at any level",
                    )
                    result.steps.append(PolicyStep(
                        level=level, file=str(file), status="violation",
                        detail=violation.issue,
                    ))
                    result.violations.append(violation)
                    return policy

        result.steps.append(PolicyStep(
            level=level, file=str(file), status="ok",
        ))
        return policy

    def _validate_exceptions(self, agent_name: str, result: ValidationResult) -> None:
        """Validate all exceptions in the agent's policy."""
        agent_dir = self.home / "agents" / agent_name
        policy_file = agent_dir / "policy.yaml"

        if not policy_file.exists():
            return

        with open(policy_file) as f:
            policy = yaml.safe_load(f) or {}

        exceptions = policy.get("exceptions", [])
        for exc in exceptions:
            info = self._validate_single_exception(exc)
            result.exceptions.append(info)
            if info.status == "active":
                result.active_exceptions += 1
            elif info.status == "expired":
                result.expired_exceptions += 1
            elif info.status == "invalid":
                result.valid = False
                result.violations.append(PolicyViolation(
                    str(policy_file),
                    f"Invalid exception: {info.exception_id}",
                    info.detail,
                ))

    def _validate_single_exception(self, exc: dict) -> ExceptionInfo:
        """Validate a single exception entry."""
        exception_id = exc.get("exception_id", "unknown")
        grant_ref = exc.get("grant_ref", "")
        parameter = exc.get("parameter", "")
        granted_value = exc.get("granted_value", "")
        expires = exc.get("expires", "")

        # Hard floors cannot be overridden by exceptions
        if parameter and is_hard_floor(parameter):
            return ExceptionInfo(
                exception_id=exception_id,
                grant_ref=grant_ref,
                parameter=parameter,
                granted_value=str(granted_value),
                status="invalid",
                detail=f"Parameter '{parameter}' is a hard floor and cannot be overridden by exception",
            )

        # Check Key 1: delegation grant exists
        if not grant_ref:
            return ExceptionInfo(
                exception_id=exception_id,
                grant_ref="",
                parameter=parameter,
                granted_value=str(granted_value),
                status="invalid",
                detail="Missing grant_ref (Key 1) - exception requires delegation grant",
            )

        # Check grant exists in org policy
        grant = self._find_delegation_grant(grant_ref)
        if grant is None:
            return ExceptionInfo(
                exception_id=exception_id,
                grant_ref=grant_ref,
                parameter=parameter,
                granted_value=str(granted_value),
                status="invalid",
                detail=f"Delegation grant '{grant_ref}' not found in org policy",
            )

        # Check Key 2: required fields present
        if not exc.get("approved_by"):
            return ExceptionInfo(
                exception_id=exception_id,
                grant_ref=grant_ref,
                parameter=parameter,
                granted_value=str(granted_value),
                status="invalid",
                detail="Missing approved_by (Key 2) - exception requires approval",
            )

        # Check expiry
        if expires:
            try:
                expiry_dt = datetime.fromisoformat(str(expires).replace("Z", "+00:00"))
                # Ensure timezone-aware for comparison
                if expiry_dt.tzinfo is None:
                    expiry_dt = expiry_dt.replace(tzinfo=timezone.utc)
                if expiry_dt < datetime.now(timezone.utc):
                    return ExceptionInfo(
                        exception_id=exception_id,
                        grant_ref=grant_ref,
                        parameter=parameter,
                        granted_value=str(granted_value),
                        status="expired",
                        detail=f"Expired on {expires}",
                    )
            except ValueError:
                return ExceptionInfo(
                    exception_id=exception_id,
                    grant_ref=grant_ref,
                    parameter=parameter,
                    granted_value=str(granted_value),
                    status="invalid",
                    detail=f"Unparseable expiry date: {expires}",
                )

        # Check grant expiry — expired grants invalidate child exceptions.
        # max_expiry can be an ISO date (enforced) or a human-readable duration
        # like "6 months" (documentation only, not enforced).
        grant_expiry = grant.get("constraints", {}).get("max_expiry")
        if grant_expiry:
            grant_expiry_str = str(grant_expiry)
            # Only enforce if it looks like an ISO date (starts with a digit, contains -)
            if grant_expiry_str and grant_expiry_str[0].isdigit():
                try:
                    grant_expiry_dt = datetime.fromisoformat(
                        grant_expiry_str.replace("Z", "+00:00")
                    )
                    if grant_expiry_dt.tzinfo is None:
                        grant_expiry_dt = grant_expiry_dt.replace(tzinfo=timezone.utc)
                    if grant_expiry_dt < datetime.now(timezone.utc):
                        return ExceptionInfo(
                            exception_id=exception_id,
                            grant_ref=grant_ref,
                            parameter=parameter,
                            granted_value=str(granted_value),
                            status="expired",
                            detail=f"Delegation grant expired on {grant_expiry}",
                        )
                except ValueError:
                    pass  # Non-ISO date string starting with digit — skip

        return ExceptionInfo(
            exception_id=exception_id,
            grant_ref=grant_ref,
            parameter=parameter,
            granted_value=str(granted_value),
            status="active",
        )

    def _find_delegation_grant(self, grant_ref: str) -> Optional[dict]:
        """Find a delegation grant in org policy."""
        org_policy_file = self.home / "policy.yaml"
        if not org_policy_file.exists():
            return None

        with open(org_policy_file) as f:
            policy = yaml.safe_load(f) or {}

        grants = policy.get("delegation_grants", [])
        for grant in grants:
            if grant.get("grant_id") == grant_ref:
                return grant
        return None

    def _validate_hard_floors(self, agent_name: str, result: ValidationResult) -> None:
        """Verify hard floors are intact across entire chain."""
        # Hard floors are checked in _load_and_validate_level
        # This is a final sweep
        result.hard_floors_ok = all(
            step.status != "violation" or "hard floor" not in step.detail
            for step in result.steps
        )
        result.parameters_ok = all(
            step.status != "violation" or "loosened" not in step.detail
            for step in result.steps
        )

    def _resolved_parameters(self, agent_name: str) -> dict:
        """Walk the chain and resolve effective parameters.

        Resolution order: platform > org > department > team > agent.
        Each level can only restrict (tighten), never loosen.
        """
        params = {k: v["value"] for k, v in PARAMETER_DEFAULTS.items()}

        # Apply org policy
        org_params = self._load_params(self.home / "policy.yaml")
        for k, v in org_params.items():
            if k in params and not is_loosening(k, params[k], v):
                params[k] = v

        # Apply department policy
        dept_name = self._get_department(agent_name)
        if dept_name:
            dept_params = self._load_params(
                self.home / "departments" / dept_name / "policy.yaml"
            )
            for k, v in dept_params.items():
                if k in params and not is_loosening(k, params[k], v):
                    params[k] = v

        # Apply team policy
        team_name = self._get_team(agent_name)
        if team_name:
            team_params = self._load_params(
                self.home / "teams" / team_name / "policy.yaml"
            )
            for k, v in team_params.items():
                if k in params and not is_loosening(k, params[k], v):
                    params[k] = v

        # Apply agent policy
        agent_params = self._load_params(
            self.home / "agents" / agent_name / "policy.yaml"
        )
        for k, v in agent_params.items():
            if k in params and not is_loosening(k, params[k], v):
                params[k] = v

        return params

    def _resolved_rules(self, agent_name: str) -> list[dict]:
        """Walk the chain and collect all rules (additive only).

        Rules accumulate: platform + org + department + team + agent.
        """
        rules = list(DEFAULT_RULES)

        # Add org rules
        org_rules = self._load_rules(self.home / "policy.yaml")
        rules.extend(org_rules)

        # Add department rules
        dept_name = self._get_department(agent_name)
        if dept_name:
            dept_rules = self._load_rules(
                self.home / "departments" / dept_name / "policy.yaml"
            )
            rules.extend(dept_rules)

        # Add team rules
        team_name = self._get_team(agent_name)
        if team_name:
            team_rules = self._load_rules(
                self.home / "teams" / team_name / "policy.yaml"
            )
            rules.extend(team_rules)

        # Add agent rules
        agent_rules = self._load_rules(
            self.home / "agents" / agent_name / "policy.yaml"
        )
        rules.extend(agent_rules)

        return rules

    def _load_policy_data(self, file: Path) -> dict:
        """Load and resolve a policy file (including extends)."""
        if not file.exists():
            return {}
        with open(file) as f:
            policy = yaml.safe_load(f) or {}
        return self._registry.resolve_extends(policy)

    def _load_params(self, file: Path) -> dict:
        """Load parameters from a policy file."""
        policy = self._load_policy_data(file)
        return policy.get("parameters", {})

    def _load_rules(self, file: Path) -> list[dict]:
        """Load rules from a policy file."""
        policy = self._load_policy_data(file)
        additions = policy.get("additions", [])
        rules = policy.get("rules", [])
        return additions + rules

    @staticmethod
    def _validate_hierarchy_name(name: str) -> bool:
        """Validate a department/team name extracted from inherits_from."""
        import re
        return bool(re.match(r"^[a-z0-9]([a-z0-9-]*[a-z0-9])?$", name)) and len(name) >= 2

    def _get_department(self, agent_name: str) -> Optional[str]:
        """Get the department for an agent (from agent.yaml)."""
        agent_yaml = self.home / "agents" / agent_name / "agent.yaml"
        if not agent_yaml.exists():
            return None
        with open(agent_yaml) as f:
            config = yaml.safe_load(f) or {}
        inherits = config.get("policy", {}).get("inherits_from", "")
        if "departments/" in inherits:
            parts = inherits.split("/")
            idx = parts.index("departments") if "departments" in parts else -1
            if idx >= 0 and idx + 1 < len(parts):
                name = parts[idx + 1]
                if not self._validate_hierarchy_name(name):
                    return None
                return name
        return None

    def _get_team(self, agent_name: str) -> Optional[str]:
        """Get the team for an agent (from agent.yaml)."""
        agent_yaml = self.home / "agents" / agent_name / "agent.yaml"
        if not agent_yaml.exists():
            return None
        with open(agent_yaml) as f:
            config = yaml.safe_load(f) or {}
        inherits = config.get("policy", {}).get("inherits_from", "")
        if "teams/" in inherits:
            parts = inherits.split("/")
            idx = parts.index("teams") if "teams" in parts else -1
            if idx >= 0 and idx + 1 < len(parts):
                name = parts[idx + 1]
                if not self._validate_hierarchy_name(name):
                    return None
                return name
        return None
