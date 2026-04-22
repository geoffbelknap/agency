import json
import hashlib
import threading
from http.server import HTTPServer, BaseHTTPRequestHandler
from pathlib import Path
from unittest.mock import MagicMock
from types import SimpleNamespace


class MockEnforcerHandler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == "/constraints":
            constraints = {"budget": {"max_daily_usd": 5.0}}
            # Canonical JSON: sorted keys, compact separators (matches Go json.Marshal)
            data = json.dumps(constraints, sort_keys=True, separators=(",", ":")).encode()
            h = hashlib.sha256(data).hexdigest()
            self._respond(200, {
                "version": 2,
                "hash": h,
                "severity": "MEDIUM",
                "constraints": constraints,
                "sealed_at": "2026-03-21T14:30:00Z",
            })

    def do_POST(self):
        if self.path == "/constraints/ack":
            length = int(self.headers.get("Content-Length", 0))
            body = json.loads(self.rfile.read(length))
            self.server.last_ack = body
            self._respond(200, {"status": "ok"})

    def _respond(self, code, body):
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(json.dumps(body).encode())

    def log_message(self, *args):
        pass


def test_reload_constraints():
    # Start mock enforcer on dynamic port
    server = HTTPServer(("127.0.0.1", 0), MockEnforcerHandler)
    port = server.server_address[1]
    server.last_ack = None
    t = threading.Thread(target=server.serve_forever, daemon=True)
    t.start()

    try:
        from body import Body
        body = Body.__new__(Body)
        body.enforcer_url = f"http://127.0.0.1:{port}"
        body.config_dir = Path(".")
        body.is_meeseeks = False
        body._active_mission = None
        body._task_features = {}
        body._config_overrides = {}
        body._skills_manager = MagicMock()
        body._skills_manager.get_system_prompt_section.return_value = ""
        body.assemble_system_prompt = MagicMock(return_value="stub prompt")
        # Body uses _mcp_policy as the internal attribute name
        body._mcp_policy = None
        body._log = MagicMock()

        body.reload_constraints(version=2, severity="MEDIUM")

        assert server.last_ack is not None
        assert server.last_ack["version"] == 2
        assert len(server.last_ack["hash"]) == 64  # SHA-256 hex
    finally:
        server.shutdown()


def test_on_config_change_refreshes_identity(tmp_path):
    from body import Body

    body = Body.__new__(Body)
    body.config_dir = Path(tmp_path)
    body._config_overrides = {}
    body._reload_mission = MagicMock()
    body._service_dispatcher = None
    body._fetch_config = lambda filename: "RECONFIG_ALPHA_READY" if filename == "identity.md" else None
    body.assemble_system_prompt = lambda: body._config_overrides["identity.md"].strip()

    body._on_config_change()

    assert body._config_overrides["identity.md"] == "RECONFIG_ALPHA_READY"
    assert body._system_prompt == "RECONFIG_ALPHA_READY"
    body._reload_mission.assert_called_once()


def test_build_direct_idle_prompt_prioritizes_identity():
    from body import Body

    body = Body.__new__(Body)
    body._config_overrides = {
        "identity.md": "Always reply with EXACT_TOKEN_ONLY and nothing else."
    }

    prompt = body._build_direct_idle_prompt("dm-alpha", "operator", "What is 2+2?")

    assert "Always reply with EXACT_TOKEN_ONLY and nothing else." in prompt
    assert "response policy for this message" in prompt
    assert "use that literally" in prompt
    assert "Do not answer the underlying question in a default helpful style" in prompt
    assert "Only fall back to normal concise conversational help when your identity is silent" in prompt


def test_assemble_system_prompt_includes_operating_loop(tmp_path):
    from body import Body

    body = Body.__new__(Body)
    body.config_dir = Path(tmp_path)
    body.is_meeseeks = False
    body._active_mission = None
    body._task_features = {"prompt_tier": "minimal"}
    body._config_overrides = {"identity.md": "IDENTITY"}
    body._skills_manager = MagicMock()
    body._skills_manager.get_system_prompt_section.return_value = ""

    prompt = body.assemble_system_prompt()

    assert "# Operating Loop" in prompt
    assert "Do not claim you used a tool" in prompt
    assert "Do not write simulated tool markup" in prompt
    assert "latest, current, recent, or time-sensitive information" in prompt
    assert "If blocked by missing tools" in prompt


def test_assemble_system_prompt_includes_provider_tools(tmp_path):
    from body import Body

    (tmp_path / "provider-tools.yaml").write_text(
        "agent: scout\n"
        "grants:\n"
        "  - capability: provider-web-search\n"
    )
    body = Body.__new__(Body)
    body.config_dir = Path(tmp_path)
    body.is_meeseeks = False
    body._active_mission = None
    body._task_features = {"prompt_tier": "minimal"}
    body._config_overrides = {"identity.md": "IDENTITY"}
    body._skills_manager = MagicMock()
    body._skills_manager.get_system_prompt_section.return_value = ""

    prompt = body.assemble_system_prompt()

    assert "# Provider Tools" in prompt
    assert "web_search" in prompt
    assert "do not simulate them" in prompt


