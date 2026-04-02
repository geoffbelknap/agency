"""Constraints configuration schema."""

from typing import Literal

from pydantic import BaseModel, ConfigDict, Field

from agency_core.models.egress import EgressMode


class HardLimit(BaseModel):
    model_config = ConfigDict(extra="forbid")
    rule: str
    reason: str


class EscalationConfig(BaseModel):
    model_config = ConfigDict(extra="forbid")
    always_escalate: list[str] = []
    flag_before_proceeding: list[str] = []


class NotifyConfig(BaseModel):
    """Notification targets for budget alerts."""

    model_config = ConfigDict(extra="forbid")

    webhook: str | None = None
    email: str | None = None
    log: bool = True


class BudgetConfig(BaseModel):
    """Per-agent LLM spend budget controls."""

    model_config = ConfigDict(extra="forbid")

    mode: Literal["hard", "soft", "notify"] = "notify"
    soft_limit: float = Field(default=0.0, ge=0.0)  # dollars, alert threshold
    hard_limit: float = Field(default=0.0, ge=0.0)   # dollars, block threshold
    max_daily_usd: float = Field(default=0.0, ge=0.0)       # 0 = unlimited
    max_session_usd: float = Field(default=0.0, ge=0.0)
    max_total_usd: float = Field(default=0.0, ge=0.0)
    warning_threshold_pct: int = Field(default=80, ge=1, le=100)
    notify: NotifyConfig = NotifyConfig()


class MCPPolicy(BaseModel):
    """Per-agent MCP tool access policy.

    Controls which MCP servers and individual tools the agent can use.
    Default mode is allowlist — only explicitly allowed servers/tools
    are available. Set mode to "denylist" to allow all except denied.
    """

    model_config = ConfigDict(extra="forbid")

    mode: Literal["allowlist", "denylist"] = "denylist"
    allowed_servers: list[str] = []
    denied_servers: list[str] = []
    allowed_tools: list[str] = []
    denied_tools: list[str] = []
    pinned_hashes: dict[str, str] = {}  # server_name -> sha256 hex of command binary

    def is_server_allowed(self, server_name: str) -> bool:
        """Check if an MCP server is allowed by this policy."""
        if self.mode == "allowlist":
            return server_name in self.allowed_servers
        else:
            return server_name not in self.denied_servers

    def is_tool_allowed(self, tool_name: str) -> bool:
        """Check if an individual MCP tool is allowed by this policy."""
        if self.denied_tools and tool_name in self.denied_tools:
            return False
        if self.allowed_tools:
            return tool_name in self.allowed_tools
        return True


class NetworkConfig(BaseModel):
    """Network access configuration."""

    model_config = ConfigDict(extra="forbid")

    egress_mode: EgressMode = "denylist"


class IdentityConstraint(BaseModel):
    model_config = ConfigDict(extra="forbid")
    role: str
    purpose: str


class ConstraintsConfig(BaseModel):
    """Schema for constraints.yaml."""

    # Use "ignore" so that legacy constraints.yaml files containing an
    # "autonomy" key (or any other unknown keys) are silently accepted.
    model_config = ConfigDict(extra="ignore")

    version: str = "0.1"
    agent: str
    identity: IdentityConstraint
    hard_limits: list[HardLimit] = []
    escalation: EscalationConfig = EscalationConfig()
    network: NetworkConfig = NetworkConfig()
    budget: BudgetConfig = BudgetConfig()
    mcp: MCPPolicy = MCPPolicy()
