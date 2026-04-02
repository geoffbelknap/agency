import json
import pytest
from post_task import (
    build_capture_prompt,
    parse_capture_response,
    enrich_procedure,
    enrich_episode,
)

SAMPLE_METADATA = {
    "mission_name": "ticket-triage",
    "task_id": "task-abc123",
    "tools_used": ["jira_get_issue", "channel_send"],
    "duration_minutes": 5,
    "outcome": "success",
    "agent": "henrybot900",
    "mission_id": "8a3f-...",
    "timestamp": "2026-03-27T10:00:00Z",
}

# --- build_capture_prompt ---

def test_prompt_both_enabled():
    prompt = build_capture_prompt(SAMPLE_METADATA, procedural_enabled=True, episodic_enabled=True)
    assert "Procedure Extract" in prompt
    assert "Episode Extract" in prompt
    assert "ticket-triage" in prompt
    assert "task-abc123" in prompt
    assert "jira_get_issue" in prompt

def test_prompt_procedural_only():
    prompt = build_capture_prompt(SAMPLE_METADATA, procedural_enabled=True, episodic_enabled=False)
    assert "Procedure Extract" in prompt
    assert "Episode Extract" not in prompt

def test_prompt_episodic_only():
    prompt = build_capture_prompt(SAMPLE_METADATA, procedural_enabled=False, episodic_enabled=True)
    assert "Procedure Extract" not in prompt
    assert "Episode Extract" in prompt

def test_prompt_neither_enabled():
    prompt = build_capture_prompt(SAMPLE_METADATA, procedural_enabled=False, episodic_enabled=False)
    assert prompt == ""

def test_prompt_empty_metadata():
    prompt = build_capture_prompt({})
    assert "Mission:" in prompt
    assert "Task ID:" in prompt

def test_prompt_includes_duration_and_outcome():
    prompt = build_capture_prompt(SAMPLE_METADATA)
    assert "5 minutes" in prompt
    assert "success" in prompt

# --- parse_capture_response ---

def test_parse_both_sections():
    resp = json.dumps({
        "procedure": {"approach": "1. Read ticket", "tools_used": ["jira"], "outcome": "success", "lessons": []},
        "episode": {"summary": "Triaged INC-123", "notable_events": [], "entities_mentioned": [], "operational_tone": "routine", "tags": ["triage"]},
    })
    result = parse_capture_response(resp)
    assert "procedure" in result
    assert "episode" in result
    assert result["procedure"]["outcome"] == "success"
    assert result["episode"]["operational_tone"] == "routine"

def test_parse_procedure_only():
    resp = json.dumps({"procedure": {"approach": "steps", "outcome": "success"}})
    result = parse_capture_response(resp)
    assert "procedure" in result
    assert "episode" not in result

def test_parse_episode_only():
    resp = json.dumps({"episode": {"summary": "what happened", "tags": []}})
    result = parse_capture_response(resp)
    assert "episode" in result
    assert "procedure" not in result

def test_parse_with_surrounding_text():
    resp = 'Here is the extraction:\n' + json.dumps({"procedure": {"approach": "steps"}}) + '\nDone.'
    result = parse_capture_response(resp)
    assert result is not None
    assert "procedure" in result

def test_parse_malformed():
    assert parse_capture_response("not json at all") is None

def test_parse_empty():
    assert parse_capture_response("") is None
    assert parse_capture_response(None) is None

def test_parse_nested_braces():
    resp = json.dumps({"procedure": {"approach": "used {template} syntax", "outcome": "success"}})
    result = parse_capture_response(resp)
    assert result is not None

def test_parse_no_recognized_keys():
    resp = json.dumps({"something_else": "value"})
    assert parse_capture_response(resp) is None

# --- enrich_procedure ---

def test_enrich_procedure():
    proc = {"approach": "steps", "tools_used": ["jira"], "outcome": "success"}
    enriched = enrich_procedure(proc, SAMPLE_METADATA)
    assert enriched["agent"] == "henrybot900"
    assert enriched["mission_id"] == "8a3f-..."
    assert enriched["task_id"] == "task-abc123"
    assert enriched["duration_minutes"] == 5
    assert enriched["reflection_notes"] == ""
    assert enriched["lessons"] == []

def test_enrich_procedure_preserves_existing():
    proc = {"lessons": ["learned something"], "reflection_notes": "good"}
    enriched = enrich_procedure(proc, SAMPLE_METADATA)
    assert enriched["lessons"] == ["learned something"]
    assert enriched["reflection_notes"] == "good"

# --- enrich_episode ---

def test_enrich_episode():
    ep = {"summary": "what happened", "tags": ["incident"]}
    enriched = enrich_episode(ep, SAMPLE_METADATA)
    assert enriched["agent"] == "henrybot900"
    assert enriched["outcome"] == "success"
    assert enriched["operational_tone"] == "routine"
    assert enriched["notable_events"] == []
    assert enriched["entities_mentioned"] == []
    assert enriched["tags"] == ["incident"]

def test_enrich_episode_preserves_existing():
    ep = {"operational_tone": "problematic", "notable_events": ["something weird"]}
    enriched = enrich_episode(ep, SAMPLE_METADATA)
    assert enriched["operational_tone"] == "problematic"
    assert enriched["notable_events"] == ["something weird"]
