import sys
import json
from pathlib import Path

import httpx

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "body"))

from images.body.body import (
    Body,
    _activation_task_id,
    _pact_activation_for_storage,
    _pact_metadata_for_storage,
    _pact_verdict_payload,
    _provider_tool_definitions,
    _provider_tool_prompt_section,
    _sanitize_current_info_answer,
    _sanitize_outbound_content,
)
from work_contract import EvidenceLedger, classify_work


class _EmptyBuiltins:
    def get_tool_definitions(self):
        return []


def test_provider_web_search_declared_from_constraints(tmp_path):
    (tmp_path / "constraints.yaml").write_text(
        "agent: scout\n"
        "granted_capabilities:\n"
        "  - provider-web-search\n"
    )

    assert _provider_tool_definitions(tmp_path) == [{"type": "web_search"}]


def test_provider_web_search_declared_from_effective_provider_tools(tmp_path):
    (tmp_path / "constraints.yaml").write_text("agent: scout\ngranted_capabilities: []\n")
    (tmp_path / "provider-tools.yaml").write_text(
        "agent: scout\n"
        "grants:\n"
        "  - capability: provider-web-search\n"
        "    source: capabilities.yaml\n"
        "    granted_by: operator\n"
    )

    assert _provider_tool_definitions(tmp_path) == [{"type": "web_search"}]


def test_provider_tools_omitted_without_grant(tmp_path):
    (tmp_path / "constraints.yaml").write_text("agent: scout\ngranted_capabilities: []\n")

    assert _provider_tool_definitions(tmp_path) == []
    assert _provider_tool_prompt_section(tmp_path) == ""


def test_provider_tool_prompt_section_lists_web_search(tmp_path):
    (tmp_path / "provider-tools.yaml").write_text(
        "agent: scout\n"
        "grants:\n"
        "  - capability: provider-web-search\n"
    )

    section = _provider_tool_prompt_section(tmp_path)
    assert "# Provider Tools" in section
    assert "web_search" in section
    assert "do not simulate" in section


def test_sanitize_outbound_content_replaces_simulated_tool_markup():
    content = "I'll check.\n\n<search>\nquery: MSFT latest filing\n</search>\n\nweb_search(query: \"MSFT\")\n\nIt is X."

    sanitized = _sanitize_outbound_content(content)

    assert "<search>" not in sanitized
    assert "without guessing" in sanitized


def test_sanitize_current_info_answer_strips_search_preamble_lines():
    content = (
        "Let me search for more specific and current release information from the Node.js website.\n"
        "Now let me search for the official Node.js 24.15.0 LTS release blog post.\n"
        "\n"
        "Node.js 24.15.0 LTS is the latest stable Node.js release.\n"
        "Source: https://nodejs.org/en/blog/release/v24.15.0\n"
        "Checked as of: April 22, 2026"
    )

    sanitized = _sanitize_current_info_answer({"kind": "current_info"}, content)

    assert sanitized == (
        "Node.js 24.15.0 LTS is the latest stable Node.js release.\n"
        "Source: https://nodejs.org/en/blog/release/v24.15.0\n"
        "Checked as of: April 22, 2026"
    )


def test_sanitize_current_info_answer_preserves_non_current_info_content():
    content = "Let me search the file.\n\nThe test failed because pytest is missing."

    assert _sanitize_current_info_answer({"kind": "task"}, content) == content


def test_activation_task_id_prefers_explicit_event_task_id(tmp_path):
    task_id = _activation_task_id(
        {"task_id": "task-explicit"},
        tmp_path / "missing.json",
        "work-current_info",
    )

    assert task_id == "task-explicit"


def test_activation_task_id_claims_matching_current_task(tmp_path):
    ctx = tmp_path / "session-context.json"
    ctx.write_text(json.dumps({
        "current_task": {
            "task_id": "task-20260422-2cd2fa",
            "content": "[Mission trigger: channel dm-test / message]\n\nNew event from dm-test:\n  content: Find the latest stable Node.js release.",
            "work_item_id": "evt-msg-abc123",
            "metadata": {
                "channel": "dm-test",
                "event_id": "evt-msg-abc123",
            },
        }
    }), encoding="utf-8")

    task_id = _activation_task_id({
        "type": "message",
        "channel": "dm-test",
        "message": {
            "id": "msg-abc123",
            "content": "Find the latest stable Node.js release.",
        },
    }, ctx, "work-current_info")

    assert task_id == "task-20260422-2cd2fa"


def test_activation_task_id_does_not_claim_unmatched_current_task(tmp_path):
    ctx = tmp_path / "session-context.json"
    ctx.write_text(json.dumps({
        "current_task": {
            "task_id": "task-stale",
            "content": "Old unrelated request",
            "metadata": {"channel": "dm-test"},
        }
    }), encoding="utf-8")

    task_id = _activation_task_id({
        "type": "message",
        "channel": "dm-test",
        "message": {"id": "msg-new", "content": "New request"},
    }, ctx, "work-current_info")

    assert task_id.startswith("work-current_info-")


