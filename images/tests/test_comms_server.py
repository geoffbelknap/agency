"""Tests for comms HTTP server endpoints."""

import pytest

from images.comms.server import create_app

pytestmark = pytest.mark.asyncio


@pytest.fixture
def comms_app(tmp_path):
    return create_app(data_dir=tmp_path)


class TestHealthEndpoint:
    async def test_health(self, aiohttp_client, comms_app):
        client = await aiohttp_client(comms_app)
        resp = await client.get("/health")
        assert resp.status == 200
        data = await resp.json()
        assert data["status"] == "ok"


class TestChannelEndpoints:
    async def test_create_channel(self, aiohttp_client, comms_app):
        client = await aiohttp_client(comms_app)
        resp = await client.post("/channels", json={
            "name": "test",
            "type": "team",
            "created_by": "operator",
            "topic": "Test channel",
            "members": ["scout", "pm"],
        })
        assert resp.status == 201
        data = await resp.json()
        assert data["name"] == "test"
        assert data["type"] == "team"
        assert "scout" in data["members"]

    async def test_create_duplicate_channel(self, aiohttp_client, comms_app):
        client = await aiohttp_client(comms_app)
        await client.post("/channels", json={
            "name": "test", "type": "team", "created_by": "operator",
        })
        resp = await client.post("/channels", json={
            "name": "test", "type": "team", "created_by": "operator",
        })
        assert resp.status == 409

    async def test_retire_channel_frees_reusable_name(self, aiohttp_client, comms_app):
        client = await aiohttp_client(comms_app)
        created = await client.post("/channels", json={
            "name": "dm-henry",
            "type": "direct",
            "created_by": "_platform",
            "members": ["henry", "_operator"],
            "visibility": "private",
        })
        assert created.status == 201
        first = await created.json()

        retired = await client.post(
            "/channels/dm-henry/retire",
            json={"retired_by": "_platform"},
            headers={"X-Agency-Platform": "true"},
        )
        assert retired.status == 200
        retired_data = await retired.json()
        assert retired_data["id"] == first["id"]
        assert retired_data["name"].startswith("dm-henry-deleted-")
        assert retired_data["state"] == "archived"

        recreated = await client.post("/channels", json={
            "name": "dm-henry",
            "type": "direct",
            "created_by": "_platform",
            "members": ["henry", "_operator"],
            "visibility": "private",
        })
        assert recreated.status == 201
        second = await recreated.json()
        assert second["name"] == "dm-henry"
        assert second["id"] != first["id"]

    async def test_list_channels(self, aiohttp_client, comms_app):
        client = await aiohttp_client(comms_app)
        await client.post("/channels", json={
            "name": "alpha", "type": "team", "created_by": "operator",
            "members": ["scout"],
        })
        await client.post("/channels", json={
            "name": "beta", "type": "team", "created_by": "operator",
            "members": ["pm"],
        })
        resp = await client.get("/channels", params={"member": "scout"})
        assert resp.status == 200
        data = await resp.json()
        assert len(data) == 1
        assert data[0]["name"] == "alpha"

    async def test_list_all_channels(self, aiohttp_client, comms_app):
        client = await aiohttp_client(comms_app)
        await client.post("/channels", json={
            "name": "alpha", "type": "team", "created_by": "operator",
        })
        await client.post("/channels", json={
            "name": "beta", "type": "team", "created_by": "operator",
        })
        resp = await client.get("/channels")
        assert resp.status == 200
        data = await resp.json()
        assert len(data) == 2

    async def test_join_channel(self, aiohttp_client, comms_app):
        client = await aiohttp_client(comms_app)
        await client.post("/channels", json={
            "name": "test", "type": "team", "created_by": "operator",
        })
        resp = await client.post("/channels/test/join", json={
            "participant": "scout",
        })
        assert resp.status == 200

    async def test_join_nonexistent_channel(self, aiohttp_client, comms_app):
        client = await aiohttp_client(comms_app)
        resp = await client.post("/channels/nope/join", json={
            "participant": "scout",
        })
        assert resp.status == 404


