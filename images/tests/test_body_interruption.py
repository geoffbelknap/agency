"""Tests for the interruption controller."""

import pytest
from images.body.interruption import InterruptionController
from images.models.subscriptions import CommsPolicy, InterruptionRule


@pytest.fixture
def default_controller(tmp_path):
    return InterruptionController(config_dir=tmp_path)


@pytest.fixture
def custom_controller(tmp_path):
    import yaml
    policy_file = tmp_path / "comms-policy.yaml"
    policy = {
        "interruption": {
            "rules": [
                {"match": "direct", "flags": ["urgent"], "action": "interrupt"},
                {"match": "direct", "action": "notify_at_pause"},
                {"match": "interest_match", "action": "notify_at_pause"},
                {"match": "ambient", "action": "queue"},
            ],
            "max_interrupts_per_task": 2,
            "cooldown_seconds": 30,
        }
    }
    policy_file.write_text(yaml.dump(policy))
    return InterruptionController(config_dir=tmp_path)


class TestPolicyLoading:
    def test_default_policy_when_no_file(self, default_controller):
        assert default_controller.policy.max_interrupts_per_task == 3
        assert default_controller.policy.cooldown_seconds == 60

    def test_custom_policy_loaded(self, custom_controller):
        assert custom_controller.policy.max_interrupts_per_task == 2
        assert custom_controller.policy.cooldown_seconds == 30


class TestActionDecision:
    def test_direct_urgent_interrupts(self, default_controller):
        # Default policy rule requires both urgent and blocker flags
        action = default_controller.decide(
            match="direct", flags={"urgent": True, "blocker": True}
        )
        assert action == "interrupt"

    def test_direct_non_urgent_notifies(self, default_controller):
        action = default_controller.decide(
            match="direct", flags={}
        )
        assert action == "notify_at_pause"

    def test_interest_match_notifies(self, default_controller):
        action = default_controller.decide(
            match="interest_match", flags={}
        )
        assert action == "notify_at_pause"

    def test_ambient_queues(self, default_controller):
        action = default_controller.decide(match="ambient", flags={})
        assert action == "queue"

    def test_system_events_always_pass(self, default_controller):
        action = default_controller.decide(match="system", flags={})
        assert action == "interrupt"  # system events bypass policy


class TestGuardrails:
    def test_max_interrupts_downgrades(self, custom_controller):
        custom_controller.start_task("t1")
        custom_controller._cooldown_seconds = 0  # disable cooldown so max_interrupts is the binding constraint
        # First 2 interrupts go through
        for _ in range(2):
            action = custom_controller.decide(
                match="direct", flags={"urgent": True}
            )
            custom_controller.record_interrupt()
            assert action == "interrupt"

        # Third should downgrade
        action = custom_controller.decide(
            match="direct", flags={"urgent": True}
        )
        assert action == "notify_at_pause"

    def test_cooldown_enforced(self, custom_controller):
        import time
        custom_controller.start_task("t1")
        custom_controller._cooldown_seconds = 0.1  # speed up for test

        action = custom_controller.decide(match="direct", flags={"urgent": True})
        custom_controller.record_interrupt()
        assert action == "interrupt"

        # Immediately after — should be in cooldown
        action = custom_controller.decide(match="direct", flags={"urgent": True})
        assert action == "notify_at_pause"

        # After cooldown
        time.sleep(0.15)
        action = custom_controller.decide(match="direct", flags={"urgent": True})
        assert action == "interrupt"

    def test_task_reset_clears_counters(self, custom_controller):
        custom_controller.start_task("t1")
        for _ in range(2):
            custom_controller.record_interrupt()
        custom_controller.end_task()
        custom_controller.start_task("t2")
        action = custom_controller.decide(
            match="direct", flags={"urgent": True}
        )
        assert action == "interrupt"


class TestCircuitBreaker:
    def test_circuit_breaker_triggers(self, default_controller):
        default_controller.start_task("t1")
        # Simulate 20 interrupts with only 2 acted on (10% < 20% threshold)
        for i in range(20):
            default_controller.record_interrupt(acted_on=(i < 2))

        # Should be tripped
        assert default_controller.circuit_breaker_active is True

        # Should downgrade interrupt to notify_at_pause
        action = default_controller.decide(
            match="direct", flags={"urgent": True}
        )
        assert action == "notify_at_pause"
