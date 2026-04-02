"""Tests for authority_tools module (halt_agent, recommend_exception)."""

import json
import sys
from pathlib import Path

import pytest

# Make body image modules importable
sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "body"))

from authority_tools import register_authority_tools


class FakeRegistry:
    """Minimal tool registry that stores registered tools."""

    def __init__(self):
        self.tools = {}

    def register_tool(self, name, description, parameters, handler):
        self.tools[name] = {
            "name": name,
            "description": description,
            "parameters": parameters,
            "handler": handler,
        }

    def get_tool_definitions(self):
        return list(self.tools.values())


@pytest.fixture
def setup():
    """Set up registry, signal capture, and register tools."""
    registry = FakeRegistry()
    signals = []

    def capture_signal(signal_type, data):
        signals.append({"signal_type": signal_type, "data": data})

    register_authority_tools(registry, signal_fn=capture_signal, agent_name="auditor")
    return registry, signals


class TestHaltAgent:
    def test_emits_correct_signal(self, setup):
        registry, signals = setup
        handler = registry.tools["halt_agent"]["handler"]
        result = json.loads(handler({
            "target": "analyst",
            "reason": "exceeded budget",
        }))
        assert result["status"] == "halt_request_submitted"
        assert result["target"] == "analyst"
        assert result["halt_type"] == "supervised"
        assert len(signals) == 1
        assert signals[0]["signal_type"] == "halt_request"
        assert signals[0]["data"]["initiator"] == "auditor"
        assert signals[0]["data"]["target"] == "analyst"
        assert signals[0]["data"]["halt_type"] == "supervised"
        assert signals[0]["data"]["reason"] == "exceeded budget"

    def test_custom_halt_type(self, setup):
        registry, signals = setup
        handler = registry.tools["halt_agent"]["handler"]
        result = json.loads(handler({
            "target": "analyst",
            "halt_type": "immediate",
            "reason": "security violation",
        }))
        assert result["halt_type"] == "immediate"
        assert signals[0]["data"]["halt_type"] == "immediate"

    def test_requires_target(self, setup):
        registry, signals = setup
        handler = registry.tools["halt_agent"]["handler"]
        result = json.loads(handler({"reason": "bad behavior"}))
        assert "error" in result
        assert "target" in result["error"]
        assert len(signals) == 0

    def test_requires_reason(self, setup):
        registry, signals = setup
        handler = registry.tools["halt_agent"]["handler"]
        result = json.loads(handler({"target": "analyst"}))
        assert "error" in result
        assert "reason" in result["error"]
        assert len(signals) == 0

    def test_empty_target_rejected(self, setup):
        registry, signals = setup
        handler = registry.tools["halt_agent"]["handler"]
        result = json.loads(handler({"target": "", "reason": "test"}))
        assert "error" in result
        assert len(signals) == 0

    def test_empty_reason_rejected(self, setup):
        registry, signals = setup
        handler = registry.tools["halt_agent"]["handler"]
        result = json.loads(handler({"target": "analyst", "reason": ""}))
        assert "error" in result
        assert len(signals) == 0


class TestRecommendException:
    def test_emits_correct_signal(self, setup):
        registry, signals = setup
        handler = registry.tools["recommend_exception"]["handler"]
        result = json.loads(handler({
            "request_id": "exc-001",
            "action": "approve",
            "reasoning": "Low risk change with clear justification",
        }))
        assert result["status"] == "recommendation_submitted"
        assert result["request_id"] == "exc-001"
        assert result["action"] == "approve"
        assert len(signals) == 1
        assert signals[0]["signal_type"] == "exception_recommendation"
        assert signals[0]["data"]["agent"] == "auditor"
        assert signals[0]["data"]["request_id"] == "exc-001"
        assert signals[0]["data"]["action"] == "approve"
        assert signals[0]["data"]["reasoning"] == "Low risk change with clear justification"

    def test_deny_action(self, setup):
        registry, signals = setup
        handler = registry.tools["recommend_exception"]["handler"]
        result = json.loads(handler({
            "request_id": "exc-002",
            "action": "deny",
            "reasoning": "Violates security policy",
        }))
        assert result["action"] == "deny"
        assert signals[0]["data"]["action"] == "deny"

    def test_invalid_action_rejected(self, setup):
        registry, signals = setup
        handler = registry.tools["recommend_exception"]["handler"]
        result = json.loads(handler({
            "request_id": "exc-001",
            "action": "maybe",
            "reasoning": "Not sure",
        }))
        assert "error" in result
        assert "approve" in result["error"]
        assert len(signals) == 0

    def test_requires_request_id(self, setup):
        registry, signals = setup
        handler = registry.tools["recommend_exception"]["handler"]
        result = json.loads(handler({
            "action": "approve",
            "reasoning": "Looks good",
        }))
        assert "error" in result
        assert "request_id" in result["error"]
        assert len(signals) == 0

    def test_requires_reasoning(self, setup):
        registry, signals = setup
        handler = registry.tools["recommend_exception"]["handler"]
        result = json.loads(handler({
            "request_id": "exc-001",
            "action": "approve",
        }))
        assert "error" in result
        assert "reasoning" in result["error"]
        assert len(signals) == 0

    def test_empty_reasoning_rejected(self, setup):
        registry, signals = setup
        handler = registry.tools["recommend_exception"]["handler"]
        result = json.loads(handler({
            "request_id": "exc-001",
            "action": "deny",
            "reasoning": "",
        }))
        assert "error" in result
        assert len(signals) == 0


class TestRegistration:
    def test_both_tools_registered(self, setup):
        registry, _ = setup
        assert "halt_agent" in registry.tools
        assert "recommend_exception" in registry.tools

    def test_halt_agent_has_required_params(self, setup):
        registry, _ = setup
        params = registry.tools["halt_agent"]["parameters"]
        assert "target" in params["required"]
        assert "reason" in params["required"]

    def test_recommend_exception_has_required_params(self, setup):
        registry, _ = setup
        params = registry.tools["recommend_exception"]["parameters"]
        assert "request_id" in params["required"]
        assert "action" in params["required"]
        assert "reasoning" in params["required"]
