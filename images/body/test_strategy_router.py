from objective_builder import build_objective
from pact_engine import (
    ActivationContext,
    ExecutionMode,
    ExecutionState,
    WorkContract,
    build_strategy,
)


def _activation(content: str) -> ActivationContext:
    return ActivationContext.from_message(content, source="idle_direct:dm-scout:operator")


def _contract(kind: str, **overrides) -> WorkContract:
    values = {
        "kind": kind,
        "requires_action": True,
        "required_evidence": [],
        "answer_requirements": [],
        "allowed_terminal_states": ["completed", "blocked"],
        "reason": "test",
        "summary": "Test contract.",
    }
    values.update(overrides)
    return WorkContract(**values)


def _task(content: str = "Find latest Node.js release") -> dict:
    return {
        "task_id": "task-123",
        "started_at": "2026-04-22T12:00:00Z",
        "metadata": {
            "pact_activation": {
                "content": content,
                "match_type": "direct",
                "source": "idle_direct:dm-scout:operator",
                "channel": "dm-scout",
                "author": "operator",
                "mission_active": False,
            },
        },
    }


def _task_with_contract(content: str, contract: WorkContract) -> dict:
    task = _task(content)
    task["metadata"]["work_contract"] = contract.to_dict()
    return task


def _strategy_for(
    kind: str,
    content: str,
    *,
    task: dict | None = None,
    trust_level: str | None = None,
) -> tuple:
    contract = _contract(kind)
    task = task or _task(content)
    objective = build_objective(
        _activation(content),
        contract,
        task,
        trust_level=trust_level,
    )
    return build_strategy(objective, contract, task), objective, contract, task


def test_chat_routes_to_trivial_direct_without_planner_or_approval():
    strategy, _, _, _ = _strategy_for("chat", "hi")

    assert strategy.execution_mode == ExecutionMode.trivial_direct
    assert strategy.needs_planner is False
    assert strategy.needs_approval is False
    assert strategy.notes == ("reason:chat",)


def test_grounded_chat_routes_to_tool_loop_without_planner_or_approval():
    strategy, _, _, _ = _strategy_for("chat", "Investigate this repository")

    assert strategy.execution_mode == ExecutionMode.tool_loop
    assert strategy.needs_planner is False
    assert strategy.needs_approval is False
    assert strategy.notes == ("reason:grounded_informal_ask",)


def test_social_chat_still_routes_to_trivial_direct():
    strategy, _, _, _ = _strategy_for("chat", "hi")

    assert strategy.execution_mode == ExecutionMode.trivial_direct
    assert strategy.notes == ("reason:chat",)


def test_creative_chat_still_routes_to_trivial_direct():
    strategy, _, _, _ = _strategy_for("chat", "tell me a joke")

    assert strategy.execution_mode == ExecutionMode.trivial_direct
    assert strategy.notes == ("reason:chat",)


def test_persona_chat_still_routes_to_trivial_direct():
    strategy, _, _, _ = _strategy_for("chat", "who are you")

    assert strategy.execution_mode == ExecutionMode.trivial_direct
    assert strategy.notes == ("reason:chat",)


def test_escalated_grounded_chat_wins_over_grounded_chat_rule():
    strategy, objective, _, _ = _strategy_for(
        "chat",
        "Investigate this repository",
        trust_level="untrusted",
    )

    assert objective.generation_mode == "grounded"
    assert objective.risk_level == "escalated"
    assert strategy.execution_mode == ExecutionMode.escalate
    assert strategy.notes == ("reason:escalated_risk",)


def test_load_bearing_ambiguity_wins_over_grounded_chat_rule():
    contract = _contract("chat")
    task = _task("Investigate this repository")
    objective = build_objective(_activation("Investigate this repository"), contract, task)
    objective.ambiguities.append("ambiguity:target_files_missing")

    strategy = build_strategy(objective, contract, task)

    assert objective.generation_mode == "grounded"
    assert strategy.execution_mode == ExecutionMode.clarify
    assert strategy.notes == ("reason:load_bearing_ambiguity",)


def test_grounded_current_info_keeps_default_tool_loop_route():
    strategy, objective, _, _ = _strategy_for("current_info", "Find latest Node.js release")

    assert objective.generation_mode == "grounded"
    assert strategy.execution_mode == ExecutionMode.tool_loop
    assert strategy.notes == ("reason:default_tool_loop",)


def test_grounded_code_change_keeps_planned_route():
    task = _task("Fix images/body/pact_engine.py")
    task["metadata"]["target_files"] = ["images/body/pact_engine.py"]

    strategy, objective, _, _ = _strategy_for("code_change", "Fix images/body/pact_engine.py", task=task)

    assert objective.generation_mode == "grounded"
    assert strategy.execution_mode == ExecutionMode.planned
    assert strategy.notes == ("reason:code_change_default",)


def test_grounded_operator_blocked_keeps_trivial_direct_route():
    strategy, objective, _, _ = _strategy_for("operator_blocked", "Blocked waiting for approval")

    assert objective.generation_mode == "grounded"
    assert strategy.execution_mode == ExecutionMode.trivial_direct
    assert strategy.notes == ("reason:operator_blocked",)


def test_hank_replay_grounded_chat_routes_to_tool_loop():
    content = "I want to see if you can help me out by investigating this github repository..."
    strategy, objective, _, _ = _strategy_for("chat", content)

    assert objective.generation_mode == "grounded"
    assert strategy.execution_mode == ExecutionMode.tool_loop
    assert strategy.needs_planner is False
    assert strategy.needs_approval is False
    assert strategy.notes == ("reason:grounded_informal_ask",)


