import sys
import json
from pathlib import Path

import httpx

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "body"))

from images.body.body import (
    Body,
    _activation_task_id,
    _pact_verdict_payload,
    _provider_tool_definitions,
    _provider_tool_prompt_section,
    _sanitize_current_info_answer,
    _sanitize_outbound_content,
)


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
        "tools": ["provider-web-search", "web_fetch"],
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
            "tools": ["provider-web-search"],
        },
    )]


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
