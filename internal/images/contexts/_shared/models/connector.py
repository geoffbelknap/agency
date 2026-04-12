"""Pydantic models for connector schema — external system bindings."""

import re
from pydantic import BaseModel, ConfigDict, Field, field_validator, model_validator
from typing import Literal

_INTERVAL_PATTERN = re.compile(r"^\d+[smhd]$")


class ConnectorFollowUp(BaseModel):
    """Follow-up poll config: fetch a per-item URL to get nested items (e.g. Slack thread replies)."""

    model_config = ConfigDict(extra="forbid")

    url: str  # URL template; {field} substituted from parent item, ${ENV} from env
    when: str | None = None  # only follow up when this field is truthy/non-zero on the parent item
    response_key: str | None = None  # extract list from follow-up response (e.g. $.messages)
    dedup_key: str | None = None  # field to use for dedup instead of full-object hash
    skip_first: bool = False  # skip first result (e.g. thread parent repeated in Slack replies)


class ConnectorWebhookAuth(BaseModel):
    """HMAC-SHA256 signature verification for webhook sources (e.g. Slack Events API)."""

    model_config = ConfigDict(extra="forbid")

    type: Literal["hmac_sha256"] = "hmac_sha256"
    secret_env: str | None = None  # env var name containing the signing secret
    secret_credref: str | None = None  # credref name for the signing secret
    header: str = "X-Slack-Signature"  # header carrying the computed signature
    timestamp_header: str | None = "X-Slack-Request-Timestamp"  # for replay attack protection
    prefix: str = "v0="  # prefix on the signature value (Slack uses "v0=")
    challenge_field: str | None = "challenge"  # field name for URL verification handshake (Slack)
    max_skew_seconds: int = 300

    @model_validator(mode="after")
    def validate_secret_source(self) -> "ConnectorWebhookAuth":
        if not self.secret_env and not self.secret_credref:
            raise ValueError("webhook_auth requires either secret_env or secret_credref")
        return self


class ConnectorSource(BaseModel):
    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    type: Literal["webhook", "poll", "schedule", "channel-watch", "none"]
    payload_schema: dict | None = Field(default=None, alias="schema")
    webhook_auth: ConnectorWebhookAuth | None = None  # HMAC auth for webhook sources
    path: str | None = None
    body_format: str | None = None
    ack_strategy: str | None = None
    # poll fields
    url: str | None = None
    method: str = "GET"
    headers: dict[str, str] | None = None
    interval: str | None = None
    response_key: str | None = None
    dedup_key: str | None = None  # field to use for deduplication instead of full-object hash
    follow_up: ConnectorFollowUp | None = None  # per-item follow-up fetch (e.g. thread replies)
    # schedule fields
    cron: str | None = None
    # channel-watch fields
    channel: str | None = None
    pattern: str | None = None

    @model_validator(mode="after")
    def validate_source_fields(self) -> "ConnectorSource":
        if self.type == "poll":
            if not self.url:
                raise ValueError("poll source requires 'url'")
            if not self.interval:
                raise ValueError("poll source requires 'interval'")
            if not _INTERVAL_PATTERN.match(self.interval):
                raise ValueError(f"Invalid interval format: {self.interval} (expected e.g. '30s', '5m', '1h', '1d')")
        elif self.type == "none":
            inbound_fields = [
                self.webhook_auth,
                self.path,
                self.body_format,
                self.ack_strategy,
                self.url,
                self.interval,
                self.response_key,
                self.cron,
                self.channel,
                self.pattern,
            ]
            if self.headers is not None:
                inbound_fields.append("set")
            if self.method != "GET":
                inbound_fields.append("set")
            if any(f is not None for f in inbound_fields):
                raise ValueError("source type none does not accept inbound source fields")
        elif self.type == "schedule":
            if not self.cron:
                raise ValueError("schedule source requires 'cron'")
        elif self.type == "channel-watch":
            if not self.channel:
                raise ValueError("channel-watch source requires 'channel'")
            if not self.pattern:
                raise ValueError("channel-watch source requires 'pattern'")
        elif self.type == "webhook":
            poll_fields = [self.url, self.interval, self.response_key, self.cron, self.channel, self.pattern]
            if self.headers is not None:
                poll_fields.append("set")
            if self.method != "GET":
                poll_fields.append("set")
            if any(f is not None for f in poll_fields):
                raise ValueError("webhook source does not accept poll/schedule/channel-watch fields")
        return self


