"""Deterministic PACT objective builder for body-runtime execution state."""

from __future__ import annotations

import re
from typing import Any

try:
    from .pact_engine import ActivationContext, Objective, WorkContract
except ImportError:  # pragma: no cover - runtime imports this as a top-level module.
    from pact_engine import ActivationContext, Objective, WorkContract


_FILE_PATH_RE = re.compile(
    r"(?:(?:^|\s)(?:\.{1,2}/|/)?[A-Za-z0-9_.-]+/)?[A-Za-z0-9_.-]+\.[A-Za-z0-9_.-]+"
)
_TEMPORAL_ANCHOR_RE = re.compile(
    r"\bas[- ]of\b|"
    r"\b\d{4}\b|"
    r"\b\d{4}-\d{1,2}-\d{1,2}\b|"
    r"\b\d{1,2}/\d{1,2}/\d{2,4}\b|"
    r"\b(?:jan(?:uary)?|feb(?:ruary)?|mar(?:ch)?|apr(?:il)?|may|jun(?:e)?|"
    r"jul(?:y)?|aug(?:ust)?|sep(?:tember)?|oct(?:ober)?|nov(?:ember)?|"
    r"dec(?:ember)?)\s+\d{1,2}(?:,\s*\d{4})?\b",
    re.IGNORECASE,
)
_LATEST_CURRENT_RE = re.compile(r"\b(?:latest|current)\b", re.IGNORECASE)
_RELEASE_QUALIFIER_RE = re.compile(r"\b(?:lts|stable|beta)\b", re.IGNORECASE)
_TESTS_RE = re.compile(r"\b(?:test|tests|testing|pytest|go test|npm test)\b", re.IGNORECASE)
_VALIDATION_TARGET_RE = re.compile(
    r"\b(?:pytest|go test|npm test|cargo test|make test|validation target|build target)\b",
    re.IGNORECASE,
)
_OUTPUT_FORMAT_RE = re.compile(
    r"\b(?:markdown|md|csv|json|pdf|txt|text|html|yaml|yml|xml|docx)\b",
    re.IGNORECASE,
)

CREATIVE_PATTERNS = (
    ("tell me a joke", re.compile(r"\btell me (a|an) joke\b", re.IGNORECASE)),
    ("write a poem", re.compile(r"\bwrite (a|an|me a|me an) poem\b", re.IGNORECASE)),
    ("write a haiku", re.compile(r"\bwrite (a|an|me a|me an) haiku\b", re.IGNORECASE)),
    ("write a story", re.compile(r"\bwrite (a|an|me a|me an) story\b", re.IGNORECASE)),
    ("write a song", re.compile(r"\bwrite (a|an|me a|me an) song\b", re.IGNORECASE)),
    ("brainstorm", re.compile(r"\bbrainstorm (with me|ideas|on|about)\b", re.IGNORECASE)),
    ("let's brainstorm", re.compile(r"\blet'?s brainstorm\b", re.IGNORECASE)),
    ("roleplay", re.compile(r"\broleplay\b", re.IGNORECASE)),
    ("role-play", re.compile(r"\brole[- ]play\b", re.IGNORECASE)),
    ("pretend", re.compile(r"\bpretend (to be|you('?re| are))\b", re.IGNORECASE)),
    ("make up", re.compile(r"\bmake up (a|an|some)\b", re.IGNORECASE)),
)
PERSONA_PATTERNS = (
    ("what's your name", re.compile(r"\bwhat('?s| is) your name\b", re.IGNORECASE)),
    ("who are you", re.compile(r"\bwho are you\b", re.IGNORECASE)),
    ("tell me about yourself", re.compile(r"\btell me about yourself\b", re.IGNORECASE)),
    ("what do you do", re.compile(r"\bwhat do you do\b", re.IGNORECASE)),
    (
        "what are your preferences",
        re.compile(r"\bwhat are your (preferences|capabilities|tools|limits|restrictions)\b", re.IGNORECASE),
    ),
    ("what's your role", re.compile(r"\bwhat('?s| is) your (role|job|purpose)\b", re.IGNORECASE)),
    ("what's your favorite", re.compile(r"\bwhat('?s| is) your favorite\b", re.IGNORECASE)),
)
SOCIAL_PATTERNS = (
    ("greeting", re.compile(r"(hi|hello|hey|yo)[!.?]*", re.IGNORECASE)),
    ("good time", re.compile(r"good (morning|afternoon|evening|night)[!.?]*", re.IGNORECASE)),
    ("how are you", re.compile(r"how are you( (doing|today))?[?!.]*", re.IGNORECASE)),
    ("how's it going", re.compile(r"how('?s| is) it going[?!.]*", re.IGNORECASE)),
    ("what's up", re.compile(r"what('?s| is) up[?!.]*", re.IGNORECASE)),
    ("thanks", re.compile(r"thanks( so much)?[!.]*", re.IGNORECASE)),
    ("thank you", re.compile(r"thank you( (so much|very much))?[!.]*", re.IGNORECASE)),
    ("goodbye", re.compile(r"goodbye[!.]*", re.IGNORECASE)),
    ("bye", re.compile(r"bye[!.]*", re.IGNORECASE)),
    ("see you", re.compile(r"see (you|ya)[!.]*", re.IGNORECASE)),
    ("ok", re.compile(r"ok[ay]*[!.]*", re.IGNORECASE)),
    ("got it", re.compile(r"got it[!.]*", re.IGNORECASE)),
    ("sounds good", re.compile(r"sounds good[!.]*", re.IGNORECASE)),
)
_SOCIAL_TRAILING_PUNCTUATION = "!.?"

