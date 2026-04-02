"""Tests for subscription and task delivery HTTP endpoints.

Covers:
    POST   /subscriptions/{agent_name}/interests  - Register task interests
    DELETE /subscriptions/{agent_name}/interests  - Clear task interests
    POST   /tasks/{agent_name}                    - Deliver task (file + WS push)
"""

import asyncio
import json

import pytest

from images.comms.server import create_app

pytestmark = pytest.mark.asyncio


# ---------------------------------------------------------------------------
# Fixtures
# ---------------------------------------------------------------------------


@pytest.fixture
def base_app(tmp_path):
    """App with no pre-existing agent state (for subscription tests)."""
    return create_app(data_dir=tmp_path / "data", agents_dir=tmp_path / "agents")


@pytest.fixture
def delivery_app(tmp_path):
    """App with a pre-configured agent state dir and session context."""
    agents_dir = tmp_path / "agents"
    state_dir = agents_dir / "scout" / "state"
    state_dir.mkdir(parents=True)
    context_file = state_dir / "session-context.json"
    context_file.write_text(json.dumps({
        "session_id": "test-session",
    }))
    return create_app(data_dir=tmp_path / "data", agents_dir=agents_dir)


# ---------------------------------------------------------------------------
# POST /subscriptions/{agent_name}/interests
# ---------------------------------------------------------------------------


class TestRegisterInterests:
    async def test_register_returns_registered_true(self, aiohttp_client, base_app):
        client = await aiohttp_client(base_app)
        resp = await client.post("/subscriptions/scout/interests", json={
            "task_id": "task-20260314-abc123",
            "description": "Monitor security alerts",
            "keywords": ["alert", "breach", "anomaly"],
            "knowledge_filter": {},
        })
        assert resp.status == 200
        data = await resp.json()
        assert data["registered"] is True

    async def test_register_minimal_body(self, aiohttp_client, base_app):
        """task_id is the only required field; others have defaults."""
        client = await aiohttp_client(base_app)
        resp = await client.post("/subscriptions/pm/interests", json={
            "task_id": "task-20260314-xyz",
        })
        assert resp.status == 200
        data = await resp.json()
        assert data["registered"] is True

    async def test_register_invalid_json(self, aiohttp_client, base_app):
        client = await aiohttp_client(base_app)
        resp = await client.post(
            "/subscriptions/scout/interests",
            data=b"not json",
            headers={"Content-Type": "application/json"},
        )
        assert resp.status == 400

    async def test_register_too_many_keywords_returns_400(self, aiohttp_client, base_app):
        """InterestDeclaration rejects more than 20 keywords."""
        client = await aiohttp_client(base_app)
        many_keywords = [f"keyword{i:02d}" for i in range(21)]  # 21 keywords
        resp = await client.post("/subscriptions/scout/interests", json={
            "task_id": "task-20260314-abc123",
            "keywords": many_keywords,
        })
        assert resp.status == 400
        data = await resp.json()
        assert "error" in data

    async def test_register_persists_to_subscription_manager(self, aiohttp_client, base_app):
        """Registered interests are retrievable via the SubscriptionManager."""
        client = await aiohttp_client(base_app)
        await client.post("/subscriptions/scout/interests", json={
            "task_id": "task-20260314-persist",
            "keywords": ["security", "alert"],
        })
        sub_manager = base_app["sub_manager"]
        declaration = sub_manager.get("scout")
        assert declaration is not None
        assert declaration.task_id == "task-20260314-persist"

    async def test_register_overwrites_previous_declaration(self, aiohttp_client, base_app):
        """Second registration replaces the first for the same agent."""
        client = await aiohttp_client(base_app)
        await client.post("/subscriptions/scout/interests", json={
            "task_id": "task-first",
            "keywords": ["alpha"],
        })
        await client.post("/subscriptions/scout/interests", json={
            "task_id": "task-second",
            "keywords": ["beta"],
        })
        sub_manager = base_app["sub_manager"]
        declaration = sub_manager.get("scout")
        assert declaration.task_id == "task-second"


# ---------------------------------------------------------------------------
# DELETE /subscriptions/{agent_name}/interests
# ---------------------------------------------------------------------------


class TestClearInterests:
    async def test_clear_returns_cleared_true(self, aiohttp_client, base_app):
        client = await aiohttp_client(base_app)
        resp = await client.delete("/subscriptions/scout/interests")
        assert resp.status == 200
        data = await resp.json()
        assert data["cleared"] is True

    async def test_clear_removes_existing_interests(self, aiohttp_client, base_app):
        """After clear, get() returns None for the agent."""
        client = await aiohttp_client(base_app)
        # Register first
        await client.post("/subscriptions/scout/interests", json={
            "task_id": "task-20260314-abc",
            "keywords": ["alert"],
        })
        assert base_app["sub_manager"].get("scout") is not None

        # Then clear
        resp = await client.delete("/subscriptions/scout/interests")
        assert resp.status == 200
        assert base_app["sub_manager"].get("scout") is None

    async def test_clear_nonexistent_agent_is_noop(self, aiohttp_client, base_app):
        """Clearing an agent with no registered interests succeeds silently."""
        client = await aiohttp_client(base_app)
        resp = await client.delete("/subscriptions/nonexistent/interests")
        assert resp.status == 200
        data = await resp.json()
        assert data["cleared"] is True