class ConnectorRelayTarget(BaseModel):
    """Direct HTTP relay: send matched payload to an external endpoint without spawning an agent."""

    model_config = ConfigDict(extra="forbid")

    url: str  # ${ENV} expanded; target endpoint
    method: str = "POST"
    headers: dict[str, str] | None = None  # ${ENV} expanded
    body: str  # Jinja2 template rendered with payload fields, then ${ENV} expanded
    content_type: str = "application/json"


class ConnectorRoute(BaseModel):
    model_config = ConfigDict(extra="forbid")

    match: dict[str, str | list[str] | None]
    # Agent/team routing: deliver a task to an agent or team via DM channel
    target: dict[str, str] | None = None
    # Relay routing: POST directly to an HTTP endpoint, no agent spawned
    relay: ConnectorRelayTarget | None = None
    priority: Literal["high", "normal", "low"] = "normal"
    sla: str | None = None

    @model_validator(mode="after")
    def validate_routing(self) -> "ConnectorRoute":
        has_target = self.target is not None
        has_relay = self.relay is not None
        if not has_target and not has_relay:
            raise ValueError("Route must specify either 'target' (agent/team) or 'relay' (HTTP endpoint)")
        if has_target and has_relay:
            raise ValueError("Route cannot specify both 'target' and 'relay'")
        return self


class ConnectorMCPTool(BaseModel):
    model_config = ConfigDict(extra="forbid")

    name: str
    method: str = "GET"
    path: str | None = None
    parameters: dict | None = None
    input_schema: dict | None = None
    returns: dict | None = None
    description: str = ""
    requires_config: str | None = None
    whitelist_check: str | None = None
    requires_consent_token: dict | None = None

    @model_validator(mode="after")
    def validate_tool_controls(self) -> "ConnectorMCPTool":
        if not self.path and not self.input_schema:
            raise ValueError("tool requires either path or input_schema")
        params = set((self.parameters or self.input_schema or {}).keys())
        if self.whitelist_check and self.whitelist_check not in params:
            raise ValueError(f"whitelist_check references unknown parameter {self.whitelist_check!r}")
        if self.requires_consent_token:
            operation_kind = self.requires_consent_token.get("operation_kind")
            token_field = self.requires_consent_token.get("token_input_field")
            target_field = self.requires_consent_token.get("target_input_field")
            if not operation_kind:
                raise ValueError("requires_consent_token.operation_kind is required")
            if not token_field:
                raise ValueError("requires_consent_token.token_input_field is required")
            if not target_field:
                raise ValueError("requires_consent_token.target_input_field is required")
            if token_field not in params:
                raise ValueError(f"requires_consent_token references unknown token_input_field {token_field!r}")
            if target_field not in params:
                raise ValueError(f"requires_consent_token references unknown target_input_field {target_field!r}")
        return self


class ConnectorMCP(BaseModel):
    model_config = ConfigDict(extra="forbid")

    name: str
    credential: str
    api_base: str | None = None
    server: str | None = None
    tools: list[ConnectorMCPTool] | None = None


class ConnectorRequires(BaseModel):
    model_config = ConfigDict(extra="forbid")

    services: list[str] = Field(default_factory=list)


class ConnectorRateLimits(BaseModel):
    model_config = ConfigDict(extra="forbid")

    max_per_hour: int = 100
    max_concurrent: int = 10


class ConnectorConfig(BaseModel):
    model_config = ConfigDict(extra="forbid")

    kind: Literal["connector"] = "connector"
    name: str
    version: str = "1.0.0"
    description: str = ""
    author: str = ""
    requires: ConnectorRequires | None = None
    source: ConnectorSource
    routes: list[ConnectorRoute] = []
    mcp: ConnectorMCP | None = None
    config: dict = Field(default_factory=dict)
    tools: list[ConnectorMCPTool] = []
    rate_limits: ConnectorRateLimits = Field(default_factory=ConnectorRateLimits)

    @model_validator(mode="after")
    def validate_shape(self) -> "ConnectorConfig":
        if self.source.type == "none":
            if self.routes:
                raise ValueError("tool-only connectors must not define routes")
            if not self.tools and not (self.mcp and self.mcp.tools):
                raise ValueError("tool-only connectors must define at least one tool")
            return self
        if not self.routes:
            raise ValueError("Connector must define at least one route")
        return self
