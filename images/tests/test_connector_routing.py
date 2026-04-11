"""Tests for connector routing engine."""

import pytest
from datetime import timedelta

from images.models.connector import ConnectorConfig, ConnectorRoute
from images.intake.router import match_route, evaluate_routes, render_template, parse_sla_duration
from images.tests.support.agency_hub_fixtures import load_agency_hub_connector


class TestMatchRoute:
    def test_exact_match(self):
        route = ConnectorRoute(
            match={"severity": "critical"},
            target={"agent": "analyst"},
        )
        assert match_route(route, {"severity": "critical"}) is True
        assert match_route(route, {"severity": "low"}) is False

    def test_list_match(self):
        route = ConnectorRoute(
            match={"severity": ["critical", "high"]},
            target={"agent": "analyst"},
        )
        assert match_route(route, {"severity": "critical"}) is True
        assert match_route(route, {"severity": "high"}) is True
        assert match_route(route, {"severity": "low"}) is False

    def test_wildcard_match(self):
        route = ConnectorRoute(
            match={"severity": "*"},
            target={"agent": "analyst"},
        )
        assert match_route(route, {"severity": "anything"}) is True

    def test_multi_field_and(self):
        route = ConnectorRoute(
            match={"severity": "critical", "source": "firewall"},
            target={"agent": "analyst"},
        )
        assert match_route(route, {"severity": "critical", "source": "firewall"}) is True
        assert match_route(route, {"severity": "critical", "source": "ids"}) is False

    def test_missing_field_no_match(self):
        route = ConnectorRoute(
            match={"severity": "critical"},
            target={"agent": "analyst"},
        )
        assert match_route(route, {"other_field": "value"}) is False


class TestEvaluateRoutes:
    def test_first_match_wins(self):
        routes = [
            ConnectorRoute(match={"severity": "critical"}, target={"agent": "lead"}, priority="high"),
            ConnectorRoute(match={"severity": "*"}, target={"agent": "analyst"}),
        ]
        result = evaluate_routes(routes, {"severity": "critical"})
        assert result is not None
        index, route = result
        assert index == 0
        assert route.target == {"agent": "lead"}

    def test_fallthrough_to_wildcard(self):
        routes = [
            ConnectorRoute(match={"severity": "critical"}, target={"agent": "lead"}),
            ConnectorRoute(match={"severity": "*"}, target={"agent": "analyst"}),
        ]
        result = evaluate_routes(routes, {"severity": "low"})
        assert result is not None
        index, route = result
        assert index == 1

    def test_no_match_returns_none(self):
        routes = [
            ConnectorRoute(match={"severity": "critical"}, target={"agent": "lead"}),
        ]
        result = evaluate_routes(routes, {"severity": "low"})
        assert result is None


class TestRenderTemplate:
    def test_basic_render(self):
        text = render_template("Alert: {{title}}", {"title": "Suspicious login"})
        assert text == "Alert: Suspicious login"

    def test_multiple_fields(self):
        text = render_template(
            "{{severity}} alert: {{title}} (ID: {{alert_id}})",
            {"severity": "critical", "title": "Login", "alert_id": "A123"},
        )
        assert "critical" in text
        assert "Login" in text
        assert "A123" in text

    def test_missing_field_renders_empty(self):
        text = render_template("Alert: {{title}}", {"other": "value"})
        assert text == "Alert: "

    def test_invalid_template_returns_raw(self):
        text = render_template("Alert: {{unclosed", {"title": "test"})
        assert text == "Alert: {{unclosed"


class TestParseSLADuration:
    def test_minutes(self):
        assert parse_sla_duration("15m") == timedelta(minutes=15)

    def test_hours(self):
        assert parse_sla_duration("2h") == timedelta(hours=2)

    def test_none(self):
        assert parse_sla_duration(None) is None

    def test_invalid(self):
        assert parse_sla_duration("bad") is None


