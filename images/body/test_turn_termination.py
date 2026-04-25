import json

from body import Body, TURN_CAP_DEFAULT, _turn_cap_for_task
from pact_engine import ExecutionState


def _task() -> dict:
    return {
        "task_id": "task-123",
        "content": "Say done.",
        "metadata": {
            "pact_activation": {
                "content": "Say done.",
                "match_type": "direct",
                "source": "dm",
                "channel": "dm-agent",
                "author": "operator",
                "mission_active": False,
            },
            "work_contract": {
                "kind": "chat",
                "requires_action": True,
                "required_evidence": [],
                "answer_requirements": [],
                "allowed_terminal_states": ["completed", "blocked", "needs_clarification"],
            },
        },
    }


def _response(*, content: str = "", stop_reason: str = "end_turn", tool_name: str | None = None) -> dict:
    message = {"role": "assistant", "stop_reason": stop_reason}
    finish_reason = "stop"
    if content:
        message["content"] = content
    if tool_name:
        finish_reason = "tool_calls"
        message["tool_calls"] = [{
            "id": f"call-{tool_name}",
            "type": "function",
            "function": {"name": tool_name, "arguments": json.dumps({"summary": content} if tool_name == "complete_task" else {"content": content})},
        }]
    return {"choices": [{"message": message, "finish_reason": finish_reason, "stop_reason": stop_reason}]}


def _finish_only_response(*, content: str = "", finish_reason: str = "stop", tool_name: str | None = None) -> dict:
    message = {"role": "assistant"}
    if content:
        message["content"] = content
    if tool_name:
        message["tool_calls"] = [{
            "id": f"call-{tool_name}",
            "type": "function",
            "function": {"name": tool_name, "arguments": json.dumps({"content": content})},
        }]
    return {"choices": [{"message": message, "finish_reason": finish_reason}]}


def _body(tmp_path, responses: list[dict], mission: dict | None = None) -> tuple[Body, list[tuple[str, dict]], list[str]]:
    body = Body.__new__(Body)
    body.agent_name = "agent"
    body.workspace_dir = tmp_path
    body._active_mission = mission or {}
    body._total_tasks = 0
    body._total_turns = 0
    body._interrupt_metrics = {
        "turns_from_interrupts": 0,
        "interrupts_received": 0,
        "interrupts_acted_on": 0,
        "notifications_queued": 0,
    }
    body._notification_queue = []
    body._pending_interrupts = []
    body._pending_notifications = []
    body._config_overrides = {}
    body._channel_reminder_sent = False
    body._checkpoint_injected = False
    body._event_id = None
    body._reflection = None
    body._http_client = None
    signals: list[tuple[str, dict]] = []
    send_messages: list[str] = []

    body._check_budget = lambda _task: True
    body._emit_signal = lambda kind, data: signals.append((kind, data))
    body._reload_mission = lambda: None
    body.assemble_system_prompt = lambda: "system"
    body._restore_conversation = lambda _task_id: None
    body._retrieve_knowledge_context = lambda _content: ""
    body._get_all_tool_definitions = lambda: []
    body._check_cache = lambda _content: (None, None, 0.0, None)
    body._drain_event_queue = lambda: None
    body._drain_notifications_at_pause = lambda: []
    body._manage_context = lambda messages: messages
    body._persist_conversation = lambda messages, task_id=None: None
    body._write_cache_entry = lambda **_kwargs: None
    body._capture_conversation_memory_proposals = lambda _task_id: None
    body._clear_conversation_log = lambda: None
    body._auto_summarize_task = lambda *_args: None
    body._record_work_artifact = lambda *_args, **_kwargs: None
    body._post_task_response = lambda *_args, **_kwargs: None
    body._post_channel_message = lambda *_args, **_kwargs: True

    iterator = iter(responses)
    body._call_llm = lambda *_args, **_kwargs: next(iterator)

    def handle_tool(tool_call: dict) -> str:
        name = tool_call.get("function", {}).get("name", "")
        args = Body._tool_call_arguments(tool_call)
        if name == "complete_task":
            return body._handle_complete_task(args.get("summary", ""))
        if name == "send_message":
            send_messages.append(args.get("content", ""))
            return json.dumps({"status": "sent"})
        return json.dumps({"status": "ok"})

    body._handle_tool_call = handle_tool
    return body, signals, send_messages


