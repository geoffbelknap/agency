from objective_builder import build_objective
from pact_engine import (
    ActivationContext,
    ExecutionMode,
    ExecutionState,
    Plan,
    PlanStep,
    Strategy,
    WorkContract,
    build_plan,
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


def _task(content: str = "Fix images/body/pact_engine.py") -> dict:
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


def _planned(kind: str, content: str = "Fix images/body/pact_engine.py") -> tuple:
    contract = _contract(kind)
    task = _task(content)
    objective = build_objective(_activation(content), contract, task)
    strategy = Strategy(ExecutionMode.planned, needs_planner=True, needs_approval=False)
    return build_plan(objective, contract, strategy, task), objective, contract, strategy, task


def test_plan_step_and_plan_default_construction_to_dict_are_deterministic():
    step = PlanStep(
        step_id="step-01",
        phase="preparation",
        summary="locate target files",
        required_capabilities=("read_file",),
        expected_evidence=("target_files_identified",),
    )
    plan = Plan(steps=(step,), stop_conditions=("evidence_satisfied",), summary="Example")

    assert Plan().to_dict() == {"steps": [], "stop_conditions": [], "summary": ""}
    assert step.to_dict() == {
        "step_id": "step-01",
        "phase": "preparation",
        "summary": "locate target files",
        "required_capabilities": ["read_file"],
        "expected_evidence": ["target_files_identified"],
        "requires_approval": False,
    }
    assert plan.to_dict() == {
        "steps": [step.to_dict()],
        "stop_conditions": ["evidence_satisfied"],
        "summary": "Example",
    }


def test_build_plan_returns_none_when_strategy_does_not_need_planner():
    contract = _contract("chat")
    task = _task("hello there")
    objective = build_objective(_activation("hello there"), contract, task)
    strategy = Strategy(ExecutionMode.trivial_direct, needs_planner=False, needs_approval=False)

    assert build_plan(objective, contract, strategy, task) is None


def test_code_change_plan_has_expected_shape():
    plan, objective, _, _, _ = _planned("code_change")

    assert plan is not None
    assert plan.summary == f"Code change plan for {objective.statement[:80]}"
    assert plan.stop_conditions == ("evidence_satisfied", "budget_exhausted", "validation_failed")
    assert [(step.phase, step.summary) for step in plan.steps] == [
        ("preparation", "locate target files"),
        ("execution", "apply changes"),
        ("validation", "run tests or build"),
        ("validation", "summarize changes"),
    ]
    assert plan.steps[0].expected_evidence == ("target_files_identified",)
    assert plan.steps[1].required_capabilities == ("write_file",)
    assert plan.steps[1].expected_evidence == ("changed_file",)
    assert plan.steps[2].required_capabilities == ("execute_command",)
    assert plan.steps[2].expected_evidence == ("validation_result",)
    assert plan.steps[3].expected_evidence == ("tool_result",)


def test_file_artifact_plan_has_artifact_path_evidence_on_execution_step():
    plan, objective, _, _, _ = _planned("file_artifact", "Create a markdown report")

    assert plan is not None
    assert plan.summary == f"File artifact plan for {objective.statement[:80]}"
    assert len(plan.steps) == 3
    assert [step.summary for step in plan.steps] == [
        "gather inputs",
        "generate artifact",
        "validate artifact",
    ]
    assert plan.steps[1].phase == "execution"
    assert plan.steps[1].required_capabilities == ("write_file",)
    assert plan.steps[1].expected_evidence == ("artifact_path",)


def test_external_side_effect_plan_approval_precedes_execution_step():
    plan, objective, _, _, _ = _planned("external_side_effect", "Restart production")

    assert plan is not None
    assert plan.summary == f"External side effect plan for {objective.statement[:80]}"
    assert len(plan.steps) == 4
    assert plan.steps[0].summary == "verify principal authority"
    assert plan.steps[1].phase == "approval"
    assert plan.steps[1].summary == "obtain operator approval"
    assert plan.steps[1].requires_approval is True
    assert plan.steps[2].summary == "execute external operation"
    assert plan.steps[2].required_capabilities == ("external_state",)


def test_current_info_planned_variant_has_three_steps():
    plan, objective, _, _, _ = _planned("current_info", "Find latest Node.js release")

    assert plan is not None
    assert plan.summary == f"Current info plan for {objective.statement[:80]}"
    assert [(step.phase, step.summary) for step in plan.steps] == [
        ("preparation", "search for current source"),
        ("validation", "verify source is current"),
        ("execution", "formulate answer with citations"),
    ]
    assert plan.steps[0].required_capabilities == ("web", "search")
    assert plan.steps[1].expected_evidence == ("current_source",)
    assert plan.steps[2].expected_evidence == ("source_url",)


def test_external_side_effect_plan_enforces_approval_before_external_state():
    plan, _, _, _, _ = _planned("external_side_effect", "Restart production")

    approval_indexes = [
        index for index, step in enumerate(plan.steps)
        if step.requires_approval
    ]
    external_state_indexes = [
        index for index, step in enumerate(plan.steps)
        if "external_state" in step.required_capabilities
    ]

    assert approval_indexes
    assert external_state_indexes
    assert min(approval_indexes) < min(external_state_indexes)
    assert all(min(approval_indexes) < index for index in external_state_indexes)


def test_unknown_contract_kind_with_planner_returns_empty_steps_plan():
    plan, _, _, _, _ = _planned("unknown_kind", "Do something unusual")

    assert plan is not None
    assert plan.steps == ()
    assert plan.stop_conditions == ("evidence_satisfied",)
    assert plan.summary == "No template for contract kind unknown_kind"


def test_build_plan_is_pure_for_equal_inputs():
    plan, objective, contract, strategy, task = _planned("code_change")

    assert plan == build_plan(objective, contract, strategy, task)


def test_execution_state_from_task_populates_plan_only_when_strategy_needs_planner():
    code_contract = _contract("code_change")
    code_task = _task_with_contract("Fix images/body/pact_engine.py", code_contract)
    code_task["metadata"]["target_files"] = ["images/body/pact_engine.py"]
    chat_contract = _contract("chat")

    planned_state = ExecutionState.from_task(code_task, agent="scout")
    unplanned_state = ExecutionState.from_task(_task_with_contract("hello there", chat_contract), agent="scout")

    assert planned_state.strategy is not None
    assert planned_state.strategy.needs_planner is True
    assert planned_state.plan is not None
    assert planned_state.to_dict()["plan"]["steps"][0]["step_id"] == "step-01"
    assert unplanned_state.strategy is not None
    assert unplanned_state.strategy.needs_planner is False
    assert unplanned_state.plan is None
    assert unplanned_state.to_dict()["plan"] is None


def test_execution_state_attach_mission_rebuilds_plan():
    contract = _contract("code_change", allowed_terminal_states=["completed"])
    task = _task_with_contract("Fix images/body/pact_engine.py", contract)
    task["metadata"]["target_files"] = ["images/body/pact_engine.py"]
    state = ExecutionState.from_task(task, agent="scout")
    original_plan = state.plan
    state.plan = None
    state.strategy = None

    state.attach_mission({"constraints": ["mission-readonly"]})

    assert state.objective is not None
    assert state.objective.constraints == ["mission-readonly", "terminal:completed"]
    assert state.strategy is not None
    assert state.strategy.needs_planner is True
    assert state.plan is not None
    assert state.plan == original_plan


def test_plan_steps_have_stable_zero_padded_ids():
    for kind, content in (
        ("code_change", "Fix images/body/pact_engine.py"),
        ("file_artifact", "Create a markdown report"),
        ("external_side_effect", "Restart production"),
        ("current_info", "Find latest Node.js release"),
    ):
        plan, _, _, _, _ = _planned(kind, content)

        assert [step.step_id for step in plan.steps] == [
            f"step-{index:02d}" for index in range(1, len(plan.steps) + 1)
        ]
