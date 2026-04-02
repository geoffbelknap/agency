"""Tests for the org-structural contribution review gate.

Covers:
- General knowledge (kind="finding") accepted directly
- Org-structural knowledge (kind="team") held for review
- Pending contributions can be listed
- Approved contribution committed to graph
- Rejected contribution discarded
- /pending and /review HTTP endpoints
"""

import pytest

from images.knowledge.server import create_app
from images.knowledge.store import KnowledgeStore, ORG_STRUCTURAL_KINDS
from .conftest import PlatformClient


# ---------------------------------------------------------------------------
# Fixtures
# ---------------------------------------------------------------------------

@pytest.fixture
def store(tmp_path):
    return KnowledgeStore(tmp_path)


@pytest.fixture
def client(tmp_path, aiohttp_client):
    app = create_app(data_dir=tmp_path)
    return aiohttp_client(app)


async def _platform_client(aiohttp_client, app):
    """Helper: create an aiohttp test client wrapped with platform headers."""
    raw = await aiohttp_client(app)
    return PlatformClient(raw)


# ---------------------------------------------------------------------------
# Store unit tests (no HTTP layer)
# ---------------------------------------------------------------------------

class TestIsOrgStructural:
    def test_team_is_org_structural(self, store):
        assert store.is_org_structural("team") is True

    def test_department_is_org_structural(self, store):
        assert store.is_org_structural("department") is True

    def test_escalation_path_is_org_structural(self, store):
        assert store.is_org_structural("escalation-path") is True

    def test_leadership_is_org_structural(self, store):
        assert store.is_org_structural("leadership") is True

    def test_finding_is_not_org_structural(self, store):
        assert store.is_org_structural("finding") is False

    def test_fact_is_not_org_structural(self, store):
        assert store.is_org_structural("fact") is False

    def test_decision_is_not_org_structural(self, store):
        assert store.is_org_structural("decision") is False

    def test_lesson_is_not_org_structural(self, store):
        assert store.is_org_structural("lesson") is False

    def test_kind_check_is_case_insensitive(self, store):
        assert store.is_org_structural("TEAM") is True
        assert store.is_org_structural("Department") is True

    def test_org_structural_kinds_constant_has_expected_members(self):
        assert "team" in ORG_STRUCTURAL_KINDS
        assert "department" in ORG_STRUCTURAL_KINDS
        assert "escalation-path" in ORG_STRUCTURAL_KINDS
        assert "leadership" in ORG_STRUCTURAL_KINDS


class TestSubmitPending:
    def test_submit_pending_returns_id(self, store):
        pending_id = store.submit_pending(label="Alpha Team", kind="team")
        assert isinstance(pending_id, str)
        assert len(pending_id) > 0

    def test_submit_pending_does_not_add_to_main_graph(self, store):
        store.submit_pending(label="Alpha Team", kind="team")
        nodes = store.find_nodes_by_kind("team")
        assert len(nodes) == 0

    def test_submitted_node_appears_in_list_pending(self, store):
        store.submit_pending(label="Alpha Team", kind="team", summary="Our main team")
        pending = store.list_pending()
        assert len(pending) == 1
        assert pending[0]["label"] == "Alpha Team"
        assert pending[0]["kind"] == "team"
        assert pending[0]["summary"] == "Our main team"


class TestListPending:
    def test_empty_when_no_pending(self, store):
        assert store.list_pending() == []

    def test_returns_all_submitted(self, store):
        store.submit_pending(label="Team A", kind="team")
        store.submit_pending(label="Dept B", kind="department")
        pending = store.list_pending()
        assert len(pending) == 2
        labels = {p["label"] for p in pending}
        assert "Team A" in labels
        assert "Dept B" in labels

    def test_ordered_by_submitted_at(self, store):
        store.submit_pending(label="First", kind="team")
        store.submit_pending(label="Second", kind="department")
        pending = store.list_pending()
        assert pending[0]["label"] == "First"
        assert pending[1]["label"] == "Second"


class TestReviewPending:
    def test_approve_commits_node_to_graph(self, store):
        pending_id = store.submit_pending(label="Alpha Team", kind="team", summary="Main team")
        result = store.review_pending(pending_id, "approve")
        assert result is True
        # Node should now be in the main graph
        nodes = store.find_nodes_by_kind("team")
        assert len(nodes) == 1
        assert nodes[0]["label"] == "Alpha Team"

    def test_approve_removes_from_pending(self, store):
        pending_id = store.submit_pending(label="Alpha Team", kind="team")
        store.review_pending(pending_id, "approve")
        assert store.list_pending() == []

    def test_reject_removes_from_pending(self, store):
        pending_id = store.submit_pending(label="Fake Leadership", kind="leadership")
        result = store.review_pending(pending_id, "reject")
        assert result is True
        assert store.list_pending() == []

    def test_reject_does_not_commit_to_graph(self, store):
        pending_id = store.submit_pending(label="Fake Leadership", kind="leadership")
        store.review_pending(pending_id, "reject")
        nodes = store.find_nodes_by_kind("leadership")
        assert len(nodes) == 0

    def test_returns_false_for_unknown_id(self, store):
        result = store.review_pending("nonexistent", "approve")
        assert result is False

    def test_approve_preserves_properties(self, store):
        pending_id = store.submit_pending(
            label="Engineering",
            kind="department",
            summary="Engineering dept",
            properties={"size": 42},
        )
        store.review_pending(pending_id, "approve")
        nodes = store.find_nodes_by_kind("department")
        assert len(nodes) == 1
        import json
        props = json.loads(nodes[0]["properties"])
        assert props.get("size") == 42