class TestSlackConnectorExamples:
    def _load_connector(self, relative_path: str) -> ConnectorConfig:
        return load_agency_hub_connector(relative_path)

    def test_slack_events_mention_route_matches_real_payload(self):
        config = self._load_connector("connectors/slack-events/connector.yaml")
        payload = {
            "type": "event_callback",
            "event": {
                "type": "message",
                "user": "U123",
                "text": "hello <@U0YOURBOTUSERID>",
                "ts": "1712860000.1234",
                "channel": "C123",
            },
        }
        result = evaluate_routes(config.routes, payload)
        assert result is not None
        _, route = result
        rendered = render_template(route.brief, payload)
        assert "U123" in rendered
        assert "C123" in rendered

    def test_slack_events_urgent_regex_route_matches_text(self):
        config = self._load_connector("connectors/slack-events/connector.yaml")
        payload = {
            "type": "event_callback",
            "event": {
                "type": "message",
                "user": "U123",
                "text": "urgent incident in prod",
                "ts": "1712860000.1234",
                "channel": "C123",
            },
        }
        result = evaluate_routes(config.routes[1:], payload)
        assert result is not None

    def test_slack_interactivity_view_submission_route_matches_real_payload(self):
        config = self._load_connector("connectors/slack-interactivity/connector.yaml")
        payload = {
            "type": "view_submission",
            "user": {"id": "U123"},
            "view": {"callback_id": "nomination_form"},
        }
        result = evaluate_routes(config.routes, payload)
        assert result is not None
        _, route = result
        rendered = render_template(route.brief, payload)
        assert "U123" in rendered
        assert "nomination_form" in rendered

    def test_slack_commands_route_matches_real_payload(self):
        config = self._load_connector("connectors/slack-commands/connector.yaml")
        payload = {
            "command": "/agency",
            "user_id": "U123",
            "channel_id": "C123",
            "text": "summarize this thread",
        }
        result = evaluate_routes(config.routes, payload)
        assert result is not None
        _, route = result
        rendered = render_template(route.brief, payload)
        assert "U123" in rendered
        assert "C123" in rendered

    def test_slack_webhook_connectors_configure_200_empty_ack(self):
        for relpath in (
            "connectors/slack-events/connector.yaml",
            "connectors/slack-interactivity/connector.yaml",
            "connectors/slack-commands/connector.yaml",
        ):
            config = self._load_connector(relpath)
            assert config.source.response_status == 200
            assert config.source.response_body == ""
            assert config.source.response_content_type == "text/plain"

    def test_slack_bridge_ingress_connectors_target_slack_bridge(self):
        expected = {
            "connectors/slack-events/connector.yaml": {"slack-bridge"},
            "connectors/slack-interactivity/connector.yaml": {"slack-bridge"},
            "connectors/slack-commands/connector.yaml": {"slack-bridge"},
        }
        for relpath, targets in expected.items():
            config = self._load_connector(relpath)
            assert {route.target.get("agent") for route in config.routes} == targets

    def test_comms_to_slack_renders_author_from_channel_watch_payload(self):
        config = self._load_connector("connectors/comms-to-slack/connector.yaml")
        route = config.routes[0]
        payload = {
            "channel": "general",
            "author": "slack-bridge",
            "content": "hello from agency",
        }
        rendered = render_template(route.relay.body, payload)
        assert "slack-bridge" in rendered
        assert "hello from agency" in rendered

    def test_agency_bridge_slack_outbound_renders_slack_channel_and_thread(self):
        config = self._load_connector("connectors/agency-bridge-slack-outbound/connector.yaml")
        route = config.routes[0]
        payload = {
            "author": "slack-bridge",
            "content": "reply from agency",
            "metadata": {
                "source_payload": {
                    "event": {
                        "channel": "C123",
                        "ts": "1712860000.1234",
                    }
                }
            },
        }
        rendered = render_template(route.relay.body, payload)
        assert '"channel": "C123"' in rendered
        assert '"thread_ts": "1712860000.1234"' in rendered

    def test_agency_bridge_slack_events_outbound_renders_slack_channel_and_thread(self):
        config = self._load_connector("connectors/agency-bridge-slack-events-outbound/connector.yaml")
        route = config.routes[0]
        payload = {
            "author": "slack-bridge",
            "content": "reply from agency",
            "metadata": {
                "bridge": {
                    "channel_id": "C123",
                    "thread_ts": "1712860000.1234",
                },
            },
        }
        rendered = render_template(route.relay.body, payload)
        assert '"channel": "C123"' in rendered
        assert '"thread_ts": "1712860000.1234"' in rendered

    def test_agency_bridge_slack_events_outbound_ignores_artifact_metadata_in_rendering(self):
        config = self._load_connector("connectors/agency-bridge-slack-events-outbound/connector.yaml")
        route = config.routes[0]
        payload = {
            "author": "slack-bridge",
            "content": "reply from agency",
            "metadata": {
                "bridge": {
                    "channel_id": "C123",
                    "thread_ts": "1712860000.1234",
                },
                "has_artifact": True,
                "attachment_id": "task-123",
            },
        }
        rendered = render_template(route.relay.body, payload)
        assert "reply from agency" in rendered
        assert "task-123" not in rendered

    def test_agency_bridge_slack_interactivity_outbound_renders_slack_channel_and_thread(self):
        config = self._load_connector("connectors/agency-bridge-slack-interactivity-outbound/connector.yaml")
        route = config.routes[0]
        payload = {
            "author": "slack-bridge",
            "content": "reply from agency",
            "metadata": {
                "source_payload": {
                    "container": {"channel_id": "C123", "message_ts": "1712860000.1234"},
                }
            },
        }
        rendered = render_template(route.relay.body, payload)
        assert '"channel": "C123"' in rendered
        assert '"thread_ts": "1712860000.1234"' in rendered

    def test_agency_bridge_slack_commands_outbound_renders_slack_channel(self):
        config = self._load_connector("connectors/agency-bridge-slack-commands-outbound/connector.yaml")
        route = config.routes[0]
        payload = {
            "author": "slack-bridge",
            "content": "reply from agency",
            "metadata": {
                "source_payload": {
                    "channel_id": "C123",
                }
            },
        }
        rendered = render_template(route.relay.body, payload)
        assert '"channel": "C123"' in rendered
