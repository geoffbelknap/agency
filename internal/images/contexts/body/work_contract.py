"""Lightweight body-runtime work contracts.

The body uses this as external working discipline around the model: classify
inbound work, attach required evidence, and gate completion when the response
does not satisfy the contract.
"""

from __future__ import annotations

import re
from dataclasses import dataclass, field


CURRENT_INFO_RE = re.compile(
    r"\b(latest|current|recent|most recent|today|yesterday|tomorrow|now|live|"
    r"price|weather|schedule|score|news|filing|sec filing|look up|lookup|find me|search)\b",
    re.IGNORECASE,
)
ACTION_RE = re.compile(
    r"\b(find|look up|lookup|search|check|verify|debug|fix|create|write|read|"
    r"restart|start|stop|run|test|build|deploy|summarize|analyze)\b",
    re.IGNORECASE,
)
BLOCKER_RE = re.compile(
    r"\b(can't|cannot|unable|not able|blocked|failed|unavailable|no access|"
    r"do not have|don't have|missing|need .+ access|need .+ tool)\b",
    re.IGNORECASE,
)


@dataclass
class WorkContract:
    kind: str
    requires_action: bool = False
    required_evidence: list[str] = field(default_factory=list)
    allowed_terminal_states: list[str] = field(
        default_factory=lambda: ["completed", "blocked", "needs_clarification"]
    )
    reason: str = ""

    def to_dict(self) -> dict:
        return {
            "kind": self.kind,
            "requires_action": self.requires_action,
            "required_evidence": list(self.required_evidence),
            "allowed_terminal_states": list(self.allowed_terminal_states),
            "reason": self.reason,
        }


def classify_work(content: str, match_type: str = "direct", mission_active: bool = False) -> WorkContract:
    text = str(content or "").strip()
    if mission_active:
        return WorkContract(
            kind="mission_task",
            requires_action=True,
            required_evidence=["mission_result_or_blocker"],
            reason="active mission direct message",
        )
    if CURRENT_INFO_RE.search(text):
        return WorkContract(
            kind="current_info",
            requires_action=True,
            required_evidence=["current_source_or_blocker"],
            reason="time-sensitive or externally verifiable request",
        )
    if ACTION_RE.search(text):
        return WorkContract(
            kind="task",
            requires_action=True,
            required_evidence=["action_result_or_blocker"],
            reason="action verb detected",
        )
    if match_type != "direct":
        return WorkContract(kind="coordination", requires_action=False, reason="non-direct channel signal")
    return WorkContract(kind="chat", requires_action=False, reason="no action requirement detected")


def contract_prompt(contract: WorkContract) -> str:
    if not contract.requires_action:
        return ""
    evidence = ", ".join(contract.required_evidence) or "task evidence"
    return (
        "\n\n[WORK_CONTRACT]\n"
        f"kind: {contract.kind}\n"
        f"required_evidence: {evidence}\n"
        "allowed_terminal_states: completed, blocked, needs_clarification\n"
        "rules:\n"
        "- Treat this as work, not casual chat.\n"
        "- Use mediated tools or observed context when the task requires action or current facts.\n"
        "- Do not claim evidence you do not have.\n"
        "- If required evidence cannot be obtained, report a specific blocker instead of guessing.\n"
        "[/WORK_CONTRACT]"
    )


def validate_completion(contract: dict | None, evidence: dict | None, content: str) -> dict:
    if not contract or not contract.get("requires_action"):
        return {"verdict": "completed"}

    evidence = evidence or {}
    content = str(content or "")
    if BLOCKER_RE.search(content):
        return {"verdict": "blocked"}

    required = set(contract.get("required_evidence") or [])
    tool_results = evidence.get("tool_results") or []
    observed = set(evidence.get("observed") or [])

    if "current_source_or_blocker" in required:
        if tool_results or "current_source" in observed:
            return {"verdict": "completed"}
        return {
            "verdict": "needs_action",
            "missing_evidence": ["current_source_or_blocker"],
            "message": (
                "This work requires current external evidence. Use an available "
                "current-info/search/fetch tool, or report the specific blocker."
            ),
        }

    if "action_result_or_blocker" in required and not (tool_results or observed):
        return {
            "verdict": "needs_action",
            "missing_evidence": ["action_result_or_blocker"],
            "message": "This work requires an observed action result or a specific blocker.",
        }

    return {"verdict": "completed"}
