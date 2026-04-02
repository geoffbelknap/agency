"""Team configuration schema — teams of humans and agents."""

import re
from typing import Optional, Literal

from pydantic import BaseModel, ConfigDict, Field, field_validator


class DelegationScope(BaseModel):
    """What a coordinator is allowed to delegate."""

    model_config = ConfigDict(extra="forbid")

    can_delegate_to: list[str] = []
    cannot_delegate: list[str] = []
    task_requires_approval: list[dict[str, str]] = []


class SynthesisPermissions(BaseModel):
    """Constraints on coordinator output synthesis."""

    model_config = ConfigDict(extra="forbid")

    output_scope: Literal["internal", "external", "restricted"] = "internal"
    requires_human_review: list[dict[str, str]] = []
    audit_all_synthesis: bool = True


class CrossBoundaryAccess(BaseModel):
    """Cross-boundary visibility config for function agents.

    Allows a function agent to have read-only access to other agents'
    workspaces for security review, compliance checks, etc.
    """

    model_config = ConfigDict(extra="forbid")

    can_read: list[str] = []  # agent names this agent can read
    read_all: bool = False  # if True, can read all team members
    paths: list[str] = []  # specific paths within workspace (empty = all)


class HaltAuthority(BaseModel):
    """Halt authority for function agents.

    Allows a function agent to halt other agents in its team
    when security/compliance issues are detected. Tenet 10 applies:
    resumption requires equal or higher authority than the halt.
    """

    model_config = ConfigDict(extra="forbid")

    can_halt: list[str] = []  # specific agents this agent can halt
    halt_all: bool = False  # if True, can halt any team member
    halt_types: list[str] = ["supervised", "immediate"]  # allowed halt types
    requires_reason: bool = True  # must provide a reason


class TeamMember(BaseModel):
    """A member agent within a team."""

    model_config = ConfigDict(extra="forbid")

    name: str
    type: Literal["human", "agent"] = "agent"
    role: str = ""
    agent_type: Literal["standard", "coordinator", "function"] = "standard"
    delegation_scope: Optional[DelegationScope] = None
    synthesis_permissions: Optional[SynthesisPermissions] = None
    cross_boundary_access: Optional[CrossBoundaryAccess] = None
    halt_authority: Optional[HaltAuthority] = None


class ActivityEntry(BaseModel):
    """A single entry in the workspace activity register."""

    model_config = ConfigDict(extra="forbid")

    agent: str
    status: Literal["idle", "assisted", "autonomous", "halted"] = "idle"
    working_in: list[str] = []
    current_task: str = ""
    last_active: str = ""


class TeamConfig(BaseModel):
    """Schema for team.yaml — defines a team of humans and agents."""

    model_config = ConfigDict(extra="forbid")

    version: str = "0.1"
    name: str
    description: str = ""
    coordinator: str = ""
    members: list[TeamMember] = []
    shared_workspace: str = ""
    conflict_resolution: Literal["yield", "coordinator", "operator"] = "operator"
    synthesis_review_required: list[dict[str, str]] = []

    @field_validator("name")
    @classmethod
    def name_is_valid(cls, v: str) -> str:
        if len(v) < 2:
            raise ValueError("Team name must be at least 2 characters")
        if not re.match(r"^[a-z0-9][a-z0-9-]*[a-z0-9]$", v):
            raise ValueError(
                "Team name must be lowercase alphanumeric with hyphens, "
                "starting and ending with alphanumeric"
            )
        return v

    def get_member(self, agent_name: str) -> Optional[TeamMember]:
        """Find a member by agent name."""
        for m in self.members:
            if m.name == agent_name:
                return m
        return None

    def member_names(self) -> list[str]:
        """Return all member agent names."""
        return [m.name for m in self.members]

    def coordinators(self) -> list[TeamMember]:
        """Return all coordinator members."""
        return [m for m in self.members if m.agent_type == "coordinator"]

    def function_agents(self) -> list[TeamMember]:
        """Return all function agent members."""
        return [m for m in self.members if m.agent_type == "function"]
