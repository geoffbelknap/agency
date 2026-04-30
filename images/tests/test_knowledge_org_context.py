"""Tests for the /org-context endpoint and store.get_org_context method.

Covers:
- Returns team context for a known agent
- Returns escalation path (team lead -> dept lead -> operator)
- Returns peer teams in the same department
- Empty graph returns empty context (not an error)
- Missing agent query param returns 400
"""

import pytest

from services.knowledge.server import create_app
from services.knowledge.store import KnowledgeStore
from .conftest import PlatformClient


# ---------------------------------------------------------------------------
# Fixtures
# ---------------------------------------------------------------------------

@pytest.fixture
def store(tmp_path):
    return KnowledgeStore(tmp_path)


@pytest.fixture
def populated_store(tmp_path):
    """Graph with a full org structure:

        scout (agent)
          -[member_of]-> alpha-team (team)
          alpha-team -[led_by]-> alice (person)
          alpha-team -[part_of]-> engineering (department)
          engineering -[led_by]-> bob (person)

        beta-team (team) also -[part_of]-> engineering
        operator (operator) node exists

        scout also has a decision node linked to alpha-team
    """
    s = KnowledgeStore(tmp_path)

    scout_id = s.add_node(label="scout", kind="agent", summary="Integration agent")
    alice_id = s.add_node(label="alice", kind="person", summary="Team lead")
    bob_id = s.add_node(label="bob", kind="person", summary="Dept lead")
    alpha_id = s.add_node(label="alpha-team", kind="team", summary="Alpha team")
    beta_id = s.add_node(label="beta-team", kind="team", summary="Beta team")
    eng_id = s.add_node(label="engineering", kind="department", summary="Eng dept")
    op_id = s.add_node(label="operator", kind="operator", summary="Platform operator")
    decision_id = s.add_node(label="tech-debt-policy", kind="decision", summary="No new legacy code")

    s.add_edge(scout_id, alpha_id, relation="member_of")
    s.add_edge(alpha_id, alice_id, relation="led_by")
    s.add_edge(alpha_id, eng_id, relation="part_of")
    s.add_edge(beta_id, eng_id, relation="part_of")
    s.add_edge(eng_id, bob_id, relation="led_by")
    # Link a decision to the team
    s.add_edge(alpha_id, decision_id, relation="decided")

    return s


@pytest.fixture
def client(tmp_path, aiohttp_client):
    app = create_app(data_dir=tmp_path)
    return aiohttp_client(app)


@pytest.fixture
def populated_client(populated_store, aiohttp_client):
    """aiohttp test client backed by the populated store."""
    # Use the same tmp_path as populated_store so create_app reads the same DB
    app = create_app(data_dir=populated_store.data_dir)
    return aiohttp_client(app)


# ---------------------------------------------------------------------------
# Store unit tests (no HTTP layer)
# ---------------------------------------------------------------------------

