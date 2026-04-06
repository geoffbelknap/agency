"""Interruption controller for real-time comms push.

Loads operator-defined policy from comms-policy.yaml. Evaluates incoming
events against rules and returns the action: interrupt, notify_at_pause,
or queue. Includes circuit breaker, cooldown, and max-interrupt guardrails.

System events (halt, constraint updates) always bypass policy.
Operator owns the policy file; agent cannot modify it (ASK tenet 5).
"""

import json
import logging
import time
from collections import deque
from datetime import datetime, timezone
from pathlib import Path

import yaml
from typing import Optional

try:
    from images.models.subscriptions import CommsPolicy, InterruptionRule
except ImportError:
    # Inline fallback for container environment (flat module layout)
    from pydantic import BaseModel, Field

    class InterruptionRule(BaseModel):
        match: str
        flags: list[str] = Field(default_factory=list)
        action: str = "queue"

    class CommsPolicy(BaseModel):
        rules: list[InterruptionRule] = Field(default_factory=lambda: [
            InterruptionRule(match="direct", flags=["urgent", "blocker"], action="interrupt"),
            InterruptionRule(match="direct", action="notify_at_pause"),
            InterruptionRule(match="interest_match", action="notify_at_pause"),
            InterruptionRule(match="ambient", action="queue"),
        ])
        max_interrupts_per_task: int = 3
        cooldown_seconds: int = 60
        idle_action: str = "queue"
        circuit_breaker_min_action_rate: float = 0.2
        circuit_breaker_window_size: int = 20

logger = logging.getLogger("body.interruption")


class InterruptionController:
    def __init__(self, config_dir: Path):
        self._config_dir = config_dir
        self._log_path = config_dir / "interruption.log"
        self.policy = self._load_policy()
        self._cooldown_seconds = self.policy.cooldown_seconds

        # Per-task state
        self._task_id: Optional[str] = None
        self._interrupt_count = 0
        self._last_interrupt_time = 0.0

        # Circuit breaker state
        self._action_history: deque[bool] = deque(
            maxlen=self.policy.circuit_breaker_window_size
        )
        self.circuit_breaker_active = False

    def _load_policy(self) -> CommsPolicy:
        policy_file = self._config_dir / "comms-policy.yaml"
        if not policy_file.exists():
            return CommsPolicy()
        try:
            raw = yaml.safe_load(policy_file.read_text())
            cfg = raw.get("interruption", {})
            rules = [InterruptionRule(**r) for r in cfg.get("rules", [])]
            cb = cfg.get("circuit_breaker", {})
            return CommsPolicy(
                rules=rules if rules else CommsPolicy().rules,
                max_interrupts_per_task=cfg.get("max_interrupts_per_task", 3),
                cooldown_seconds=cfg.get("cooldown_seconds", 60),
                idle_action=cfg.get("idle_action", "queue"),
                circuit_breaker_min_action_rate=cb.get("min_action_rate", 0.2),
                circuit_breaker_window_size=cb.get("window_size", 20),
            )
        except Exception:
            logger.warning("Failed to load comms-policy.yaml, using defaults")
            return CommsPolicy()

    def start_task(self, task_id: str) -> None:
        self._task_id = task_id
        self._interrupt_count = 0
        self._last_interrupt_time = 0.0

    def end_task(self) -> None:
        self._task_id = None
        self._interrupt_count = 0

    def decide(self, match: str, flags: Optional[dict] = None) -> str:
        flags = flags or {}

        # System events always pass — bypass all throttling
        if match == "system":
            self._audit("system_bypass", match, "interrupt")
            return "interrupt"

        # Check max interrupts
        if self._interrupt_count >= self.policy.max_interrupts_per_task:
            self._audit("max_interrupts_exceeded", match, "notify_at_pause")
            return "notify_at_pause"

        # Check cooldown
        now = time.monotonic()
        if (
            self._last_interrupt_time > 0
            and (now - self._last_interrupt_time) < self._cooldown_seconds
        ):
            # Find the base action, downgrade if it would be interrupt
            base = self._match_rule(match, flags)
            if base == "interrupt":
                self._audit("cooldown_active", match, "notify_at_pause")
                return "notify_at_pause"
            return base

        # Check circuit breaker
        base = self._match_rule(match, flags)
        if base == "interrupt" and self.circuit_breaker_active:
            self._audit("circuit_breaker_active", match, "notify_at_pause")
            return "notify_at_pause"

        self._audit("normal", match, base)
        return base

    def _match_rule(self, match: str, flags: dict) -> str:
        for rule in self.policy.rules:
            if rule.match != match:
                continue
            if rule.flags:
                # Rule requires specific flags to be set
                if all(flags.get(f) for f in rule.flags):
                    return rule.action
            else:
                return rule.action
        return "queue"

    def record_interrupt(self, acted_on: bool = True) -> None:
        self._interrupt_count += 1
        self._last_interrupt_time = time.monotonic()
        self._action_history.append(acted_on)
        self._update_circuit_breaker()

    def _update_circuit_breaker(self) -> None:
        window = self.policy.circuit_breaker_window_size
        if len(self._action_history) < window:
            return
        rate = sum(self._action_history) / len(self._action_history)
        was_active = self.circuit_breaker_active
        self.circuit_breaker_active = rate < self.policy.circuit_breaker_min_action_rate
        if self.circuit_breaker_active != was_active:
            state = "activated" if self.circuit_breaker_active else "deactivated"
            logger.info("Circuit breaker %s (action rate: %.2f)", state, rate)
            self._audit(f"circuit_breaker_{state}", "n/a", f"rate={rate:.2f}")

    def _audit(self, reason: str, match: str, action: str) -> None:
        entry = {
            "timestamp": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
            "task_id": self._task_id,
            "reason": reason,
            "match": match,
            "action": action,
        }
        try:
            with open(self._log_path, "a") as f:
                f.write(json.dumps(entry) + "\n")
        except Exception as exc:
            import sys
            print(f"[AUDIT] write failed ({self._log_path}): {exc}", file=sys.stderr)