def test_legacy_complete_task_path_still_commits_unchanged(tmp_path):
    body, signals, _send_messages = _body(tmp_path, [_response(content="Done.", stop_reason="tool_use", tool_name="complete_task")])

    body._conversation_loop(_task())

    assert body._execution_state is None
    assert any(kind == "task_complete" for kind, _data in signals)
    assert [data["verdict"] for kind, data in signals if kind == "pact_verdict"] == ["completed"]


def test_model_terminal_path_commits_without_complete_task(tmp_path):
    body, signals, send_messages = _body(tmp_path, [_response(content="Done.", stop_reason="end_turn")])

    body._conversation_loop(_task())

    assert send_messages == []
    assert body._execution_state is None
    assert [data["verdict"] for kind, data in signals if kind == "pact_verdict"] == ["completed"]
    assert any(kind == "task_complete" for kind, _data in signals)


def test_finish_reason_only_terminal_path_commits_without_complete_task(tmp_path):
    body, signals, send_messages = _body(tmp_path, [_finish_only_response(content="Done.", finish_reason="stop")])

    body._conversation_loop(_task())

    assert send_messages == []
    assert body._execution_state is None
    assert [data["verdict"] for kind, data in signals if kind == "pact_verdict"] == ["completed"]


def test_finish_reason_only_tool_call_path_remains_non_terminal(tmp_path):
    body, signals, send_messages = _body(
        tmp_path,
        [
            _finish_only_response(content="Sent.", finish_reason="tool_calls", tool_name="send_message"),
            _finish_only_response(content="Done.", finish_reason="stop"),
        ],
    )

    body._conversation_loop(_task())

    assert send_messages == ["Sent."]
    assert body._execution_state is None
    assert [data["verdict"] for kind, data in signals if kind == "pact_verdict"] == ["completed"]


def test_dual_paths_coexist(tmp_path):
    legacy, legacy_signals, _ = _body(tmp_path / "legacy", [_response(content="Done.", stop_reason="tool_use", tool_name="complete_task")])
    native, native_signals, _ = _body(tmp_path / "native", [_response(content="Done.", stop_reason="end_turn")])

    legacy._conversation_loop(_task())
    native._conversation_loop(_task())

    assert [data["verdict"] for kind, data in legacy_signals if kind == "pact_verdict"] == ["completed"]
    assert [data["verdict"] for kind, data in native_signals if kind == "pact_verdict"] == ["completed"]


def test_hank_replay_turn_cap_blocks_and_clears_task(tmp_path):
    responses = [
        _response(content=f"message {idx}", stop_reason="tool_use", tool_name="send_message")
        for idx in range(TURN_CAP_DEFAULT + 3)
    ]
    body, signals, send_messages = _body(tmp_path, responses)

    body._conversation_loop(_task())

    verdicts = [data for kind, data in signals if kind == "pact_verdict"]
    assert body._current_task_turns == TURN_CAP_DEFAULT
    assert verdicts[-1]["verdict"] == "blocked"
    assert verdicts[-1]["reasons"] == ["runtime:turn_limit_exceeded"]
    assert body._execution_state is None
    assert len(send_messages) == TURN_CAP_DEFAULT


def test_cost_mode_turn_caps():
    assert _turn_cap_for_task({"cost_mode": "frugal"}) == 4
    assert _turn_cap_for_task({"cost_mode": "thorough"}) == 12
    assert _turn_cap_for_task({"cost_mode": "balanced"}) == TURN_CAP_DEFAULT
    assert _turn_cap_for_task({}) == TURN_CAP_DEFAULT
    assert _turn_cap_for_task(None) == TURN_CAP_DEFAULT


def test_execution_state_stop_reason_populated_after_model_response_and_serialized(tmp_path):
    body, _signals, _send_messages = _body(
        tmp_path,
        [_response(content="message", stop_reason="tool_use", tool_name="send_message") for _idx in range(4)],
        mission={"cost_mode": "frugal"},
    )

    body._conversation_loop(_task())

    artifact = (tmp_path / ".results" / "task-123.md").read_text()
    assert "stop_reason: tool_use" in artifact
    state = ExecutionState(task_id="task-456", agent="agent", stop_reason="end_turn")
    assert state.to_dict()["stop_reason"] == "end_turn"