_DEFAULT_CONSTRAINTS_BY_KIND = {
    "external_side_effect": ["requires_authority", "no_silent_retry"],
}

_DELIVERABLES_BY_KIND = {
    "current_info": ["answer_with_source"],
    "code_change": ["changed_files", "validation_result"],
    "file_artifact": ["artifact_path"],
    "external_side_effect": ["side_effect_confirmation"],
    "operator_blocked": ["blocker_description", "unblock_action"],
    "mission_task": [],
    "task": [],
    "coordination": [],
    "chat": [],
}

_SUCCESS_CRITERIA = {
    "current_source": "runtime observed a current source",
    "source_url": "answer names a source URL",
    "checked_date": "answer includes a checked date",
    "current_source_or_blocker": "runtime observed a current source or a blocker",
    "code_change_result_or_blocker": "runtime observed changed files or a blocker",
    "tests_or_blocker": "runtime observed validation or a blocker",
    "artifact_path_or_blocker": "runtime observed an artifact path or a blocker",
    "authority_check": "authority was checked before the operation",
    "operation_result_or_blocker": "operation result or blocker was observed",
    "blocker_reason": "answer names the blocker reason",
    "mission_result_or_blocker": "mission result or blocker was observed",
    "action_result_or_blocker": "action result or blocker was observed",
}


def detect_generation_mode(content: str) -> str:
    """Recognize explicit invention authorizations. Defaults to grounded."""

    content = str(content or "")
    for _, pattern in CREATIVE_PATTERNS:
        if pattern.search(content):
            return "creative"
    for _, pattern in PERSONA_PATTERNS:
        if pattern.search(content):
            return "persona"

    normalized_social = content.strip().rstrip(_SOCIAL_TRAILING_PUNCTUATION).strip()
    for _, pattern in SOCIAL_PATTERNS:
        if pattern.fullmatch(normalized_social):
            return "social"
    return "grounded"


def build_objective(
    activation: ActivationContext,
    contract: WorkContract,
    task: dict,
    *,
    mission: dict | None = None,
    trust_level: str | None = None,
) -> Objective:
    """Build a typed objective from trusted runtime state.

    Activation content contributes only the statement and ambiguity detection.
    It is never a source of constraints, deliverables, success criteria, or
    authority.
    """

    task = task if isinstance(task, dict) else {}
    mission = mission if isinstance(mission, dict) else None
    statement = str(activation.content or "").strip()[:500]
    generation_mode = detect_generation_mode(activation.content)
    ambiguities, assumptions = _detect_ambiguities(activation, contract, task)

    return Objective(
        statement=statement,
        kind=str(contract.kind or ""),
        constraints=_constraints(contract, task, mission),
        deliverables=list(_DELIVERABLES_BY_KIND.get(str(contract.kind or ""), [])),
        success_criteria=[
            _SUCCESS_CRITERIA.get(str(item), str(item))
            for item in contract.required_evidence
        ],
        ambiguities=ambiguities,
        assumptions=assumptions,
        risk_level=_risk_level(contract, ambiguities, trust_level),
        generation_mode=generation_mode,
    )


