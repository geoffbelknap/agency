"""Pydantic models for connector schema — external system bindings."""

import re
from pydantic import BaseModel, ConfigDict, Field, field_validator, model_validator
from typing import Optional, Literal, Union

_INTERVAL_PATTERN = re.compile(r"^\d+[smhd]$")


class ConnectorFollowUp(BaseModel):
    """Follow-up poll config: fetch a per-item URL to get nested items (e.g. Slack thread replies)."""

    model_config = ConfigDict(extra="forbid")

    url: str  # URL template; {field} substituted from parent item, ${ENV} from env
    when: Optional[str] = None  # only follow up when this field is truthy/non-zero on the parent item
    response_key: Optional[str] = None  # extract list from follow-up response (e.g. $.messages)
    dedup_key: Optional[str] = None  # field to use for dedup instead of full-object hash
    skip_first: bool = False  # skip first result (e.g. thread parent repeated in Slack replies)


class ConnectorWebhookAuth(BaseModel):
    """HMAC-SHA256 signature verification for webhook sources (e.g. Slack Events API)."""

    model_config = ConfigDict(extra="forbid")

    type: Literal["hmac_sha256"] = "hmac_sha256"
    secret_env: str  # env var name containing the signing secret
    header: str = "X-Slack-Signature"  # header carrying the computed signature
    timestamp_header: Optional[str] = "X-Slack-Request-Timestamp"  # for replay attack protection
    prefix: str = "v0="  # prefix on the signature value (Slack uses "v0=")
    challenge_field: Optional[str] = "challenge"  # field name for URL verification handshake (Slack)


class ConnectorSource(BaseModel):
    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    type: Literal["webhook", "poll", "schedule", "channel-watch"]
    payload_schema: Optional[dict] = Field(default=None, alias="schema")
    webhook_auth: Optional[ConnectorWebhookAuth] = None  # HMAC auth for webhook sources
    # poll fields
    url: Optional[str] = None
    method: str = "GET"
    headers: Optional[dict[str, str]] = None
    interval: Optional[str] = None
    response_key: Optional[str] = None
    dedup_key: Optional[str] = None  # field to use for deduplication instead of full-object hash
    follow_up: Optional[ConnectorFollowUp] = None  # per-item follow-up fetch (e.g. thread replies)
    # schedule fields
    cron: Optional[str] = None
    # channel-watch fields
    channel: Optional[str] = None
    pattern: Optional[str] = None
    # poll extended fields
    transform: Optional[str] = None  # dot-path extraction applied after response_key (e.g. $.data.results)
    auth: Optional[str] = None  # named service grant for authenticated endpoints

    @model_validator(mode="after")
    def validate_source_fields(self) -> "ConnectorSource":
        if self.type == "poll":
            if not self.url:
                raise ValueError("poll source requires 'url'")
            if self.interval and self.cron:
                raise ValueError("poll source: 'interval' and 'cron' are mutually exclusive")
            if not self.interval and not self.cron:
                raise ValueError("poll source requires exactly one of 'interval' or 'cron'")
            if self.interval and not _INTERVAL_PATTERN.match(self.interval):
                raise ValueError(f"Invalid interval format: {self.interval} (expected e.g. '30s', '5m', '1h', '1d')")
        elif self.type == "schedule":
            if not self.cron:
                raise ValueError("schedule source requires 'cron'")
        elif self.type == "channel-watch":
            if not self.channel:
                raise ValueError("channel-watch source requires 'channel'")
            if not self.pattern:
                raise ValueError("channel-watch source requires 'pattern'")
        elif self.type == "webhook":
            poll_fields = [self.url, self.interval, self.response_key, self.cron, self.channel, self.pattern, self.transform, self.auth]
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
    headers: Optional[dict[str, str]] = None  # ${ENV} expanded
    body: str  # Jinja2 template rendered with payload fields, then ${ENV} expanded
    content_type: str = "application/json"


