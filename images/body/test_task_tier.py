import pytest
from task_tier import classify_task_tier, expand_cost_mode, get_active_features, resolve_feature_config


# classify_task_tier tests
def test_no_mission_is_minimal():
    assert classify_task_tier({"content": "hi"}, None) == "minimal"


def test_dm_short_message_is_minimal():
    assert classify_task_tier({"source": "dm", "content": "hi"}, {"name": "triage"}) == "minimal"


def test_dm_long_message_is_standard():
    task = {"source": "dm", "content": "Please investigate the production database latency issue from this morning that affected multiple services"}
    assert classify_task_tier(task, {"name": "triage"}) == "standard"


def test_connector_trigger_is_standard():
    assert classify_task_tier({"source": "connector", "content": "New ticket"}, {"name": "t"}) == "standard"


def test_schedule_trigger_is_standard():
    assert classify_task_tier({"source": "schedule", "content": "daily scan"}, {"name": "t"}) == "standard"


def test_webhook_trigger_is_standard():
    assert classify_task_tier({"source": "webhook", "content": "deploy hook"}, {"name": "t"}) == "standard"


def test_cost_mode_frugal_forces_minimal():
    task = {"source": "connector", "content": "New ticket INC-1234"}
    assert classify_task_tier(task, {"name": "t", "cost_mode": "frugal"}) == "minimal"


def test_cost_mode_thorough_forces_full():
    task = {"source": "dm", "content": "hi"}
    assert classify_task_tier(task, {"name": "t", "cost_mode": "thorough"}) == "full"


def test_min_task_tier_raises_floor():
    task = {"source": "dm", "content": "hi"}
    assert classify_task_tier(task, {"name": "t", "min_task_tier": "standard"}) == "standard"


def test_min_task_tier_doesnt_lower():
    task = {"source": "connector", "content": "ticket"}
    assert classify_task_tier(task, {"name": "t", "min_task_tier": "minimal"}) == "standard"


def test_mention_short_is_minimal():
    assert classify_task_tier({"source": "mention", "content": "thanks"}, {"name": "t"}) == "minimal"


def test_unknown_source_is_standard():
    assert classify_task_tier({"source": "unknown", "content": "x"}, {"name": "t"}) == "standard"


def test_empty_content_is_minimal():
    assert classify_task_tier({"source": "dm", "content": ""}, {"name": "t"}) == "minimal"


# expand_cost_mode tests
def test_expand_balanced():
    d = expand_cost_mode("balanced")
    assert d["reflection"]["enabled"] is False
    assert d["procedural_memory"]["capture"] is True
    assert d["procedural_memory"]["max_retrieved"] == 3


def test_expand_thorough():
    d = expand_cost_mode("thorough")
    assert d["reflection"]["enabled"] is True
    assert d["reflection"]["max_rounds"] == 2
    assert d["procedural_memory"]["include_failures"] is True


def test_expand_frugal():
    d = expand_cost_mode("frugal")
    assert d["procedural_memory"]["capture"] is False
    assert d["episodic_memory"]["capture"] is False


def test_expand_unknown_falls_back_to_balanced():
    d = expand_cost_mode("unknown")
    assert d == expand_cost_mode("balanced")


# get_active_features tests
def test_minimal_only_trajectory():
    f = get_active_features("minimal")
    assert f["trajectory"] is True
    assert f["fallback"] is False
    assert f["reflection"] is False
    assert f["procedural_capture"] is False


def test_standard_has_capture():
    f = get_active_features("standard")
    assert f["trajectory"] is True
    assert f["fallback"] is True
    assert f["procedural_capture"] is True
    assert f["procedural_inject"] is False
    assert f["reflection"] is False


def test_full_has_everything():
    f = get_active_features("full")
    assert all(v is True for k, v in f.items() if k != "prompt_tier")
    assert f["prompt_tier"] == "full"


def test_unknown_tier_falls_back():
    assert get_active_features("bogus") == get_active_features("standard")


# resolve_feature_config tests
def test_resolve_explicit_overrides_default():
    defaults = {"procedural_memory": {"capture": True, "max_retrieved": 3}}
    mission = {"procedural_memory": {"max_retrieved": 10}}
    result = resolve_feature_config(mission, "procedural_memory", defaults)
    assert result["capture"] is True  # from default
    assert result["max_retrieved"] == 10  # explicit override


def test_resolve_no_explicit_uses_defaults():
    defaults = {"reflection": {"enabled": False}}
    result = resolve_feature_config({}, "reflection", defaults)
    assert result["enabled"] is False


def test_resolve_none_values_ignored():
    defaults = {"episodic_memory": {"capture": True, "retrieve": True}}
    mission = {"episodic_memory": {"capture": None}}
    result = resolve_feature_config(mission, "episodic_memory", defaults)
    assert result["capture"] is True  # None doesn't override
