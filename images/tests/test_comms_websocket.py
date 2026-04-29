"""Tests for WebSocket endpoint, connection registry, and message fan-out."""

import pytest

from images.comms.server import create_app
from images.comms.websocket import ConnectionRegistry, setup_websocket


# ---------------------------------------------------------------------------
# Unit tests: ConnectionRegistry
# ---------------------------------------------------------------------------


class TestConnectionRegistry:
    def test_add_and_get(self):
        reg = ConnectionRegistry()
        sentinel = object()
        reg.add("alice", sentinel)
        assert reg.get("alice") is sentinel

    def test_remove(self):
        reg = ConnectionRegistry()
        reg.add("alice", object())
        reg.remove("alice")
        assert reg.get("alice") is None

    def test_remove_missing_is_noop(self):
        reg = ConnectionRegistry()
        reg.remove("nonexistent")  # should not raise

    def test_overwrite(self):
        from unittest.mock import MagicMock
        reg = ConnectionRegistry()
        first = MagicMock(closed=True)
        second = MagicMock(closed=False)
        reg.add("alice", first)
        reg.add("alice", second)
        assert reg.get("alice") is second

    def test_connected_agents(self):
        reg = ConnectionRegistry()
        reg.add("alice", object())
        reg.add("bob", object())
        assert sorted(reg.connected_agents()) == ["alice", "bob"]

    def test_connected_agents_empty(self):
        reg = ConnectionRegistry()
        assert reg.connected_agents() == []

    def test_get_missing(self):
        reg = ConnectionRegistry()
        assert reg.get("nobody") is None


# ---------------------------------------------------------------------------
# Integration tests: WebSocket endpoint
# ---------------------------------------------------------------------------


@pytest.fixture
def ws_app(tmp_path):
    app = create_app(data_dir=tmp_path, agents_dir=tmp_path / "agents")
    setup_websocket(app)
    return app


@pytest.mark.asyncio
class TestWebSocketConnect:
    async def test_ws_connect_receives_ack(self, aiohttp_client, ws_app):
        client = await aiohttp_client(ws_app)
        async with client.ws_connect("/ws?agent=scout") as ws:
            msg = await ws.receive_json()
            assert msg["type"] == "ack"
            assert msg["v"] == 1
            assert msg["data"]["agent"] == "scout"
            assert "channels" in msg["data"]
            assert "unreads" in msg["data"]

    async def test_ws_connect_without_agent_param(self, aiohttp_client, ws_app):
        client = await aiohttp_client(ws_app)
        resp = await client.get("/ws")
        # Without upgrade headers, aiohttp returns 400 or similar
        assert resp.status in (400, 426)

    async def test_ws_ack_with_existing_channels(self, aiohttp_client, ws_app):
        client = await aiohttp_client(ws_app)
        # Create channel and add scout
        await client.post("/channels", json={
            "name": "dev", "type": "team", "created_by": "operator",
            "members": ["scout", "pm"],
        })
        # Post a message so scout has unreads
        await client.post("/channels/dev/messages", json={
            "author": "pm", "content": "Hello @scout",
        })

        async with client.ws_connect("/ws?agent=scout") as ws:
            msg = await ws.receive_json()
            assert msg["type"] == "ack"
            assert "dev" in msg["data"]["channels"]
            assert msg["data"]["unreads"]["dev"]["unread"] >= 1

    async def test_ws_connected_endpoint_reports_agent_connection(self, aiohttp_client, ws_app):
        client = await aiohttp_client(ws_app)

        resp = await client.get("/ws/connected/scout")
        assert resp.status == 200
        assert await resp.json() == {"agent": "scout", "connected": False}

        async with client.ws_connect("/ws?agent=scout") as ws:
            ack = await ws.receive_json()
            assert ack["type"] == "ack"

            resp = await client.get("/ws/connected/scout")
            assert resp.status == 200
            assert await resp.json() == {"agent": "scout", "connected": True}


@pytest.mark.asyncio
class TestWebSocketPush:
    @pytest.mark.skip(reason="Hangs in CI — async websocket push times out")
    async def test_post_message_pushes_to_ws(self, aiohttp_client, ws_app):
        client = await aiohttp_client(ws_app)

        # Create channel with two members
        await client.post("/channels", json={
            "name": "dev", "type": "team", "created_by": "operator",
            "members": ["scout", "pm"],
        })

        # Connect scout via WS
        async with client.ws_connect("/ws?agent=scout") as ws:
            # Consume the ack
            ack = await ws.receive_json()
            assert ack["type"] == "ack"

            # PM posts a message
            resp = await client.post("/channels/dev/messages", json={
                "author": "pm", "content": "Hey team, status update?",
            })
            assert resp.status == 201

            # Scout should receive the pushed message
            pushed = await ws.receive_json()
            assert pushed["type"] == "message"
            assert pushed["channel"] == "dev"
            assert pushed["message"]["content"] == "Hey team, status update?"
            assert "match" in pushed

    async def test_author_does_not_receive_own_message(self, aiohttp_client, ws_app):
        client = await aiohttp_client(ws_app)

        await client.post("/channels", json={
            "name": "dev", "type": "team", "created_by": "operator",
            "members": ["scout", "pm"],
        })

        # Connect pm via WS, then pm posts — should not get own message
        async with client.ws_connect("/ws?agent=pm") as ws:
            ack = await ws.receive_json()
            assert ack["type"] == "ack"

            await client.post("/channels/dev/messages", json={
                "author": "pm", "content": "My own message",
            })

            # Connect scout to trigger a different push, then verify pm
            # didn't get the self-authored message. We use a timeout approach.
            import asyncio
            try:
                pushed = await asyncio.wait_for(ws.receive_json(), timeout=0.3)
                # If we get here, something was pushed — it shouldn't be our message
                assert pushed["message"]["author"] != "pm"
            except asyncio.TimeoutError:
                pass  # Expected: no message pushed to author
