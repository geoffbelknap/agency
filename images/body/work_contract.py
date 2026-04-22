"""Lightweight body-runtime work contracts.

The body uses this as external working discipline around the model: classify
inbound work, attach required evidence, and gate completion when the response
does not satisfy the contract.
"""

from __future__ import annotations

import re
from dataclasses import dataclass, field
from datetime import datetime, timezone


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
URL_RE = re.compile(r"https?://[^\s<>)\],]+", re.IGNORECASE)
CHECKED_DATE_RE = re.compile(
    r"\b(checked|retrieved|accessed|as of|as-of|verified on)\b",
    re.IGNORECASE,
)
VAGUE_SEARCH_RE = re.compile(
    r"\b(based on (?:the |my |these )?search results|the search (?:shows|indicates)|search results indicate)\b",
    re.IGNORECASE,
)
DATE_REQUEST_RE = re.compile(r"\b(date|dated|filed|released|release date|as of|as-of)\b", re.IGNORECASE)
ABSOLUTE_DATE_RE = re.compile(
    r"\b(?:Jan(?:uary)?|Feb(?:ruary)?|Mar(?:ch)?|Apr(?:il)?|May|Jun(?:e)?|"
    r"Jul(?:y)?|Aug(?:ust)?|Sep(?:tember)?|Oct(?:ober)?|Nov(?:ember)?|"
    r"Dec(?:ember)?)\s+\d{1,2},\s+\d{4}\b|\b\d{4}-\d{2}-\d{2}\b",
    re.IGNORECASE,
)
CHECKED_CLAUSE_RE = re.compile(
    r"\b(?:checked|retrieved|accessed|as of|as-of|verified on)\b[^.\n]*(?:\.|$)",
    re.IGNORECASE,
)
TRAILING_URL_PUNCTUATION = ".,;:!?"