def test_build_direct_idle_prompt_includes_recent_context():
    from body import Body

    body = Body.__new__(Body)
    body.config_dir = Path("/nonexistent")
    body._config_overrides = {}

    prompt = body._build_direct_idle_prompt(
        "dm-jarvis",
        "_operator",
        "Whatever one is most recent",
        "Recent conversation in this channel:\n"
        "_operator: PLTR's more recent SEC filing\n"
        "jarvis: Could you clarify the filing type?",
    )

    assert "PLTR's more recent SEC filing" in prompt
    assert "Whatever one is most recent" in prompt
    assert "resolve follow-up references" in prompt


def test_build_direct_idle_prompt_includes_scratchpad_and_graph_memory():
    from body import Body

    body = Body.__new__(Body)
    body.config_dir = Path("/nonexistent")
    body._config_overrides = {}

    prompt = body._build_direct_idle_prompt(
        "dm-jarvis",
        "_operator",
        "Whatever one is most recent",
        recent_context="Recent conversation in this channel:\n_operator: PLTR SEC filing",
        session_scratchpad="[SESSION_SCRATCHPAD]\nactive_entities: PLTR, SEC\n[/SESSION_SCRATCHPAD]",
        graph_memory_context="[KNOWLEDGE_GRAPH_CONTEXT]\n## Relevant Long-Term Memory\n[/KNOWLEDGE_GRAPH_CONTEXT]",
    )

    assert "[SESSION_SCRATCHPAD]" in prompt
    assert "active_entities: PLTR, SEC" in prompt
    assert "Relevant Long-Term Memory" in prompt


def test_fetch_recent_channel_context_formats_bounded_messages():
    from body import Body

    class Response:
        def raise_for_status(self):
            pass

        def json(self):
            return [
                {"author": "_operator", "content": "PLTR's more recent SEC filing"},
                {"author": "jarvis", "content": "Could you clarify the filing type?"},
                {"author": "_operator", "content": "Whatever one is most recent"},
            ]

    class Client:
        def get(self, url, params=None, timeout=None):
            self.url = url
            self.params = params
            self.timeout = timeout
            return Response()

    client = Client()
    body = Body.__new__(Body)
    body.agent_name = "jarvis"
    body._http_client = client

    context = body._fetch_recent_channel_context("dm-jarvis", limit=3)

    assert client.params == {"reader": "jarvis", "limit": "3"}
    assert "Recent conversation in this channel:" in context
    assert "_operator: PLTR's more recent SEC filing" in context
    assert "jarvis: Could you clarify the filing type?" in context
    assert "_operator: Whatever one is most recent" in context


def test_fetch_recent_channel_messages_returns_raw_messages():
    from body import Body

    class Response:
        def raise_for_status(self):
            pass

        def json(self):
            return [
                {"author": "_operator", "content": "first"},
                {"author": "jarvis", "content": "second"},
                {"author": "_operator", "content": "third"},
            ]

    class Client:
        def get(self, url, params=None, timeout=None):
            self.params = params
            return Response()

    client = Client()
    body = Body.__new__(Body)
    body.agent_name = "jarvis"
    body._http_client = client

    messages = body._fetch_recent_channel_messages("dm-jarvis", limit=2)

    assert client.params == {"reader": "jarvis", "limit": "2"}
    assert [m["content"] for m in messages] == ["second", "third"]


def test_cache_filters_include_policy_hash_and_mission_scope():
    from body import Body

    body = Body.__new__(Body)
    body.agent_name = "alpha"
    body._active_mission = {"id": "mission-123", "name": "Mission Alpha"}
    body._config_overrides = {
        "identity.md": "IDENTITY",
        "FRAMEWORK.md": "FRAMEWORK",
        "AGENTS.md": "AGENTS",
    }

    filters = body._cache_filters("2026-04-13T00:00:00Z")

    assert filters["agent"] == "alpha"
    assert filters["created_after"] == "2026-04-13T00:00:00Z"
    assert filters["mission_id"] == "mission-123"
    assert "policy_hash" in filters
    assert len(filters["policy_hash"]) == 12


def test_check_cache_queries_with_policy_hash():
    from body import Body

    captured = {}

    class StubHTTPClient:
        def post(self, url, json=None, timeout=None):
            captured["url"] = url
            captured["json"] = json
            captured["timeout"] = timeout
            return SimpleNamespace(status_code=200, json=lambda: {"results": []})

    body = Body.__new__(Body)
    body.agent_name = "alpha"
    body._active_mission = None
    body._http_client = StubHTTPClient()
    body._knowledge_url = "http://knowledge"
    body._config_overrides = {
        "identity.md": "IDENTITY",
        "FRAMEWORK.md": "FRAMEWORK",
        "AGENTS.md": "AGENTS",
    }

    hit_type, _, _, _ = body._check_cache("hello world")

    assert hit_type is None
    assert captured["url"] == "http://knowledge/query"
    assert captured["json"]["filters"]["agent"] == "alpha"
    assert "policy_hash" in captured["json"]["filters"]
