"""Tests for the policy computation engine."""

import pytest
import yaml

from images.tests.support.policy.defaults import (
    HARD_FLOORS,
    PARAMETER_DEFAULTS,
    is_hard_floor,
    is_loosening,
)
from images.tests.support.policy.engine import PolicyEngine, PolicyViolation
from images.tests.support.policy.registry import PolicyRegistry, PolicyRegistryError


@pytest.fixture
def policy_home(tmp_path):
    """Create a minimal Agency structure for policy testing."""
    home = tmp_path / ".agency"
    home.mkdir()

    # Org policy
    org_policy = {
        "agency_version": "0.1",
        "parameters": {
            "risk_tolerance": "medium",
            "max_concurrent_tasks": 5,
        },
        "rules": [],
    }
    (home / "policy.yaml").write_text(yaml.dump(org_policy))

    # Agent
    agent_dir = home / "agents" / "test-agent"
    agent_dir.mkdir(parents=True)

    agent_yaml = {
        "agency_version": "0.1",
        "name": "test-agent",
        "role": "test",
        "tier": "standard",
        "workspace": {"ref": "ubuntu-default"},
        "requires": {"tools": ["git"]},
        "policy": {"inherits_from": ""},
    }
    (agent_dir / "agent.yaml").write_text(yaml.dump(agent_yaml))

    # Agent policy (valid — only restricts)
    agent_policy = {
        "agency_version": "0.1",
        "parameters": {
            "risk_tolerance": "low",  # More restrictive — valid
            "max_concurrent_tasks": 3,  # More restrictive — valid
        },
        "additions": [
            {"rule": "all PRs must be draft", "applies_to": ["git_push"]},
        ],
    }
    (agent_dir / "policy.yaml").write_text(yaml.dump(agent_policy))

    return home


class TestPolicyChainResolution:
    def test_valid_chain_passes(self, policy_home):
        engine = PolicyEngine(agency_home=policy_home)
        result = engine.validate_chain("test-agent")

        assert result.valid
        assert len(result.violations) == 0

        # Check steps
        levels = [s.level for s in result.steps]
        assert "platform" in levels
        assert "org" in levels
        assert "agent" in levels

    def test_resolution_order(self, policy_home):
        """Verify correct resolution order: platform > org > agent."""
        engine = PolicyEngine(agency_home=policy_home)
        result = engine.validate_chain("test-agent")

        ok_steps = [s for s in result.steps if s.status == "ok"]
        levels = [s.level for s in ok_steps]
        assert levels.index("platform") < levels.index("org")
        assert levels.index("org") < levels.index("agent")

    def test_missing_department_and_team_noted(self, policy_home):
        engine = PolicyEngine(agency_home=policy_home)
        result = engine.validate_chain("test-agent")

        missing = [s for s in result.steps if s.status == "missing"]
        missing_levels = [s.level for s in missing]
        assert "department" in missing_levels
        assert "team" in missing_levels


class TestParameterLoosening:
    def test_loosening_rejected(self, policy_home):
        """Verify lower level cannot loosen bounded parameter."""
        # Modify agent policy to loosen risk_tolerance
        agent_dir = policy_home / "agents" / "test-agent"
        bad_policy = {
            "parameters": {
                "risk_tolerance": "high",  # Loosened — INVALID
            },
        }
        (agent_dir / "policy.yaml").write_text(yaml.dump(bad_policy))

        engine = PolicyEngine(agency_home=policy_home)
        result = engine.validate_chain("test-agent")

        assert not result.valid
        assert len(result.violations) > 0
        assert "loosened" in result.violations[0].issue

    def test_restriction_allowed(self, policy_home):
        """Verify lower level can restrict parameters."""
        engine = PolicyEngine(agency_home=policy_home)
        result = engine.validate_chain("test-agent")
        assert result.valid  # Agent sets "low" which is more restrictive

    def test_numeric_loosening_detected(self, policy_home):
        agent_dir = policy_home / "agents" / "test-agent"
        bad_policy = {
            "parameters": {
                "max_concurrent_tasks": 10,  # Org sets 5 — loosened
            },
        }
        (agent_dir / "policy.yaml").write_text(yaml.dump(bad_policy))

        engine = PolicyEngine(agency_home=policy_home)
        result = engine.validate_chain("test-agent")

        assert not result.valid

    def test_is_loosening_function(self):
        assert is_loosening("risk_tolerance", "medium", "high") is True
        assert is_loosening("risk_tolerance", "medium", "low") is False
        assert is_loosening("risk_tolerance", "low", "medium") is True
        assert is_loosening("max_concurrent_tasks", 5, 10) is True
        assert is_loosening("max_concurrent_tasks", 5, 3) is False


