from datetime import datetime, timezone

from pact_engine import (
    ExecutionError,
    ExecutionState,
    NextAction,
    RecoveryState,
    RecoveryStatus,
    Retryability,
    ToolError,
    ToolObservation,
    ToolStatus,
)


NOW = datetime(2026, 4, 22, 12, 0, tzinfo=timezone.utc)
OBSERVED = datetime(2026, 4, 22, 11, 59, tzinfo=timezone.utc)


def _error_obs(
    *,
    retryability: Retryability,
    tool: str = "execute_command",
    error: ToolError | None = None,
    summary: str = "Tool failed.",
) -> ToolObservation:
    return ToolObservation(
        tool=tool,
        status=ToolStatus.error,
        observed_at=OBSERVED,
        error=error,
        retryability=retryability,
        summary=summary,
    )


def test_default_recovery_state_is_idle_with_no_next_action():
    state = RecoveryState()

    assert state.status == RecoveryStatus.idle
    assert state.next_action == NextAction.none
    assert state.attempt == 0
    assert state.max_attempts == 3


def test_retry_safe_tool_failure_retries_when_budget_available():
    state = RecoveryState()

    state.record_tool_failure(_error_obs(retryability=Retryability.retry_safe), now=NOW)

    assert state.status == RecoveryStatus.retrying
    assert state.next_action == NextAction.retry
    assert state.attempt == 1


def test_retry_with_backoff_tool_failure_retries_when_budget_available():
    state = RecoveryState()

    state.record_tool_failure(
        _error_obs(
            retryability=Retryability.retry_with_backoff,
            error=ToolError(message="rate limited", kind="transient", retry_after_ms=500),
        ),
        now=NOW,
    )

    assert state.status == RecoveryStatus.retrying
    assert state.next_action == NextAction.retry


def test_not_retryable_tool_failure_fails_regardless_of_budget():
    state = RecoveryState(max_attempts=10)

    state.record_tool_failure(_error_obs(retryability=Retryability.not_retryable), now=NOW)

    assert state.status == RecoveryStatus.failed
    assert state.next_action == NextAction.fail
    assert state.attempt == 1


def test_tool_failure_with_exhausted_budget_fails_or_escalates_unknown():
    retry_safe = RecoveryState(attempt=2, max_attempts=3)
    unknown = RecoveryState(attempt=2, max_attempts=3)

    retry_safe.record_tool_failure(_error_obs(retryability=Retryability.retry_safe), now=NOW)
    unknown.record_tool_failure(_error_obs(retryability=Retryability.unknown), now=NOW)

    assert retry_safe.status == RecoveryStatus.failed
    assert retry_safe.next_action == NextAction.fail
    assert unknown.status == RecoveryStatus.escalated
    assert unknown.next_action == NextAction.escalate


def test_unknown_tool_failure_uses_fallback_when_budget_available():
    state = RecoveryState()

    state.record_tool_failure(_error_obs(retryability=Retryability.unknown), now=NOW)

    assert state.status == RecoveryStatus.fallback
    assert state.next_action == NextAction.fallback


def test_non_error_tool_observation_is_noop():
    state = RecoveryState(updated_at=NOW)
    obs = ToolObservation(tool="read_file", status=ToolStatus.ok, retryability=Retryability.retry_safe)

    state.record_tool_failure(obs, now=datetime(2026, 4, 22, 12, 1, tzinfo=timezone.utc))

    assert state.to_dict() == RecoveryState(updated_at=NOW).to_dict()


def test_evidence_gap_blocks_and_names_missing_evidence():
    state = RecoveryState()

    state.record_evidence_gap(["source_url", "current_source"], now=NOW)

    assert state.status == RecoveryStatus.blocked
    assert state.next_action == NextAction.block
    assert state.reason == "evidence_gap:current_source,source_url"


def test_load_bearing_ambiguity_clarifies():
    state = RecoveryState()

    state.record_load_bearing_ambiguity("release category missing", now=NOW)

    assert state.status == RecoveryStatus.clarifying
    assert state.next_action == NextAction.clarify
    assert state.reason == "ambiguity:release category missing"


def test_operator_halt_halts():
    state = RecoveryState()

    state.record_operator_halt("operator requested stop", now=NOW)

    assert state.status == RecoveryStatus.halted
    assert state.next_action == NextAction.halt
    assert state.reason == "halt:operator requested stop"


def test_success_resets_attempt_and_last_error_to_idle():
    state = RecoveryState(
        status=RecoveryStatus.failed,
        reason="tool_failure:execute_command:not_retryable",
        attempt=2,
        last_error=ExecutionError(message="failed", phase="execute_command", observed_at=OBSERVED),
        next_action=NextAction.fail,
    )

    state.record_success(now=NOW)

    assert state.status == RecoveryStatus.idle
    assert state.next_action == NextAction.none
    assert state.reason == ""
    assert state.attempt == 0
    assert state.last_error is None


def test_expiration_and_superseded_are_terminal_with_no_next_action():
    expired = RecoveryState()
    superseded = RecoveryState()

    expired.record_expiration("timeout", now=NOW)
    superseded.record_superseded("new task", now=NOW)

    assert expired.status == RecoveryStatus.expired
    assert expired.next_action == NextAction.none
    assert expired.reason == "expired:timeout"
    assert superseded.status == RecoveryStatus.superseded
    assert superseded.next_action == NextAction.none
    assert superseded.reason == "superseded:new task"


def test_to_dict_serializes_every_field_with_last_error_and_enum_values():
    state = RecoveryState(max_attempts=5)
    state.record_tool_failure(
        _error_obs(
            retryability=Retryability.not_retryable,
            error=ToolError(message="permission denied", kind="permission_denied"),
        ),
        now=NOW,
    )

    assert state.to_dict() == {
        "status": "failed",
        "reason": "tool_failure:execute_command:not_retryable",
        "attempt": 1,
        "max_attempts": 5,
        "last_error": {
            "message": "permission denied",
            "phase": "execute_command",
            "observed_at": "2026-04-22T11:59:00Z",
        },
        "next_action": "fail",
        "updated_at": "2026-04-22T12:00:00Z",
    }


def test_transition_methods_are_deterministic_with_same_inputs():
    left = RecoveryState()
    right = RecoveryState()
    obs = _error_obs(
        retryability=Retryability.retry_safe,
        error=ToolError(message="timeout", kind="timeout"),
    )

    left.record_tool_failure(obs, now=NOW)
    right.record_tool_failure(obs, now=NOW)

    assert left.to_dict() == right.to_dict()


def test_execution_state_note_tool_failure_populates_recovery_state():
    state = ExecutionState(task_id="task-123", agent="scout")

    state.note_tool_failure(_error_obs(retryability=Retryability.retry_safe), now=NOW)

    assert state.recovery_state is not None
    assert state.recovery_state.status == RecoveryStatus.retrying
    assert state.recovery_state.next_action == NextAction.retry
    assert state.to_dict()["recovery_state"]["status"] == "retrying"