@dataclass
class WorkContract:
    kind: str
    requires_action: bool = False
    required_evidence: list[str] = field(default_factory=list)
    answer_requirements: list[str] = field(default_factory=list)
    allowed_terminal_states: list[str] = field(
        default_factory=lambda: ["completed", "blocked", "needs_clarification"]
    )
    reason: str = ""

    def to_dict(self) -> dict:
        return {
            "kind": self.kind,
            "requires_action": self.requires_action,
            "required_evidence": list(self.required_evidence),
            "answer_requirements": list(self.answer_requirements),
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
        answer_requirements = [
            "direct_answer",
            "primary_or_official_source",
            "source_url",
            "checked_date",
            "ambiguous_category_clarified",
        ]
        if DATE_REQUEST_RE.search(text):
            answer_requirements.append("requested_absolute_date")
        return WorkContract(
            kind="current_info",
            requires_action=True,
            required_evidence=["current_source_or_blocker"],
            answer_requirements=answer_requirements,
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
    answer_requirements = ", ".join(contract.answer_requirements)
    answer_requirement_line = (
        f"answer_requirements: {answer_requirements}\n" if answer_requirements else ""
    )
    current_info_rules = ""
    if contract.kind == "current_info":
        current_info_rules = (
            "\n[ANSWER_CONTRACT]\n"
            "current_info_rules:\n"
            "- Give the direct answer first.\n"
            "- Include an official or primary source URL when available.\n"
            "- Make sure each source URL directly supports the claimed version, date, status, or fact.\n"
            "- Include a checked/as-of date for the answer.\n"
            "- If the user asks for a date, include an absolute date in the answer, not only a relative date.\n"
            "- If the user's wording is ambiguous, separate the relevant categories instead of collapsing them.\n"
            "- If only secondary sources are available, mark the answer unverified.\n"
            "- Avoid saying \"search results\" unless you name and link the source.\n"
            "[/ANSWER_CONTRACT]"
        )
    return (
        "\n\n[WORK_CONTRACT]\n"
        f"kind: {contract.kind}\n"
        f"required_evidence: {evidence}\n"
        f"{answer_requirement_line}"
        "allowed_terminal_states: completed, blocked, needs_clarification\n"
        "rules:\n"
        "- Treat this as work, not casual chat.\n"
        "- Use mediated tools or observed context when the task requires action or current facts.\n"
        "- Do not claim evidence you do not have.\n"
        "- If required evidence cannot be obtained, report a specific blocker instead of guessing.\n"
        "[/WORK_CONTRACT]"
        f"{current_info_rules}"
    )


def extract_urls(text: str) -> list[str]:
    urls: list[str] = []
    for match in URL_RE.finditer(str(text or "")):
        url = match.group(0).rstrip(TRAILING_URL_PUNCTUATION)
        if url and url not in urls:
            urls.append(url)
    return urls


def _evidence_source_urls(evidence: dict) -> list[str]:
    urls: list[str] = []
    for value in evidence.get("source_urls") or []:
        if isinstance(value, str):
            for url in extract_urls(value):
                if url not in urls:
                    urls.append(url)
    return urls


def _checked_date(checked_at: str | None = None) -> str:
    if checked_at:
        return checked_at
    now = datetime.now(timezone.utc)
    return f"{now:%B} {now.day}, {now:%Y}"


def _tool_summary(evidence: dict) -> str:
    tools = []
    for item in evidence.get("tool_results") or []:
        if not isinstance(item, dict):
            continue
        tool = str(item.get("tool") or "").strip()
        if tool and tool not in tools:
            tools.append(tool)
    return ", ".join(tools) if tools else "none recorded"


def format_blocked_completion(
    contract: dict | None,
    evidence: dict | None,
    content: str = "",
    checked_at: str | None = None,
) -> str:
    """Return a concise harness-owned blocker response."""
    evidence = evidence or {}
    kind = (contract or {}).get("kind")
    if kind != "current_info":
        return str(content or "I cannot complete this task with the available evidence.")

    source_urls = _evidence_source_urls(evidence)
    if source_urls:
        reason = "Available source URLs did not satisfy the official/current-source evidence contract."
    elif evidence.get("tool_results") or "current_source" in set(evidence.get("observed") or []):
        reason = "Current-information tool evidence was insufficient to verify the requested fact."
    else:
        reason = "No current-information source or tool result was available."

    lines = [
        "I cannot verify this from an official/current source without guessing.",
        "",
        f"Blocked: {reason}",
        f"Evidence checked: tools={_tool_summary(evidence)}",
    ]
    if source_urls:
        lines.append("Source URLs observed: " + ", ".join(source_urls[:5]))
    lines.extend([
        "What would unblock this: an official or primary source URL that directly supports the requested current fact.",
        f"Checked: {_checked_date(checked_at)}.",
    ])
    return "\n".join(lines)


def _validate_current_info_answer(contract: dict, evidence: dict, content: str) -> dict:
    requirements = set(contract.get("answer_requirements") or [])
    missing: list[str] = []
    answer_without_checked_clause = CHECKED_CLAUSE_RE.sub("", content)
    answer_urls = extract_urls(content)
    evidence_urls = _evidence_source_urls(evidence)

    if "source_url" in requirements and not answer_urls:
        missing.append("source_url")
    elif evidence_urls and not any(url in evidence_urls for url in answer_urls):
        missing.append("source_url_from_evidence")
    if "checked_date" in requirements and not CHECKED_DATE_RE.search(content):
        missing.append("checked_date")
    if "requested_absolute_date" in requirements and not ABSOLUTE_DATE_RE.search(answer_without_checked_clause):
        missing.append("requested_absolute_date")
    if VAGUE_SEARCH_RE.search(content):
        missing.append("named_source")

    if not missing:
        return {"verdict": "completed"}

    return {
        "verdict": "needs_action",
        "missing_evidence": missing,
        "message": (
            "The answer has current-source evidence but does not satisfy the "
            "answer contract. Provide direct source links, a checked/as-of date, "
            "and name sources instead of referring vaguely to search results."
        ),
    }


def validate_completion(contract: dict | None, evidence: dict | None, content: str) -> dict:
    if not contract or not contract.get("requires_action"):
        return {"verdict": "completed"}

    evidence = evidence or {}
    content = str(content or "")
    if BLOCKER_RE.search(content):
        return {"verdict": "blocked", "message": format_blocked_completion(contract, evidence, content)}

    required = set(contract.get("required_evidence") or [])
    tool_results = evidence.get("tool_results") or []
    observed = set(evidence.get("observed") or [])

    if "current_source_or_blocker" in required:
        if tool_results or "current_source" in observed:
            return _validate_current_info_answer(contract, evidence, content)
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