class TestHardFloors:
    def test_hard_floor_violation_rejected(self, policy_home):
        """Verify hard floor cannot be modified at any level."""
        agent_dir = policy_home / "agents" / "test-agent"
        bad_policy = {
            "parameters": {
                "logging": "optional",  # Hard floor — INVALID
            },
        }
        (agent_dir / "policy.yaml").write_text(yaml.dump(bad_policy))

        engine = PolicyEngine(agency_home=policy_home)
        result = engine.validate_chain("test-agent")

        assert not result.valid
        assert any("hard floor" in v.issue for v in result.violations)

    def test_hard_floors_exist(self):
        assert is_hard_floor("logging")
        assert is_hard_floor("constraints_readonly")
        assert is_hard_floor("llm_credentials_isolated")
        assert is_hard_floor("network_mediation")
        assert not is_hard_floor("risk_tolerance")

    def test_hard_floor_exception_rejected(self, policy_home):
        """Verify exceptions targeting hard floor params are rejected even with valid grants."""
        for hard_floor_param in ("logging", "constraints_readonly",
                                 "llm_credentials_isolated", "network_mediation"):
            org_policy = {
                "parameters": {hard_floor_param: HARD_FLOORS[hard_floor_param]},
                "delegation_grants": [
                    {
                        "grant_id": f"grant-{hard_floor_param}",
                        "delegated_to": "agents",
                        "scope": {"parameter": hard_floor_param},
                        "constraints": {"max_expiry": "2028-01-01"},
                    },
                ],
            }
            (policy_home / "policy.yaml").write_text(yaml.dump(org_policy))

            agent_dir = policy_home / "agents" / "test-agent"
            agent_policy = {
                "exceptions": [
                    {
                        "exception_id": f"exc-{hard_floor_param}",
                        "grant_ref": f"grant-{hard_floor_param}",
                        "parameter": hard_floor_param,
                        "granted_value": "optional" if isinstance(
                            HARD_FLOORS[hard_floor_param], str
                        ) else False,
                        "approved_by": "operator",
                        "expires": "2027-06-01",
                        "reason": "testing hard floor bypass",
                    },
                ],
            }
            (agent_dir / "policy.yaml").write_text(yaml.dump(agent_policy))

            engine = PolicyEngine(agency_home=policy_home)
            result = engine.validate_chain("test-agent")

            assert not result.valid, (
                f"Hard floor {hard_floor_param} should not be bypassable via exception"
            )