class ConnectorRoute(BaseModel):
    model_config = ConfigDict(extra="forbid")

    match: dict[str, Union[str, list[str], None]]
    # Agent/team routing: deliver a task to an agent or team via DM channel
    target: Optional[dict[str, str]] = None
    # Relay routing: POST directly to an HTTP endpoint, no agent spawned
    relay: Optional[ConnectorRelayTarget] = None
    priority: Literal["high", "normal", "low"] = "normal"
    sla: Optional[str] = None
    brief: Optional[str] = None  # Jinja2 template for task brief delivered to agent

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
    path: str
    parameters: Optional[dict] = None
    description: str = ""


class ConnectorMCP(BaseModel):
    model_config = ConfigDict(extra="forbid")

    name: str
    credential: str
    api_base: Optional[str] = None
    server: Optional[str] = None
    tools: Optional[list[ConnectorMCPTool]] = None


class ConnectorCredential(BaseModel):
    """A single credential requirement for a connector."""
    model_config = ConfigDict(extra="forbid")
    name: str
    description: str = ""
    type: Literal["secret", "config"] = "secret"
    scope: Literal["service-grant", "env-var", "file"] = "service-grant"
    grant_name: Optional[str] = None
    setup_url: Optional[str] = None
    example: Optional[str] = None


class ConnectorAuth(BaseModel):
    """Authentication method for a connector's external API."""
    model_config = ConfigDict(extra="forbid")
    type: Literal["none", "bearer", "jwt-exchange", "oauth2"] = "none"
    token_url: Optional[str] = None
    token_params: Optional[dict[str, str]] = None
    token_response_field: str = "access_token"
    token_ttl_seconds: int = 3600


class ConnectorRequires(BaseModel):
    model_config = ConfigDict(extra="forbid")

    services: list[str] = Field(default_factory=list)
    credentials: list[ConnectorCredential] = Field(default_factory=list)
    auth: Optional[ConnectorAuth] = None
    egress_domains: list[str] = Field(default_factory=list)


class ConnectorRateLimits(BaseModel):
    model_config = ConfigDict(extra="forbid")

    max_per_hour: int = 100
    max_concurrent: int = 10


class GraphIngestNode(BaseModel):
    """Node upsert rule for graph_ingest."""
    kind: str
    label: str                         # Jinja2 template
    properties: dict[str, str] = {}    # Jinja2 templates for values


class GraphIngestEdge(BaseModel):
    """Edge upsert rule for graph_ingest."""
    relation: str
    from_label: str                    # Jinja2 template
    to_kind: str
    to_label: str                      # Jinja2 template


class CorrelateConfig(BaseModel):
    """Cross-source correlation config for graph_ingest rules."""
    source: str              # Name of another active connector
    on: str                  # Field name to join on
    window_seconds: int = 60


class GraphIngestRule(BaseModel):
    """Single graph_ingest rule with optional match filter."""
    match: Optional[dict] = None       # Same semantics as route match; None = all events
    nodes: list[GraphIngestNode] = []
    edges: list[GraphIngestEdge] = []
    correlate: Optional[CorrelateConfig] = None


class ConnectorConfig(BaseModel):
    model_config = ConfigDict(extra="forbid")

    kind: Literal["connector"] = "connector"
    name: str
    version: str = "1.0.0"
    description: str = ""
    author: str = ""
    license: str = ""
    requires: Optional[ConnectorRequires] = None
    source: ConnectorSource
    routes: list[ConnectorRoute] = []
    mcp: Optional[ConnectorMCP] = None
    rate_limits: ConnectorRateLimits = Field(default_factory=ConnectorRateLimits)
    graph_ingest: list[GraphIngestRule] = []

    @model_validator(mode="after")
    def _routes_or_graph_ingest(self) -> "ConnectorConfig":
        if not self.routes and not self.graph_ingest:
            raise ValueError("Connector must define at least one route or graph_ingest rule")
        return self
