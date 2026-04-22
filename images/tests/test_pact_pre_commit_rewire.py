import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "body"))

from images.body.work_contract import (
    ActivationContext,
    EvidenceLedger,
    ExecutionMode,
    ExecutionState,
    Plan,
    PlanStep,
    RecoveryState,
    RecoveryStatus,
    Strategy,
    WorkContract,
    classify_activation,
    evaluate_pre_commit,
    map_pre_commit_verdict,
)


def _state(content: str, *, contract: WorkContract | None = None, metadata: dict | None = None) -> ExecutionState:
    activation = ActivationContext.from_message(
        content,
        match_type="direct",
        source="dm",
        channel="dm-agent",
        author="operator",
    )
    work_contract = contract or classify_activation(activation)
    task_metadata = {
        "pact_activation": activation.to_dict(),
        "work_contract": work_contract.to_dict(),
    }
    task_metadata.update(metadata or {})
    return ExecutionState.from_task(
        {
            "task_id": "task-123",
            "content": content,
            "metadata": task_metadata,
        },
        agent="agent",
    )


def _current_info_ledger() -> EvidenceLedger:
    ledger = EvidenceLedger()
    ledger.record_tool_result("provider-web-search", True)
    ledger.observe("current_source", producer="provider-web-search")
    ledger.record_source_url("https://nodejs.org/en/blog/release/v24.15.0", producer="provider-web-search")
    return ledger


def _code_change_ledger() -> EvidenceLedger:
    ledger = EvidenceLedger()
    ledger.record_tool_result("write_file", True)
    ledger.record_changed_file("parser.py", producer="write_file")
    ledger.record_tool_result("execute_command", True)
    ledger.record_validation_result("pytest tests/test_parser.py", True, producer="execute_command")
    return ledger


def _payload(state: ExecutionState, verdict):
    return map_pre_commit_verdict(
        verdict,
        state.task_id,
        state.contract.kind if state.contract else "",
        contract=state.contract,
        evidence=state.evidence,
    )


def test_happy_path_current_info_maps_completed_with_committable_reason():
    state = _state("Find the latest stable Node.js release")
    state.evidence = _current_info_ledger()

    verdict = evaluate_pre_commit(
        state,
        content=(
            "Node.js 24.15.0 LTS is the latest stable release. "
            "Source: Node.js https://nodejs.org/en/blog/release/v24.15.0. "
            "Checked: April 22, 2026."
        ),
    )
    payload = _payload(state, verdict)

    assert verdict.committable is True
    assert payload["verdict"] == "completed"
    assert payload["reasons"] == ["committable"]


def test_happy_path_operator_blocked_maps_terminal_blocked_with_committable_reason():
    state = _state("I am blocked waiting for operator approval to continue")

    verdict = evaluate_pre_commit(
        state,
        content="Blocked: approval is missing. What would unblock this: operator approval to continue.",
    )
    payload = _payload(state, verdict)

    assert verdict.committable is True
    assert payload["verdict"] == "blocked"
    assert payload["reasons"] == ["committable"]


def test_contract_needs_action_retries_once_then_allows_completed_commit():
    state = _state("Find the latest stable Node.js release")
    first = evaluate_pre_commit(state, content="Node.js 24.15.0 LTS is the latest stable release.")
    first_payload = _payload(state, first)
    retry_sent = False
    platform_messages = []

    if not first.committable and "contract:needs_action" in first.reasons and not retry_sent:
        retry_sent = True
        platform_messages.append(first.contract_verdict["message"])

    state.evidence = _current_info_ledger()
    second = evaluate_pre_commit(
        state,
        content=(
            "Node.js 24.15.0 LTS is the latest stable release. "
            "Source: Node.js https://nodejs.org/en/blog/release/v24.15.0. "
            "Checked: April 22, 2026."
        ),
    )
    second_payload = _payload(state, second)

    assert first_payload["verdict"] == "needs_action"
    assert first_payload["reasons"] == ["contract:needs_action"]
    assert platform_messages == [first.contract_verdict["message"]]
    assert second.committable is True
    assert second_payload["verdict"] == "completed"


