"""Tests for connector schema validation."""

import pytest
import yaml

from images.models.connector import (
    ConnectorConfig,
    ConnectorMCP,
    ConnectorMCPTool,
    ConnectorRateLimits,
    ConnectorRoute,
    ConnectorSource,
)


class TestConnectorSource:
    def test_webhook(self):
        src = ConnectorSource(type="webhook")
        assert src.type == "webhook"
        assert src.payload_schema is None

    def test_webhook_secret_credref(self):
        src = ConnectorSource(
            type="webhook",
            webhook_auth={
                "type": "hmac_sha256",
                "secret_credref": "slack_signing_secret",
            },
        )
        assert src.webhook_auth.secret_credref == "slack_signing_secret"

    def test_tool_only_source(self):
        src = ConnectorSource(type="none")
        assert src.type == "none"

    def test_with_schema(self):
        src = ConnectorSource(
            type="webhook",
            schema={"required": ["alert_id"], "properties": {"alert_id": {"type": "string"}}},
        )
        assert src.payload_schema["required"] == ["alert_id"]

    def test_invalid_type(self):
        with pytest.raises(Exception):
            ConnectorSource(type="ftp")


class TestConnectorRoute:
    def test_minimal(self):
        route = ConnectorRoute(
            match={"severity": "critical"},
            target={"agent": "analyst"},

        )
        assert route.priority == "normal"
        assert route.sla is None

    def test_full(self):
        route = ConnectorRoute(
            match={"severity": ["critical", "high"]},
            target={"team": "soc-team"},

            priority="high",
            sla="15m",
        )
        assert route.priority == "high"
        assert route.sla == "15m"

    def test_invalid_priority(self):
        with pytest.raises(Exception):
            ConnectorRoute(
                match={"x": "y"},
                target={"agent": "a"},

                priority="urgent",
            )


class TestConnectorMCP:
    def test_minimal(self):
        mcp = ConnectorMCP(name="test", credential="cred")
        assert mcp.api_base is None
        assert mcp.tools is None
        assert mcp.server is None

    def test_with_tools(self):
        mcp = ConnectorMCP(
            name="alerts",
            credential="splunk",
            api_base="https://splunk.example.com",
            tools=[
                ConnectorMCPTool(
                    name="get_alert",
                    method="GET",
                    path="/alerts/{{alert_id}}",
                    description="Get alert details",
                ),
            ],
        )
        assert len(mcp.tools) == 1
        assert mcp.tools[0].name == "get_alert"

    def test_with_server(self):
        mcp = ConnectorMCP(
            name="custom",
            credential="svc",
            server="/usr/local/bin/mcp-server",
        )
        assert mcp.server == "/usr/local/bin/mcp-server"

    def test_with_consent_directive(self):
        tool = ConnectorMCPTool(
            name="drive_add_whitelist_entry",
            method="POST",
            path="/drive/v3/files",
            parameters={
                "drive_id": {"type": "string"},
                "consent_token": {"type": "string"},
            },
            whitelist_check="drive_id",
            requires_consent_token={
                "operation_kind": "add_managed_doc",
                "token_input_field": "consent_token",
                "target_input_field": "drive_id",
            },
        )
        assert tool.requires_consent_token["operation_kind"] == "add_managed_doc"

    def test_tool_with_input_schema(self):
        tool = ConnectorMCPTool(
            name="slack_view_open",
            input_schema={
                "trigger_id": {"type": "string"},
                "view": {"type": "object"},
            },
            returns={"view_id": {"type": "string"}},
        )
        assert tool.input_schema["trigger_id"]["type"] == "string"

    def test_rejects_unknown_consent_field(self):
        with pytest.raises(Exception):
            ConnectorMCPTool(
                name="drive_add_whitelist_entry",
                method="POST",
                path="/drive/v3/files",
                parameters={"drive_id": {"type": "string"}},
                requires_consent_token={
                    "operation_kind": "add_managed_doc",
                    "token_input_field": "consent_token",
                    "target_input_field": "drive_id",
                },
            )


class TestConnectorRateLimits:
    def test_defaults(self):
        rl = ConnectorRateLimits()
        assert rl.max_per_hour == 100
        assert rl.max_concurrent == 10

    def test_custom(self):
        rl = ConnectorRateLimits(max_per_hour=50, max_concurrent=5)
        assert rl.max_per_hour == 50