def test_body_tool_collection_includes_provider_web_search(tmp_path):
    (tmp_path / "constraints.yaml").write_text(
        "agent: scout\n"
        "granted_capabilities:\n"
        "  - provider-web-search\n"
    )
    body = Body.__new__(Body)
    body.config_dir = Path(tmp_path)
    body._builtin_tools = _EmptyBuiltins()
    body._service_dispatcher = None
    body._mcp_tools = {}

    assert {"type": "web_search"} in body._get_all_tool_definitions()


def test_pact_verdict_payload_summarizes_contract_and_evidence():
    payload = _pact_verdict_payload(
        "task-123",
        {
            "kind": "current_info",
            "requires_action": True,
            "required_evidence": ["current_source_or_blocker"],
            "answer_requirements": ["source_url", "checked_date"],
        },
        {
            "tool_results": [
                {"tool": "provider-web-search", "ok": True},
                {"tool": "provider-web-search", "ok": True},
                {"tool": "web_fetch", "ok": True},
            ],
            "observed": ["current_source"],
            "source_urls": ["https://nodejs.org/en"],
            "artifact_paths": [".results/report.md"],
            "changed_files": ["app.py"],
            "validation_results": [{"command": "pytest tests/test_app.py", "ok": True}],
            "entries": [
                {
                    "kind": "source_url",
                    "producer": "provider-web-search",
                    "source_url": "https://nodejs.org/en",
                },
                {"kind": "changed_file", "producer": "write_file", "value": "app.py"},
            ],
        },
        {
            "verdict": "needs_action",
            "missing_evidence": ["source_url_from_evidence"],
        },
    )

    assert payload == {
        "task_id": "task-123",
        "kind": "current_info",
        "verdict": "needs_action",
        "required_evidence": ["current_source_or_blocker"],
        "answer_requirements": ["source_url", "checked_date"],
        "missing_evidence": ["source_url_from_evidence"],
        "observed": ["current_source"],
        "source_urls": ["https://nodejs.org/en"],
        "artifact_paths": [".results/report.md"],
        "changed_files": ["app.py"],
        "validation_results": [{"command": "pytest tests/test_app.py", "ok": True}],
        "evidence_entries": [
            {
                "kind": "source_url",
                "producer": "provider-web-search",
                "source_url": "https://nodejs.org/en",
            },
            {"kind": "changed_file", "producer": "write_file", "value": "app.py"},
        ],
        "tools": ["provider-web-search", "web_fetch"],
    }


def test_pact_metadata_for_storage_drops_task_id_but_keeps_audit_fields():
    metadata = _pact_metadata_for_storage({
        "task_id": "task-123",
        "kind": "current_info",
        "verdict": "completed",
        "required_evidence": ["current_source_or_blocker"],
        "answer_requirements": ["source_url"],
        "missing_evidence": [],
        "observed": ["current_source"],
        "source_urls": ["https://nodejs.org/en"],
        "artifact_paths": [".results/report.md"],
        "changed_files": ["app.py"],
        "validation_results": [{"command": "pytest tests/test_app.py", "ok": True}],
        "evidence_entries": [{"kind": "source_url", "producer": "provider-web-search"}],
        "tools": ["provider-web-search"],
    })

    assert metadata == {
        "kind": "current_info",
        "verdict": "completed",
        "required_evidence": ["current_source_or_blocker"],
        "answer_requirements": ["source_url"],
        "missing_evidence": [],
        "observed": ["current_source"],
        "source_urls": ["https://nodejs.org/en"],
        "artifact_paths": [".results/report.md"],
        "changed_files": ["app.py"],
        "validation_results": [{"command": "pytest tests/test_app.py", "ok": True}],
        "evidence_entries": [{"kind": "source_url", "producer": "provider-web-search"}],
        "tools": ["provider-web-search"],
    }


def test_pact_activation_for_storage_keeps_activation_audit_fields():
    metadata = _pact_activation_for_storage({
        "pact_activation": {
            "content": "Find the latest Node.js release",
            "match_type": "direct",
            "source": "idle_direct",
            "channel": "dm-test",
            "author": "operator",
            "mission_active": False,
            "ignored": "value",
        }
    })

    assert metadata == {
        "content": "Find the latest Node.js release",
        "match_type": "direct",
        "source": "idle_direct",
        "channel": "dm-test",
        "author": "operator",
        "mission_active": False,
    }


def test_emit_pact_verdict_skips_non_action_contract():
    body = Body.__new__(Body)
    body._work_contract = {"kind": "chat", "requires_action": False}
    body._work_evidence = {}
    seen = []
    body._emit_signal = lambda signal_type, data: seen.append((signal_type, data))

    body._emit_pact_verdict("task-123", {"verdict": "completed"})

    assert seen == []


