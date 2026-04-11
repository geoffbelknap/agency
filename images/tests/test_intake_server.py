"""Tests for intake HTTP server."""

import json
import os
import sys
import types
from pathlib import Path
from urllib.parse import urlencode
from unittest.mock import AsyncMock, MagicMock, patch

import pytest
import yaml
from images.tests.support.agency_hub_fixtures import load_agency_hub_connector

# Intake server imports scheduler at module import time. In lean local envs
# croniter may be absent, so provide a narrow stub only in that case.
try:
    import croniter  # noqa: F401
except ModuleNotFoundError:
    croniter_stub = types.ModuleType("croniter")
    croniter_stub.croniter = type("Croniter", (), {"match": staticmethod(lambda expr, dt: False)})
    sys.modules.setdefault("croniter", croniter_stub)
sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from images.intake.server import create_app, _load_connectors

pytestmark = pytest.mark.asyncio


@pytest.fixture
def connectors_dir(tmp_path):
    d = tmp_path / "connectors"
    d.mkdir()
    # Write a test connector
    (d / "test-connector.yaml").write_text(yaml.dump({
        "kind": "connector",
        "name": "test-connector",
        "source": {"type": "webhook"},
        "routes": [
            {
                "match": {"severity": "critical"},
                "target": {"agent": "lead"},
                "priority": "high",
                "sla": "15m",
            },
            {
                "match": {"severity": "*"},
                "target": {"agent": "analyst"},
            },
        ],
        "rate_limits": {"max_per_hour": 100, "max_concurrent": 5},
    }))
    return d


@pytest.fixture
def data_dir(tmp_path):
    d = tmp_path / "data"
    d.mkdir()
    return d


@pytest.fixture
def intake_app(connectors_dir, data_dir):
    return create_app(connectors_dir=connectors_dir, data_dir=data_dir)


class TestIntakeHealth:
    async def test_health(self, aiohttp_client, intake_app):
        client = await aiohttp_client(intake_app)
        resp = await client.get("/health")
        assert resp.status == 200
        data = await resp.json()
        assert data["status"] == "ok"


