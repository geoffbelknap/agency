"""Principals configuration schema."""

from pydantic import BaseModel, ConfigDict


class HumanPrincipal(BaseModel):
    """A human principal."""

    model_config = ConfigDict(extra="forbid")

    id: str
    name: str
    roles: list[str]
    created: str
    status: str = "active"
    exception_domains: list[str] = []  # domains this principal reviews


class AgentPrincipal(BaseModel):
    """An agent principal."""

    model_config = ConfigDict(extra="forbid")

    id: str
    name: str
    type: str = "standard"
    status: str = "active"


class TeamPrincipal(BaseModel):
    """A team principal."""

    model_config = ConfigDict(extra="forbid")

    id: str
    name: str
    members: list[str] = []


class ExceptionRoute(BaseModel):
    """Maps exception domains to approving principals."""

    model_config = ConfigDict(extra="forbid")

    domain: str  # e.g., "privacy", "security", "legal", "compliance"
    approvers: list[str]  # principal IDs who can approve
    requires_dual_approval: bool = False  # needs 2+ approvers


class PrincipalsConfig(BaseModel):
    """Schema for principals.yaml."""

    model_config = ConfigDict(extra="forbid")

    version: str = "0.1"
    humans: list[HumanPrincipal] = []
    agents: list[AgentPrincipal] = []
    teams: list[TeamPrincipal] = []
    exception_routes: list[ExceptionRoute] = []