def test_emit_pact_verdict_emits_structured_signal():
    body = Body.__new__(Body)
    body._work_contract = {
        "kind": "current_info",
        "requires_action": True,
        "required_evidence": ["current_source_or_blocker"],
    }
    body._work_evidence = {
        "tool_results": [{"tool": "provider-web-search", "ok": True}],
        "observed": ["current_source"],
        "source_urls": ["https://nodejs.org/en"],
    }
    seen = []
    body._emit_signal = lambda signal_type, data: seen.append((signal_type, data))

    body._emit_pact_verdict("task-123", {"verdict": "blocked"})

    assert seen == [(
        "pact_verdict",
        {
            "task_id": "task-123",
            "kind": "current_info",
            "verdict": "blocked",
            "required_evidence": ["current_source_or_blocker"],
            "answer_requirements": [],
            "missing_evidence": [],
            "observed": ["current_source"],
            "source_urls": ["https://nodejs.org/en"],
            "artifact_paths": [],
            "changed_files": [],
            "validation_results": [],
            "evidence_entries": [],
            "tools": ["provider-web-search"],
        },
    )]


def test_save_result_artifact_includes_pact_frontmatter(tmp_path):
    body = Body.__new__(Body)
    body.workspace_dir = tmp_path
    body.agent_name = "scout"
    body._last_pact_verdict = {
        "task_id": "task-123",
        "kind": "current_info",
        "verdict": "completed",
        "required_evidence": ["current_source_or_blocker"],
        "answer_requirements": ["source_url", "checked_date"],
        "missing_evidence": [],
        "observed": ["current_source"],
        "source_urls": ["https://nodejs.org/en/blog/release/v24.15.0"],
        "tools": ["provider-web-search"],
    }
    body._task_metadata = {
        "pact_activation": {
            "content": "Find latest Node.js",
            "match_type": "direct",
            "source": "idle_direct",
            "channel": "dm-scout",
            "author": "operator",
            "mission_active": False,
        }
    }

    artifact_ref = body._save_result_artifact("task-123", "Find latest Node.js", "Node.js 24.15.0", 2)

    assert artifact_ref == ".results/task-123.md"
    artifact = (tmp_path / ".results" / "task-123.md").read_text()
    frontmatter = artifact.split("---", 2)[1]
    assert "pact:" in frontmatter
    assert "kind: current_info" in frontmatter
    assert "verdict: completed" in frontmatter
    assert "https://nodejs.org/en/blog/release/v24.15.0" in frontmatter
    assert "pact_activation:" in frontmatter
    assert "channel: dm-scout" in frontmatter


def test_save_result_artifact_records_artifact_evidence(tmp_path):
    body = Body.__new__(Body)
    body.workspace_dir = tmp_path
    body.agent_name = "scout"
    body._last_pact_verdict = None
    body._task_metadata = {}
    body._work_evidence_ledger = EvidenceLedger()
    body._work_evidence = body._work_evidence_ledger.to_dict()

    artifact_ref = body._save_result_artifact("task-123", "Create report", "Report body", 1)

    assert artifact_ref == ".results/task-123.md"
    assert body._work_evidence["artifact_paths"] == [".results/task-123.md"]
    assert body._work_evidence_ledger.artifact_paths() == [".results/task-123.md"]


def test_write_cache_entry_includes_pact_metadata():
    posted = []

    class _Client:
        def post(self, url, json, timeout):
            posted.append((url, json, timeout))

    body = Body.__new__(Body)
    body.agent_name = "scout"
    body._active_mission = {}
    body._knowledge_url = "http://knowledge"
    body._http_client = _Client()
    body._tools_used_this_task = set()
    body._get_cache_config = lambda: {"enabled": True, "ttl_hours": 24, "scope": "agent"}
    body._current_response_policy_hash = lambda: "policy123"
    body._last_pact_verdict = {
        "task_id": "task-123",
        "kind": "current_info",
        "verdict": "completed",
        "required_evidence": ["current_source_or_blocker"],
        "answer_requirements": ["source_url"],
        "missing_evidence": [],
        "observed": ["current_source"],
        "source_urls": ["https://nodejs.org/en"],
        "tools": ["provider-web-search"],
    }
    body._task_metadata = {
        "pact_activation": {
            "content": "Find latest Node.js",
            "match_type": "direct",
            "source": "idle_direct",
            "channel": "dm-scout",
            "author": "operator",
            "mission_active": False,
        }
    }

    body._write_cache_entry("task-123", "Find latest Node.js", "Node.js 24.15.0", {})

    pact = posted[0][1]["nodes"][0]["properties"]["pact"]
    assert pact["kind"] == "current_info"
    assert pact["verdict"] == "completed"
    assert pact["source_urls"] == ["https://nodejs.org/en"]
    activation = posted[0][1]["nodes"][0]["properties"]["pact_activation"]
    assert activation["channel"] == "dm-scout"
    assert activation["source"] == "idle_direct"