class TestWebhookEndpoint:
    async def test_valid_webhook(self, aiohttp_client, intake_app):
        client = await aiohttp_client(intake_app)
        with patch("images.intake.server._deliver_task", new_callable=AsyncMock) as mock_deliver:
            mock_deliver.return_value = True
            resp = await client.post(
                "/webhooks/test-connector",
                json={"severity": "critical", "title": "Suspicious login"},
            )
            assert resp.status == 202
            data = await resp.json()
            assert data["status"] == "ok"
            assert data["delivered"] is True
            mock_deliver.assert_called_once()

    async def test_unknown_connector_404(self, aiohttp_client, intake_app):
        client = await aiohttp_client(intake_app)
        resp = await client.post("/webhooks/nonexistent", json={"x": 1})
        assert resp.status == 404

    async def test_invalid_json_400(self, aiohttp_client, intake_app):
        client = await aiohttp_client(intake_app)
        resp = await client.post(
            "/webhooks/test-connector",
            data=b"not json",
            headers={"Content-Type": "application/json"},
        )
        assert resp.status == 400

    async def test_custom_webhook_path(self, aiohttp_client, connectors_dir, data_dir):
        (connectors_dir / "custom-path.yaml").write_text(yaml.dump({
            "kind": "connector",
            "name": "custom-path",
            "source": {"type": "webhook", "path": "/hooks/custom-path"},
            "routes": [{"match": {"severity": "*"}, "target": {"agent": "analyst"}}],
        }))
        app = create_app(connectors_dir=connectors_dir, data_dir=data_dir)
        client = await aiohttp_client(app)
        with patch("images.intake.server._deliver_task", new_callable=AsyncMock) as mock_deliver:
            mock_deliver.return_value = True
            resp = await client.post("/hooks/custom-path", json={"severity": "medium"})
            assert resp.status == 202
            data = await resp.json()
            assert data["delivered"] is True

    async def test_form_urlencoded_body(self, aiohttp_client, connectors_dir, data_dir):
        (connectors_dir / "form-webhook.yaml").write_text(yaml.dump({
            "kind": "connector",
            "name": "form-webhook",
            "source": {"type": "webhook", "path": "/hooks/form", "body_format": "form_urlencoded"},
            "routes": [{"match": {"severity": "*"}, "target": {"agent": "analyst"}}],
        }))
        app = create_app(connectors_dir=connectors_dir, data_dir=data_dir)
        client = await aiohttp_client(app)
        with patch("images.intake.server._deliver_task", new_callable=AsyncMock) as mock_deliver:
            mock_deliver.return_value = True
            resp = await client.post(
                "/hooks/form",
                data=urlencode({"severity": "high", "title": "Encoded"}),
                headers={"Content-Type": "application/x-www-form-urlencoded"},
            )
            assert resp.status == 202
            assert mock_deliver.await_count == 1

    async def test_wrapped_form_json_body(self, aiohttp_client, connectors_dir, data_dir):
        (connectors_dir / "wrapped-webhook.yaml").write_text(yaml.dump({
            "kind": "connector",
            "name": "wrapped-webhook",
            "source": {
                "type": "webhook",
                "path": "/hooks/wrapped",
                "body_format": "form_urlencoded_payload_json_field",
                "payload_field": "event",
            },
            "routes": [{"match": {"severity": "*"}, "target": {"agent": "analyst"}}],
        }))
        app = create_app(connectors_dir=connectors_dir, data_dir=data_dir)
        client = await aiohttp_client(app)
        with patch("images.intake.server._deliver_task", new_callable=AsyncMock) as mock_deliver:
            mock_deliver.return_value = True
            resp = await client.post(
                "/hooks/wrapped",
                data=urlencode({"event": json.dumps({"severity": "critical", "title": "Wrapped"})}),
                headers={"Content-Type": "application/x-www-form-urlencoded"},
            )
            assert resp.status == 202
            assert mock_deliver.await_count == 1

    async def test_no_matching_route_202(self, aiohttp_client, intake_app, connectors_dir):
        """Connector with no wildcard, payload doesn't match any route."""
        (connectors_dir / "strict-connector.yaml").write_text(yaml.dump({
            "kind": "connector",
            "name": "strict-connector",
            "source": {"type": "webhook"},
            "routes": [
                {"match": {"type": "specific"}, "target": {"agent": "a"}},
            ],
        }))
        intake_app["connectors"] = _load_connectors(connectors_dir)

        client = await aiohttp_client(intake_app)
        resp = await client.post(
            "/webhooks/strict-connector",
            json={"type": "other"},
        )
        assert resp.status == 202
        data = await resp.json()
        assert data.get("status") == "ok"
        assert data.get("delivered") is False

    async def test_route_match_sets_priority(self, aiohttp_client, intake_app):
        client = await aiohttp_client(intake_app)
        with patch("images.intake.server._deliver_task", new_callable=AsyncMock) as mock_deliver:
            mock_deliver.return_value = True
            resp = await client.post(
                "/webhooks/test-connector",
                json={"severity": "critical", "title": "Test"},
            )
            assert resp.status == 202

    async def test_slack_events_delivery_builds_normalized_bridge_metadata(self, aiohttp_client, connectors_dir, data_dir):
        slack_data = load_agency_hub_connector("connectors/slack-events/connector.yaml").model_dump(mode="json", exclude_none=True)
        slack_data["source"].pop("webhook_auth", None)
        (connectors_dir / "slack-events.yaml").write_text(yaml.safe_dump(slack_data))
        app = create_app(connectors_dir=connectors_dir, data_dir=data_dir)
        client = await aiohttp_client(app)

        payload = {
            "type": "event_callback",
            "team_id": "T123",
            "event": {
                "type": "message",
                "user": "U123",
                "text": "hello <@U0YOURBOTUSERID>",
                "ts": "1712860000.1234",
                "channel": "D123",
            },
        }

        with patch("images.intake.server._deliver_task", new_callable=AsyncMock) as mock_deliver:
            mock_deliver.return_value = True
            resp = await client.post("/webhooks/slack-events", json=payload)
            assert resp.status == 200

        kwargs = mock_deliver.await_args.kwargs
        metadata = kwargs["metadata"]
        assert metadata["connector_name"] == "slack-events"
        assert metadata["bridge"] == {
            "platform": "slack",
            "workspace_id": "T123",
            "user_id": "U123",
            "channel_id": "D123",
            "message_ts": "1712860000.1234",
            "thread_ts": "1712860000.1234",
            "root_ts": "1712860000.1234",
            "conversation_key": "slack:D123:1712860000.1234",
            "conversation_kind": "dm",
        }
        assert metadata["principal"] == {
            "platform": "slack",
            "workspace_id": "T123",
            "user_id": "U123",
            "channel_id": "D123",
            "conversation_key": "slack:D123:1712860000.1234",
            "is_dm": True,
        }
        stored = app["bridge_state"].get_conversation("slack:D123:1712860000.1234")
        assert stored is not None
        assert stored["target_agent"] == "slack-bridge"
        assert stored["metadata"]["principal"]["user_id"] == "U123"

    async def test_slack_events_known_conversation_enriches_bridge_metadata(self, aiohttp_client, connectors_dir, data_dir):
        slack_data = load_agency_hub_connector("connectors/slack-events/connector.yaml").model_dump(mode="json", exclude_none=True)
        slack_data["source"].pop("webhook_auth", None)
        (connectors_dir / "slack-events.yaml").write_text(yaml.safe_dump(slack_data))
        app = create_app(connectors_dir=connectors_dir, data_dir=data_dir)
        app["bridge_state"].upsert_conversation(
            "slack:D123:1712860000.1234",
            platform="slack",
            workspace_id="T123",
            channel_id="D123",
            root_ts="1712860000.1234",
            thread_ts="1712860000.1234",
            conversation_kind="dm",
            user_id="U123",
            target_agent="slack-bridge",
            connector_name="slack-events",
            metadata={"bridge": {"conversation_key": "slack:D123:1712860000.1234"}},
        )
        client = await aiohttp_client(app)

        payload = {
            "type": "event_callback",
            "team_id": "T123",
            "event": {
                "type": "message",
                "user": "U123",
                "text": "hello <@U0YOURBOTUSERID>",
                "ts": "1712860000.1234",
                "channel": "D123",
            },
        }

        with patch("images.intake.server._deliver_task", new_callable=AsyncMock) as mock_deliver:
            mock_deliver.return_value = True
            resp = await client.post("/webhooks/slack-events", json=payload)
            assert resp.status == 200

        metadata = mock_deliver.await_args.kwargs["metadata"]
        assert metadata["bridge"]["known"] is True
        assert metadata["bridge"]["target_agent"] == "slack-bridge"
        assert metadata["principal"]["known"] is True


class TestRateLimiting:
    async def test_rate_limit_max_per_hour(self, aiohttp_client, intake_app, connectors_dir):
        """Create connector with low max_per_hour, verify 429 after exceeding."""
        (connectors_dir / "limited.yaml").write_text(yaml.dump({
            "kind": "connector",
            "name": "limited",
            "source": {"type": "webhook"},
            "routes": [{"match": {"x": "*"}, "target": {"agent": "a"}}],
            "rate_limits": {"max_per_hour": 2, "max_concurrent": 100},
        }))
        intake_app["connectors"] = _load_connectors(connectors_dir)

        client = await aiohttp_client(intake_app)
        with patch("images.intake.server._deliver_task", new_callable=AsyncMock) as mock_deliver:
            mock_deliver.return_value = True
            # First two should succeed
            for _ in range(2):
                resp = await client.post("/webhooks/limited", json={"x": "1"})
                assert resp.status == 202
            # Third should be rate limited
            resp = await client.post("/webhooks/limited", json={"x": "1"})
            assert resp.status == 429

    async def test_unknown_custom_webhook_path_404(self, aiohttp_client, intake_app):
        client = await aiohttp_client(intake_app)
        resp = await client.post("/hooks/missing", json={"x": 1})
        assert resp.status == 404
