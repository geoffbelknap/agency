import json
import pytest
from reflection import (
    build_reflection_criteria,
    build_reflection_prompt,
    parse_reflection_verdict,
    ReflectionState,
    DEFAULT_REFLECTION_CRITERIA,
)

# --- build_reflection_criteria ---

def test_criteria_from_reflection_config():
    mission = {"reflection": {"criteria": ["C1", "C2"]}}
    assert build_reflection_criteria(mission) == ["C1", "C2"]

def test_criteria_from_success_checklist():
    mission = {"success_criteria": {"checklist": [
        {"id": "a", "description": "Check A", "required": True},
        {"id": "b", "description": "Check B", "required": False},
    ]}}
    assert build_reflection_criteria(mission) == ["Check A", "Check B"]

def test_criteria_reflection_takes_priority_over_success():
    mission = {
        "reflection": {"criteria": ["R1"]},
        "success_criteria": {"checklist": [{"id": "a", "description": "S1"}]},
    }
    assert build_reflection_criteria(mission) == ["R1"]

def test_criteria_defaults_when_nothing_set():
    assert build_reflection_criteria({}) == list(DEFAULT_REFLECTION_CRITERIA)
    assert build_reflection_criteria(None) == list(DEFAULT_REFLECTION_CRITERIA)

# --- build_reflection_prompt ---

def test_prompt_contains_summary():
    prompt = build_reflection_prompt("My summary", {})
    assert "My summary" in prompt

def test_prompt_contains_criteria():
    mission = {"reflection": {"criteria": ["Be accurate", "Be complete"]}}
    prompt = build_reflection_prompt("Summary", mission)
    assert "1. Be accurate" in prompt
    assert "2. Be complete" in prompt

def test_prompt_contains_json_template():
    prompt = build_reflection_prompt("Summary", {})
    assert '"verdict"' in prompt
    assert '"criteria_results"' in prompt

# --- parse_reflection_verdict ---

def test_parse_approved():
    resp = json.dumps({"verdict": "APPROVED", "criteria_results": [], "issues": []})
    v = parse_reflection_verdict(resp)
    assert v["verdict"] == "APPROVED"
    assert v["issues"] == []

def test_parse_revision_needed():
    resp = json.dumps({"verdict": "REVISION_NEEDED", "criteria_results": [], "issues": ["Missing X"]})
    v = parse_reflection_verdict(resp)
    assert v["verdict"] == "REVISION_NEEDED"
    assert v["issues"] == ["Missing X"]

def test_parse_with_surrounding_text():
    resp = 'Here is my analysis:\n{"verdict": "APPROVED", "criteria_results": [], "issues": []}\nDone.'
    v = parse_reflection_verdict(resp)
    assert v["verdict"] == "APPROVED"

def test_parse_malformed_json():
    v = parse_reflection_verdict("This looks good to me!")
    assert v["verdict"] == "REVISION_NEEDED"
    assert "unparseable" in v["issues"][0].lower()

def test_parse_wrong_verdict_value():
    resp = json.dumps({"verdict": "MAYBE", "issues": []})
    v = parse_reflection_verdict(resp)
    assert v["verdict"] == "REVISION_NEEDED"

def test_parse_empty_string():
    v = parse_reflection_verdict("")
    assert v["verdict"] == "REVISION_NEEDED"

def test_parse_missing_verdict_key():
    resp = json.dumps({"result": "good", "issues": []})
    v = parse_reflection_verdict(resp)
    assert v["verdict"] == "REVISION_NEEDED"

# --- ReflectionState ---

def test_state_intercept():
    state = ReflectionState(max_rounds=3)
    assert state.intercept_completion("My work") is True
    assert state.pending is True
    assert state.summary == "My work"

def test_state_record_round_not_maxed():
    state = ReflectionState(max_rounds=3)
    state.intercept_completion("x")
    assert state.record_round() is False  # round 1 of 3
    assert state.round == 1
    assert state.pending is False

def test_state_record_round_hits_max():
    state = ReflectionState(max_rounds=2)
    state.intercept_completion("x")
    state.record_round()  # round 1
    state.intercept_completion("x")
    assert state.record_round() is True  # round 2 = max
    assert state.round == 2

def test_state_force_completion():
    state = ReflectionState()
    state.intercept_completion("x")
    state.round = 2
    state.force_completion()
    assert state.forced is True
    assert state.pending is False

def test_state_force_completion_budget():
    state = ReflectionState()
    state.intercept_completion("x")
    state.force_completion(budget_exhausted=True)
    assert state.budget_exhausted is True

def test_signal_data_no_reflection():
    state = ReflectionState()
    data = state.get_signal_data("t1")
    assert data == {"task_id": "t1", "result": ""}
    assert "reflection_rounds" not in data

def test_signal_data_with_reflection():
    state = ReflectionState()
    state.intercept_completion("done")
    state.record_round()
    data = state.get_signal_data("t1")
    assert data["reflection_rounds"] == 1
    assert data["reflection_forced"] is False
    assert data["result"] == "done"

def test_signal_data_forced():
    state = ReflectionState(max_rounds=1)
    state.intercept_completion("done")
    state.record_round()
    state.force_completion()
    data = state.get_signal_data("t1")
    assert data["reflection_forced"] is True

def test_signal_data_budget_exhausted():
    state = ReflectionState()
    state.intercept_completion("done")
    state.round = 1
    state.force_completion(budget_exhausted=True)
    data = state.get_signal_data("t1")
    assert data["reflection_budget_exhausted"] is True

def test_cycle_signal_data():
    state = ReflectionState()
    state.intercept_completion("x")
    state.record_round()
    verdict = {"issues": ["Missing severity"]}
    data = state.get_cycle_signal_data("t1", verdict)
    assert data["round"] == 1
    assert data["verdict"] == "REVISION_NEEDED"
    assert data["issues"] == ["Missing severity"]
