"""Agent configuration schema."""

import re
from typing import Optional, Literal

from pydantic import BaseModel, ConfigDict, field_validator

from images.models.routing import VALID_TIERS


class MCPServerConfig(BaseModel):
    model_config = ConfigDict(extra="forbid")
    command: str
    args: list[str] = []
    env: dict[str, str] = {}


class AgentBodyConfig(BaseModel):
    model_config = ConfigDict(extra="forbid")
    runtime: str
    version: str
    skills_dirs: list[str] = []
    mcp_servers: dict[str, MCPServerConfig] = {}


class AgentWorkspaceRef(BaseModel):
    model_config = ConfigDict(extra="forbid")
    ref: str


class AgentRequires(BaseModel):
    model_config = ConfigDict(extra="forbid")
    tools: list[str] = []
    capabilities: list[str] = []
    models: list[str] = []


class AgentPolicyRef(BaseModel):
    model_config = ConfigDict(extra="forbid")
    inherits_from: Optional[str] = None
    ref: Optional[str] = None


class AgentInstanceAttachment(BaseModel):
    model_config = ConfigDict(extra="forbid")
    instance_id: str
    node_id: str
    actions: list[str] = []


class AgentInstancesConfig(BaseModel):
    model_config = ConfigDict(extra="forbid")
    attach: list[AgentInstanceAttachment] = []


class AgentTriageConfig(BaseModel):
    """Triage configuration for notification classification."""
    model_config = ConfigDict(extra="forbid")
    domains: list[str] = []
    prompt: str = ""


class ResponsivenessConfig(BaseModel):
    """Channel-scoped responsiveness configuration.

    Controls how the agent responds to messages in different channels:
    - silent: never responds unless given a task targeting this channel
    - mention-only: responds to @agent_name mentions only (default)
    - active: evaluates all messages for relevance, responds when it can contribute
    """
    model_config = ConfigDict(extra="forbid")
    default: Literal["silent", "mention-only", "active"] = "mention-only"
    channels: dict[str, Literal["silent", "mention-only", "active"]] = {}


class ExpertiseConfig(BaseModel):
    """Base expertise profile — operator-defined, read-only to agent (tenet 5).

    Keywords and description used by the comms server for message filtering.
    Agents only receive messages matching their expertise in active channels.
    """
    model_config = ConfigDict(extra="forbid")
    description: str = ""
    keywords: list[str] = []


class AgentConfig(BaseModel):
    """Schema for agent.yaml."""

    model_config = ConfigDict(extra="forbid")

    version: str = "0.1"
    name: str
    role: str
    tier: Literal["standard", "elevated", "function"] = "standard"
    type: Literal["standard", "coordinator", "function"] = "standard"
    model_tier: Optional[str] = None
    body: AgentBodyConfig
    workspace: AgentWorkspaceRef
    requires: AgentRequires = AgentRequires()
    policy: AgentPolicyRef = AgentPolicyRef()
    instances: AgentInstancesConfig = AgentInstancesConfig()
    triage: Optional[AgentTriageConfig] = None
    responsiveness: ResponsivenessConfig = ResponsivenessConfig()
    expertise: ExpertiseConfig = ExpertiseConfig()

    @field_validator("model_tier")
    @classmethod
    def validate_model_tier(cls, v: Optional[str]) -> Optional[str]:
        if v is not None and v not in VALID_TIERS:
            raise ValueError(f"model_tier must be one of {VALID_TIERS}, got '{v}'")
        return v

    @field_validator("name")
    @classmethod
    def name_is_valid(cls, v: str) -> str:
        if len(v) < 2:
            raise ValueError("Agent name must be at least 2 characters")
        if not re.match(r"^[a-z0-9][a-z0-9-]*[a-z0-9]$", v):
            raise ValueError(
                "Agent name must be lowercase alphanumeric with hyphens, "
                "starting and ending with alphanumeric"
            )
        return v
