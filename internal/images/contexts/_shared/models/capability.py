"""Capability registry models.

Defines schemas for capability entries (MCP servers, skills, services)
and the central capabilities.yaml configuration.
"""

from typing import Literal

from pydantic import BaseModel, ConfigDict, field_validator


CapabilityKind = Literal["mcp-server", "skill", "service"]
CapabilityState = Literal["available", "restricted", "disabled"]
ToolApproval = Literal["available", "ask-once", "ask-always", "denied"]
ApprovalStatus = Literal["pending", "routed", "approved", "rejected", "denied", "canceled"]


class CapabilityRequirements(BaseModel):
    """What a capability needs from the workspace."""

    model_config = ConfigDict(extra="forbid")

    runtime_packages: list[str] = []
    network: list[str] = []
    capabilities: list[str] = []


class CapabilityPermissions(BaseModel):
    """What a capability can do — used for policy validation."""

    model_config = ConfigDict(extra="forbid")

    filesystem: Literal["none", "read-only", "read-write"] = "none"
    network: bool = False
    execution: bool = False


class CapabilityIntegrity(BaseModel):
    """Integrity verification for marketplace packages."""

    model_config = ConfigDict(extra="forbid")

    sha256: str | None = None
    signed_by: str | None = None
    signature: str | None = None
    verified_at: str | None = None


class MCPServerSpec(BaseModel):
    """MCP server-specific configuration."""

    model_config = ConfigDict(extra="forbid")

    command: str
    args: list[str] = []
    env: dict[str, str] = {}


class CapabilityEntry(BaseModel):
    """A capability in the registry.

    Common envelope for all capability types: mcp-server, skill, service.
    """

    model_config = ConfigDict(extra="forbid")

    kind: CapabilityKind
    name: str
    version: str = "0.1.0"
    display_name: str = ""
    description: str = ""
    source: str = "local"
    publisher: str = ""

    integrity: CapabilityIntegrity = CapabilityIntegrity()
    requires: CapabilityRequirements = CapabilityRequirements()
    permissions: CapabilityPermissions = CapabilityPermissions()

    # Type-specific spec — present for mcp-server entries
    spec: MCPServerSpec | None = None

    # For service entries — reference to a service definition file
    service_ref: str | None = None

    # For skill entries — path to the skill directory
    skill_path: str | None = None

    tags: list[str] = []

    @field_validator("name")
    @classmethod
    def name_is_valid(cls, v: str) -> str:
        if not v.replace("-", "").replace("_", "").isalnum():
            raise ValueError(
                "Capability name must be alphanumeric with hyphens or underscores"
            )
        return v


# ---------------------------------------------------------------------------
# Central capabilities.yaml configuration
# ---------------------------------------------------------------------------


class ToolPolicy(BaseModel):
    """Per-tool approval policy within a capability."""

    model_config = ConfigDict(extra="forbid")

    approval: ToolApproval = "available"


class CapabilityAuth(BaseModel):
    """Authentication configuration for a capability.

    Controls which API key/token is injected into the MCP server
    environment at start time.

    - env: env var name in the secrets file holding the default key
    - inject_as: env var name inside the MCP server process
    - agents: per-agent env var overrides (agent_name -> env_var_name)
    """

    model_config = ConfigDict(extra="forbid")

    env: str                           # e.g. BRAVE_API_KEY
    inject_as: str = ""                # defaults to same as env if empty
    agents: dict[str, str] = {}        # e.g. {"atlas": "BRAVE_API_KEY_ATLAS"}


class CapabilityConfig(BaseModel):
    """Configuration for a single capability in capabilities.yaml.

    Controls who can use it, what auth is injected, and what
    approval is needed per-tool.
    """

    model_config = ConfigDict(extra="forbid")

    state: CapabilityState = "available"
    agents: list[str] = []  # only used when state=restricted
    auth: CapabilityAuth | None = None
    tools: dict[str, ToolApproval] = {}  # per-tool approval overrides

    @field_validator("agents")
    @classmethod
    def agents_only_with_restricted(cls, v, info):
        # Allow agents list regardless — validation at usage time
        return v


class CapabilitiesFile(BaseModel):
    """Schema for ~/.agency/capabilities.yaml.

    Central configuration controlling which capabilities are active,
    who can use them, and what tool-level approval is required.
    """

    model_config = ConfigDict(extra="forbid")

    capabilities: dict[str, CapabilityConfig] = {}


# ---------------------------------------------------------------------------
# Tool approval state tracking
# ---------------------------------------------------------------------------


class ToolApprovalRecord(BaseModel):
    """Tracks operator approval for ask-once tools."""

    model_config = ConfigDict(extra="forbid")

    capability: str
    tool: str
    agent: str
    approved: bool
    status: ApprovalStatus | None = None
    approved_by: str = "operator"
    approved_at: str = ""


# ---------------------------------------------------------------------------
# Legacy compatibility (kept for policy.yaml migration)
# ---------------------------------------------------------------------------


class CapabilityPolicy(BaseModel):
    """Legacy capability policy section in policy.yaml.

    Retained for backward compatibility. New deployments should use
    capabilities.yaml instead.
    """

    model_config = ConfigDict(extra="forbid")

    required: list[str] = []
    available: list[str] = []
    denied: list[str] = []
    enabled: list[str] = []