# ---------------------------------------------------------------------------
# POST /tasks/{agent_name}
# ---------------------------------------------------------------------------


class TestDeliverTaskV2:
    async def test_deliver_returns_delivered_true_and_task_id(self, aiohttp_client, delivery_app):
        client = await aiohttp_client(delivery_app)
        resp = await client.post("/tasks/scout", json={
            "task_content": "Investigate anomaly A-42",
            "source": "connector:siem",
            "work_item_id": "wi-20260314-001",
            "priority": "high",
        })
        assert resp.status == 200
        data = await resp.json()
        assert data["delivered"] is True
        assert "task_id" in data
        assert data["task_id"].startswith("task-")

    async def test_deliver_writes_context_file(self, aiohttp_client, delivery_app):
        """Task must be written to session-context.json for durability."""
        client = await aiohttp_client(delivery_app)
        resp = await client.post("/tasks/scout", json={
            "task_content": "Review PR #101",
        })
        assert resp.status == 200

        agents_dir = delivery_app["agents_dir"]
        context_file = agents_dir / "scout" / "state" / "session-context.json"
        context = json.loads(context_file.read_text())
        assert "current_task" in context
        assert context["current_task"]["content"] == "Review PR #101"
        assert context["current_task"]["type"] == "task"
        assert "stopping_conditions" in context["current_task"]

    async def test_deliver_unknown_agent_returns_404(self, aiohttp_client, delivery_app):
        client = await aiohttp_client(delivery_app)
        resp = await client.post("/tasks/nonexistent-agent", json={
            "task_content": "Some task",
        })
        assert resp.status == 404

    async def test_deliver_missing_task_content_returns_400(self, aiohttp_client, delivery_app):
        client = await aiohttp_client(delivery_app)
        resp = await client.post("/tasks/scout", json={
            "source": "connector:test",
        })
        assert resp.status == 400

    async def test_deliver_invalid_json_returns_400(self, aiohttp_client, delivery_app):
        client = await aiohttp_client(delivery_app)
        resp = await client.post(
            "/tasks/scout",
            data=b"not json",
            headers={"Content-Type": "application/json"},
        )
        assert resp.status == 400

    async def test_deliver_with_ws_connected_pushes_event(self, aiohttp_client, delivery_app):
        """When agent has an open WebSocket, task delivery pushes a task event."""
        client = await aiohttp_client(delivery_app)

        async with client.ws_connect("/ws?agent=scout") as ws:
            # Consume the ack
            ack = await ws.receive_json()
            assert ack["type"] == "ack"

            # Deliver a task via HTTP
            resp = await client.post("/tasks/scout", json={
                "task_content": "Triage incident INC-007",
                "source": "connector:pagerduty",
                "priority": "urgent",
            })
            assert resp.status == 200
            data = await resp.json()
            assert data["pushed"] is True

            # Agent should receive the task event on WS
            pushed = await ws.receive_json()
            assert pushed["type"] == "task"
            assert pushed["v"] == 1
            assert pushed["task"]["content"] == "Triage incident INC-007"
            assert pushed["task"]["type"] == "task"
            assert "task_id" in pushed["task"]

    async def test_deliver_without_ws_pushed_false(self, aiohttp_client, delivery_app):
        """When agent has no WebSocket connection, pushed=False and file is still written."""
        client = await aiohttp_client(delivery_app)

        resp = await client.post("/tasks/scout", json={
            "task_content": "Background sweep",
            "source": "connector:scheduler",
        })
        assert resp.status == 200
        data = await resp.json()
        assert data["delivered"] is True
        assert data["pushed"] is False

        # Context file must still be written
        agents_dir = delivery_app["agents_dir"]
        context_file = agents_dir / "scout" / "state" / "session-context.json"
        context = json.loads(context_file.read_text())
        assert context["current_task"]["content"] == "Background sweep"

    async def test_deliver_creates_context_file_when_missing(self, aiohttp_client, tmp_path):
        """If session-context.json doesn't exist yet, it is created."""
        agents_dir = tmp_path / "agents"
        state_dir = agents_dir / "newagent" / "state"
        state_dir.mkdir(parents=True)
        # No context file created — endpoint should create it
        app = create_app(data_dir=tmp_path / "data", agents_dir=agents_dir)
        client = await aiohttp_client(app)

        resp = await client.post("/tasks/newagent", json={
            "task_content": "First ever task",
        })
        assert resp.status == 200

        context_file = state_dir / "session-context.json"
        assert context_file.exists()
        context = json.loads(context_file.read_text())
        assert context["current_task"]["content"] == "First ever task"

    async def test_deliver_preserves_existing_context_fields(self, aiohttp_client, delivery_app):
        """Existing context fields (session_id, mode) must survive task write."""
        client = await aiohttp_client(delivery_app)
        resp = await client.post("/tasks/scout", json={
            "task_content": "Preserve context test",
        })
        assert resp.status == 200

        agents_dir = delivery_app["agents_dir"]
        context_file = agents_dir / "scout" / "state" / "session-context.json"
        context = json.loads(context_file.read_text())
        assert context["session_id"] == "test-session"
        assert context["current_task"]["content"] == "Preserve context test"
