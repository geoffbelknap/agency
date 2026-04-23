from pathlib import Path
from types import SimpleNamespace
from unittest.mock import MagicMock

import body as body_module
from body import Body, _execution_mode_prompt_section
from pact_engine import ExecutionMode, ExecutionState, Strategy, WorkContract


HANK_REPLAY = "I want to see if you can help me out by investigating this github repository..."


def _contract(kind: str = "chat") -> WorkContract:
    return WorkContract(
        kind=kind,
        requires_action=True,
        required_evidence=[],
        answer_requirements=[],
        allowed_terminal_states=["completed", "blocked"],
        reason="test",
        summary="Test contract.",
    )


def _task_with_contract(content: str, contract: WorkContract) -> dict:
    return {
        "task_id": "idle-reply-hank4-123",
        "started_at": "2026-04-23T00:00:00Z",
        "metadata": {
            "pact_activation": {
                "content": content,
                "match_type": "direct",
                "source": "idle_direct:dm-hank4:operator",
                "channel": "dm-hank4",
                "author": "operator",
                "mission_active": False,
            },
            "work_contract": contract.to_dict(),
        },
    }


def _strategy(execution_mode: ExecutionMode | str) -> Strategy:
    return Strategy(
        execution_mode=execution_mode,
        needs_planner=False,
        needs_approval=False,
    )


def _state(execution_mode: ExecutionMode | str) -> ExecutionState:
    return ExecutionState(
        task_id="task-123",
        agent="hank4",
        strategy=_strategy(execution_mode),
        context_depth="minimal",
    )


def _body(tmp_path: Path, state: ExecutionState | None) -> Body:
    (tmp_path / "PLATFORM.md").write_text("PLATFORM STATIC")
    (tmp_path / "provider-tools.yaml").write_text(
        "agent: hank4\n"
        "grants:\n"
        "  - capability: provider-web-search\n"
    )
    body = Body.__new__(Body)
    body.config_dir = tmp_path
    body.is_meeseeks = False
    body.agent_name = "hank4"
    body._active_mission = None
    body._config_overrides = {
        "identity.md": "IDENTITY STATIC",
        "FRAMEWORK.md": "FRAMEWORK STATIC",
        "AGENTS.md": "AGENTS STATIC",
    }
    body._fetch_config = lambda filename: None
    body._skills_manager = MagicMock()
    body._skills_manager.get_system_prompt_section.return_value = "SKILLS STATIC"
    body._execution_state = state
    return body


def _compose_prompt(tmp_path: Path, state: ExecutionState | None, monkeypatch) -> str:
    monkeypatch.setattr(
        "comms_tools.build_comms_context",
        lambda comms_url, agent_name: "COMMS STATIC",
    )
    monkeypatch.setattr(body_module, "fetch_procedural_memory", lambda *args, **kwargs: "")
    monkeypatch.setattr(body_module, "fetch_episodic_memory", lambda *args, **kwargs: "")
    return _body(tmp_path, state).assemble_system_prompt()


def test_tool_loop_mode_injects_must_call_instruction(tmp_path, monkeypatch):
    prompt = _compose_prompt(tmp_path, _state(ExecutionMode.tool_loop), monkeypatch)

    assert "# Execution Mode: tool_loop" in prompt
    assert "You MUST call one of the available tools (e.g., web_search) to gather evidence." in prompt
    assert "Do not emit tool-shaped text" in prompt
    assert "Do not announce tool use" in prompt


def test_clarify_mode_injects_clarification_instruction(tmp_path, monkeypatch):
    prompt = _compose_prompt(tmp_path, _state(ExecutionMode.clarify), monkeypatch)

    assert "# Execution Mode: clarify" in prompt
    assert "Respond with a specific, scoped clarification question" in prompt


def test_escalate_mode_injects_escalation_instruction(tmp_path, monkeypatch):
    prompt = _compose_prompt(tmp_path, _state(ExecutionMode.escalate), monkeypatch)

    assert "# Execution Mode: escalate" in prompt
    assert "Explain specifically what cannot be done and what operator action is" in prompt


def test_external_side_effect_mode_injects_approval_instruction(tmp_path, monkeypatch):
    prompt = _compose_prompt(tmp_path, _state(ExecutionMode.external_side_effect), monkeypatch)

    assert "# Execution Mode: external_side_effect" in prompt
    assert "Request explicit operator approval. Do not act on assumed" in prompt


def test_trivial_direct_mode_does_not_inject_mode_section(tmp_path, monkeypatch):
    prompt = _compose_prompt(tmp_path, _state(ExecutionMode.trivial_direct), monkeypatch)

    assert "# Execution Mode:" not in prompt


def test_planned_mode_does_not_inject_mode_section(tmp_path, monkeypatch):
    prompt = _compose_prompt(tmp_path, _state(ExecutionMode.planned), monkeypatch)

    assert "# Execution Mode:" not in prompt


def test_missing_execution_state_or_strategy_does_not_inject_mode_section(tmp_path, monkeypatch):
    no_state_prompt = _compose_prompt(tmp_path, None, monkeypatch)
    no_strategy_prompt = _compose_prompt(
        tmp_path,
        ExecutionState(task_id="task-123", agent="hank4", strategy=None, context_depth="minimal"),
        monkeypatch,
    )

    assert _execution_mode_prompt_section(None) == ""
    assert "# Execution Mode:" not in no_state_prompt
    assert "# Execution Mode:" not in no_strategy_prompt


def test_hank4_replay_routes_to_tool_loop_and_prompt_contains_instruction(tmp_path, monkeypatch):
    task = _task_with_contract(HANK_REPLAY, _contract("chat"))
    state = ExecutionState.from_task(task, agent="hank4")

    prompt = _compose_prompt(tmp_path, state, monkeypatch)

    assert state.objective is not None
    assert state.objective.generation_mode == "grounded"
    assert state.strategy is not None
    assert state.strategy.execution_mode == "tool_loop"
    assert "# Execution Mode: tool_loop" in prompt
    assert "MUST call" in prompt
    assert "Do not emit tool-shaped text" in prompt


def test_execution_mode_section_is_between_provider_tools_and_how_to_respond(tmp_path, monkeypatch):
    prompt = _compose_prompt(tmp_path, _state(ExecutionMode.tool_loop), monkeypatch)

    provider_index = prompt.index("# Provider Tools")
    mode_index = prompt.index("# Execution Mode:")
    response_index = prompt.index("# How to Respond")

    assert provider_index < mode_index < response_index


def test_unknown_future_mode_does_not_inject_mode_section(tmp_path, monkeypatch):
    state = ExecutionState(task_id="task-123", agent="hank4", context_depth="minimal")
    state.strategy = SimpleNamespace(execution_mode="future_mode")

    prompt = _compose_prompt(tmp_path, state, monkeypatch)

    assert _execution_mode_prompt_section(state) == ""
    assert "# Execution Mode:" not in prompt
