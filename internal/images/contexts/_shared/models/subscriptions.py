"""Models for real-time comms subscriptions, matching, and interruption policy.

Supports WebSocket push events, interest declarations with keyword/knowledge
filtering, and operator-defined interruption policies.
"""

from enum import Enum
from typing import Any

from pydantic import BaseModel, Field, field_validator


class MatchClassification(str, Enum):
    DIRECT = "direct"
    INTEREST_MATCH = "interest_match"
    AMBIENT = "ambient"


class ExpertiseTier(str, Enum):
    BASE = "base"
    STANDING = "standing"
    LEARNED = "learned"
    TASK = "task"


class ExpertiseDeclaration(BaseModel):
    """A single tier of an agent's expertise profile.

    Four tiers:
    - base: operator-defined in agent.yaml (read-only to agent, tenet 5)
    - standing: operator-assigned via task delivery ("help anyone who asks about X")
    - learned: accumulated from past work (agent-managed)
    - task: current task focus (temporary, cleared on task end)
    """
    tier: ExpertiseTier
    description: str = ""
    keywords: list[str] = Field(default_factory=list)
    persistent: bool = False

    @field_validator("keywords")
    @classmethod
    def validate_keywords(cls, v: list[str]) -> list[str]:
        if len(v) > 30:
            raise ValueError("Expertise declarations support at most 30 keywords")
        return [kw for kw in v if len(kw) >= 3]


# Keep InterestDeclaration as alias for backward compatibility during migration
class InterestDeclaration(BaseModel):
    task_id: str
    description: str = ""
    keywords: list[str] = Field(default_factory=list)
    knowledge_filter: dict[str, list[str]] = Field(default_factory=dict)

    @field_validator("keywords")
    @classmethod
    def validate_keywords(cls, v: list[str]) -> list[str]:
        if len(v) > 20:
            raise ValueError("Interest declarations support at most 20 keywords")
        return [kw for kw in v if len(kw) >= 3]

    @field_validator("knowledge_filter")
    @classmethod
    def validate_knowledge_filter(cls, v: dict[str, list[str]]) -> dict[str, list[str]]:
        total = sum(len(entries) for entries in v.values())
        if total > 10:
            raise ValueError(
                "Knowledge filter supports at most 10 entries (kinds + topics combined)"
            )
        return v


class WSEvent(BaseModel):
    v: int = 1
    type: str
    channel: str | None = None
    match: str | None = None
    matched_keywords: list[str] | None = None
    message: dict[str, Any] | None = None
    task: dict[str, Any] | None = None
    event: str | None = None
    data: dict[str, Any] | None = None


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