def _constraints(contract: WorkContract, task: dict, mission: dict | None) -> list[str]:
    metadata = task.get("metadata") if isinstance(task.get("metadata"), dict) else {}
    constraints: list[str] = []

    task_constraints = metadata.get("constraints")
    if isinstance(task_constraints, list):
        constraints.extend(item for item in task_constraints if isinstance(item, str))

    if mission is not None:
        mission_constraints = mission.get("constraints")
        if isinstance(mission_constraints, list):
            constraints.extend(item for item in mission_constraints if isinstance(item, str))

    constraints.extend(f"terminal:{state}" for state in contract.allowed_terminal_states)
    constraints.extend(_DEFAULT_CONSTRAINTS_BY_KIND.get(str(contract.kind or ""), []))
    return constraints


def _detect_ambiguities(
    activation: ActivationContext,
    contract: WorkContract,
    task: dict,
) -> tuple[list[str], list[str]]:
    content = str(activation.content or "")
    kind = str(contract.kind or "")
    ambiguities: list[str] = []
    assumptions: list[str] = []

    if kind == "current_info":
        if not _TEMPORAL_ANCHOR_RE.search(content):
            ambiguities.append("ambiguity:no_temporal_anchor")
            assumptions.append(f"checked_date={_task_started_at(task)}")
        if _LATEST_CURRENT_RE.search(content) and not _RELEASE_QUALIFIER_RE.search(content):
            ambiguities.append("ambiguity:release_category")
        return ambiguities, assumptions

    if kind == "code_change":
        if not _FILE_PATH_RE.search(content) and not _metadata_string_list(task, "target_files"):
            ambiguities.append("ambiguity:target_files_missing")
        if _TESTS_RE.search(content) and not _has_validation_target(content, task):
            ambiguities.append("ambiguity:validation_target_missing")
        return ambiguities, assumptions

    if kind == "file_artifact":
        if _requires_artifact_path(contract) and not _OUTPUT_FORMAT_RE.search(content):
            ambiguities.append("ambiguity:output_format_missing")
        return ambiguities, assumptions

    if kind == "external_side_effect":
        if not _has_authority_scope(task):
            ambiguities.append("ambiguity:external_authority_scope")
        return ambiguities, assumptions

    return ambiguities, assumptions


def _risk_level(contract: WorkContract, ambiguities: list[str], trust_level: str | None) -> str:
    if str(trust_level or "").lower() in {"untrusted", "low"}:
        return "escalated"
    if contract.kind == "external_side_effect":
        return "high"
    if contract.kind == "code_change":
        if "ambiguity:target_files_missing" in ambiguities:
            return "high"
        return "medium"
    if contract.kind in {"file_artifact", "current_info"}:
        return "medium"
    return "low"


def _task_started_at(task: dict) -> str:
    metadata = task.get("metadata") if isinstance(task.get("metadata"), dict) else {}
    value = task.get("started_at") or metadata.get("started_at")
    return str(value or "")


def _metadata_string_list(task: dict, key: str) -> list[str]:
    metadata = task.get("metadata") if isinstance(task.get("metadata"), dict) else {}
    value = metadata.get(key)
    if not isinstance(value, list):
        return []
    return [item for item in value if isinstance(item, str)]


def _metadata_value(task: dict, key: str) -> Any:
    metadata = task.get("metadata") if isinstance(task.get("metadata"), dict) else {}
    return metadata.get(key)


def _has_validation_target(content: str, task: dict) -> bool:
    if _VALIDATION_TARGET_RE.search(content):
        return True
    for key in ("validation_target", "validation_targets", "build_target", "build_targets"):
        value = _metadata_value(task, key)
        if isinstance(value, str) and value.strip():
            return True
        if isinstance(value, list) and any(isinstance(item, str) and item.strip() for item in value):
            return True
    return False


def _requires_artifact_path(contract: WorkContract) -> bool:
    return any("artifact_path" in str(item) for item in contract.required_evidence)


def _has_authority_scope(task: dict) -> bool:
    for key in ("principal_authorized_scope", "authorized_scope", "authority_scope"):
        value = _metadata_value(task, key)
        if isinstance(value, str) and value.strip():
            return True
        if isinstance(value, list) and any(isinstance(item, str) and item.strip() for item in value):
            return True
    return False
