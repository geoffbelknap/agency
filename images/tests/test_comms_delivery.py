"""Tests for comms task delivery endpoint."""

import json
import os
import sys

import pytest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from images.comms.server import create_app

pytestmark = pytest.mark.asyncio


@pytest.fixture
def delivery_app(tmp_path):
    agents_dir = tmp_path / "agents"
    agents_dir.mkdir()
    # Set up a test agent with state dir and session context
    state_dir = agents_dir / "test-agent" / "state"
    state_dir.mkdir(parents=True)
    context_file = state_dir / "session-context.json"
    context_file.write_text(json.dumps({
        "session_id": "test-session",
    }))
    return create_app(data_dir=tmp_path / "comms", agents_dir=agents_dir)


class TestTaskDelivery:
    async def test_deliver_task_success(self, aiohttp_client, delivery_app):
        client = await aiohttp_client(delivery_app)
        resp = await client.post("/tasks/deliver", json={
            "agent_name": "test-agent",
            "task_content": "Triage alert A123",
            "work_item_id": "wi-20260310-abc12345",
            "priority": "high",
            "source": "connector:splunk-soc",
            "metadata": {
                "event_id": "evt-abc12345",
                "connector_name": "slack-events",
                "source_payload": {"event": {"channel": "C123", "ts": "1712860000.1234"}},
                "bridge": {
                    "platform": "slack",
                    "workspace_id": "T123",
                    "user_id": "U123",
                    "channel_id": "C123",
                    "message_ts": "1712860000.1234",
                    "thread_ts": "1712860000.1234",
                    "root_ts": "1712860000.1234",
                    "conversation_key": "slack:C123:1712860000.1234",
                    "conversation_kind": "thread",
                },
                "principal": {
                    "platform": "slack",
                    "workspace_id": "T123",
                    "user_id": "U123",
                    "channel_id": "C123",
                    "conversation_key": "slack:C123:1712860000.1234",
                    "is_dm": False,
                },
            },
        })
        assert resp.status == 200
        data = await resp.json()
        assert data["delivered"] is True
        assert "task_id" in data

        # Verify task was written to context file
        agents_dir = delivery_app["agents_dir"]
        context_file = agents_dir / "test-agent" / "state" / "session-context.json"
        updated = json.loads(context_file.read_text())
        assert "current_task" in updated
        assert updated["current_task"]["content"] == "Triage alert A123"
        assert updated["current_task"]["work_item_id"] == "wi-20260310-abc12345"
        assert updated["current_task"]["priority"] == "high"
        assert updated["current_task"]["event_id"] == "evt-abc12345"
        assert updated["current_task"]["metadata"]["connector_name"] == "slack-events"
        assert updated["current_task"]["metadata"]["bridge"]["conversation_key"] == "slack:C123:1712860000.1234"
        assert updated["current_task"]["metadata"]["principal"]["user_id"] == "U123"

    async def test_deliver_task_unknown_agent(self, aiohttp_client, delivery_app):
        client = await aiohttp_client(delivery_app)
        resp = await client.post("/tasks/deliver", json={
            "agent_name": "nonexistent",
            "task_content": "test",
            "work_item_id": "wi-123",
            "priority": "normal",
            "source": "connector:test",
        })
        assert resp.status == 404

    async def test_deliver_task_missing_fields(self, aiohttp_client, delivery_app):
        client = await aiohttp_client(delivery_app)
        resp = await client.post("/tasks/deliver", json={
            "agent_name": "test-agent",
        })
        assert resp.status == 400

    async def test_deliver_task_invalid_json(self, aiohttp_client, delivery_app):
        client = await aiohttp_client(delivery_app)
        resp = await client.post(
            "/tasks/deliver",
            data=b"not json",
            headers={"Content-Type": "application/json"},
        )
        assert resp.status == 400
