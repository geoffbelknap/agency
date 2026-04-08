"""Tests for the gateway HTTP client."""
import os
import sys
import unittest
from unittest.mock import AsyncMock, MagicMock, patch

import pytest

# Add intake source directory to path so we can import gateway_client directly.
sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "intake"))

from gateway_client import GatewayClient  # noqa: E402


class TestGatewayClient(unittest.TestCase):
    """Unit tests for GatewayClient."""

    # ---- init ----

    def test_init_defaults(self):
        client = GatewayClient()
        assert client.base_url == "http://gateway:8200"
        assert client.token == ""

    def test_init_custom_url(self):
        client = GatewayClient(base_url="http://localhost:9000/", token="tok")
        assert client.base_url == "http://localhost:9000"
        assert client.token == "tok"

    # ---- headers ----

    def test_headers_with_token(self):
        client = GatewayClient(token="secret")
        headers = client._headers()
        assert headers["Authorization"] == "Bearer secret"
        assert headers["Content-Type"] == "application/json"

    def test_headers_without_token(self):
        client = GatewayClient()
        headers = client._headers()
        assert "Authorization" not in headers

    @patch.dict(os.environ, {"AGENCY_CALLER": "intake"})
    def test_headers_with_caller(self):
        client = GatewayClient()
        headers = client._headers()
        assert headers["X-Agency-Caller"] == "intake"

    @patch.dict(os.environ, {}, clear=True)
    def test_headers_without_caller(self):
        client = GatewayClient()
        headers = client._headers()
        assert "X-Agency-Caller" not in headers

    # ---- publish_event ----

    @pytest.mark.asyncio
    async def test_publish_event(self):
        client = GatewayClient(base_url="http://gw:8200", token="t")

        mock_resp = AsyncMock()
        mock_resp.status = 200

        mock_session_ctx = AsyncMock()
        mock_session_ctx.__aenter__ = AsyncMock(return_value=mock_session_ctx)
        mock_session_ctx.__aexit__ = AsyncMock(return_value=False)
        mock_session_ctx.post = AsyncMock(return_value=mock_resp)

        with patch("gateway_client.aiohttp.ClientSession", return_value=mock_session_ctx):
            await client.publish_event(
                source_name="intake",
                event_type="doc.ingested",
                data={"id": "123"},
                metadata={"source": "test"},
            )

        mock_session_ctx.post.assert_called_once()
        args, kwargs = mock_session_ctx.post.call_args
        assert args[0] == "http://gw:8200/api/v1/events/publish"
        payload = kwargs["json"]
        assert payload["source_type"] == "platform"
        assert payload["source_name"] == "intake"
        assert payload["event_type"] == "doc.ingested"
        assert payload["data"] == {"id": "123"}
        assert payload["metadata"] == {"source": "test"}

    # ---- graph_ingest ----

    @pytest.mark.asyncio
    async def test_graph_ingest(self):
        client = GatewayClient(base_url="http://gw:8200")

        mock_resp = AsyncMock()
        mock_resp.status = 200
        mock_resp.json = AsyncMock(return_value={"status": "ok"})

        mock_session_ctx = AsyncMock()
        mock_session_ctx.__aenter__ = AsyncMock(return_value=mock_session_ctx)
        mock_session_ctx.__aexit__ = AsyncMock(return_value=False)
        mock_session_ctx.post = AsyncMock(return_value=mock_resp)

        with patch("gateway_client.aiohttp.ClientSession", return_value=mock_session_ctx):
            result = await client.graph_ingest(
                content='{"key": "val"}',
                filename="data.json",
            )

        assert result == {"status": "ok"}
        args, kwargs = mock_session_ctx.post.call_args
        assert args[0] == "http://gw:8200/api/v1/graph/ingest"
        payload = kwargs["json"]
        assert payload["content"] == '{"key": "val"}'
        assert payload["filename"] == "data.json"
        assert payload["content_type"] == "application/json"

    # ---- post_channel_message ----

    @pytest.mark.asyncio
    async def test_post_channel_message(self):
        client = GatewayClient(base_url="http://gw:8200")

        mock_resp = AsyncMock()
        mock_resp.status = 201
        mock_resp.json = AsyncMock(return_value={"id": "msg1"})

        mock_session_ctx = AsyncMock()
        mock_session_ctx.__aenter__ = AsyncMock(return_value=mock_session_ctx)
        mock_session_ctx.__aexit__ = AsyncMock(return_value=False)
        mock_session_ctx.post = AsyncMock(return_value=mock_resp)

        with patch("gateway_client.aiohttp.ClientSession", return_value=mock_session_ctx):
            result = await client.post_channel_message(
                channel_name="general",
                content="hello",
                author="bot",
            )

        assert result == {"id": "msg1"}
        args, _ = mock_session_ctx.post.call_args
        assert args[0] == "http://gw:8200/api/v1/comms/channels/general/messages"

    # ---- get_channel_messages ----

    @pytest.mark.asyncio
    async def test_get_channel_messages(self):
        client = GatewayClient(base_url="http://gw:8200")

        mock_resp = AsyncMock()
        mock_resp.status = 200
        mock_resp.json = AsyncMock(return_value=[{"id": "m1"}])

        mock_session_ctx = AsyncMock()
        mock_session_ctx.__aenter__ = AsyncMock(return_value=mock_session_ctx)
        mock_session_ctx.__aexit__ = AsyncMock(return_value=False)
        mock_session_ctx.get = AsyncMock(return_value=mock_resp)

        with patch("gateway_client.aiohttp.ClientSession", return_value=mock_session_ctx):
            result = await client.get_channel_messages(
                channel_name="alerts",
                since="2025-01-01T00:00:00Z",
                limit=50,
            )

        assert result == [{"id": "m1"}]
        args, kwargs = mock_session_ctx.get.call_args
        assert args[0] == "http://gw:8200/api/v1/comms/channels/alerts/messages"
        assert kwargs["params"]["limit"] == "50"
        assert kwargs["params"]["since"] == "2025-01-01T00:00:00Z"
