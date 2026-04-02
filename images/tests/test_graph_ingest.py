"""Tests for connector graph_ingest block."""
import pytest
from images.models.connector import (
    ConnectorConfig, ConnectorSource, GraphIngestRule,
    GraphIngestNode, GraphIngestEdge,
)


class TestGraphIngestModels:
    def test_graph_ingest_node(self):
        node = GraphIngestNode(kind="Alert", label="{{payload.id}}", properties={"severity": "{{payload.severity}}"})
        assert node.kind == "Alert"

    def test_graph_ingest_edge(self):
        edge = GraphIngestEdge(relation="REFERENCES", from_label="{{payload.id}}", to_kind="Device", to_label="{{payload.device}}")
        assert edge.relation == "REFERENCES"

    def test_graph_ingest_rule_with_match(self):
        rule = GraphIngestRule(match={"event_type": "alert_created"}, nodes=[GraphIngestNode(kind="Alert", label="{{payload.id}}")])
        assert rule.match == {"event_type": "alert_created"}

    def test_graph_ingest_rule_no_match_means_all(self):
        rule = GraphIngestRule(nodes=[GraphIngestNode(kind="Alert", label="{{payload.id}}")])
        assert rule.match is None

    def test_connector_with_graph_ingest(self):
        config = ConnectorConfig(name="test-connector", source=ConnectorSource(type="webhook"), routes=[
            {"match": {"event_type": "alert"}, "target": {"agent": "ops"}}
        ], graph_ingest=[
            GraphIngestRule(nodes=[GraphIngestNode(kind="Alert", label="{{payload.id}}")])
        ])
        assert len(config.graph_ingest) == 1


from unittest.mock import patch, MagicMock
from images.intake.graph_ingest import render_sandboxed_template, evaluate_graph_ingest
from images.models.connector import CorrelateConfig


class TestSandboxedTemplateRendering:
    def test_simple_field(self):
        result = render_sandboxed_template("{{payload.name}}", {"payload": {"name": "alice"}})
        assert result == "alice"

    def test_nested_field(self):
        result = render_sandboxed_template("{{payload.alert.id}}", {"payload": {"alert": {"id": "A-123"}}})
        assert result == "A-123"

    def test_missing_field_returns_empty(self):
        result = render_sandboxed_template("{{payload.missing}}", {"payload": {"name": "alice"}})
        assert result == ""

    def test_blocks_dunder_access(self):
        # SandboxedEnvironment blocks dunder access — returns empty string via Undefined
        # rather than raising, which still prevents information leakage
        result = render_sandboxed_template("{{''.__ class__}}".replace("__ class__", "__class__"), {"payload": {}})
        assert result == ""


class TestEvaluateGraphIngest:
    @patch("images.intake.graph_ingest._post_node")
    def test_upserts_matching_node(self, mock_post):
        mock_post.return_value = "node-abc123"
        rules = [GraphIngestRule(match={"event_type": "alert"}, nodes=[GraphIngestNode(kind="Alert", label="{{payload.id}}", properties={"severity": "{{payload.severity}}"})])]
        payload = {"event_type": "alert", "id": "A-1", "severity": "high"}
        count = evaluate_graph_ingest(rules, payload, "http://knowledge:18092", "test-conn", "wi-001")
        assert count == 1
        call_args = mock_post.call_args[0]
        assert call_args[1]["label"] == "A-1"
        assert call_args[1]["properties"]["severity"] == "high"
        assert call_args[1]["properties"]["_provenance_connector"] == "test-conn"

    @patch("images.intake.graph_ingest._post_node")
    def test_skips_non_matching(self, mock_post):
        rules = [GraphIngestRule(match={"event_type": "incident"}, nodes=[GraphIngestNode(kind="Incident", label="{{payload.id}}")])]
        count = evaluate_graph_ingest(rules, {"event_type": "alert", "id": "A-1"}, "http://knowledge:18092", "test-conn", "wi-001")
        assert count == 0
        mock_post.assert_not_called()

    @patch("images.intake.graph_ingest._post_node")
    def test_no_match_means_all(self, mock_post):
        mock_post.return_value = "node-abc123"
        rules = [GraphIngestRule(nodes=[GraphIngestNode(kind="Event", label="{{payload.id}}")])]
        count = evaluate_graph_ingest(rules, {"id": "E-1"}, "http://knowledge:18092", "test-conn", "wi-001")
        assert count == 1