class TestConnectorConfig:
    def test_minimal(self):
        config = ConnectorConfig(
            name="test-connector",
            source=ConnectorSource(type="webhook"),
            routes=[
                ConnectorRoute(
                    match={"type": "*"},
                    target={"agent": "handler"},
                ),
            ],
        )
        assert config.kind == "connector"
        assert config.version == "1.0.0"
        assert config.rate_limits.max_per_hour == 100

    def test_full_yaml_roundtrip(self):
        raw = {
            "kind": "connector",
            "name": "splunk-soc",
            "version": "1.0.0",
            "description": "Splunk SOAR alert triage",
            "author": "acme",
            "source": {
                "type": "webhook",
                "schema": {
                    "required": ["alert_id", "severity"],
                    "properties": {
                        "alert_id": {"type": "string"},
                        "severity": {"type": "string"},
                    },
                },
            },
            "routes": [
                {
                    "match": {"severity": ["critical", "high"]},
                    "target": {"team": "soc-team"},
                    "priority": "high",
                    "sla": "15m",
                },
                {
                    "match": {"severity": "*"},
                    "target": {"agent": "triage-analyst"},
                },
            ],
            "rate_limits": {"max_per_hour": 50, "max_concurrent": 5},
            "mcp": {
                "name": "splunk-alerts",
                "credential": "splunk",
                "tools": [
                    {
                        "name": "get_alert_details",
                        "method": "GET",
                        "path": "/services/notable/{{alert_id}}",
                        "description": "Get full alert context",
                    },
                ],
            },
        }
        config = ConnectorConfig.model_validate(raw)
        assert config.name == "splunk-soc"
        assert len(config.routes) == 2
        assert config.routes[0].sla == "15m"
        assert config.mcp.tools[0].name == "get_alert_details"
        assert config.rate_limits.max_per_hour == 50

    def test_wrong_kind_rejected(self):
        with pytest.raises(Exception):
            ConnectorConfig(
                kind="pack",
                name="test",
                source=ConnectorSource(type="webhook"),
                routes=[ConnectorRoute(match={"x": "y"}, target={"agent": "a"})],
            )

    def test_extra_fields_rejected(self):
        with pytest.raises(Exception):
            ConnectorConfig(
                name="test",
                source=ConnectorSource(type="webhook"),
                routes=[ConnectorRoute(match={"x": "y"}, target={"agent": "a"})],
                unknown_field="bad",
            )

    def test_empty_routes_rejected(self):
        with pytest.raises(Exception):
            ConnectorConfig(
                name="test",
                source=ConnectorSource(type="webhook"),
                routes=[],
            )

    def test_load_from_yaml_file(self, tmp_path):
        connector_yaml = tmp_path / "connector.yaml"
        connector_yaml.write_text(yaml.dump({
            "kind": "connector",
            "name": "file-test",
            "source": {"type": "webhook"},
            "routes": [{"match": {"event_type": "*"}, "target": {"agent": "responder"}}],
        }))
        data = yaml.safe_load(connector_yaml.read_text())
        config = ConnectorConfig.model_validate(data)
        assert config.name == "file-test"

    def test_tool_only_connector(self):
        config = ConnectorConfig.model_validate(
            {
                "kind": "connector",
                "name": "google-drive-admin",
                "source": {"type": "none"},
                "tools": [
                    {
                        "name": "drive_share_file",
                        "input_schema": {
                            "file_id": {"type": "string"},
                            "email": {"type": "string"},
                        },
                        "whitelist_check": "file_id",
                    }
                ],
            }
        )
        assert config.source.type == "none"
        assert config.tools[0].name == "drive_share_file"


class TestConnectorSourceAdvanced:
    def test_poll_source(self):
        src = ConnectorSource(type="poll", url="https://api.example.com/items", interval="5m")
        assert src.type == "poll"
        assert src.url == "https://api.example.com/items"
        assert src.interval == "5m"
        assert src.method == "GET"
        assert src.headers is None
        assert src.response_key is None

    def test_poll_with_all_fields(self):
        src = ConnectorSource(
            type="poll",
            url="https://api.example.com/items",
            interval="1h",
            method="POST",
            headers={"Authorization": "Bearer token"},
            response_key="$.items",
        )
        assert src.method == "POST"
        assert src.headers == {"Authorization": "Bearer token"}
        assert src.response_key == "$.items"

    def test_poll_missing_url_rejected(self):
        with pytest.raises(Exception):
            ConnectorSource(type="poll", interval="5m")

    def test_poll_missing_interval_rejected(self):
        with pytest.raises(Exception):
            ConnectorSource(type="poll", url="https://example.com")

    def test_schedule_source(self):
        src = ConnectorSource(type="schedule", cron="0 9 * * 1-5")
        assert src.type == "schedule"
        assert src.cron == "0 9 * * 1-5"

    def test_schedule_missing_cron_rejected(self):
        with pytest.raises(Exception):
            ConnectorSource(type="schedule")

    def test_channel_watch_source(self):
        src = ConnectorSource(type="channel-watch", channel="support", pattern="^/request\\s+")
        assert src.type == "channel-watch"
        assert src.channel == "support"
        assert src.pattern == "^/request\\s+"

    def test_channel_watch_missing_channel_rejected(self):
        with pytest.raises(Exception):
            ConnectorSource(type="channel-watch", pattern="^/req")

    def test_channel_watch_missing_pattern_rejected(self):
        with pytest.raises(Exception):
            ConnectorSource(type="channel-watch", channel="support")

    def test_webhook_still_works(self):
        src = ConnectorSource(type="webhook")
        assert src.type == "webhook"

    def test_webhook_rejects_poll_fields(self):
        with pytest.raises(Exception):
            ConnectorSource(type="webhook", url="https://example.com")

    def test_webhook_rejects_method(self):
        with pytest.raises(Exception):
            ConnectorSource(type="webhook", method="POST")

    def test_interval_seconds_accepted(self):
        src = ConnectorSource(type="poll", url="https://example.com", interval="15s")
        assert src.interval == "15s"

    def test_invalid_interval_rejected(self):
        with pytest.raises(Exception):
            ConnectorSource(type="poll", url="https://example.com", interval="bad")
