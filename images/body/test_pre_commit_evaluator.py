from datetime import datetime, timezone

from pact_engine import (
    ActivationContext,
    EvidenceEntry,
    ExecutionMode,
    ExecutionState,
    NextAction,
    Objective,
    Plan,
    PlanStep,
    PreCommitVerdict,
    RecoveryState,
    RecoveryStatus,
    Strategy,
    ToolObservation,
    ToolStatus,
    WorkContract,
    evaluate_pre_commit,
)


NOW = datetime(2026, 4, 22, 12, 0, tzinfo=timezone.utc)


def _activation() -> ActivationContext:
    return ActivationContext.from_message(
        "hello",
        source="idle_direct:dm-scout:operator",
        channel="dm-scout",
        author="operator",
    )


def _chat_contract() -> WorkContract:
    return WorkContract(kind="chat", requires_action=False)


def _state(contract: WorkContract | None = None) -> ExecutionState:
    return ExecutionState(
        task_id="task-123",
        agent="scout",
        activation=_activation(),
        contract=contract or _chat_contract(),
    )


def test_pre_commit_verdict_to_dict_is_deterministic():
    verdict = PreCommitVerdict(
        committable=False,
        reasons=("contract:needs_action",),
        missing=("action_result_or_blocker",),
        contract_verdict={"verdict": "needs_action"},
        evaluated_at=NOW,
    )

    assert verdict.to_dict() == {
        "committable": False,
        "reasons": ["contract:needs_action"],
        "missing": ["action_result_or_blocker"],
        "contract_verdict": {"verdict": "needs_action"},
        "evaluated_at": "2026-04-22T12:00:00Z",
    }


def test_layer_0_blocks_missing_activation():
    state = _state()
    state.activation = None

    verdict = evaluate_pre_commit(state, now=NOW)

    assert verdict.committable is False
    assert verdict.reasons == ("incomplete_state:activation",)


def test_layer_1_blocks_halted_recovery_status():
    state = _state()
    state.recovery_state = RecoveryState(status=RecoveryStatus.halted)

    verdict = evaluate_pre_commit(state, now=NOW)

    assert verdict.committable is False
    assert verdict.reasons == ("halt:halted",)


def test_layer_2_blocks_recovery_next_action_escalate():
    state = _state()
    state.recovery_state = RecoveryState(next_action=NextAction.escalate)

    verdict = evaluate_pre_commit(state, now=NOW)

    assert verdict.committable is False
    assert verdict.reasons == ("recovery:escalate",)


def test_layer_3_blocks_clarify_strategy():
    state = _state()
    state.strategy = Strategy(
        execution_mode=ExecutionMode.clarify,
        needs_planner=False,
        needs_approval=False,
    )

    verdict = evaluate_pre_commit(state, now=NOW)

    assert verdict.committable is False
    assert verdict.reasons == ("strategy:clarify",)


def test_layer_4_blocks_load_bearing_ambiguity():
    state = _state()
    state.objective = Objective(
        statement="fix the code",
        kind="code_change",
        ambiguities=["ambiguity:target_files_missing"],
    )

    verdict = evaluate_pre_commit(state, now=NOW)

    assert verdict.committable is False
    assert verdict.reasons == ("ambiguity:target_files_missing",)


def test_layer_5_blocks_required_approval_without_decision():
    state = _state()
    state.strategy = Strategy(
        execution_mode=ExecutionMode.external_side_effect,
        needs_planner=True,
        needs_approval=True,
    )

    verdict = evaluate_pre_commit(state, now=NOW)

    assert verdict.committable is False
    assert verdict.reasons == ("approval_required:no_approval_decision",)
    assert verdict.missing == ("approval_decision",)


def test_layer_5_passes_with_typed_approval_decision_observation():
    state = _state()
    state.strategy = Strategy(
        execution_mode=ExecutionMode.external_side_effect,
        needs_planner=True,
        needs_approval=True,
    )
    state.tool_observations.append(
        ToolObservation(
            tool="approval",
            status=ToolStatus.ok,
            evidence_classification=("approval_decision",),
        )
    )

    verdict = evaluate_pre_commit(state, now=NOW)

    assert verdict.committable is True
    assert verdict.reasons == ("committable",)


def test_layer_6_records_plan_evidence_advisory_without_blocking():
    state = _state()
    state.plan = Plan(
        steps=(
            PlanStep(
                step_id="step-01",
                phase="execution",
                expected_evidence=("artifact_path",),
            ),
        ),
    )

    verdict = evaluate_pre_commit(state, now=NOW)

    assert verdict.committable is True
    assert "plan_advisory:missing:artifact_path" in verdict.reasons


def test_layer_7_blocks_needs_action_contract_verdict():
    state = _state(WorkContract(
        kind="task",
        requires_action=True,
        required_evidence=["action_result_or_blocker"],
    ))

    verdict = evaluate_pre_commit(state, content="Done.", now=NOW)

    assert verdict.committable is False
    assert verdict.reasons == ("contract:needs_action",)
    assert verdict.missing == ("action_result_or_blocker",)
    assert verdict.contract_verdict["verdict"] == "needs_action"


def test_layer_7_passes_completed_contract_verdict():
    verdict = evaluate_pre_commit(_state(), content="Hello.", now=NOW)

    assert verdict.committable is True
    assert verdict.reasons == ("committable",)
    assert verdict.contract_verdict == {"verdict": "completed"}


def test_layer_7_passes_blocked_contract_verdict():
    state = _state(WorkContract(
        kind="operator_blocked",
        requires_action=True,
        required_evidence=["blocker_reason"],
        answer_requirements=["next_actor_or_unblocker"],
        allowed_terminal_states=["blocked", "needs_clarification"],
    ))

    verdict = evaluate_pre_commit(
        state,
        content="I am blocked because approval is missing. Please approve to unblock this.",
        now=NOW,
    )

    assert verdict.committable is True
    assert verdict.reasons == ("committable",)
    assert verdict.contract_verdict["verdict"] == "blocked"


def test_all_pass_happy_path_is_committable():
    verdict = evaluate_pre_commit(_state(), content="Hello.", now=NOW)

    assert verdict.committable is True
    assert verdict.reasons == ("committable",)
    assert verdict.missing == ()


def test_short_circuit_returns_first_failing_layer_only():
    state = _state()
    state.recovery_state = RecoveryState(status=RecoveryStatus.halted)
    state.strategy = Strategy(
        execution_mode=ExecutionMode.clarify,
        needs_planner=False,
        needs_approval=False,
    )

    verdict = evaluate_pre_commit(state, now=NOW)

    assert verdict.committable is False
    assert verdict.reasons == ("halt:halted",)


def test_evaluate_pre_commit_does_not_mutate_state():
    state = _state()
    state.evidence.record_entry(EvidenceEntry(kind="observation", producer="runtime", value="observed"))
    before = state.to_dict()
    evidence_id = id(state.evidence)
    observations_id = id(state.tool_observations)

    verdict = evaluate_pre_commit(state, now=NOW)

    assert verdict.committable is True
    assert state.to_dict() == before
    assert id(state.evidence) == evidence_id
    assert id(state.tool_observations) == observations_id


def test_evaluate_pre_commit_is_deterministic_with_explicit_now():
    state = _state()

    left = evaluate_pre_commit(state, content="Hello.", now=NOW)
    right = evaluate_pre_commit(state, content="Hello.", now=NOW)

    assert left == right
    assert left.to_dict() == right.to_dict()