def test_stream_records_provider_tool_evidence_and_ignores_empty_tool_delta():
    events = [
        {
            "choices": [{
                "delta": {"content": "Found it."},
                "finish_reason": None,
            }],
        },
        {
            "choices": [{
                "delta": {"tool_calls": [{"index": 0}]},
                "finish_reason": None,
            }],
        },
        {
            "object": "agency.provider_tool_evidence",
            "agency_provider_tool_evidence": {
                "provider_tool_capabilities": "provider-web-search",
                "provider_response_tool_types": "server_tool_use,web_search_result,web_search_tool_result",
                "provider_source_urls": "https://example.com/source",
            },
            "choices": [],
        },
    ]
    body_bytes = b"".join(
        f"data: {json.dumps(event)}\n\n".encode("utf-8")
        for event in events
    ) + b"data: [DONE]\n\n"

    def handler(request):
        return httpx.Response(
            200,
            headers={"content-type": "text/event-stream"},
            content=body_bytes,
        )

    body = Body.__new__(Body)
    body._http_client = httpx.Client(transport=httpx.MockTransport(handler))
    body._work_evidence = {"tool_results": [], "observed": []}

    response = body._stream_llm_response(
        "http://enforcer/v1/chat/completions",
        {"model": "claude-sonnet", "messages": [], "stream": True},
        {},
    )

    message = response["choices"][0]["message"]
    assert message["content"] == "Found it."
    assert "tool_calls" not in message
    assert body._work_evidence["tool_results"] == [{"tool": "provider-web-search", "ok": True}]
    assert "current_source" in body._work_evidence["observed"]
    assert body._work_evidence["source_urls"] == ["https://example.com/source"]
    assert isinstance(body._work_evidence_ledger, EvidenceLedger)
    assert body._work_evidence_ledger.source_urls() == ["https://example.com/source"]


def test_blocked_completion_is_terminal_outcome():
    body = Body.__new__(Body)
    body._work_contract = classify_work("Find the latest SEC filing").to_dict()
    body._work_evidence = {"tool_results": [], "observed": []}
    body._current_task_id = "task-123"
    body._task_complete_called = False
    body._task_terminal_outcome = None
    body._emit_pact_verdict = lambda _task_id, _verdict: None

    result = json.loads(body._handle_complete_task("I cannot access a current source."))

    assert result["status"] == "complete"
    assert body._task_complete_called is True
    assert body._task_terminal_outcome == "blocked"
    assert "without guessing" in body._task_result_summary


def test_file_artifact_completion_materializes_runtime_artifact(tmp_path):
    body = Body.__new__(Body)
    body.workspace_dir = tmp_path
    body.agent_name = "scout"
    body._work_contract = classify_work("Create a markdown report").to_dict()
    body._work_evidence_ledger = EvidenceLedger()
    body._work_evidence = body._work_evidence_ledger.to_dict()
    body._current_task_id = "task-123"
    body._task_content = "Create a markdown report"
    body._current_task_turns = 2
    body._task_complete_called = False
    body._task_terminal_outcome = None
    body._last_pact_verdict = None
    body._task_metadata = {}
    body._emit_pact_verdict = lambda _task_id, _verdict: None

    result = json.loads(body._handle_complete_task("Release report body"))

    assert result["status"] == "complete"
    assert result["summary"].endswith("Artifact: .results/task-123.md")
    assert body._task_complete_called is True
    assert body._task_terminal_outcome == "completed"
    assert body._work_evidence["artifact_paths"] == [".results/task-123.md"]
    assert (tmp_path / ".results" / "task-123.md").exists()


def test_records_code_change_evidence_from_write_and_validation_tools(tmp_path):
    body = Body.__new__(Body)
    body.workspace_dir = tmp_path
    body._work_evidence_ledger = EvidenceLedger()
    body._work_evidence = body._work_evidence_ledger.to_dict()

    body._record_work_tool_result(
        "write_file",
        json.dumps({"status": "ok", "path": str(tmp_path / "app.py"), "bytes": 12}),
        {"path": "app.py", "content": "print('hi')"},
    )
    body._record_work_tool_result(
        "execute_command",
        json.dumps({"stdout": ".", "stderr": "", "exit_code": 0}),
        {"command": "pytest tests/test_app.py"},
    )

    assert body._work_evidence["changed_files"] == ["app.py"]
    assert body._work_evidence["validation_results"] == [
        {"command": "pytest tests/test_app.py", "ok": True, "exit_code": 0},
    ]