class TestGeneralKnowledgeNotGated:
    """General knowledge kinds (finding, fact, decision) bypass the gate."""

    def test_general_kind_not_held_in_pending(self, store):
        store.add_node(label="SQL injection found", kind="finding", summary="XSS vuln")
        assert store.list_pending() == []

    def test_general_kind_goes_directly_to_graph(self, store):
        store.add_node(label="SQL injection found", kind="finding")
        nodes = store.find_nodes_by_kind("finding")
        assert len(nodes) == 1


# ---------------------------------------------------------------------------
# HTTP endpoint tests
# ---------------------------------------------------------------------------

class TestPendingEndpoint:
    @pytest.mark.asyncio
    async def test_get_pending_empty(self, client):
        c = PlatformClient(await client)
        resp = await c.get("/pending")
        assert resp.status == 200
        data = await resp.json()
        assert data == {"items": []}

    @pytest.mark.asyncio
    async def test_get_pending_returns_submitted_items(self, tmp_path, aiohttp_client):
        store = KnowledgeStore(tmp_path)
        store.submit_pending(label="Alpha Team", kind="team", summary="Our team")
        app = create_app(data_dir=tmp_path)
        c = await _platform_client(aiohttp_client, app)
        resp = await c.get("/pending")
        assert resp.status == 200
        data = await resp.json()
        assert len(data["items"]) == 1
        assert data["items"][0]["label"] == "Alpha Team"

    @pytest.mark.asyncio
    async def test_ingest_org_structural_goes_to_pending(self, client):
        c = PlatformClient(await client)
        resp = await c.post("/ingest/nodes", json={
            "nodes": [{"label": "Beta Team", "kind": "team", "summary": "Beta"}]
        })
        assert resp.status == 200
        data = await resp.json()
        assert data["ingested"] == 0
        assert data["pending_review"] == 1

        # Confirm it's in /pending
        resp2 = await c.get("/pending")
        data2 = await resp2.json()
        assert len(data2["items"]) == 1
        assert data2["items"][0]["label"] == "Beta Team"

    @pytest.mark.asyncio
    async def test_ingest_general_kind_not_held(self, client):
        c = PlatformClient(await client)
        resp = await c.post("/ingest/nodes", json={
            "nodes": [{"label": "XSS vuln", "kind": "finding", "summary": "Found XSS"}]
        })
        assert resp.status == 200
        data = await resp.json()
        assert data["ingested"] == 1
        assert data["pending_review"] == 0

        # Confirm pending is empty
        resp2 = await c.get("/pending")
        data2 = await resp2.json()
        assert data2["items"] == []

    @pytest.mark.asyncio
    async def test_ingest_mixed_nodes(self, client):
        c = PlatformClient(await client)
        resp = await c.post("/ingest/nodes", json={
            "nodes": [
                {"label": "Alpha Team", "kind": "team"},
                {"label": "SQL vuln", "kind": "finding"},
                {"label": "Engineering", "kind": "department"},
            ]
        })
        assert resp.status == 200
        data = await resp.json()
        assert data["ingested"] == 1
        assert data["pending_review"] == 2


class TestReviewEndpoint:
    @pytest.mark.asyncio
    async def test_approve_commits_to_graph(self, tmp_path, aiohttp_client):
        store = KnowledgeStore(tmp_path)
        pending_id = store.submit_pending(label="Alpha Team", kind="team")
        app = create_app(data_dir=tmp_path)
        c = await _platform_client(aiohttp_client, app)

        resp = await c.post(f"/review/{pending_id}", json={"action": "approve"})
        assert resp.status == 200
        data = await resp.json()
        assert data["pending_id"] == pending_id
        assert data["action"] == "approve"

        # Pending should now be empty
        resp2 = await c.get("/pending")
        data2 = await resp2.json()
        assert data2["items"] == []

    @pytest.mark.asyncio
    async def test_reject_discards_contribution(self, tmp_path, aiohttp_client):
        store = KnowledgeStore(tmp_path)
        pending_id = store.submit_pending(label="Fake Dept", kind="department")
        app = create_app(data_dir=tmp_path)
        c = await _platform_client(aiohttp_client, app)

        resp = await c.post(f"/review/{pending_id}", json={"action": "reject"})
        assert resp.status == 200
        data = await resp.json()
        assert data["action"] == "reject"

        # Pending should now be empty
        resp2 = await c.get("/pending")
        data2 = await resp2.json()
        assert data2["items"] == []

    @pytest.mark.asyncio
    async def test_review_unknown_id_returns_404(self, client):
        c = PlatformClient(await client)
        resp = await c.post("/review/nonexistent123", json={"action": "approve"})
        assert resp.status == 404

    @pytest.mark.asyncio
    async def test_invalid_action_returns_400(self, tmp_path, aiohttp_client):
        store = KnowledgeStore(tmp_path)
        pending_id = store.submit_pending(label="Team X", kind="team")
        app = create_app(data_dir=tmp_path)
        c = await _platform_client(aiohttp_client, app)

        resp = await c.post(f"/review/{pending_id}", json={"action": "merge"})
        assert resp.status == 400
        data = await resp.json()
        assert "error" in data
