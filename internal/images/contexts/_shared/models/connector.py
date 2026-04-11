"""Pydantic models for connector schema — external system bindings."""

import re
from urllib.parse import urlsplit
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
    secret_env: str  # env var name containing the signing secret
    header: str = "X-Slack-Signature"  # header carrying the computed signature
    timestamp_header: str | None = "X-Slack-Request-Timestamp"  # for replay attack protection
    prefix: str = "v0="  # prefix on the signature value (Slack uses "v0=")
    challenge_field: str | None = "challenge"  # field name for URL verification handshake (Slack)


class ConnectorSource(BaseModel):
    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    type: Literal["webhook", "poll", "schedule", "channel-watch", "none"]
    payload_schema: dict | None = Field(default=None, alias="schema")
    webhook_auth: ConnectorWebhookAuth | None = None  # HMAC auth for webhook sources
    path: str | None = None
    body_format: Literal["json", "form_urlencoded", "form_urlencoded_payload_json_field"] | None = None
    payload_field: str | None = None
    response_status: int | None = None
    response_body: str | None = None
    response_content_type: str | None = None
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
        if self.type == "none":
            fields = [
                self.webhook_auth,
                self.path,
                self.body_format,
                self.payload_field,
                self.response_status,
                self.response_body,
                self.response_content_type,
                self.url,
                self.interval,
                self.response_key,
                self.cron,
                self.channel,
                self.pattern,
                self.follow_up,
            ]
            if self.headers is not None:
                fields.append("set")
            if self.method != "GET":
                fields.append("set")
            if any(f is not None for f in fields):
                raise ValueError("none source does not accept webhook/poll/schedule/channel-watch fields")
        elif self.type == "poll":
            if not self.url:
                raise ValueError("poll source requires 'url'")
            if not self.interval:
                raise ValueError("poll source requires 'interval'")
            if not _INTERVAL_PATTERN.match(self.interval):
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
            if self.path:
                parts = urlsplit(self.path)
                if not self.path.startswith("/") or parts.scheme or parts.netloc or parts.query or parts.fragment:
                    raise ValueError("webhook source path must be an absolute path without query or fragment")
            if self.payload_field and self.body_format != "form_urlencoded_payload_json_field":
                raise ValueError("payload_field is only valid with body_format 'form_urlencoded_payload_json_field'")
            if self.response_status is not None and not 200 <= self.response_status <= 299:
                raise ValueError("webhook response_status must be a 2xx status code")
            poll_fields = [self.url, self.interval, self.response_key, self.cron, self.channel, self.pattern]
            if self.headers is not None:
                poll_fields.append("set")
            if self.method != "GET":
                poll_fields.append("set")
            if any(f is not None for f in poll_fields):
                raise ValueError("webhook source does not accept poll/schedule/channel-watch fields")
        elif self.path or self.body_format or self.payload_field or self.response_status is not None or self.response_body is not None or self.response_content_type is not None:
            raise ValueError(f"{self.type} source does not accept webhook body/path fields")
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
    path: str
    parameters: dict | None = None
    description: str = ""


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
    routes: list[ConnectorRoute] = Field(default_factory=list)
    mcp: ConnectorMCP | None = None
    rate_limits: ConnectorRateLimits = Field(default_factory=ConnectorRateLimits)

    @model_validator(mode="after")
    def _routes_or_mcp(self) -> "ConnectorConfig":
        if not self.routes and self.mcp is None:
            raise ValueError("Connector must define at least one route or MCP tool")
        if self.source.type == "none" and self.routes:
            raise ValueError("none source connectors cannot define routes")
        return self