class TestExceptions:
    def test_exception_requires_both_keys(self, policy_home):
        """Verify exception without valid delegation grant is rejected."""
        agent_dir = policy_home / "agents" / "test-agent"
        bad_policy = {
            "exceptions": [
                {
                    "exception_id": "test-exc",
                    "grant_ref": "nonexistent-grant",
                    "parameter": "max_concurrent_tasks",
                    "granted_value": 15,
                    "approved_by": "operator",
                    "expires": "2027-01-01",
                },
            ],
        }
        (agent_dir / "policy.yaml").write_text(yaml.dump(bad_policy))

        engine = PolicyEngine(agency_home=policy_home)
        result = engine.validate_chain("test-agent")

        assert not result.valid
        invalid_exc = [e for e in result.exceptions if e.status == "invalid"]
        assert len(invalid_exc) > 0

    def test_valid_exception_with_grant(self, policy_home):
        """Verify exception with valid delegation grant passes."""
        # Add delegation grant to org policy
        org_policy = {
            "parameters": {"risk_tolerance": "medium", "max_concurrent_tasks": 5},
            "delegation_grants": [
                {
                    "grant_id": "eng-task-scaling",
                    "delegated_to": "agents",
                    "scope": {"parameter": "max_concurrent_tasks", "max_value": 20},
                    "constraints": {"requires_reason": True, "max_expiry": "6 months"},
                },
            ],
        }
        (policy_home / "policy.yaml").write_text(yaml.dump(org_policy))

        # Add exception to agent policy
        agent_dir = policy_home / "agents" / "test-agent"
        agent_policy = {
            "parameters": {"risk_tolerance": "low"},
            "exceptions": [
                {
                    "exception_id": "test-exc",
                    "grant_ref": "eng-task-scaling",
                    "parameter": "max_concurrent_tasks",
                    "granted_value": 15,
                    "approved_by": "operator",
                    "approved_date": "2026-02-22",
                    "expires": "2027-08-22",
                    "reason": "parallel test execution",
                },
            ],
        }
        (agent_dir / "policy.yaml").write_text(yaml.dump(agent_policy))

        engine = PolicyEngine(agency_home=policy_home)
        result = engine.validate_chain("test-agent")

        assert result.valid
        assert result.active_exceptions == 1

    def test_grant_expiry_invalidates_exceptions(self, policy_home):
        """Verify expired exception is flagged."""
        org_policy = {
            "parameters": {"max_concurrent_tasks": 5},
            "delegation_grants": [
                {
                    "grant_id": "eng-task-scaling",
                    "delegated_to": "agents",
                    "scope": {"parameter": "max_concurrent_tasks"},
                },
            ],
        }
        (policy_home / "policy.yaml").write_text(yaml.dump(org_policy))

        agent_dir = policy_home / "agents" / "test-agent"
        agent_policy = {
            "exceptions": [
                {
                    "exception_id": "expired-exc",
                    "grant_ref": "eng-task-scaling",
                    "parameter": "max_concurrent_tasks",
                    "granted_value": 15,
                    "approved_by": "operator",
                    "expires": "2020-01-01",  # Expired
                },
            ],
        }
        (agent_dir / "policy.yaml").write_text(yaml.dump(agent_policy))

        engine = PolicyEngine(agency_home=policy_home)
        result = engine.validate_chain("test-agent")

        assert result.expired_exceptions == 1

    def test_exception_missing_approval_rejected(self, policy_home):
        """Verify exception without approved_by is rejected."""
        org_policy = {
            "parameters": {"max_concurrent_tasks": 5},
            "delegation_grants": [
                {"grant_id": "test-grant", "delegated_to": "agents"},
            ],
        }
        (policy_home / "policy.yaml").write_text(yaml.dump(org_policy))

        agent_dir = policy_home / "agents" / "test-agent"
        agent_policy = {
            "exceptions": [
                {
                    "exception_id": "no-approval",
                    "grant_ref": "test-grant",
                    "parameter": "max_concurrent_tasks",
                    "granted_value": 15,
                    # No approved_by — invalid
                },
            ],
        }
        (agent_dir / "policy.yaml").write_text(yaml.dump(agent_policy))

        engine = PolicyEngine(agency_home=policy_home)
        result = engine.validate_chain("test-agent")

        assert not result.valid


class TestComputeEffectivePolicy:
    def test_compute_returns_sealed_policy(self, policy_home):
        engine = PolicyEngine(agency_home=policy_home)
        policy = engine.compute("test-agent")

        assert policy.sealed
        assert policy.agent == "test-agent"
        assert policy.parameters["risk_tolerance"] == "low"  # Most restrictive wins
        assert policy.parameters["max_concurrent_tasks"] == 3

    def test_compute_raises_on_violation(self, policy_home):
        agent_dir = policy_home / "agents" / "test-agent"
        (agent_dir / "policy.yaml").write_text(
            yaml.dump({"parameters": {"risk_tolerance": "high"}})
        )

        engine = PolicyEngine(agency_home=policy_home)
        with pytest.raises(PolicyViolation):
            engine.compute("test-agent")

    def test_rules_are_additive(self, policy_home):
        engine = PolicyEngine(agency_home=policy_home)
        policy = engine.compute("test-agent")

        # Should have platform defaults + agent additions
        rule_texts = [r.get("rule", "") for r in policy.rules]
        assert "irreversible actions require confirmation" in rule_texts
        assert "all PRs must be draft" in rule_texts


# --- Helpers for hierarchy tests ---

def _setup_hierarchy(home, dept_name="engineering", team_name="backend"):
    """Create department and team directories with policies."""
    # Department policy
    dept_dir = home / "departments" / dept_name
    dept_dir.mkdir(parents=True)
    dept_policy = {
        "parameters": {
            "risk_tolerance": "low",
            "max_concurrent_tasks": 4,
        },
        "rules": [
            {"rule": "department code review required", "applies_to": ["git_push"]},
        ],
    }
    (dept_dir / "policy.yaml").write_text(yaml.dump(dept_policy))

    # Team policy
    team_dir = home / "teams" / team_name
    team_dir.mkdir(parents=True)
    team_policy = {
        "parameters": {
            "max_concurrent_tasks": 3,
        },
        "rules": [
            {"rule": "team CI must pass", "applies_to": ["git_push"]},
        ],
    }
    (team_dir / "policy.yaml").write_text(yaml.dump(team_policy))

    return dept_dir, team_dir