def test_contract_needs_action_after_retry_exhausted_terminates_blocked_with_reason():
    state = _state("Find the latest stable Node.js release")
    retry_sent = True

    verdict = evaluate_pre_commit(state, content="Node.js 24.15.0 LTS is the latest stable release.")
    payload = _payload(state, verdict)
    if not verdict.committable and "contract:needs_action" in verdict.reasons and retry_sent:
        payload = dict(payload)
        payload["verdict"] = "blocked"

    assert payload["verdict"] == "blocked"
    assert payload["reasons"] == ["contract:needs_action"]
    assert payload["missing_evidence"] == ["current_source_or_blocker"]


def test_load_bearing_ambiguity_blocks_commit_without_contract_retry():
    state = _state("Fix the bug in the code")
    state.strategy = None

    verdict = evaluate_pre_commit(state, content="Changed parser.py. Validation: pytest tests/test_parser.py")
    payload = _payload(state, verdict)

    assert verdict.committable is False
    assert payload["verdict"] == "blocked"
    assert payload["reasons"] == ["ambiguity:target_files_missing"]
    assert "contract:needs_action" not in payload["reasons"]


def test_strategy_clarify_blocks_commit_without_platform_retry():
    state = _state("Fix the bug in the code")

    verdict = evaluate_pre_commit(state, content="Changed parser.py. Validation: pytest tests/test_parser.py")
    payload = _payload(state, verdict)

    assert verdict.committable is False
    assert payload["verdict"] == "blocked"
    assert payload["reasons"] == ["strategy:clarify"]


def test_approval_required_without_decision_blocks_with_missing_approval_decision():
    state = _state(
        "Deploy the production change",
        contract=WorkContract(
            kind="external_side_effect",
            requires_action=True,
            required_evidence=["authority_check", "operation_result_or_blocker"],
        ),
        metadata={"authority_scope": "production-deploy"},
    )

    verdict = evaluate_pre_commit(state, content="Deployed the change.")
    payload = _payload(state, verdict)

    assert verdict.committable is False
    assert payload["verdict"] == "blocked"
    assert payload["reasons"] == ["approval_required:no_approval_decision"]
    assert payload["missing_evidence"] == ["approval_decision"]


def test_recovery_halted_blocks_commit_and_preserves_halt_reason():
    state = _state("Find the latest stable Node.js release")
    state.recovery_state = RecoveryState(status=RecoveryStatus.halted)

    verdict = evaluate_pre_commit(state, content="Node.js 24.15.0 LTS is the latest stable release.")
    payload = _payload(state, verdict)

    assert verdict.committable is False
    assert payload["verdict"] == "blocked"
    assert payload["reasons"] == ["halt:halted"]
    assert state.recovery_state.status == RecoveryStatus.halted


def test_advisory_plan_evidence_surfaces_while_commit_completes():
    state = _state(
        "Fix the failing pytest test in the parser module",
        metadata={
            "target_files": ["parser.py"],
            "validation_target": "pytest tests/test_parser.py",
        },
    )
    state.evidence = _code_change_ledger()

    verdict = evaluate_pre_commit(state, content="Changed parser.py. Validation: pytest tests/test_parser.py")
    payload = _payload(state, verdict)

    assert verdict.committable is True
    assert "plan_advisory:missing:target_files_identified" in payload["reasons"]
    assert payload["reasons"][-1] == "committable"
    assert payload["verdict"] == "completed"


def test_pact_run_projection_verdict_payload_has_reasons_field_available_to_gateway():
    state = _state("Find the latest stable Node.js release")
    state.evidence = _current_info_ledger()

    verdict = evaluate_pre_commit(
        state,
        content=(
            "Node.js 24.15.0 LTS is the latest stable release. "
            "Source: Node.js https://nodejs.org/en/blog/release/v24.15.0. "
            "Checked: April 22, 2026."
        ),
    )
    payload = _payload(state, verdict)

    assert payload["reasons"] == ["committable"]
    assert set(payload) >= {"verdict", "missing_evidence", "reasons"}


def test_audit_hash_inputs_are_stable_for_repeated_reads_of_same_projected_payload():
    state = _state("Find the latest stable Node.js release")
    state.evidence = _current_info_ledger()
    verdict = evaluate_pre_commit(
        state,
        content=(
            "Node.js 24.15.0 LTS is the latest stable release. "
            "Source: Node.js https://nodejs.org/en/blog/release/v24.15.0. "
            "Checked: April 22, 2026."
        ),
    )

    first = _payload(state, verdict)
    second = _payload(state, verdict)

    assert first == second
    assert first["reasons"] == second["reasons"]
