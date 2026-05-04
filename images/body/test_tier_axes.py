from body import Body
from pact_engine import ExecutionMode, ExecutionState, Objective, Strategy, WorkContract
from task_tier import classify_context_depth, classify_reasoning_depth, select_model


HANK_REPLAY = "I want to see if you can help me out by investigating this github repository..."


def _objective(kind: str = "chat", *, generation_mode: str = "grounded", risk_level: str = "medium") -> Objective:
    return Objective(kind=kind, generation_mode=generation_mode, risk_level=risk_level)


def _strategy(
    execution_mode: ExecutionMode | str = ExecutionMode.tool_loop,
    *,
    needs_planner: bool = False,
    needs_approval: bool = False,
) -> Strategy:
    return Strategy(
        execution_mode=execution_mode,
        needs_planner=needs_planner,
        needs_approval=needs_approval,
    )


def _contract(kind: str) -> WorkContract:
    return WorkContract(
        kind=kind,
        requires_action=True,
        required_evidence=[],
        answer_requirements=[],
        allowed_terminal_states=["completed", "blocked"],
        reason="test",
        summary="Test contract.",
    )


def _task_with_contract(content: str, contract: WorkContract, *, task_id: str = "task-123") -> dict:
    return {
        "task_id": task_id,
        "started_at": "2026-04-23T00:00:00Z",
        "metadata": {
            "pact_activation": {
                "content": content,
                "match_type": "direct",
                "source": "idle_direct:dm-hank3:operator",
                "channel": "dm-hank3",
                "author": "operator",
                "mission_active": False,
            },
            "work_contract": contract.to_dict(),
        },
    }


def test_reasoning_social_is_direct():
    assert classify_reasoning_depth({}, None, objective=_objective(generation_mode="social")) == "direct"


def test_reasoning_creative_is_direct():
    assert classify_reasoning_depth({}, None, objective=_objective(generation_mode="creative")) == "direct"


def test_reasoning_clarify_is_direct():
    assert classify_reasoning_depth({}, None, strategy=_strategy(ExecutionMode.clarify)) == "direct"


def test_reasoning_escalated_risk_is_deliberative():
    assert classify_reasoning_depth({}, None, objective=_objective(risk_level="escalated")) == "deliberative"


def test_reasoning_external_side_effect_is_deliberative():
    assert classify_reasoning_depth({}, None, objective=_objective(kind="external_side_effect")) == "deliberative"


def test_reasoning_needs_approval_is_deliberative():
    assert classify_reasoning_depth({}, None, strategy=_strategy(needs_approval=True)) == "deliberative"


def test_reasoning_high_risk_is_reflective():
    assert classify_reasoning_depth({}, None, objective=_objective(risk_level="high")) == "reflective"


def test_reasoning_code_change_is_reflective():
    assert classify_reasoning_depth({}, None, objective=_objective(kind="code_change")) == "reflective"


def test_reasoning_grounded_default_is_reflective():
    assert classify_reasoning_depth({}, None, objective=_objective(generation_mode="grounded")) == "reflective"


def test_reasoning_empty_inputs_are_reflective():
    assert classify_reasoning_depth({}, None) == "reflective"


def test_context_social_is_minimal():
    assert classify_context_depth({}, None, objective=_objective(generation_mode="social")) == "minimal"


def test_context_clarify_is_minimal():
    assert classify_context_depth({}, None, strategy=_strategy(ExecutionMode.clarify)) == "minimal"


def test_context_active_mission_is_task_relevant():
    mission = {"status": "active"}
    assert classify_context_depth({}, mission, objective=_objective(generation_mode="grounded")) == "task-relevant"


def test_context_code_change_is_task_relevant():
    assert classify_context_depth({}, None, objective=_objective(kind="code_change")) == "task-relevant"


def test_context_grounded_is_task_relevant():
    assert classify_context_depth({}, None, objective=_objective(generation_mode="grounded")) == "task-relevant"


def test_context_empty_inputs_are_task_relevant():
    assert classify_context_depth({}, None) == "task-relevant"


def test_model_social_low_risk_is_small():
    assert select_model({}, None, objective=_objective(generation_mode="social", risk_level="low")) == "fast"


def test_model_creative_low_risk_is_small():
    assert select_model({}, None, objective=_objective(generation_mode="creative", risk_level="low")) == "fast"


def test_model_escalated_risk_is_large():
    assert select_model({}, None, objective=_objective(risk_level="escalated")) == "frontier"


def test_model_external_side_effect_is_large():
    assert select_model({}, None, objective=_objective(kind="external_side_effect")) == "frontier"


def test_model_grounded_is_standard():
    assert select_model({}, None, objective=_objective(generation_mode="grounded")) == "standard"


def test_model_code_change_is_standard():
    assert select_model({}, None, objective=_objective(kind="code_change")) == "standard"


def test_model_empty_inputs_are_standard():
    assert select_model({}, None) == "standard"


def test_frugal_external_side_effect_keeps_safety_floor():
    mission = {"cost_mode": "frugal"}
    objective = _objective(kind="external_side_effect", risk_level="high")
    strategy = _strategy(ExecutionMode.external_side_effect, needs_planner=True, needs_approval=True)

    assert classify_reasoning_depth({}, mission, objective=objective, strategy=strategy) == "reflective"
    assert select_model({}, mission, objective=objective, strategy=strategy) == "standard"


def test_hank_replay_chat_contract_keeps_standard_model_but_direct_route():
    state = ExecutionState.from_task(_task_with_contract(HANK_REPLAY, _contract("chat")), agent="hank3")

    assert state.objective is not None
    assert state.objective.generation_mode == "grounded"
    assert state.strategy is not None
    assert state.strategy.execution_mode == ExecutionMode.trivial_direct
    assert state.reasoning_depth == "reflective"
    assert state.context_depth == "task-relevant"
    assert state.model == "standard"


def test_current_model_keeps_hank_replay_on_standard_with_idle_reply_prefix():
    task = _task_with_contract(HANK_REPLAY, _contract("chat"), task_id="idle-reply-hank3-123")
    body = Body.__new__(Body)
    body.model = "standard"
    body.admin_model = "fast"
    body.large_model = "frontier"
    body._active_mission = None
    body._task_metadata = task["metadata"]
    body._execution_state = ExecutionState.from_task(task, agent="hank3")

    assert body._current_model() == "standard"