def _set_agent_inherits(home, agent_name, inherits_from):
    """Update agent.yaml inherits_from field."""
    agent_yaml = {
        "agency_version": "0.1",
        "name": agent_name,
        "role": "test",
        "tier": "standard",
        "workspace": {"ref": "ubuntu-default"},
        "requires": {"tools": ["git"]},
        "policy": {"inherits_from": inherits_from},
    }
    agent_dir = home / "agents" / agent_name
    agent_dir.mkdir(parents=True, exist_ok=True)
    (agent_dir / "agent.yaml").write_text(yaml.dump(agent_yaml))


class TestDepartmentTeamHierarchy:
    """Tests for full 5-level policy hierarchy: platform > org > department > team > agent."""

    @pytest.fixture
    def hierarchy_home(self, tmp_path):
        """Create a full hierarchy with org, department, team, and agent."""
        home = tmp_path / ".agency"
        home.mkdir()

        # Org policy
        org_policy = {
            "parameters": {
                "risk_tolerance": "medium",
                "max_concurrent_tasks": 5,
                "max_task_duration": "4 hours",
            },
            "rules": [
                {"rule": "org security review", "applies_to": ["security_config"]},
            ],
        }
        (home / "policy.yaml").write_text(yaml.dump(org_policy))

        # Department and team
        _setup_hierarchy(home)

        # Agent referencing both department and team
        _set_agent_inherits(home, "test-agent", "departments/engineering/teams/backend")

        # Agent policy (restricts further)
        agent_dir = home / "agents" / "test-agent"
        agent_policy = {
            "parameters": {
                "max_concurrent_tasks": 2,
            },
            "additions": [
                {"rule": "agent must log all commands", "applies_to": ["execute_command"]},
            ],
        }
        (agent_dir / "policy.yaml").write_text(yaml.dump(agent_policy))

        return home

    def test_full_chain_validates(self, hierarchy_home):
        engine = PolicyEngine(agency_home=hierarchy_home)
        result = engine.validate_chain("test-agent")

        assert result.valid
        levels = [s.level for s in result.steps]
        assert levels == ["platform", "org", "department", "team", "agent"]

    def test_full_chain_all_ok(self, hierarchy_home):
        engine = PolicyEngine(agency_home=hierarchy_home)
        result = engine.validate_chain("test-agent")

        ok_steps = [s for s in result.steps if s.status == "ok"]
        assert len(ok_steps) == 5

    def test_parameters_cascade_restrictively(self, hierarchy_home):
        """Parameters tighten through each level."""
        engine = PolicyEngine(agency_home=hierarchy_home)
        policy = engine.compute("test-agent")

        # org=medium, dept=low -> low wins
        assert policy.parameters["risk_tolerance"] == "low"
        # org=5, dept=4, team=3, agent=2 -> 2 wins
        assert policy.parameters["max_concurrent_tasks"] == 2
        # Only org sets this, passes through unchanged
        assert policy.parameters["max_task_duration"] == "4 hours"

    def test_rules_accumulate_all_levels(self, hierarchy_home):
        engine = PolicyEngine(agency_home=hierarchy_home)
        policy = engine.compute("test-agent")

        rule_texts = [r.get("rule", "") for r in policy.rules]
        # Platform default
        assert "irreversible actions require confirmation" in rule_texts
        # Org
        assert "org security review" in rule_texts
        # Department
        assert "department code review required" in rule_texts
        # Team
        assert "team CI must pass" in rule_texts
        # Agent
        assert "agent must log all commands" in rule_texts

    def test_department_loosening_rejected(self, hierarchy_home):
        """Department cannot loosen org parameters."""
        dept_dir = hierarchy_home / "departments" / "engineering"
        bad_policy = {
            "parameters": {
                "risk_tolerance": "high",  # Loosened from org medium
            },
        }
        (dept_dir / "policy.yaml").write_text(yaml.dump(bad_policy))

        engine = PolicyEngine(agency_home=hierarchy_home)
        result = engine.validate_chain("test-agent")

        assert not result.valid
        assert any("loosened" in v.issue for v in result.violations)

    def test_team_loosening_vs_department_rejected(self, hierarchy_home):
        """Team cannot loosen department parameters."""
        team_dir = hierarchy_home / "teams" / "backend"
        bad_policy = {
            "parameters": {
                "max_concurrent_tasks": 5,  # Dept sets 4 — loosened
            },
        }
        (team_dir / "policy.yaml").write_text(yaml.dump(bad_policy))

        engine = PolicyEngine(agency_home=hierarchy_home)
        result = engine.validate_chain("test-agent")

        assert not result.valid
        assert any("loosened" in v.issue for v in result.violations)

    def test_agent_loosening_vs_team_rejected(self, hierarchy_home):
        """Agent cannot loosen team parameters."""
        agent_dir = hierarchy_home / "agents" / "test-agent"
        bad_policy = {
            "parameters": {
                "max_concurrent_tasks": 4,  # Team sets 3 — loosened
            },
        }
        (agent_dir / "policy.yaml").write_text(yaml.dump(bad_policy))

        engine = PolicyEngine(agency_home=hierarchy_home)
        result = engine.validate_chain("test-agent")

        assert not result.valid

    def test_department_only_no_team(self, tmp_path):
        """Agent in a department but no team."""
        home = tmp_path / ".agency"
        home.mkdir()

        org_policy = {
            "parameters": {"risk_tolerance": "medium", "max_concurrent_tasks": 5},
        }
        (home / "policy.yaml").write_text(yaml.dump(org_policy))

        # Department only
        dept_dir = home / "departments" / "security"
        dept_dir.mkdir(parents=True)
        dept_policy = {
            "parameters": {"risk_tolerance": "low"},
        }
        (dept_dir / "policy.yaml").write_text(yaml.dump(dept_policy))

        _set_agent_inherits(home, "sec-agent", "departments/security")
        agent_dir = home / "agents" / "sec-agent"
        (agent_dir / "policy.yaml").write_text(yaml.dump({"parameters": {}}))

        engine = PolicyEngine(agency_home=home)
        policy = engine.compute("sec-agent")

        assert policy.parameters["risk_tolerance"] == "low"
        # Team step should be missing
        result = engine.validate_chain("sec-agent")
        missing = [s for s in result.steps if s.status == "missing"]
        assert any(s.level == "team" for s in missing)

    def test_team_only_no_department(self, tmp_path):
        """Agent in a team but no department."""
        home = tmp_path / ".agency"
        home.mkdir()

        org_policy = {
            "parameters": {"max_concurrent_tasks": 5},
        }
        (home / "policy.yaml").write_text(yaml.dump(org_policy))

        # Team only
        team_dir = home / "teams" / "frontend"
        team_dir.mkdir(parents=True)
        team_policy = {
            "parameters": {"max_concurrent_tasks": 3},
        }
        (team_dir / "policy.yaml").write_text(yaml.dump(team_policy))

        _set_agent_inherits(home, "fe-agent", "teams/frontend")
        agent_dir = home / "agents" / "fe-agent"
        (agent_dir / "policy.yaml").write_text(yaml.dump({"parameters": {}}))

        engine = PolicyEngine(agency_home=home)
        policy = engine.compute("fe-agent")

        assert policy.parameters["max_concurrent_tasks"] == 3
        result = engine.validate_chain("fe-agent")
        missing = [s for s in result.steps if s.status == "missing"]
        assert any(s.level == "department" for s in missing)

    def test_hard_floor_in_department_rejected(self, hierarchy_home):
        """Hard floors cannot be modified at department level."""
        dept_dir = hierarchy_home / "departments" / "engineering"
        bad_policy = {
            "parameters": {"logging": "optional"},
        }
        (dept_dir / "policy.yaml").write_text(yaml.dump(bad_policy))

        engine = PolicyEngine(agency_home=hierarchy_home)
        result = engine.validate_chain("test-agent")

        assert not result.valid
        assert any("hard floor" in v.issue for v in result.violations)

    def test_hard_floor_in_team_rejected(self, hierarchy_home):
        """Hard floors cannot be modified at team level."""
        team_dir = hierarchy_home / "teams" / "backend"
        bad_policy = {
            "parameters": {"constraints_readonly": False},
        }
        (team_dir / "policy.yaml").write_text(yaml.dump(bad_policy))

        engine = PolicyEngine(agency_home=hierarchy_home)
        result = engine.validate_chain("test-agent")

        assert not result.valid
        assert any("hard floor" in v.issue for v in result.violations)

    def test_missing_department_dir_noted(self, tmp_path):
        """Agent references nonexistent department — step shows missing."""
        home = tmp_path / ".agency"
        home.mkdir()
        (home / "policy.yaml").write_text(yaml.dump({"parameters": {}}))

        _set_agent_inherits(home, "orphan", "departments/nonexistent")
        agent_dir = home / "agents" / "orphan"
        (agent_dir / "policy.yaml").write_text(yaml.dump({"parameters": {}}))

        engine = PolicyEngine(agency_home=home)
        result = engine.validate_chain("orphan")

        dept_steps = [s for s in result.steps if s.level == "department"]
        assert len(dept_steps) == 1
        assert dept_steps[0].status == "missing"

    def test_department_restriction_flows_to_agent(self, tmp_path):
        """Department restricts a param; agent inherits that restriction."""
        home = tmp_path / ".agency"
        home.mkdir()

        org_policy = {
            "parameters": {"max_concurrent_tasks": 10},
        }
        (home / "policy.yaml").write_text(yaml.dump(org_policy))

        dept_dir = home / "departments" / "ops"
        dept_dir.mkdir(parents=True)
        (dept_dir / "policy.yaml").write_text(
            yaml.dump({"parameters": {"max_concurrent_tasks": 3}})
        )

        _set_agent_inherits(home, "ops-agent", "departments/ops")
        agent_dir = home / "agents" / "ops-agent"
        # Agent doesn't set this param — inherits department restriction
        (agent_dir / "policy.yaml").write_text(yaml.dump({"parameters": {}}))

        engine = PolicyEngine(agency_home=home)
        policy = engine.compute("ops-agent")

        assert policy.parameters["max_concurrent_tasks"] == 3


