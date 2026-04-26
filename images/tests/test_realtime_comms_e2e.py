"""End-to-end integration tests for real-time agent communications.

Tests the full pipeline: WebSocket connection, interest registration,
message fan-out with classification (interest_match, ambient, direct),
task delivery via HTTP, knowledge push filtering, and summary sanitization.
"""

import asyncio

import pytest

from images.comms.server import create_app
from images.models.comms import ChannelType

pytestmark = pytest.mark.asyncio


# ---------------------------------------------------------------------------
# Fixture
# ---------------------------------------------------------------------------


@pytest.fixture
def e2e_app(tmp_path):
    """Full comms app with two channels and two agents."""
    agents_dir = tmp_path / "agents"
    for agent in ["agent-alice", "agent-bob"]:
        (agents_dir / agent / "state").mkdir(parents=True)

    # create_app already calls setup_websocket internally
    app = create_app(data_dir=tmp_path, agents_dir=agents_dir)

    store = app["store"]
    store.create_channel(
        name="team-platform",
        type=ChannelType.TEAM,
        created_by="operator",
        members=["agent-alice", "agent-bob"],
    )
    store.create_channel(
        name="_knowledge-updates",
        type=ChannelType.SYSTEM,
        created_by="_platform",
        visibility="platform-write",
        members=["agent-alice", "agent-bob"],
    )

    # These tests exercise the active fan-out path for non-mention messages.
    # The product default is mention-only, so opt Bob into active delivery here.
    app["sub_manager"].register_responsiveness("agent-bob", {"default": "active"})

    return app


# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------


