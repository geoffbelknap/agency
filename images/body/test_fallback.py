import pytest
from fallback import FallbackTracker

# --- match_trigger ---

def test_match_exact_tool():
    policies = [{"trigger": "tool_error", "tool": "jira_get_issue",
                 "strategy": [{"action": "retry", "max_attempts": 2}]}]
    t = FallbackTracker(policies)
    assert t.match_trigger("tool_error", tool="jira_get_issue") is not None
    assert t.match_trigger("tool_error", tool="jira_get_issue")["tool"] == "jira_get_issue"

def test_match_wildcard_tool():
    policies = [{"trigger": "tool_error", "tool": "*",
                 "strategy": [{"action": "retry", "max_attempts": 1}]}]
    t = FallbackTracker(policies)
    assert t.match_trigger("tool_error", tool="anything") is not None

def test_exact_beats_wildcard():
    policies = [
        {"trigger": "tool_error", "tool": "*", "strategy": [{"action": "retry"}]},
        {"trigger": "tool_error", "tool": "jira", "strategy": [{"action": "escalate", "severity": "warning"}]},
    ]
    t = FallbackTracker(policies)
    m = t.match_trigger("tool_error", tool="jira")
    assert m["tool"] == "jira"

def test_match_default_policy():
    default = {"strategy": [{"action": "retry", "max_attempts": 1}]}
    t = FallbackTracker([], default_policy=default)
    m = t.match_trigger("tool_error", tool="unknown")
    assert m is not None
    assert m["strategy"][0]["action"] == "retry"

def test_no_match_returns_none():
    t = FallbackTracker([])
    assert t.match_trigger("tool_error", tool="x") is None

def test_match_capability_unavailable():
    policies = [{"trigger": "capability_unavailable", "capability": "jira",
                 "strategy": [{"action": "degrade"}]}]
    t = FallbackTracker(policies)
    m = t.match_trigger("capability_unavailable", capability="jira")
    assert m is not None

def test_match_budget_warning():
    policies = [{"trigger": "budget_warning", "threshold": 80,
                 "strategy": [{"action": "simplify"}]}]
    t = FallbackTracker(policies)
    m = t.check_budget_warning(85)
    assert m is not None

def test_budget_warning_below_threshold():
    policies = [{"trigger": "budget_warning", "threshold": 80,
                 "strategy": [{"action": "simplify"}]}]
    t = FallbackTracker(policies)
    assert t.check_budget_warning(50) is None

# --- record_outcome ---

def test_record_success_resets_errors():
    t = FallbackTracker([{"trigger": "consecutive_errors", "count": 3,
                          "strategy": [{"action": "pause_and_assess"}]}])
    t.record_outcome("tool_a", False)
    t.record_outcome("tool_a", False)
    t.record_outcome("tool_a", True)  # reset
    assert t.record_outcome("tool_a", False) is None  # only 1 error

def test_record_consecutive_errors_fires():
    t = FallbackTracker([{"trigger": "consecutive_errors", "count": 3,
                          "strategy": [{"action": "pause_and_assess"}]}])
    t.record_outcome("a", False)
    t.record_outcome("b", False)
    result = t.record_outcome("c", False)
    assert result is not None
    assert result["trigger"] == "consecutive_errors"

def test_record_tool_error_fires():
    policies = [{"trigger": "tool_error", "tool": "jira",
                 "strategy": [{"action": "retry"}]}]
    t = FallbackTracker(policies)
    result = t.record_outcome("jira", False)
    assert result is not None
    assert result["trigger"] == "tool_error"

def test_record_success_returns_none():
    t = FallbackTracker([{"trigger": "tool_error", "tool": "*",
                          "strategy": [{"action": "retry"}]}])
    assert t.record_outcome("tool", True) is None

# --- build_fallback_message ---

def test_message_contains_header():
    policy = {"trigger": "tool_error", "strategy": [{"action": "retry", "max_attempts": 2}]}
    msg = FallbackTracker().build_fallback_message(policy, {"tool": "jira"})
    assert "## Fallback Policy Activated: tool_error (jira)" in msg

def test_message_contains_retry_details():
    policy = {"trigger": "tool_error", "strategy": [
        {"action": "retry", "max_attempts": 3, "backoff": "exponential", "delay_seconds": 5}
    ]}
    msg = FallbackTracker().build_fallback_message(policy)
    assert "up to 3 more attempts" in msg
    assert "exponential" in msg

def test_message_contains_alternative_tool():
    policy = {"trigger": "tool_error", "strategy": [
        {"action": "alternative_tool", "tool": "jira_search", "hint": "Search by key"}
    ]}
    msg = FallbackTracker().build_fallback_message(policy)
    assert "jira_search" in msg
    assert "Search by key" in msg

def test_message_contains_escalate():
    policy = {"trigger": "tool_error", "strategy": [
        {"action": "escalate", "severity": "warning", "message": "Tool {tool} failed"}
    ]}
    msg = FallbackTracker().build_fallback_message(policy, {"tool": "jira"})
    assert "WARNING" in msg
    assert "Tool jira failed" in msg

def test_message_follow_chain():
    policy = {"trigger": "tool_error", "strategy": [
        {"action": "retry", "max_attempts": 1},
        {"action": "escalate", "severity": "warning"},
    ]}
    msg = FallbackTracker().build_fallback_message(policy)
    assert "Follow this chain in order" in msg

def test_message_degrade_with_hint():
    policy = {"trigger": "capability_unavailable", "strategy": [
        {"action": "degrade", "hint": "Proceed without Jira data"}
    ]}
    msg = FallbackTracker().build_fallback_message(policy)
    assert "Proceed without Jira data" in msg
