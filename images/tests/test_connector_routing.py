"""Tests for connector routing engine."""

import pytest
from datetime import timedelta

from images.models.connector import ConnectorRoute
from images.intake.router import match_route, evaluate_routes, render_template, parse_sla_duration


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