class TestRealtimeCommsE2E:
    async def test_full_flow_message_push(self, aiohttp_client, e2e_app):
        """Bob registers keyword interests; Alice posts matching message; Bob receives interest_match."""
        client = await aiohttp_client(e2e_app)

        async with client.ws_connect("/ws?agent=agent-bob") as ws:
            ack = await ws.receive_json()
            assert ack["type"] == "ack"

            # Bob registers interests
            resp = await client.post(
                "/subscriptions/agent-bob/interests",
                json={
                    "task_id": "task-001",
                    "description": "Monitor payments and latency",
                    "keywords": ["payments", "latency"],
                },
            )
            assert resp.status == 200

            # Alice posts a matching message
            resp = await client.post(
                "/channels/team-platform/messages",
                json={
                    "author": "agent-alice",
                    "content": "The payments service is showing high latency",
                },
            )
            assert resp.status == 201

            # Bob receives the push
            event = await asyncio.wait_for(ws.receive_json(), timeout=2.0)
            assert event["type"] == "message"
            assert event["channel"] == "team-platform"
            assert event["match"] == "interest_match"
            assert len(event["matched_keywords"]) > 0

    async def test_full_flow_ambient_message(self, aiohttp_client, e2e_app):
        """Bob registers narrow interests; Alice posts unrelated message; Bob receives ambient."""
        client = await aiohttp_client(e2e_app)

        async with client.ws_connect("/ws?agent=agent-bob") as ws:
            ack = await ws.receive_json()
            assert ack["type"] == "ack"

            # Bob registers interests unrelated to the upcoming message
            resp = await client.post(
                "/subscriptions/agent-bob/interests",
                json={
                    "task_id": "task-002",
                    "description": "Auth monitoring",
                    "keywords": ["authentication", "oauth"],
                },
            )
            assert resp.status == 200

            # Alice posts about a different topic
            resp = await client.post(
                "/channels/team-platform/messages",
                json={
                    "author": "agent-alice",
                    "content": "The deploy process finished successfully",
                },
            )
            assert resp.status == 201

            event = await asyncio.wait_for(ws.receive_json(), timeout=2.0)
            assert event["type"] == "message"
            assert event["channel"] == "team-platform"
            assert event["match"] == "ambient"

    async def test_full_flow_direct_mention(self, aiohttp_client, e2e_app):
        """Alice @mentions Bob explicitly; Bob receives with match=direct."""
        client = await aiohttp_client(e2e_app)

        async with client.ws_connect("/ws?agent=agent-bob") as ws:
            ack = await ws.receive_json()
            assert ack["type"] == "ack"

            # Bob registers interests (needed so matcher is engaged)
            resp = await client.post(
                "/subscriptions/agent-bob/interests",
                json={
                    "task_id": "task-003",
                    "description": "Deploy logs",
                    "keywords": ["kubernetes", "cluster"],
                },
            )
            assert resp.status == 200

            # Alice directly mentions Bob
            resp = await client.post(
                "/channels/team-platform/messages",
                json={
                    "author": "agent-alice",
                    "content": "@agent-bob can you check the deploy logs?",
                },
            )
            assert resp.status == 201

            event = await asyncio.wait_for(ws.receive_json(), timeout=2.0)
            assert event["type"] == "message"
            assert event["match"] == "direct"

    async def test_full_flow_task_delivery(self, aiohttp_client, e2e_app):
        """POST /tasks/agent-bob delivers a task; Bob receives type=task event."""
        client = await aiohttp_client(e2e_app)

        async with client.ws_connect("/ws?agent=agent-bob") as ws:
            ack = await ws.receive_json()
            assert ack["type"] == "ack"

            # Deliver task via HTTP
            resp = await client.post(
                "/tasks/agent-bob",
                json={
                    "task_content": "Investigate memory spike in payments service",
                    "source": "operator",
                    "work_item_id": "WI-42",
                    "priority": "high",
                },
            )
            assert resp.status == 200
            body = await resp.json()
            assert body["delivered"] is True
            assert body["pushed"] is True

            # Bob receives the task push
            event = await asyncio.wait_for(ws.receive_json(), timeout=2.0)
            assert event["type"] == "task"
            assert "task" in event
            assert event["task"]["content"] == "Investigate memory spike in payments service"

    async def test_full_flow_knowledge_push(self, aiohttp_client, e2e_app):
        """Bob registers keyword interests; Alice posts to _knowledge-updates; Bob receives interest_match.

        Note: MessageFlags only supports structured boolean flags (decision, question, blocker,
        urgent). Custom metadata like 'kind'/'topic' is not stored in the Message model, so
        knowledge filtering via knowledge_filter.kinds/topics requires matching through the
        keywords path instead — the FTS5 match runs against the message content.
        """
        client = await aiohttp_client(e2e_app)

        async with client.ws_connect("/ws?agent=agent-bob") as ws:
            ack = await ws.receive_json()
            assert ack["type"] == "ack"

            # Bob registers interests with keywords that will match the knowledge update content
            resp = await client.post(
                "/subscriptions/agent-bob/interests",
                json={
                    "task_id": "task-004",
                    "description": "Track payment incidents",
                    "keywords": ["outage", "payments"],
                    "knowledge_filter": {"kinds": ["incident"]},
                },
            )
            assert resp.status == 200

            # Alice posts a knowledge update to the platform-write channel
            resp = await client.post(
                "/channels/_knowledge-updates/messages",
                json={
                    "author": "agent-alice",
                    "content": "Payments outage root cause identified: DB connection pool exhausted",
                },
                headers={"X-Agency-Platform": "true"},
            )
            assert resp.status == 201

            # Bob receives the push because keywords match the content
            event = await asyncio.wait_for(ws.receive_json(), timeout=2.0)
            assert event["type"] == "message"
            assert event["channel"] == "_knowledge-updates"
            assert event["match"] == "interest_match"
            assert len(event["matched_keywords"]) > 0

    async def test_summary_sanitized(self, aiohttp_client, e2e_app):
        """Alice sends message with control chars; Bob's summary has them stripped."""
        client = await aiohttp_client(e2e_app)

        async with client.ws_connect("/ws?agent=agent-bob") as ws:
            ack = await ws.receive_json()
            assert ack["type"] == "ack"

            # Register interests so Bob gets the push (avoids ambient silencing)
            await client.post(
                "/subscriptions/agent-bob/interests",
                json={
                    "task_id": "task-005",
                    "description": "General monitoring",
                    "keywords": ["alert", "service"],
                },
            )

            raw_content = "alert\x00service\x01is \x1bdown\x7f now"
            resp = await client.post(
                "/channels/team-platform/messages",
                json={
                    "author": "agent-alice",
                    "content": raw_content,
                },
            )
            assert resp.status == 201

            event = await asyncio.wait_for(ws.receive_json(), timeout=2.0)
            summary = event["message"]["summary"]

            # Control characters must be stripped
            import re
            assert not re.search(r"[\x00-\x1f\x7f-\x9f]", summary), (
                f"Summary contains control chars: {summary!r}"
            )
            # Meaningful content preserved
            assert "service" in summary
            assert "down" in summary
