from datetime import timedelta

from pact_engine import (
    EvidenceEntry,
    ExecutionState,
    StepRecord,
    ToolObservation,
)


def _task() -> dict:
    return {
        "task_id": "task-123",
        "metadata": {
            "pact_activation": {
                "content": "Find the latest Node.js release",
                "match_type": "direct",
                "source": "idle_direct:dm-scout:operator",
                "channel": "dm-scout",
                "author": "operator",
                "mission_active": False,
            },
            "work_contract": {
                "kind": "current_info",
                "requires_action": True,
                "required_evidence": ["current_source_or_blocker"],
                "answer_requirements": ["source_url"],
                "allowed_terminal_states": ["completed", "blocked"],
                "reason": "time-sensitive request",
                "summary": "Requires fresh evidence.",
            },
        },
    }


def test_execution_state_constructs_from_task_dict():
    state = ExecutionState.from_task(_task(), agent="scout")

    assert state.task_id == "task-123"
    assert state.agent == "scout"
    assert state.activation is not None
    assert state.activation.channel == "dm-scout"
    assert state.contract is not None
    assert state.contract.kind == "current_info"
    assert state.evidence.to_dict()["entries"] == []


def test_record_evidence_updates_ledger_and_updated_at():
    state = ExecutionState.from_task(_task(), agent="scout")
    before = state.updated_at
    state.updated_at = before - timedelta(seconds=1)

    state.record_evidence(EvidenceEntry(kind="observation", producer="runtime", value="current_source"))

    assert state.evidence.observed() == ["current_source"]
    assert state.updated_at > before - timedelta(seconds=1)


def test_record_step_appends_to_history_and_updates_timestamp():
    state = ExecutionState.from_task(_task(), agent="scout")
    before = state.updated_at
    state.updated_at = before - timedelta(seconds=1)

    state.record_step(StepRecord(step_id="step-1", phase="tool_loop", turn=1, summary="Checked source."))

    assert [step.step_id for step in state.step_history] == ["step-1"]
    assert state.updated_at > before - timedelta(seconds=1)


def test_record_observation_appends_to_tool_observations_and_updates_timestamp():
    state = ExecutionState.from_task(_task(), agent="scout")
    before = state.updated_at
    state.updated_at = before - timedelta(seconds=1)

    state.record_observation(ToolObservation(tool="provider-web-search", status="ok", summary="Observed source."))

    assert [obs.tool for obs in state.tool_observations] == ["provider-web-search"]
    assert state.updated_at > before - timedelta(seconds=1)


def test_to_dict_round_trips_ledger_projection():
    state = ExecutionState.from_task(_task(), agent="scout")
    state.record_evidence(EvidenceEntry(kind="source_url", producer="provider", source_url="https://example.com"))

    serialized = state.to_dict()

    assert serialized["evidence"]["source_urls"] == ["https://example.com"]
    assert serialized["evidence"]["entries"] == [
        {
            "kind": "source_url",
            "producer": "provider",
            "source_url": "https://example.com",
        }
    ]


def test_placeholder_fields_default_to_none_or_empty_lists():
    state = ExecutionState.from_task({"task_id": "task-456"}, agent="scout")

    assert state.activation is None
    assert state.objective is None
    assert state.contract is None
    assert state.plan is None
    assert state.step_history == []
    assert state.tool_observations == []
    assert state.partial_outputs == []
    assert state.errors == []
    assert state.recovery_state is None
    assert state.proposed_outcome is None
