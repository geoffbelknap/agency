"""End-to-end test for agent comms through HTTP server.

Tests the full path: create channel -> post messages -> check unreads ->
read messages -> search -> verify cursor advancement.
"""

import pytest

from images.comms.server import create_app

pytestmark = pytest.mark.asyncio


@pytest.fixture
def comms_app(tmp_path):
    return create_app(data_dir=tmp_path)


class TestEndToEndHTTP:
    async def test_full_workflow(self, aiohttp_client, comms_app):
        client = await aiohttp_client(comms_app)

        # Operator creates channel with members
        resp = await client.post("/channels", json={
            "name": "chefhub-beta",
            "type": "team",
            "created_by": "operator",
            "topic": "ChefHub beta readiness",
            "members": ["scout", "pm", "operator"],
        })
        assert resp.status == 201

        # PM posts a decision
        resp = await client.post("/channels/chefhub-beta/messages", json={
            "author": "pm",
            "content": "Recommend Option C: ChefHubDB as separate API product.",
            "flags": {"decision": True},
        })
        assert resp.status == 201
        pm_msg = await resp.json()
        assert pm_msg["flags"]["decision"] is True

        # Scout has 1 unread
        resp = await client.get("/unreads/scout")
        data = await resp.json()
        assert data["chefhub-beta"]["unread"] == 1
        assert data["chefhub-beta"]["mentions"] == 0

        # PM has 0 (own message)
        resp = await client.get("/unreads/pm")
        data = await resp.json()
        assert data["chefhub-beta"]["unread"] == 0

        # Scout reads messages (advances cursor)
        resp = await client.get("/channels/chefhub-beta/messages", params={
            "reader": "scout",
        })
        data = await resp.json()
        assert len(data) == 1
        assert data[0]["flags"]["decision"] is True

        # Scout now has 0 unread
        resp = await client.get("/unreads/scout")
        data = await resp.json()
        assert data["chefhub-beta"]["unread"] == 0

        # Scout replies
        resp = await client.post("/channels/chefhub-beta/messages", json={
            "author": "scout",
            "content": "Agreed. ChefHub already has RecipeAdapter pattern.",
            "reply_to": pm_msg["id"],
        })
        assert resp.status == 201
        reply = await resp.json()
        assert reply["reply_to"] == pm_msg["id"]

        # PM now has 1 unread
        resp = await client.get("/unreads/pm")
        data = await resp.json()
        assert data["chefhub-beta"]["unread"] == 1

        # Operator @mentions both
        resp = await client.post("/channels/chefhub-beta/messages", json={
            "author": "operator",
            "content": "@scout @pm proceed with API-first launch.",
        })
        assert resp.status == 201

        # Scout has 1 unread with 1 mention
        resp = await client.get("/unreads/scout")
        data = await resp.json()
        assert data["chefhub-beta"]["unread"] == 1
        assert data["chefhub-beta"]["mentions"] == 1

        # Mark read via explicit endpoint
        await client.post("/channels/chefhub-beta/mark-read", json={
            "participant": "scout",
        })
        resp = await client.get("/unreads/scout")
        data = await resp.json()
        assert data["chefhub-beta"]["unread"] == 0

        # Search finds the decision message
        resp = await client.get("/search", params={"q": "ChefHubDB"})
        data = await resp.json()
        assert len(data) >= 1
        assert any("ChefHubDB" in r["content"] for r in data)

        # Search respects visibility — create private channel
        resp = await client.post("/channels", json={
            "name": "ops-internal",
            "type": "team",
            "created_by": "operator",
            "members": ["operator"],
        })
        assert resp.status == 201
        await client.post("/channels/ops-internal/messages", json={
            "author": "operator",
            "content": "Budget approval pending for ChefHubDB.",
        })

        # Scout cannot see ops-internal messages
        resp = await client.get("/search", params={
            "q": "budget", "participant": "scout",
        })
        data = await resp.json()
        assert len(data) == 0

        # Operator can see them
        resp = await client.get("/search", params={
            "q": "budget", "participant": "operator",
        })
        data = await resp.json()
        assert len(data) == 1

    async def test_multi_agent_conversation(self, aiohttp_client, comms_app):
        client = await aiohttp_client(comms_app)

        # Create channel
        await client.post("/channels", json={
            "name": "dev",
            "type": "team",
            "created_by": "operator",
            "members": ["scout", "pm", "operator"],
        })

        # Three-way conversation
        await client.post("/channels/dev/messages", json={
            "author": "operator", "content": "Sprint goal: deploy ChefHubDB API",
        })
        await client.post("/channels/dev/messages", json={
            "author": "scout", "content": "Schema migration ready. Need 2h for adapter.",
        })
        await client.post("/channels/dev/messages", json={
            "author": "pm", "content": "Priority: /recipes and /ingredients endpoints first.",
            "flags": {"decision": True},
        })

        # All messages visible
        resp = await client.get("/channels/dev/messages")
        data = await resp.json()
        assert len(data) == 3

        # Read with limit
        resp = await client.get("/channels/dev/messages", params={"limit": "2"})
        data = await resp.json()
        assert len(data) == 2
        assert data[0]["author"] == "scout"  # last 2 messages

    async def test_private_channel_visibility(self, aiohttp_client, comms_app):
        client = await aiohttp_client(comms_app)

        # Create private channel
        resp = await client.post("/channels", json={
            "name": "confidential",
            "type": "team",
            "created_by": "operator",
            "members": ["operator", "pm"],
            "visibility": "private",
        })
        assert resp.status == 201
        data = await resp.json()
        assert data["visibility"] == "private"

        # Scout cannot see it in channel list
        resp = await client.get("/channels", params={"member": "scout"})
        channels = await resp.json()
        names = [c["name"] for c in channels]
        assert "confidential" not in names

        # PM can see it
        resp = await client.get("/channels", params={"member": "pm"})
        channels = await resp.json()
        names = [c["name"] for c in channels]
        assert "confidential" in names

        # Scout cannot join
        resp = await client.post("/channels/confidential/join", json={
            "participant": "scout",
        })
        assert resp.status == 403

        # Scout cannot post
        resp = await client.post("/channels/confidential/messages", json={
            "author": "scout",
            "content": "Can I see this?",
        })
        assert resp.status == 403

        # PM can post
        resp = await client.post("/channels/confidential/messages", json={
            "author": "pm",
            "content": "Budget projections attached.",
        })
        assert resp.status == 201

        # Scout cannot search private channel content
        resp = await client.get("/search", params={
            "q": "budget projections", "participant": "scout",
        })
        data = await resp.json()
        assert len(data) == 0

        # PM can search it
        resp = await client.get("/search", params={
            "q": "budget projections", "participant": "pm",
        })
        data = await resp.json()
        assert len(data) == 1

    async def test_join_and_visibility(self, aiohttp_client, comms_app):
        client = await aiohttp_client(comms_app)

        # Create channel without scout
        await client.post("/channels", json={
            "name": "strategy",
            "type": "team",
            "created_by": "operator",
            "members": ["pm"],
        })

        # Post message
        await client.post("/channels/strategy/messages", json={
            "author": "pm", "content": "Pricing: Free, Starter 29, Pro 99",
        })

        # Scout can't search it
        resp = await client.get("/search", params={
            "q": "pricing", "participant": "scout",
        })
        data = await resp.json()
        assert len(data) == 0

        # Scout joins
        await client.post("/channels/strategy/join", json={
            "participant": "scout",
        })

        # Now scout can search
        resp = await client.get("/search", params={
            "q": "pricing", "participant": "scout",
        })
        data = await resp.json()
        assert len(data) == 1