class TestCorrelationTemplateInjection:
    """Tests for cross-source correlation in graph_ingest templates."""

    @patch("images.intake.graph_ingest._post_node")
    @patch("images.intake.graph_ingest._post_edge")
    def test_correlated_payload_available_in_templates(self, mock_edge, mock_node):
        mock_node.return_value = "node-1"
        mock_edge.return_value = True

        # Simulate: unifi-sites payload has hostId, correlated unifi payload has hostName
        rules = [GraphIngestRule(
            correlate=CorrelateConfig(source="unifi", on="hostId", window_seconds=3600),
            nodes=[
                GraphIngestNode(kind="network_segment", label="{{payload.siteName}}"),
            ],
            edges=[
                GraphIngestEdge(
                    relation="ON_SEGMENT",
                    from_label="{{correlated.hostName or payload.hostId}}",
                    to_label="{{payload.siteName}}",
                    from_kind="Device",
                    to_kind="network_segment",
                ),
            ],
        )]

        payload = {"hostId": "abc-123", "siteName": "Home"}

        # Mock event buffer that returns the correlated payload from the unifi connector
        buffer = MagicMock()
        buffer.lookup.return_value = {"hostId": "abc-123", "hostName": "Chagall"}

        count = evaluate_graph_ingest(rules, payload, "http://knowledge:18092", "unifi-sites", "wi-001", event_buffer=buffer)

        assert count == 1
        buffer.lookup.assert_called_once_with("unifi", "hostId", "abc-123", 3600)

        # Check the edge used the correlated hostName, not the payload hostId
        edge_call = mock_edge.call_args[0]
        assert edge_call[1] == "Chagall"  # from_label resolved via correlated.hostName
        assert edge_call[2] == "Home"     # to_label from payload.siteName

    @patch("images.intake.graph_ingest._post_node")
    def test_no_correlation_match_skips_rule(self, mock_node):
        rules = [GraphIngestRule(
            correlate=CorrelateConfig(source="unifi", on="hostId", window_seconds=3600),
            nodes=[GraphIngestNode(kind="network_segment", label="{{payload.siteName}}")],
        )]

        payload = {"hostId": "abc-123", "siteName": "Home"}

        buffer = MagicMock()
        buffer.lookup.return_value = None  # No match

        count = evaluate_graph_ingest(rules, payload, "http://knowledge:18092", "unifi-sites", "wi-001", event_buffer=buffer)

        assert count == 0
        mock_node.assert_not_called()

    @patch("images.intake.graph_ingest._post_node")
    def test_correlated_fallback_when_field_missing(self, mock_node):
        mock_node.return_value = "node-1"

        rules = [GraphIngestRule(
            correlate=CorrelateConfig(source="unifi", on="hostId", window_seconds=3600),
            nodes=[
                GraphIngestNode(
                    kind="Device",
                    # correlated.missingField is empty → falls back to payload.hostId
                    label="{{correlated.missingField or payload.hostId}}",
                ),
            ],
        )]

        payload = {"hostId": "abc-123"}

        buffer = MagicMock()
        buffer.lookup.return_value = {"hostId": "abc-123"}  # No missingField

        count = evaluate_graph_ingest(rules, payload, "http://knowledge:18092", "test", "wi-001", event_buffer=buffer)

        assert count == 1
        node_call = mock_node.call_args[0]
        assert node_call[1]["label"] == "abc-123"  # Fell back to payload.hostId