class TestNamedPolicyRegistry:
    """Tests for reusable named policy templates."""

    @pytest.fixture
    def registry_home(self, tmp_path):
        home = tmp_path / ".agency"
        home.mkdir()
        (home / "policy.yaml").write_text(yaml.dump({
            "parameters": {"risk_tolerance": "medium", "max_concurrent_tasks": 5},
        }))
        return home

    def test_create_policy(self, registry_home):
        reg = PolicyRegistry(registry_home)
        path = reg.create_policy("strict", description="Strict policy",
                                 parameters={"risk_tolerance": "low"})
        assert path.exists()
        data = reg.get_policy("strict")
        assert data["parameters"]["risk_tolerance"] == "low"

    def test_create_duplicate_rejected(self, registry_home):
        reg = PolicyRegistry(registry_home)
        reg.create_policy("strict", parameters={"risk_tolerance": "low"})
        with pytest.raises(PolicyRegistryError, match="already exists"):
            reg.create_policy("strict")

    def test_create_loosening_rejected(self, registry_home):
        reg = PolicyRegistry(registry_home)
        with pytest.raises(PolicyRegistryError, match="loosens"):
            reg.create_policy("loose", parameters={"risk_tolerance": "high"})

    def test_create_hard_floor_rejected(self, registry_home):
        reg = PolicyRegistry(registry_home)
        with pytest.raises(PolicyRegistryError, match="hard floor"):
            reg.create_policy("bad", parameters={"logging": "optional"})

    def test_list_policies(self, registry_home):
        reg = PolicyRegistry(registry_home)
        reg.create_policy("alpha", parameters={"risk_tolerance": "low"})
        reg.create_policy("beta", parameters={"max_concurrent_tasks": 3})
        policies = reg.list_policies()
        names = [p["name"] for p in policies]
        assert "alpha" in names
        assert "beta" in names

    def test_delete_policy(self, registry_home):
        reg = PolicyRegistry(registry_home)
        reg.create_policy("temp", parameters={})
        reg.delete_policy("temp")
        with pytest.raises(PolicyRegistryError, match="not found"):
            reg.get_policy("temp")

    def test_delete_nonexistent_rejected(self, registry_home):
        reg = PolicyRegistry(registry_home)
        with pytest.raises(PolicyRegistryError, match="not found"):
            reg.delete_policy("nope")

    def test_validate_valid_policy(self, registry_home):
        reg = PolicyRegistry(registry_home)
        reg.create_policy("good", parameters={"risk_tolerance": "low"})
        issues = reg.validate_policy("good")
        assert issues == []

    def test_validate_unknown_parameter(self, registry_home):
        reg = PolicyRegistry(registry_home)
        policies_dir = registry_home / "policies"
        policies_dir.mkdir(exist_ok=True)
        (policies_dir / "weird.yaml").write_text(yaml.dump({
            "parameters": {"made_up_param": "foo"},
        }))
        issues = reg.validate_policy("weird")
        assert any("Unknown" in i for i in issues)

    def test_resolve_extends(self, registry_home):
        reg = PolicyRegistry(registry_home)
        reg.create_policy("base-strict", parameters={"risk_tolerance": "low"},
                          rules=[{"rule": "template rule", "applies_to": ["all"]}])

        policy_data = {
            "extends": "base-strict",
            "parameters": {"max_concurrent_tasks": 2},
            "rules": [{"rule": "local rule", "applies_to": ["local"]}],
        }
        merged = reg.resolve_extends(policy_data)

        assert merged["parameters"]["risk_tolerance"] == "low"
        assert merged["parameters"]["max_concurrent_tasks"] == 2
        rule_texts = [r.get("rule", "") for r in merged["rules"]]
        assert "template rule" in rule_texts
        assert "local rule" in rule_texts

    def test_resolve_extends_local_overrides_template(self, registry_home):
        reg = PolicyRegistry(registry_home)
        reg.create_policy("base", parameters={"max_concurrent_tasks": 3})

        policy_data = {
            "extends": "base",
            "parameters": {"max_concurrent_tasks": 2},
        }
        merged = reg.resolve_extends(policy_data)
        assert merged["parameters"]["max_concurrent_tasks"] == 2

    def test_resolve_extends_missing_template(self, registry_home):
        reg = PolicyRegistry(registry_home)
        policy_data = {"extends": "nonexistent", "parameters": {"risk_tolerance": "low"}}
        merged = reg.resolve_extends(policy_data)
        assert merged["parameters"]["risk_tolerance"] == "low"

    def test_resolve_no_extends(self, registry_home):
        reg = PolicyRegistry(registry_home)
        policy_data = {"parameters": {"risk_tolerance": "low"}}
        merged = reg.resolve_extends(policy_data)
        assert merged == policy_data

    def test_extends_in_policy_engine(self, registry_home):
        """End-to-end: agent policy extends a named template."""
        reg = PolicyRegistry(registry_home)
        reg.create_policy("cautious", parameters={"risk_tolerance": "low"},
                          rules=[{"rule": "extra caution", "applies_to": ["all"]}])

        _set_agent_inherits(registry_home, "careful-agent", "")
        agent_dir = registry_home / "agents" / "careful-agent"
        agent_policy = {
            "extends": "cautious",
            "parameters": {"max_concurrent_tasks": 2},
        }
        (agent_dir / "policy.yaml").write_text(yaml.dump(agent_policy))

        engine = PolicyEngine(agency_home=registry_home)
        policy = engine.compute("careful-agent")

        assert policy.parameters["risk_tolerance"] == "low"
        assert policy.parameters["max_concurrent_tasks"] == 2
        rule_texts = [r.get("rule", "") for r in policy.rules]
        assert "extra caution" in rule_texts


class TestCommunicationPolicy:
    def test_default_communication_scanning(self):
        """Default policy has scanning enabled with no_credentials."""
        from images.models.policy import PolicyConfig
        policy = PolicyConfig()
        assert policy.communication.scanning.enabled is True
        assert "no_credentials" in policy.communication.scanning.rules

    def test_operator_adds_scanning_rules(self):
        """Operators can add industry-specific scanning rules."""
        from images.models.policy import PolicyConfig
        policy = PolicyConfig(communication={
            "scanning": {
                "rules": ["no_credentials", "no_pii", "retain_all"],
            },
        })
        assert "no_pii" in policy.communication.scanning.rules
        assert "no_credentials" in policy.communication.scanning.rules

    def test_no_credentials_always_present(self):
        """no_credentials is a hard floor -- always present even if omitted."""
        from images.models.policy import PolicyConfig
        policy = PolicyConfig(communication={
            "scanning": {
                "rules": ["no_pii"],
            },
        })
        assert "no_credentials" in policy.communication.scanning.rules
