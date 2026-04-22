import json
from datetime import datetime, timedelta, timezone

from pact_engine import (
    EvidenceLedger,
    ExecutionState,
    Retryability,
    SideEffectClass,
    ToolError,
    ToolObservation,
    ToolProvenance,
    ToolStatus,
)


def _state() -> ExecutionState:
    return ExecutionState(task_id="task-123", agent="scout")


def _ledger_bytes(ledger: EvidenceLedger) -> bytes:
    return json.dumps(ledger.to_dict(), separators=(",", ":")).encode("utf-8")


def test_tool_observation_constructs_with_full_field_set():
    started_at = datetime(2026, 4, 22, 12, 0, tzinfo=timezone.utc)
    observed_at = datetime(2026, 4, 22, 12, 1, tzinfo=timezone.utc)

    obs = ToolObservation(
        tool="provider-web-search",
        status=ToolStatus.ok,
        data={"source_urls": ["https://example.com"]},
        provenance=ToolProvenance.provider,
        producer="provider-web-search",
        started_at=started_at,
        observed_at=observed_at,
        error=None,
        retryability=Retryability.not_retryable,
        side_effects=SideEffectClass.read_only,
        evidence_classification=("current_source", "source_url"),
        summary="Observed current source.",
    )

    assert obs.tool == "provider-web-search"
    assert obs.status == ToolStatus.ok
    assert obs.data == {"source_urls": ["https://example.com"]}
    assert obs.provenance == ToolProvenance.provider
    assert obs.producer == "provider-web-search"
    assert obs.started_at == started_at
    assert obs.observed_at == observed_at
    assert obs.error is None
    assert obs.retryability == Retryability.not_retryable
    assert obs.side_effects == SideEffectClass.read_only
    assert obs.evidence_classification == ("current_source", "source_url")
    assert obs.summary == "Observed current source."


def test_tool_observation_to_dict_round_trip_preserves_fields_and_enum_values():
    observed_at = datetime(2026, 4, 22, 12, 1, tzinfo=timezone.utc)
    obs = ToolObservation(
        tool="execute_command",
        status=ToolStatus.error,
        data={"command": "pytest", "metadata": {"exit_code": 1}},
        provenance=ToolProvenance.mediated,
        producer="execute_command",
        started_at=None,
        observed_at=observed_at,
        error=ToolError(message="failed", kind="validation"),
        retryability=Retryability.unknown,
        side_effects=SideEffectClass.read_only,
        evidence_classification=("tool_result", "validation_result"),
        summary="Validation failed.",
    )

    assert obs.to_dict() == {
        "tool": "execute_command",
        "status": "error",
        "data": {"command": "pytest", "metadata": {"exit_code": 1}},
        "provenance": "mediated",
        "producer": "execute_command",
        "started_at": None,
        "observed_at": "2026-04-22T12:01:00Z",
        "error": {"message": "failed", "kind": "validation", "retry_after_ms": None},
        "retryability": "unknown",
        "side_effects": "read_only",
        "evidence_classification": ["tool_result", "validation_result"],
        "summary": "Validation failed.",
    }


def test_tool_observation_defaults_unknown_classifications():
    obs = ToolObservation(tool="opaque-tool", status=ToolStatus.unknown)

    assert obs.provenance == ToolProvenance.unknown
    assert obs.retryability == Retryability.unknown
    assert obs.side_effects == SideEffectClass.unknown


def test_tool_error_carries_kind_message_and_optional_retry_after():
    without_retry = ToolError(message="not found", kind="not_found")
    with_retry = ToolError(message="rate limited", kind="transient", retry_after_ms=500)

    assert without_retry.to_dict() == {
        "message": "not found",
        "kind": "not_found",
        "retry_after_ms": None,
    }
    assert with_retry.to_dict()["retry_after_ms"] == 500


def test_evidence_classification_is_ordered_tuple():
    obs = ToolObservation(
        tool="provider-web-search",
        status=ToolStatus.ok,
        evidence_classification=("source_url", "current_source", "source_url"),
    )

    assert obs.evidence_classification == ("source_url", "current_source", "source_url")
    assert isinstance(obs.evidence_classification, tuple)


def test_record_tool_observation_appends_and_bumps_updated_at():
    state = _state()
    before = state.updated_at
    state.updated_at = before - timedelta(seconds=1)
    obs = ToolObservation(tool="read_file", status=ToolStatus.ok)

    state.record_tool_observation(obs)

    assert state.tool_observations == [obs]
    assert state.updated_at > before - timedelta(seconds=1)


def test_projection_current_source_source_url_matches_legacy_ledger():
    state = _state()
    obs = ToolObservation(
        tool="provider-web-search",
        status=ToolStatus.ok,
        data={
            "source_urls": ["https://example.com/source"],
            "source_url_producer": "provider-web-search",
        },
        provenance=ToolProvenance.provider,
        producer="provider-web-search",
        evidence_classification=("current_source", "source_url"),
    )
    state.record_tool_observation(obs)

    legacy = EvidenceLedger()
    legacy.record_tool_result("provider-web-search", True)
    legacy.observe("current_source")
    legacy.record_source_url("https://example.com/source", producer="provider-web-search")

    assert _ledger_bytes(state.evidence) == _ledger_bytes(legacy)


def test_diff_equivalence_fixture_for_body_call_site_scenarios():
    cases = [
        (
            ToolObservation(
                tool="execute_command",
                status=ToolStatus.ok,
                data={
                    "command": "pytest tests/test_app.py",
                    "validation_ok": True,
                    "validation_producer": "execute_command",
                    "metadata": {"exit_code": 0},
                },
                provenance=ToolProvenance.mediated,
                producer="execute_command",
                retryability=Retryability.unknown,
                side_effects=SideEffectClass.read_only,
                evidence_classification=("tool_result", "validation_result"),
            ),
            lambda ledger: (
                ledger.record_tool_result("execute_command", True),
                ledger.record_validation_result(
                    "pytest tests/test_app.py",
                    True,
                    producer="execute_command",
                    metadata={"exit_code": 0},
                ),
            ),
        ),
        (
            ToolObservation(
                tool="provider-web-search",
                status=ToolStatus.ok,
                data={
                    "source_urls": ["https://example.com/source"],
                    "source_url_producer": "provider",
                },
                provenance=ToolProvenance.provider,
                producer="provider-web-search",
                retryability=Retryability.unknown,
                side_effects=SideEffectClass.unknown,
                evidence_classification=("tool_result", "current_source", "source_url"),
            ),
            lambda ledger: (
                ledger.record_tool_result("provider-web-search", True),
                ledger.observe("current_source"),
                ledger.record_source_url("https://example.com/source", producer="provider"),
            ),
        ),
        (
            ToolObservation(
                tool="runtime:artifact",
                status=ToolStatus.ok,
                data={"path": ".results/task-123.md", "metadata": {"artifact_id": "task-123"}},
                provenance=ToolProvenance.runtime,
                producer="runtime:artifact",
                retryability=Retryability.unknown,
                side_effects=SideEffectClass.local_state,
                evidence_classification=("artifact_path",),
            ),
            lambda ledger: ledger.record_artifact_path(
                ".results/task-123.md",
                metadata={"artifact_id": "task-123"},
            ),
        ),
    ]

    for obs, legacy_recorder in cases:
        state = _state()
        state.record_tool_observation(obs)
        legacy = EvidenceLedger()
        legacy_recorder(legacy)

        assert _ledger_bytes(state.evidence) == _ledger_bytes(legacy)