class TestGetOrgContextStore:
    def test_returns_empty_for_unknown_agent(self, store):
        result = store.get_org_context("nobody")
        assert result["team"] == {}
        assert result["department"] == {}
        assert result["escalation_path"] == []
        assert result["peer_teams"] == []
        assert result["org_history"] == []

    def test_returns_empty_for_empty_graph(self, store):
        result = store.get_org_context("scout")
        assert result["team"] == {}

    def test_returns_team_info(self, populated_store):
        result = populated_store.get_org_context("scout")
        assert result["team"]["label"] == "alpha-team"

    def test_team_members_included(self, populated_store):
        result = populated_store.get_org_context("scout")
        member_labels = [m["label"] for m in result["team"]["members"]]
        assert "scout" in member_labels

    def test_team_lead_included(self, populated_store):
        result = populated_store.get_org_context("scout")
        assert result["team"]["lead"] is not None
        assert result["team"]["lead"]["label"] == "alice"

    def test_department_included(self, populated_store):
        result = populated_store.get_org_context("scout")
        assert result["department"]["label"] == "engineering"

    def test_department_lead_included(self, populated_store):
        result = populated_store.get_org_context("scout")
        assert result["department"]["lead"] is not None
        assert result["department"]["lead"]["label"] == "bob"

    def test_escalation_path_order(self, populated_store):
        result = populated_store.get_org_context("scout")
        roles = [e["role"] for e in result["escalation_path"]]
        # team_lead comes before dept_lead; operator comes last
        assert roles.index("team_lead") < roles.index("dept_lead")
        assert roles[-1] == "operator"

    def test_escalation_path_labels(self, populated_store):
        result = populated_store.get_org_context("scout")
        by_role = {e["role"]: e["label"] for e in result["escalation_path"]}
        assert by_role["team_lead"] == "alice"
        assert by_role["dept_lead"] == "bob"
        assert by_role["operator"] == "operator"

    def test_peer_teams_excludes_own_team(self, populated_store):
        result = populated_store.get_org_context("scout")
        peer_labels = [p["label"] for p in result["peer_teams"]]
        assert "beta-team" in peer_labels
        assert "alpha-team" not in peer_labels

    def test_org_history_includes_linked_decisions(self, populated_store):
        result = populated_store.get_org_context("scout")
        history_labels = [h["label"] for h in result["org_history"]]
        assert "tech-debt-policy" in history_labels

    def test_agent_kind_system_also_works(self, tmp_path):
        """After ontology migration, agent nodes may have kind='system'."""
        s = KnowledgeStore(tmp_path)
        agent_id = s.add_node(label="sage", kind="system", summary="")
        team_id = s.add_node(label="ops-team", kind="team", summary="")
        s.add_edge(agent_id, team_id, relation="member_of")
        result = s.get_org_context("sage")
        assert result["team"]["label"] == "ops-team"

    def test_agent_without_team_returns_empty(self, tmp_path):
        s = KnowledgeStore(tmp_path)
        s.add_node(label="loner", kind="agent", summary="")
        result = s.get_org_context("loner")
        assert result["team"] == {}

    def test_team_without_department_returns_partial(self, tmp_path):
        s = KnowledgeStore(tmp_path)
        agent_id = s.add_node(label="roamer", kind="agent", summary="")
        team_id = s.add_node(label="solo-team", kind="team", summary="")
        s.add_edge(agent_id, team_id, relation="member_of")
        result = s.get_org_context("roamer")
        assert result["team"]["label"] == "solo-team"
        assert result["department"] == {}
        assert result["escalation_path"] == []
        assert result["peer_teams"] == []


# ---------------------------------------------------------------------------
# HTTP endpoint tests
# ---------------------------------------------------------------------------

class TestOrgContextEndpoint:
    @pytest.mark.asyncio
    async def test_missing_agent_param_returns_400(self, client):
        c = PlatformClient(await client)
        resp = await c.get("/org-context")
        assert resp.status == 400
        data = await resp.json()
        assert "error" in data

    @pytest.mark.asyncio
    async def test_unknown_agent_returns_empty_context(self, client):
        """Platform callers bypass agent-identity check; unknown agent returns empty context."""
        c = PlatformClient(await client)
        resp = await c.get("/org-context", params={"agent": "nobody"})
        assert resp.status == 200
        data = await resp.json()
        assert data["team"] == {}
        assert data["department"] == {}

    @pytest.mark.asyncio
    async def test_returns_team_for_known_agent(self, populated_client):
        c = PlatformClient(await populated_client)
        resp = await c.get("/org-context", params={"agent": "scout"})
        assert resp.status == 200
        data = await resp.json()
        assert data["team"]["label"] == "alpha-team"

    @pytest.mark.asyncio
    async def test_returns_escalation_path(self, populated_client):
        c = PlatformClient(await populated_client)
        resp = await c.get("/org-context", params={"agent": "scout"})
        assert resp.status == 200
        data = await resp.json()
        roles = [e["role"] for e in data["escalation_path"]]
        assert "team_lead" in roles
        assert "dept_lead" in roles
        assert "operator" in roles

    @pytest.mark.asyncio
    async def test_returns_peer_teams(self, populated_client):
        c = PlatformClient(await populated_client)
        resp = await c.get("/org-context", params={"agent": "scout"})
        assert resp.status == 200
        data = await resp.json()
        peer_labels = [p["label"] for p in data["peer_teams"]]
        assert "beta-team" in peer_labels

    @pytest.mark.asyncio
    async def test_response_has_required_keys(self, populated_client):
        c = PlatformClient(await populated_client)
        resp = await c.get("/org-context", params={"agent": "scout"})
        assert resp.status == 200
        data = await resp.json()
        for key in ("team", "department", "escalation_path", "peer_teams", "org_history"):
            assert key in data, f"missing key: {key}"
