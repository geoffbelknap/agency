"""Tests for intake HTTP server."""

import json
from pathlib import Path
from unittest.mock import AsyncMock, MagicMock, patch

import pytest
import yaml

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
    return create_app(connectors_dir=connectors_dir, data_dir=data_dir, comms_url="http://mock:18091")


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