def test_current_info_at_medium_risk_routes_to_tool_loop():
    strategy, objective, _, _ = _strategy_for("current_info", "Find latest Node.js release")

    assert objective.risk_level == "medium"
    assert strategy.execution_mode == ExecutionMode.tool_loop
    assert strategy.notes == ("reason:default_tool_loop",)


def test_code_change_without_load_bearing_ambiguity_routes_to_planned():
    task = _task("Fix images/body/pact_engine.py")
    task["metadata"]["target_files"] = ["images/body/pact_engine.py"]

    strategy, objective, _, _ = _strategy_for("code_change", "Fix images/body/pact_engine.py", task=task)

    assert "ambiguity:target_files_missing" not in objective.ambiguities
    assert objective.risk_level == "medium"
    assert strategy.execution_mode == ExecutionMode.planned
    assert strategy.needs_planner is True
    assert strategy.notes == ("reason:code_change_default",)


def test_code_change_with_missing_target_files_routes_to_clarify():
    strategy, _, _, _ = _strategy_for("code_change", "Fix the failing tests")

    assert strategy.execution_mode == ExecutionMode.clarify
    assert strategy.needs_planner is False
    assert strategy.needs_approval is False
    assert strategy.notes == ("reason:load_bearing_ambiguity",)


def test_escalated_external_side_effect_wins_over_side_effect_rule():
    strategy, objective, _, _ = _strategy_for(
        "external_side_effect",
        "Restart production",
        trust_level="untrusted",
    )

    assert objective.risk_level == "escalated"
    assert "ambiguity:external_authority_scope" in objective.ambiguities
    assert strategy.execution_mode == ExecutionMode.escalate
    assert strategy.needs_approval is True
    assert strategy.notes == ("reason:escalated_risk",)


def test_authorized_external_side_effect_routes_to_approval_gated_mode():
    task = _task("Restart production")
    task["metadata"]["authority_scope"] = "production-restart-window"

    strategy, objective, _, _ = _strategy_for("external_side_effect", "Restart production", task=task)

    assert objective.risk_level == "high"
    assert objective.ambiguities == []
    assert strategy.execution_mode == ExecutionMode.external_side_effect
    assert strategy.needs_planner is True
    assert strategy.needs_approval is True
    assert strategy.notes == ("reason:external_side_effect",)


def test_operator_blocked_routes_to_trivial_direct():
    strategy, _, _, _ = _strategy_for("operator_blocked", "Blocked waiting for approval")

    assert strategy.execution_mode == ExecutionMode.trivial_direct
    assert strategy.needs_planner is False
    assert strategy.needs_approval is False
    assert strategy.notes == ("reason:operator_blocked",)


def test_high_risk_file_artifact_routes_to_planned():
    contract = _contract("file_artifact")
    task = _task("Create a markdown report")
    objective = build_objective(_activation("Create a markdown report"), contract, task)
    objective.risk_level = "high"

    strategy = build_strategy(objective, contract, task)

    assert objective.ambiguities == []
    assert strategy.execution_mode == ExecutionMode.planned
    assert strategy.needs_planner is True
    assert strategy.needs_approval is False
    assert strategy.notes == ("reason:high_risk",)


def test_untrusted_trust_level_escalates_every_kind():
    for kind in ("chat", "current_info", "code_change", "file_artifact", "task"):
        strategy, objective, _, _ = _strategy_for(kind, "hello there", trust_level="untrusted")

        assert objective.risk_level == "escalated"
        assert strategy.execution_mode == ExecutionMode.escalate
        assert strategy.needs_approval is True


def test_build_strategy_is_pure_for_equal_inputs():
    strategy, objective, contract, task = _strategy_for("current_info", "Find latest Node.js release")

    assert strategy == build_strategy(objective, contract, task)


def test_execution_state_from_task_populates_strategy_only_when_objective_is_populated():
    contract = _contract("current_info", required_evidence=["current_source_or_blocker"])
    populated = ExecutionState.from_task(_task_with_contract("Find latest Node.js release", contract), agent="scout")
    no_activation = ExecutionState.from_task({
        "task_id": "task-456",
        "metadata": {"work_contract": contract.to_dict()},
    }, agent="scout")
    no_contract = ExecutionState.from_task(_task("Find latest Node.js release"), agent="scout")

    assert populated.objective is not None
    assert populated.strategy is not None
    assert populated.to_dict()["strategy"]["execution_mode"] == "tool_loop"
    assert no_activation.objective is None
    assert no_activation.strategy is None
    assert no_activation.to_dict()["strategy"] is None
    assert no_contract.objective is None
    assert no_contract.strategy is None


def test_execution_state_attach_mission_rebuilds_objective_and_strategy():
    contract = _contract("chat", allowed_terminal_states=["completed"])
    state = ExecutionState.from_task(_task_with_contract("hi", contract), agent="scout")
    state.strategy = None

    state.attach_mission({"constraints": ["mission-readonly"]})

    assert state.objective is not None
    assert state.objective.constraints == ["mission-readonly", "terminal:completed"]
    assert state.strategy is not None
    assert state.strategy.execution_mode == ExecutionMode.trivial_direct


def test_execution_state_from_task_routes_hank_replay_to_tool_loop():
    content = "I want to see if you can help me out by investigating this github repository..."
    contract = _contract("chat", allowed_terminal_states=["completed"])
    state = ExecutionState.from_task(_task_with_contract(content, contract), agent="scout")

    assert state.objective is not None
    assert state.objective.generation_mode == "grounded"
    assert state.strategy is not None
    assert state.strategy.execution_mode == ExecutionMode.tool_loop
    assert state.strategy.needs_planner is False
    assert state.strategy.notes == ("reason:grounded_informal_ask",)
