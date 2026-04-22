import sys
import json
from pathlib import Path

import httpx

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "body"))

from images.body.body import (
    Body,
    _activation_task_id,
    _provider_tool_definitions,
    _provider_tool_prompt_section,
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
