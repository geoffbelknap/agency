"""Egress domain policy models.

Defines per-agent egress mode and approved domain list.
Runtime state written to ~/.agency/agents/{name}/egress-domains.yaml.
"""

from typing import Literal

from pydantic import BaseModel, ConfigDict, field_validator


EGRESS_MODES = ("denylist", "allowlist", "supervised-strict", "supervised-permissive")
EgressMode = Literal["denylist", "allowlist", "supervised-strict", "supervised-permissive"]


class EgressDomainEntry(BaseModel):
    """A single approved egress domain."""

    model_config = ConfigDict(extra="forbid")

    domain: str
    approved_at: str
    approved_by: str
    reason: str = ""

    @field_validator("domain")
    @classmethod
    def domain_not_empty(cls, v: str) -> str:
        v = v.strip().lower()
        if not v:
            raise ValueError("domain must not be empty")
        return v

    @field_validator("approved_at")
    @classmethod
    def timestamp_not_empty(cls, v: str) -> str:
        if not v:
            raise ValueError("approved_at must not be empty")
        return v

    @field_validator("approved_by")
    @classmethod
    def approver_not_empty(cls, v: str) -> str:
        if not v:
            raise ValueError("approved_by must not be empty")
        return v


class AgentEgressConfig(BaseModel):
    """Per-agent egress configuration.

    Written to ~/.agency/agents/{name}/egress-domains.yaml.
    """

    model_config = ConfigDict(extra="forbid")

    agent: str
    mode: EgressMode = "denylist"
    domains: list[EgressDomainEntry] = []

    @field_validator("agent")
    @classmethod
    def agent_not_empty(cls, v: str) -> str:
        if not v:
            raise ValueError("agent must not be empty")
        return v