class TestMessageEndpoints:
    async def test_post_message(self, aiohttp_client, comms_app):
        client = await aiohttp_client(comms_app)
        await client.post("/channels", json={
            "name": "test", "type": "team", "created_by": "operator",
            "members": ["scout"],
        })
        resp = await client.post("/channels/test/messages", json={
            "author": "scout",
            "content": "Hello team!",
        })
        assert resp.status == 201
        data = await resp.json()
        assert data["content"] == "Hello team!"
        assert data["id"] is not None

    async def test_post_message_with_flags(self, aiohttp_client, comms_app):
        client = await aiohttp_client(comms_app)
        await client.post("/channels", json={
            "name": "test", "type": "team", "created_by": "operator",
            "members": ["pm"],
        })
        resp = await client.post("/channels/test/messages", json={
            "author": "pm",
            "content": "Launch API first.",
            "flags": {"decision": True},
        })
        assert resp.status == 201
        data = await resp.json()
        assert data["flags"]["decision"] is True

    async def test_post_to_nonexistent_channel(self, aiohttp_client, comms_app):
        client = await aiohttp_client(comms_app)
        resp = await client.post("/channels/nope/messages", json={
            "author": "scout", "content": "Hello",
        })
        assert resp.status == 404

    async def test_read_messages(self, aiohttp_client, comms_app):
        client = await aiohttp_client(comms_app)
        await client.post("/channels", json={
            "name": "test", "type": "team", "created_by": "operator",
            "members": ["scout"],
        })
        await client.post("/channels/test/messages", json={
            "author": "scout", "content": "One",
        })
        await client.post("/channels/test/messages", json={
            "author": "scout", "content": "Two",
        })
        resp = await client.get("/channels/test/messages", params={
            "reader": "scout",
        })
        assert resp.status == 200
        data = await resp.json()
        assert len(data) == 2

    async def test_read_with_limit(self, aiohttp_client, comms_app):
        client = await aiohttp_client(comms_app)
        await client.post("/channels", json={
            "name": "test", "type": "team", "created_by": "operator",
            "members": ["scout"],
        })
        for i in range(5):
            await client.post("/channels/test/messages", json={
                "author": "scout", "content": f"Msg {i}",
            })
        resp = await client.get("/channels/test/messages", params={"limit": "2"})
        data = await resp.json()
        assert len(data) == 2

    async def test_read_messages_accepts_legacy_decoded_utc_offset(self, aiohttp_client, comms_app):
        client = await aiohttp_client(comms_app)
        await client.post("/channels", json={
            "name": "test", "type": "team", "created_by": "operator",
            "members": ["scout"],
        })
        resp = await client.get("/channels/test/messages?since=2026-04-19T01:39:35.833388+00:00")
        assert resp.status == 200

    async def test_read_messages_rejects_invalid_since_without_traceback(self, aiohttp_client, comms_app):
        client = await aiohttp_client(comms_app)
        await client.post("/channels", json={
            "name": "test", "type": "team", "created_by": "operator",
            "members": ["scout"],
        })
        resp = await client.get("/channels/test/messages", params={"since": "not-a-time"})
        data = await resp.json()
        assert resp.status == 400
        assert data["error"] == "since must be an ISO timestamp"


class TestUnreadEndpoints:
    async def test_get_unreads(self, aiohttp_client, comms_app):
        client = await aiohttp_client(comms_app)
        await client.post("/channels", json={
            "name": "test", "type": "team", "created_by": "operator",
            "members": ["scout", "pm"],
        })
        await client.post("/channels/test/messages", json={
            "author": "pm", "content": "Hey @scout",
        })
        resp = await client.get("/unreads/scout")
        assert resp.status == 200
        data = await resp.json()
        assert data["test"]["unread"] == 1
        assert data["test"]["mentions"] == 1

    async def test_mark_read(self, aiohttp_client, comms_app):
        client = await aiohttp_client(comms_app)
        await client.post("/channels", json={
            "name": "test", "type": "team", "created_by": "operator",
            "members": ["scout", "pm"],
        })
        await client.post("/channels/test/messages", json={
            "author": "pm", "content": "Hey",
        })
        await client.post("/channels/test/mark-read", json={
            "participant": "scout",
        })
        resp = await client.get("/unreads/scout")
        data = await resp.json()
        assert data["test"]["unread"] == 0


class TestSearchEndpoint:
    async def test_search_messages(self, aiohttp_client, comms_app):
        client = await aiohttp_client(comms_app)
        await client.post("/channels", json={
            "name": "dev", "type": "team", "created_by": "operator",
            "members": ["scout", "pm"],
        })
        await client.post("/channels/dev/messages", json={
            "author": "scout", "content": "Database schema needs migration",
        })
        await client.post("/channels/dev/messages", json={
            "author": "pm", "content": "API integration ready",
        })
        resp = await client.get("/search", params={"q": "database"})
        assert resp.status == 200
        data = await resp.json()
        assert len(data) >= 1
        assert any("database" in r["content"].lower() for r in data)

    async def test_search_with_channel_filter(self, aiohttp_client, comms_app):
        client = await aiohttp_client(comms_app)
        await client.post("/channels", json={
            "name": "dev", "type": "team", "created_by": "operator",
            "members": ["scout"],
        })
        await client.post("/channels", json={
            "name": "product", "type": "team", "created_by": "operator",
            "members": ["pm"],
        })
        await client.post("/channels/dev/messages", json={
            "author": "scout", "content": "Deploy the schema",
        })
        await client.post("/channels/product/messages", json={
            "author": "pm", "content": "Deploy the pricing page",
        })
        resp = await client.get("/search", params={
            "q": "deploy", "channel": "dev",
        })
        data = await resp.json()
        assert len(data) == 1
        assert data[0]["channel"] == "dev"

    async def test_search_with_participant_visibility(self, aiohttp_client, comms_app):
        client = await aiohttp_client(comms_app)
        await client.post("/channels", json={
            "name": "dev", "type": "team", "created_by": "operator",
            "members": ["scout"],
        })
        await client.post("/channels", json={
            "name": "product", "type": "team", "created_by": "operator",
            "members": ["pm"],
        })
        await client.post("/channels/product/messages", json={
            "author": "pm", "content": "Secret pricing info",
        })
        resp = await client.get("/search", params={
            "q": "pricing", "participant": "scout",
        })
        data = await resp.json()
        assert len(data) == 0

    async def test_search_no_query_returns_400(self, aiohttp_client, comms_app):
        client = await aiohttp_client(comms_app)
        resp = await client.get("/search")
        assert resp.status == 400
