"""Session-scoped conversation scratchpad helpers.

The scratchpad is derived working state for the current channel session. It is
not durable memory and does not write to the knowledge graph.
"""

from __future__ import annotations

import re
from dataclasses import dataclass, field


_ENTITY_RE = re.compile(r"\b[A-Z][A-Z0-9&.-]{1,12}\b")
_CHANNEL_RE = re.compile(r"#[A-Za-z0-9_.-]+")
_MENTION_RE = re.compile(r"@[A-Za-z0-9_.-]+")
_FOLLOW_UP_RE = re.compile(
    r"\b(that|it|this|they|them|those|one|ones|whatever|same|latest|recent|most recent)\b",
    re.IGNORECASE,
)


@dataclass
class SessionScratchpad:
    channel: str
    participant: str
    latest_message: str
    recent_turns: list[dict] = field(default_factory=list)
    active_entities: list[str] = field(default_factory=list)
    likely_intent: str = ""
    follow_up: bool = False
    previous_user_request: str = ""

    def to_prompt_section(self) -> str:
        lines = [
            "[SESSION_SCRATCHPAD]",
            f"scope: channel={self.channel} participant={self.participant}",
            f"latest_message: {self.latest_message}",
        ]
        if self.likely_intent:
            lines.append(f"likely_current_intent: {self.likely_intent}")
        if self.active_entities:
            lines.append("active_entities: " + ", ".join(self.active_entities))
        if self.follow_up:
            lines.append("follow_up_reference_detected: true")
            if self.previous_user_request:
                lines.append(f"most_recent_user_request: {self.previous_user_request}")
            lines.append(
                "resolution_rule: resolve pronouns and elliptical phrases against "
                "the recent transcript before asking for clarification."
            )
        lines.append(
            "durability: this scratchpad is temporary session state; do not treat it "
            "as long-term memory unless a mediated memory-capture step records it."
        )
        lines.append("[/SESSION_SCRATCHPAD]")
        return "\n".join(lines)


def build_session_scratchpad(
    channel: str,
    participant: str,
    latest_message: str,
    recent_messages: list[dict] | None = None,
) -> SessionScratchpad:
    """Derive bounded working state for a channel conversation."""
    recent = _normalize_messages(recent_messages or [])
    active_text = "\n".join([m.get("content", "") for m in recent[-8:]] + [latest_message])
    previous_user_request = _previous_request(recent, participant, latest_message)
    likely_intent = latest_message.strip()
    if _FOLLOW_UP_RE.search(latest_message) and previous_user_request:
        likely_intent = f"{latest_message.strip()} (follow-up to: {previous_user_request})"

    return SessionScratchpad(
        channel=channel,
        participant=participant,
        latest_message=_clip(latest_message, 500),
        recent_turns=recent[-8:],
        active_entities=_extract_entities(active_text),
        likely_intent=_clip(likely_intent, 700),
        follow_up=bool(_FOLLOW_UP_RE.search(latest_message)),
        previous_user_request=_clip(previous_user_request, 500),
    )


def format_recent_transcript(messages: list[dict], limit: int = 8) -> str:
    """Format bounded recent messages for prompt injection."""
    lines = []
    for msg in _normalize_messages(messages)[-limit:]:
        sender = msg.get("author", "unknown")
        content = msg.get("content", "").strip()
        if content:
            lines.append(f"{sender}: {_clip(content, 500)}")
    if not lines:
        return ""
    return "Recent conversation in this channel:\n" + "\n".join(lines)


def _normalize_messages(messages: list[dict]) -> list[dict]:
    normalized = []
    for msg in messages:
        if not isinstance(msg, dict):
            continue
        content = str(msg.get("content", "")).strip()
        if not content:
            continue
        normalized.append({
            "author": str(msg.get("author", "unknown")),
            "content": content,
            "id": str(msg.get("id", "")),
            "timestamp": str(msg.get("timestamp", msg.get("created_at", ""))),
        })
    return normalized


def _previous_request(messages: list[dict], participant: str, latest_message: str) -> str:
    latest = latest_message.strip()
    for msg in reversed(messages):
        author = str(msg.get("author", ""))
        content = str(msg.get("content", "")).strip()
        if not content or content == latest:
            continue
        if author in (participant, "_operator", "operator"):
            return content
    return ""


def _extract_entities(text: str) -> list[str]:
    found = []
    for token in _CHANNEL_RE.findall(text) + _MENTION_RE.findall(text) + _ENTITY_RE.findall(text):
        cleaned = token.strip()
        if cleaned and cleaned not in found:
            found.append(cleaned)
    return found[:12]


def _clip(value: str, limit: int) -> str:
    value = str(value or "").strip()
    if len(value) <= limit:
        return value
    return value[:limit] + "..."
