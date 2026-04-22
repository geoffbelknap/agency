"""Core PACT contract engine for the body runtime.

This module owns PACT contract definitions, activation classification, evidence
modeling, and completion evaluation. Body runtime compatibility imports are kept
in ``work_contract`` so runtime integration can evolve independently from the
engine boundary.
"""

from __future__ import annotations

import re
from dataclasses import dataclass, field
from datetime import datetime, timezone
from enum import StrEnum


DEFAULT_ALLOWED_VERDICTS = ("completed", "blocked", "needs_clarification")
CURRENT_INFO_ANSWER_REQUIREMENTS = (
    "direct_answer",
    "primary_or_official_source",
    "source_url",
    "checked_date",
    "ambiguous_category_clarified",
)
CURRENT_INFO_ANSWER_CONTRACT = (
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

CURRENT_INFO_RE = re.compile(
    r"\b(latest|current|recent|most recent|today|yesterday|tomorrow|now|live|"
    r"price|weather|schedule|score|news|filing|sec filing|look up|lookup|find me|search)\b",
    re.IGNORECASE,
)
CODE_CHANGE_RE = re.compile(
    r"\b(debug|fix|patch|implement|modify|update|change|refactor)\b"
    r"[^.\n]{0,100}\b(code|bug|test|tests|failing|failure|function|module|file|repo|build)\b|"
    r"\b(code|bug|test|tests|build)\b[^.\n]{0,100}\b(debug|fix|patch|implement|modify|update|change|refactor)\b",
    re.IGNORECASE,
)
ACTION_RE = re.compile(
    r"\b(find|look up|lookup|search|check|verify|debug|fix|create|write|read|"
    r"restart|start|stop|run|test|build|deploy|summarize|analyze)\b",
    re.IGNORECASE,
)
FILE_ARTIFACT_RE = re.compile(
    r"\b(create|write|draft|generate|produce|save|export|attach)\b"
    r"[^.\n]{0,80}\b(file|report|artifact|document|markdown|csv|json|pdf|summary)\b|"
    r"\b(file|report|artifact|document)\b[^.\n]{0,80}\b(save|export|attach|downloadable)\b",
    re.IGNORECASE,
)
OPERATOR_BLOCKED_RE = re.compile(
    r"\b(blocked|stuck|can't proceed|cannot proceed|unable to proceed|need .+ from (?:you|operator|admin)|"
    r"waiting for (?:you|operator|approval|access|credentials|input)|missing (?:access|credentials|permission|approval|input))\b",
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
UNBLOCKER_RE = re.compile(
    r"\b(unblock|what would unblock|would unblock|next step|need (?:you|operator|admin)|"
    r"please provide|provide .+ (?:access|approval|credential|input|permission)|"
    r"grant .+ (?:access|permission)|approve|retry after)\b",
    re.IGNORECASE,
)
TRAILING_URL_PUNCTUATION = ".,;:!?"


@dataclass(frozen=True)
class ContractDefinition:
    kind: str
    summary: str
    required_evidence: tuple[str, ...] = ()
    answer_requirements: tuple[str, ...] = ()
    allowed_verdicts: tuple[str, ...] = DEFAULT_ALLOWED_VERDICTS
    answer_contract: str = ""


CONTRACT_REGISTRY: dict[str, ContractDefinition] = {
    "current_info": ContractDefinition(
        kind="current_info",
        summary="Current or externally verifiable facts require fresh evidence or a specific blocker.",
        required_evidence=("current_source_or_blocker",),
        answer_requirements=CURRENT_INFO_ANSWER_REQUIREMENTS,
        answer_contract=CURRENT_INFO_ANSWER_CONTRACT,
    ),
    "code_change": ContractDefinition(
        kind="code_change",
        summary="Code changes require changed-file evidence and validation evidence or a blocker.",
        required_evidence=("code_change_result_or_blocker", "tests_or_blocker"),
        answer_requirements=("files_changed", "tests_run_or_blocker"),
    ),
    "file_artifact": ContractDefinition(
        kind="file_artifact",
        summary="File artifacts require a concrete path, link, or a specific blocker.",
        required_evidence=("artifact_path_or_blocker",),
        answer_requirements=("artifact_reference",),
    ),
    "external_side_effect": ContractDefinition(
        kind="external_side_effect",
        summary="External side effects require authority, mediated execution, and outcome evidence.",
        required_evidence=("authority_check", "operation_result_or_blocker"),
        answer_requirements=("side_effect_status",),
    ),
    "operator_blocked": ContractDefinition(
        kind="operator_blocked",
        summary="Blocked work requires the blocker, checked evidence, and a clear unblock condition.",
        required_evidence=("blocker_reason",),
        answer_requirements=("next_actor_or_unblocker",),
        allowed_verdicts=("blocked", "needs_clarification"),
    ),
    "mission_task": ContractDefinition(
        kind="mission_task",
        summary="Active mission work requires a mission result or a specific blocker.",
        required_evidence=("mission_result_or_blocker",),
    ),
    "task": ContractDefinition(
        kind="task",
        summary="Action-oriented work requires an observed action result or a specific blocker.",
        required_evidence=("action_result_or_blocker",),
    ),
    "coordination": ContractDefinition(
        kind="coordination",
        summary="Non-direct coordination signals do not require action by default.",
    ),
    "chat": ContractDefinition(
        kind="chat",
        summary="Casual direct chat does not require action by default.",
    ),
}


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
    summary: str = ""

    def to_dict(self) -> dict:
        return {
            "kind": self.kind,
            "requires_action": self.requires_action,
            "required_evidence": list(self.required_evidence),
            "answer_requirements": list(self.answer_requirements),
            "allowed_terminal_states": list(self.allowed_terminal_states),
            "reason": self.reason,
            "summary": self.summary,
        }


def _utc_now() -> datetime:
    return datetime.now(timezone.utc)


def _datetime_to_dict(value: datetime | None) -> str | None:
    if value is None:
        return None
    return value.astimezone(timezone.utc).isoformat().replace("+00:00", "Z")


def _enum_value(value: StrEnum | str) -> str:
    if isinstance(value, StrEnum):
        return value.value
    return str(value)


def _work_contract_from_dict(contract: dict | WorkContract | None) -> WorkContract | None:
    if isinstance(contract, WorkContract):
        return contract
    if not isinstance(contract, dict):
        return None
    kind = str(contract.get("kind") or "").strip()
    if not kind:
        return None
    return WorkContract(
        kind=kind,
        requires_action=bool(contract.get("requires_action")),
        required_evidence=list(contract.get("required_evidence") or []),
        answer_requirements=list(contract.get("answer_requirements") or []),
        allowed_terminal_states=list(
            contract.get("allowed_terminal_states")
            or ["completed", "blocked", "needs_clarification"]
        ),
        reason=str(contract.get("reason") or ""),
        summary=str(contract.get("summary") or ""),
    )


@dataclass(frozen=True)
class ActivationContext:
    content: str = ""
    match_type: str = "direct"
    source: str = ""
    channel: str = ""
    author: str = ""
    mission_active: bool = False

    @classmethod
    def from_message(
        cls,
        content: str,
        *,
        match_type: str = "direct",
        mission_active: bool = False,
        source: str = "",
        channel: str = "",
        author: str = "",
    ) -> "ActivationContext":
        return cls(
            content=str(content or ""),
            match_type=str(match_type or "direct"),
            source=str(source or ""),
            channel=str(channel or ""),
            author=str(author or ""),
            mission_active=bool(mission_active),
        )

    def to_dict(self) -> dict:
        return {
            "content": self.content,
            "match_type": self.match_type,
            "source": self.source,
            "channel": self.channel,
            "author": self.author,
            "mission_active": self.mission_active,
        }


@dataclass(slots=True)
class Objective:
    """Placeholder for Wave 2 #1 objective builder; see spec Wave 2 item 1."""

    statement: str = ""
    kind: str = ""
    constraints: list[str] = field(default_factory=list)
    deliverables: list[str] = field(default_factory=list)
    success_criteria: list[str] = field(default_factory=list)
    ambiguities: list[str] = field(default_factory=list)
    assumptions: list[str] = field(default_factory=list)
    risk_level: str = ""

    def to_dict(self) -> dict:
        return {
            "statement": self.statement,
            "kind": self.kind,
            "constraints": list(self.constraints),
            "deliverables": list(self.deliverables),
            "success_criteria": list(self.success_criteria),
            "ambiguities": list(self.ambiguities),
            "assumptions": list(self.assumptions),
            "risk_level": self.risk_level,
        }


@dataclass(slots=True)
class Plan:
    """Placeholder for Wave 2 #3 planner runtime object; see spec Wave 2 item 3."""

    steps: list[str] = field(default_factory=list)
    summary: str = ""

    def to_dict(self) -> dict:
        return {
            "steps": list(self.steps),
            "summary": self.summary,
        }


@dataclass(slots=True)
class StepRecord:
    """Placeholder populated by Wave 2 #3 planner/execution state; see spec Wave 2 item 3."""

    step_id: str
    phase: str = ""
    turn: int | None = None
    started_at: datetime | None = None
    ended_at: datetime | None = None
    summary: str = ""

    def to_dict(self) -> dict:
        return {
            "step_id": self.step_id,
            "phase": self.phase,
            "turn": self.turn,
            "started_at": _datetime_to_dict(self.started_at),
            "ended_at": _datetime_to_dict(self.ended_at),
            "summary": self.summary,
        }


class ToolStatus(StrEnum):
    """Classifies whether a tool observation succeeded, failed, partially succeeded, or is unknown."""

    ok = "ok"
    error = "error"
    partial = "partial"
    unknown = "unknown"


class ToolProvenance(StrEnum):
    """Classifies the runtime boundary that produced a tool observation."""

    mediated = "mediated"
    provider = "provider"
    runtime = "runtime"
    unknown = "unknown"


class Retryability(StrEnum):
    """Classifies whether a failed or partial tool observation can be retried safely."""

    retry_safe = "retry_safe"
    retry_with_backoff = "retry_with_backoff"
    not_retryable = "not_retryable"
    unknown = "unknown"


class SideEffectClass(StrEnum):
    """Classifies the side-effect boundary crossed by a tool observation."""

    read_only = "read_only"
    local_state = "local_state"
    external_state = "external_state"
    unknown = "unknown"


TOOL_ERROR_KINDS = frozenset({
    "timeout",
    "permission_denied",
    "not_found",
    "validation",
    "transient",
    "unknown",
})


@dataclass(slots=True)
class ToolError:
    """Classifies a structured tool error and optional retry delay."""

    message: str
    kind: str = "unknown"
    retry_after_ms: int | None = None

    def __post_init__(self) -> None:
        if self.kind not in TOOL_ERROR_KINDS:
            self.kind = "unknown"

    def to_dict(self) -> dict:
        return {
            "message": self.message,
            "kind": self.kind,
            "retry_after_ms": self.retry_after_ms,
        }


@dataclass(slots=True)
class ToolObservation:
    """Structured protocol for tool status, provenance, evidence, retry, and side-effect classification."""

    tool: str
    status: ToolStatus | str
    data: dict = field(default_factory=dict)
    provenance: ToolProvenance | str = ToolProvenance.unknown
    producer: str = ""
    started_at: datetime | None = None
    observed_at: datetime = field(default_factory=_utc_now)
    error: ToolError | None = None
    retryability: Retryability | str = Retryability.unknown
    side_effects: SideEffectClass | str = SideEffectClass.unknown
    evidence_classification: tuple[str, ...] = ()
    summary: str = ""

    def __post_init__(self) -> None:
        if not isinstance(self.status, ToolStatus):
            self.status = ToolStatus(str(self.status or ToolStatus.unknown.value))
        if not isinstance(self.provenance, ToolProvenance):
            self.provenance = ToolProvenance(str(self.provenance or ToolProvenance.unknown.value))
        if not isinstance(self.retryability, Retryability):
            self.retryability = Retryability(str(self.retryability or Retryability.unknown.value))
        if not isinstance(self.side_effects, SideEffectClass):
            self.side_effects = SideEffectClass(str(self.side_effects or SideEffectClass.unknown.value))
        self.data = dict(self.data or {})
        self.producer = str(self.producer or self.tool or "")
        self.evidence_classification = tuple(str(item) for item in self.evidence_classification)

    def to_dict(self) -> dict:
        return {
            "tool": self.tool,
            "status": _enum_value(self.status),
            "data": dict(self.data),
            "provenance": _enum_value(self.provenance),
            "producer": self.producer,
            "started_at": _datetime_to_dict(self.started_at),
            "observed_at": _datetime_to_dict(self.observed_at),
            "error": self.error.to_dict() if self.error else None,
            "retryability": _enum_value(self.retryability),
            "side_effects": _enum_value(self.side_effects),
            "evidence_classification": list(self.evidence_classification),
            "summary": self.summary,
        }


@dataclass(slots=True)
class ExecutionError:
    """Placeholder for Wave 2 #5 recovery state machine; see spec Wave 2 item 5."""

    message: str
    phase: str = ""
    observed_at: datetime = field(default_factory=_utc_now)

    def to_dict(self) -> dict:
        return {
            "message": self.message,
            "phase": self.phase,
            "observed_at": _datetime_to_dict(self.observed_at),
        }


@dataclass(slots=True)
class RecoveryState:
    """Placeholder for Wave 2 #5 recovery state machine; see spec Wave 2 item 5."""

    status: str = ""
    reason: str = ""
    updated_at: datetime = field(default_factory=_utc_now)

    def to_dict(self) -> dict:
        return {
            "status": self.status,
            "reason": self.reason,
            "updated_at": _datetime_to_dict(self.updated_at),
        }


@dataclass(slots=True)
class ProposedOutcome:
    """Placeholder for Wave 2 #4 pre-commit evaluator; see spec Wave 2 item 4."""

    outcome: str = ""
    summary: str = ""
    proposed_at: datetime = field(default_factory=_utc_now)

    def to_dict(self) -> dict:
        return {
            "outcome": self.outcome,
            "summary": self.summary,
            "proposed_at": _datetime_to_dict(self.proposed_at),
        }


@dataclass(slots=True)
class ExecutionState:
    """Runtime-owned mutable state for a PACT run, introduced by Wave 1 #1."""

    task_id: str
    agent: str
    activation: ActivationContext | None = None
    objective: Objective | None = None
    contract: WorkContract | None = None
    plan: Plan | None = None
    step_history: list[StepRecord] = field(default_factory=list)
    tool_observations: list[ToolObservation] = field(default_factory=list)
    evidence: EvidenceLedger = field(default_factory=lambda: EvidenceLedger())
    partial_outputs: list[str] = field(default_factory=list)
    errors: list[ExecutionError] = field(default_factory=list)
    recovery_state: RecoveryState | None = None
    proposed_outcome: ProposedOutcome | None = None
    started_at: datetime = field(default_factory=_utc_now)
    updated_at: datetime = field(default_factory=_utc_now)

    @classmethod
    def from_task(cls, task: dict, *, agent: str) -> "ExecutionState":
        task = task if isinstance(task, dict) else {}
        metadata = task.get("metadata") if isinstance(task.get("metadata"), dict) else {}
        activation = metadata.get("pact_activation") if isinstance(metadata, dict) else None
        now = _utc_now()
        return cls(
            task_id=str(task.get("task_id") or "unknown"),
            agent=str(agent or ""),
            activation=ActivationContext.from_message(
                str(activation.get("content") or ""),
                match_type=str(activation.get("match_type") or "direct"),
                mission_active=bool(activation.get("mission_active")),
                source=str(activation.get("source") or ""),
                channel=str(activation.get("channel") or ""),
                author=str(activation.get("author") or ""),
            ) if isinstance(activation, dict) else None,
            contract=_work_contract_from_dict(metadata.get("work_contract")) if isinstance(metadata, dict) else None,
            started_at=now,
            updated_at=now,
        )

    def to_dict(self) -> dict:
        return {
            "task_id": self.task_id,
            "agent": self.agent,
            "activation": self.activation.to_dict() if self.activation else None,
            "objective": self.objective.to_dict() if self.objective else None,
            "contract": self.contract.to_dict() if self.contract else None,
            "plan": self.plan.to_dict() if self.plan else None,
            "step_history": [step.to_dict() for step in self.step_history],
            "tool_observations": [obs.to_dict() for obs in self.tool_observations],
            "evidence": self.evidence.to_dict(),
            "partial_outputs": list(self.partial_outputs),
            "errors": [error.to_dict() for error in self.errors],
            "recovery_state": self.recovery_state.to_dict() if self.recovery_state else None,
            "proposed_outcome": self.proposed_outcome.to_dict() if self.proposed_outcome else None,
            "started_at": _datetime_to_dict(self.started_at),
            "updated_at": _datetime_to_dict(self.updated_at),
        }

    def record_evidence(self, entry: "EvidenceEntry") -> None:
        self.evidence.record_entry(entry)
        self.updated_at = _utc_now()

    def record_step(self, step: StepRecord) -> None:
        self.step_history.append(step)
        self.updated_at = _utc_now()

    def record_tool_observation(self, obs: ToolObservation) -> None:
        self.tool_observations.append(obs)
        labels = set(obs.evidence_classification)
        ok = obs.status in (ToolStatus.ok, ToolStatus.partial)
        data = dict(obs.data or {})
        producer = str(obs.producer or obs.tool or "runtime")
        should_record_tool_result = (
            "tool_result" in labels
            or bool(labels.intersection({"current_source", "source_url"}))
        ) and not bool(data.get("suppress_tool_result"))
        if should_record_tool_result:
            self.evidence.record_tool_result(
                obs.tool,
                ok,
                metadata=data.get("tool_result_metadata") if isinstance(data.get("tool_result_metadata"), dict) else None,
            )
        if "current_source" in labels:
            self.evidence.observe(
                "current_source",
                producer=str(data.get("observation_producer") or "runtime"),
            )
        if "source_url" in labels:
            for url in data.get("source_urls") or []:
                self.evidence.record_source_url(
                    str(url),
                    producer=str(data.get("source_url_producer") or producer),
                )
        if "artifact_path" in labels:
            paths = data.get("artifact_paths")
            if paths is None:
                paths = [data.get("path")]
            for path in paths or []:
                self.evidence.record_artifact_path(
                    str(path),
                    producer=str(data.get("artifact_producer") or "runtime"),
                    metadata=data.get("metadata") if isinstance(data.get("metadata"), dict) else None,
                )
        if "changed_file" in labels:
            paths = data.get("changed_files")
            if paths is None:
                paths = [data.get("path")]
            for path in paths or []:
                self.evidence.record_changed_file(
                    str(path),
                    producer=str(data.get("changed_file_producer") or producer),
                    metadata=data.get("metadata") if isinstance(data.get("metadata"), dict) else None,
                )
        if "validation_result" in labels:
            command = str(data.get("command") or "")
            validation_ok = data.get("validation_ok", ok)
            self.evidence.record_validation_result(
                command,
                bool(validation_ok) if validation_ok is not None else None,
                producer=str(data.get("validation_producer") or producer),
                metadata=data.get("metadata") if isinstance(data.get("metadata"), dict) else None,
            )
        self.updated_at = _utc_now()

    def record_observation(self, obs: ToolObservation) -> None:
        self.record_tool_observation(obs)


@dataclass(frozen=True)
class EvidenceView:
    tool_results: tuple[dict, ...] = ()
    observed: frozenset[str] = frozenset()
    source_urls: tuple[str, ...] = ()
    artifact_paths: tuple[str, ...] = ()
    changed_files: tuple[str, ...] = ()
    validation_results: tuple[dict, ...] = ()

    @classmethod
    def from_dict(cls, evidence: dict | None) -> "EvidenceView":
        evidence = evidence or {}
        tool_results = tuple(
            item for item in evidence.get("tool_results") or []
            if isinstance(item, dict)
        )
        observed = frozenset(str(item) for item in evidence.get("observed") or [])
        source_urls: list[str] = []
        for value in evidence.get("source_urls") or []:
            if isinstance(value, str):
                for url in extract_urls(value):
                    if url not in source_urls:
                        source_urls.append(url)
        artifact_paths: list[str] = []
        for value in evidence.get("artifact_paths") or []:
            if isinstance(value, str) and value.strip() and value.strip() not in artifact_paths:
                artifact_paths.append(value.strip())
        changed_files: list[str] = []
        for value in evidence.get("changed_files") or []:
            if isinstance(value, str) and value.strip() and value.strip() not in changed_files:
                changed_files.append(value.strip())
        validation_results = tuple(
            item for item in evidence.get("validation_results") or []
            if isinstance(item, dict)
        )
        return cls(
            tool_results=tool_results,
            observed=observed,
            source_urls=tuple(source_urls),
            artifact_paths=tuple(artifact_paths),
            changed_files=tuple(changed_files),
            validation_results=validation_results,
        )

    def has_tool_or_observation(self) -> bool:
        return bool(self.tool_results or self.observed)


@dataclass(frozen=True)
class EvidenceEntry:
    kind: str
    producer: str
    value: str = ""
    ok: bool | None = None
    source_url: str = ""
    metadata: dict = field(default_factory=dict)

    def to_dict(self) -> dict:
        entry: dict[str, object] = {
            "kind": self.kind,
            "producer": self.producer,
        }
        if self.value:
            entry["value"] = self.value
        if self.ok is not None:
            entry["ok"] = self.ok
        if self.source_url:
            entry["source_url"] = self.source_url
        if self.metadata:
            entry["metadata"] = dict(self.metadata)
        return entry


class EvidenceLedger:
    def __init__(self, entries: list[EvidenceEntry] | None = None):
        self._entries = list(entries or [])

    @classmethod
    def from_dict(cls, evidence: dict | None) -> "EvidenceLedger":
        ledger = cls()
        evidence = evidence or {}
        for item in evidence.get("tool_results") or []:
            if not isinstance(item, dict):
                continue
            ledger.record_tool_result(
                str(item.get("tool") or ""),
                bool(item.get("ok")) if "ok" in item else None,
                metadata={k: v for k, v in item.items() if k not in {"tool", "ok"}},
            )
        for item in evidence.get("observed") or []:
            ledger.observe(str(item))
        for item in evidence.get("source_urls") or []:
            if isinstance(item, str):
                for url in extract_urls(item):
                    ledger.record_source_url(url)
        for item in evidence.get("artifact_paths") or []:
            if isinstance(item, str):
                ledger.record_artifact_path(item)
        for item in evidence.get("changed_files") or []:
            if isinstance(item, str):
                ledger.record_changed_file(item)
        for item in evidence.get("validation_results") or []:
            if not isinstance(item, dict):
                continue
            ledger.record_validation_result(
                str(item.get("command") or ""),
                bool(item.get("ok")) if "ok" in item else None,
                metadata={k: v for k, v in item.items() if k not in {"command", "ok"}},
            )
        return ledger

    def entries(self) -> list[EvidenceEntry]:
        return list(self._entries)

    def record_entry(self, entry: EvidenceEntry) -> None:
        self._entries.append(entry)

    def record_tool_result(
        self,
        tool: str,
        ok: bool | None = True,
        metadata: dict | None = None,
    ) -> None:
        tool = str(tool or "").strip()
        if not tool:
            return
        self._entries.append(EvidenceEntry(
            kind="tool_result",
            producer=tool,
            ok=ok,
            metadata=dict(metadata or {}),
        ))

    def observe(self, value: str, producer: str = "runtime") -> None:
        value = str(value or "").strip()
        if not value:
            return
        if value in self.observed():
            return
        self._entries.append(EvidenceEntry(
            kind="observation",
            producer=str(producer or "runtime"),
            value=value,
        ))

    def record_source_url(self, url: str, producer: str = "runtime") -> None:
        for extracted in extract_urls(str(url or "")):
            if extracted in self.source_urls():
                continue
            self._entries.append(EvidenceEntry(
                kind="source_url",
                producer=str(producer or "runtime"),
                source_url=extracted,
            ))

    def record_artifact_path(
        self,
        path: str,
        producer: str = "runtime",
        metadata: dict | None = None,
    ) -> None:
        path = str(path or "").strip()
        if not path:
            return
        if path in self.artifact_paths():
            return
        self._entries.append(EvidenceEntry(
            kind="artifact_path",
            producer=str(producer or "runtime"),
            value=path,
            metadata=dict(metadata or {}),
        ))

    def record_changed_file(
        self,
        path: str,
        producer: str = "runtime",
        metadata: dict | None = None,
    ) -> None:
        path = str(path or "").strip()
        if not path:
            return
        if path in self.changed_files():
            return
        self._entries.append(EvidenceEntry(
            kind="changed_file",
            producer=str(producer or "runtime"),
            value=path,
            metadata=dict(metadata or {}),
        ))

    def record_validation_result(
        self,
        command: str,
        ok: bool | None,
        producer: str = "runtime",
        metadata: dict | None = None,
    ) -> None:
        command = str(command or "").strip()
        if not command:
            return
        self._entries.append(EvidenceEntry(
            kind="validation_result",
            producer=str(producer or "runtime"),
            value=command,
            ok=ok,
            metadata=dict(metadata or {}),
        ))

    def tool_results(self) -> list[dict]:
        results: list[dict] = []
        for entry in self._entries:
            if entry.kind != "tool_result":
                continue
            result = {"tool": entry.producer}
            if entry.ok is not None:
                result["ok"] = entry.ok
            result.update(entry.metadata)
            results.append(result)
        return results

    def observed(self) -> list[str]:
        values: list[str] = []
        for entry in self._entries:
            if entry.kind == "observation" and entry.value and entry.value not in values:
                values.append(entry.value)
        return values

    def source_urls(self) -> list[str]:
        urls: list[str] = []
        for entry in self._entries:
            if entry.kind == "source_url" and entry.source_url and entry.source_url not in urls:
                urls.append(entry.source_url)
        return urls

    def artifact_paths(self) -> list[str]:
        paths: list[str] = []
        for entry in self._entries:
            if entry.kind == "artifact_path" and entry.value and entry.value not in paths:
                paths.append(entry.value)
        return paths

    def changed_files(self) -> list[str]:
        paths: list[str] = []
        for entry in self._entries:
            if entry.kind == "changed_file" and entry.value and entry.value not in paths:
                paths.append(entry.value)
        return paths

    def validation_results(self) -> list[dict]:
        results: list[dict] = []
        for entry in self._entries:
            if entry.kind != "validation_result":
                continue
            result = {"command": entry.value}
            if entry.ok is not None:
                result["ok"] = entry.ok
            result.update(entry.metadata)
            results.append(result)
        return results

    def to_dict(self) -> dict:
        return {
            "tool_results": self.tool_results(),
            "observed": self.observed(),
            "source_urls": self.source_urls(),
            "artifact_paths": self.artifact_paths(),
            "changed_files": self.changed_files(),
            "validation_results": self.validation_results(),
            "entries": [entry.to_dict() for entry in self._entries],
        }

    def to_view(self) -> EvidenceView:
        return EvidenceView.from_dict(self.to_dict())


@dataclass(frozen=True)
class EvaluationResult:
    verdict: str
    missing_evidence: tuple[str, ...] = ()
    message: str = ""

    def to_dict(self) -> dict:
        result: dict[str, object] = {"verdict": self.verdict}
        if self.missing_evidence:
            result["missing_evidence"] = list(self.missing_evidence)
        if self.message:
            result["message"] = self.message
        return result


class PactEvaluator:
    """Registry-backed evaluator for body-runtime PACT contracts."""

    def __init__(self, registry: dict[str, ContractDefinition] | None = None):
        self._registry = dict(registry or CONTRACT_REGISTRY)

    def list_contract_kinds(self) -> list[str]:
        return sorted(self._registry.keys())

    def contract_definition(self, kind: str) -> ContractDefinition:
        try:
            return self._registry[kind]
        except KeyError as exc:
            raise ValueError(f"unknown work contract kind: {kind}") from exc

    def build_contract(
        self,
        kind: str,
        *,
        requires_action: bool,
        reason: str,
        extra_answer_requirements: list[str] | None = None,
    ) -> WorkContract:
        definition = self.contract_definition(kind)
        answer_requirements = list(definition.answer_requirements)
        for requirement in extra_answer_requirements or []:
            if requirement not in answer_requirements:
                answer_requirements.append(requirement)
        return WorkContract(
            kind=definition.kind,
            requires_action=requires_action,
            required_evidence=list(definition.required_evidence),
            answer_requirements=answer_requirements,
            allowed_terminal_states=list(definition.allowed_verdicts),
            reason=reason,
            summary=definition.summary,
        )

    def classify_activation(self, activation: ActivationContext) -> WorkContract:
        text = str(activation.content or "").strip()
        if activation.mission_active:
            return self.build_contract(
                "mission_task",
                requires_action=True,
                reason="active mission direct message",
            )
        if OPERATOR_BLOCKED_RE.search(text):
            return self.build_contract(
                "operator_blocked",
                requires_action=True,
                reason="explicit blocker or missing-operator-input signal",
            )
        if CURRENT_INFO_RE.search(text):
            extra_answer_requirements = []
            if DATE_REQUEST_RE.search(text):
                extra_answer_requirements.append("requested_absolute_date")
            return self.build_contract(
                "current_info",
                requires_action=True,
                reason="time-sensitive or externally verifiable request",
                extra_answer_requirements=extra_answer_requirements,
            )
        if CODE_CHANGE_RE.search(text):
            return self.build_contract(
                "code_change",
                requires_action=True,
                reason="code-change request",
            )
        if FILE_ARTIFACT_RE.search(text):
            return self.build_contract(
                "file_artifact",
                requires_action=True,
                reason="artifact-producing request",
            )
        if ACTION_RE.search(text):
            return self.build_contract(
                "task",
                requires_action=True,
                reason="action verb detected",
            )
        if activation.match_type != "direct":
            return self.build_contract("coordination", requires_action=False, reason="non-direct channel signal")
        return self.build_contract("chat", requires_action=False, reason="no action requirement detected")

    def classify_work(self, content: str, match_type: str = "direct", mission_active: bool = False) -> WorkContract:
        return self.classify_activation(
            ActivationContext.from_message(
                content,
                match_type=match_type,
                mission_active=mission_active,
            )
        )

    def contract_prompt(self, contract: WorkContract) -> str:
        if not contract.requires_action:
            return ""
        evidence = ", ".join(contract.required_evidence) or "task evidence"
        answer_requirements = ", ".join(contract.answer_requirements)
        answer_requirement_line = (
            f"answer_requirements: {answer_requirements}\n" if answer_requirements else ""
        )
        definition = self.contract_definition(contract.kind)
        return (
            "\n\n[WORK_CONTRACT]\n"
            f"kind: {contract.kind}\n"
            f"summary: {contract.summary or definition.summary}\n"
            f"required_evidence: {evidence}\n"
            f"{answer_requirement_line}"
            f"allowed_terminal_states: {', '.join(contract.allowed_terminal_states)}\n"
            "rules:\n"
            "- Treat this as work, not casual chat.\n"
            "- Use mediated tools or observed context when the task requires action or current facts.\n"
            "- Do not claim evidence you do not have.\n"
            "- If required evidence cannot be obtained, report a specific blocker instead of guessing.\n"
            "[/WORK_CONTRACT]"
            f"{definition.answer_contract}"
        )

    def format_blocked_completion(
        self,
        contract: dict | None,
        evidence: dict | None,
        content: str = "",
        checked_at: str | None = None,
    ) -> str:
        evidence_view = EvidenceView.from_dict(evidence)
        kind = (contract or {}).get("kind")
        if kind != "current_info":
            return str(content or "I cannot complete this task with the available evidence.")

        source_urls = list(evidence_view.source_urls)
        if source_urls:
            reason = "Available source URLs did not satisfy the official/current-source evidence contract."
        elif evidence_view.tool_results or "current_source" in evidence_view.observed:
            reason = "Current-information tool evidence was insufficient to verify the requested fact."
        else:
            reason = "No current-information source or tool result was available."

        lines = [
            "I cannot verify this from an official/current source without guessing.",
            "",
            f"- Blocked: {reason}",
            f"- Evidence checked: tools={_tool_summary(evidence)}",
        ]
        if source_urls:
            lines.append("- Source URLs observed:")
            lines.extend(_source_url_lines(source_urls))
        lines.extend([
            "- What would unblock this: an official or primary source URL that directly supports the requested current fact.",
            f"- Checked: {_checked_date(checked_at)}.",
        ])
        return "\n".join(lines)

    def validate_completion(self, contract: dict | None, evidence: dict | None, content: str) -> dict:
        return self.evaluate_completion(contract, evidence, content).to_dict()

    def evaluate_completion(
        self,
        contract: dict | None,
        evidence: dict | None,
        content: str,
    ) -> EvaluationResult:
        if not contract or not contract.get("requires_action"):
            return EvaluationResult("completed")

        evidence_view = EvidenceView.from_dict(evidence)
        content = str(content or "")
        kind = str(contract.get("kind") or "")
        if kind not in self._registry:
            return EvaluationResult(
                "blocked",
                missing_evidence=("known_contract_kind",),
                message=f"Unknown work contract kind: {kind or '(missing)'}.",
            )
        if BLOCKER_RE.search(content):
            if kind == "operator_blocked":
                return _validate_operator_blocked_answer(contract, content)
            return EvaluationResult(
                "blocked",
                message=self.format_blocked_completion(contract, evidence, content),
            )

        required = set(contract.get("required_evidence") or [])

        if "current_source_or_blocker" in required:
            if evidence_view.tool_results or "current_source" in evidence_view.observed:
                return _validate_current_info_answer(contract, evidence_view, content)
            return EvaluationResult(
                "needs_action",
                missing_evidence=("current_source_or_blocker",),
                message=(
                    "This work requires current external evidence. Use an available "
                    "current-info/search/fetch tool, or report the specific blocker."
                ),
            )

        if "artifact_path_or_blocker" in required:
            if not evidence_view.artifact_paths:
                return EvaluationResult(
                    "needs_action",
                    missing_evidence=("artifact_path_or_blocker",),
                    message="This work requires a runtime-observed artifact path or a specific blocker.",
                )
            return _validate_file_artifact_answer(contract, evidence_view, content)

        if "code_change_result_or_blocker" in required:
            if not evidence_view.changed_files:
                return EvaluationResult(
                    "needs_action",
                    missing_evidence=("code_change_result_or_blocker",),
                    message="This work requires runtime-observed changed-file evidence or a specific blocker.",
                )
            if "tests_or_blocker" in required and not evidence_view.validation_results:
                return EvaluationResult(
                    "needs_action",
                    missing_evidence=("tests_or_blocker",),
                    message="This code change requires runtime-observed validation evidence or a specific blocker.",
                )
            return _validate_code_change_answer(contract, evidence_view, content)

        if "blocker_reason" in required:
            return _validate_operator_blocked_answer(contract, content)

        if "action_result_or_blocker" in required and not evidence_view.has_tool_or_observation():
            return EvaluationResult(
                "needs_action",
                missing_evidence=("action_result_or_blocker",),
                message="This work requires an observed action result or a specific blocker.",
            )

        return EvaluationResult("completed")


DEFAULT_EVALUATOR = PactEvaluator()


def list_contract_kinds() -> list[str]:
    return DEFAULT_EVALUATOR.list_contract_kinds()


def contract_definition(kind: str) -> ContractDefinition:
    return DEFAULT_EVALUATOR.contract_definition(kind)


def build_contract(
    kind: str,
    *,
    requires_action: bool,
    reason: str,
    extra_answer_requirements: list[str] | None = None,
) -> WorkContract:
    return DEFAULT_EVALUATOR.build_contract(
        kind,
        requires_action=requires_action,
        reason=reason,
        extra_answer_requirements=extra_answer_requirements,
    )


def classify_work(content: str, match_type: str = "direct", mission_active: bool = False) -> WorkContract:
    return DEFAULT_EVALUATOR.classify_work(content, match_type=match_type, mission_active=mission_active)


def classify_activation(activation: ActivationContext) -> WorkContract:
    return DEFAULT_EVALUATOR.classify_activation(activation)


def contract_prompt(contract: WorkContract) -> str:
    return DEFAULT_EVALUATOR.contract_prompt(contract)


def extract_urls(text: str) -> list[str]:
    urls: list[str] = []
    for match in URL_RE.finditer(str(text or "")):
        url = match.group(0).rstrip(TRAILING_URL_PUNCTUATION)
        if url and url not in urls:
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


def _source_url_lines(source_urls: list[str], limit: int = 5) -> list[str]:
    shown = source_urls[:limit]
    lines = [f"  - {url}" for url in shown]
    remaining = len(source_urls) - len(shown)
    if remaining > 0:
        lines.append(f"  - ...and {remaining} more observed URLs.")
    return lines


def format_blocked_completion(
    contract: dict | None,
    evidence: dict | None,
    content: str = "",
    checked_at: str | None = None,
) -> str:
    """Return a concise harness-owned blocker response."""
    return DEFAULT_EVALUATOR.format_blocked_completion(contract, evidence, content, checked_at)


def _validate_current_info_answer(
    contract: dict,
    evidence: EvidenceView,
    content: str,
) -> EvaluationResult:
    requirements = set(contract.get("answer_requirements") or [])
    missing: list[str] = []
    answer_without_checked_clause = CHECKED_CLAUSE_RE.sub("", content)
    answer_urls = extract_urls(content)
    evidence_urls = list(evidence.source_urls)

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
        return EvaluationResult("completed")

    return EvaluationResult(
        "needs_action",
        missing_evidence=tuple(missing),
        message=(
            "The answer has current-source evidence but does not satisfy the "
            "answer contract. Provide direct source links, a checked/as-of date, "
            "and name sources instead of referring vaguely to search results."
        ),
    )


def _validate_file_artifact_answer(
    contract: dict,
    evidence: EvidenceView,
    content: str,
) -> EvaluationResult:
    requirements = set(contract.get("answer_requirements") or [])
    missing: list[str] = []

    if "artifact_reference" in requirements:
        if not any(path in content for path in evidence.artifact_paths):
            missing.append("artifact_reference")

    if not missing:
        return EvaluationResult("completed")

    return EvaluationResult(
        "needs_action",
        missing_evidence=tuple(missing),
        message="The artifact exists, but the completion must include its concrete path or link.",
    )


def _validate_code_change_answer(
    contract: dict,
    evidence: EvidenceView,
    content: str,
) -> EvaluationResult:
    requirements = set(contract.get("answer_requirements") or [])
    missing: list[str] = []

    if "files_changed" in requirements:
        if not any(path in content for path in evidence.changed_files):
            missing.append("files_changed")

    validations = list(evidence.validation_results)
    passing_validations = [item for item in validations if item.get("ok") is True]
    if "tests_run_or_blocker" in requirements:
        if not passing_validations:
            missing.append("tests_run_or_blocker")
        elif not any(str(item.get("command") or "") in content for item in passing_validations):
            missing.append("tests_run_or_blocker")

    if not missing:
        return EvaluationResult("completed")

    return EvaluationResult(
        "needs_action",
        missing_evidence=tuple(missing),
        message="The code change evidence exists, but the completion must name changed files and passing validation commands.",
    )


def _validate_operator_blocked_answer(contract: dict, content: str) -> EvaluationResult:
    requirements = set(contract.get("answer_requirements") or [])
    missing: list[str] = []

    if not BLOCKER_RE.search(content):
        missing.append("blocker_reason")
    if "next_actor_or_unblocker" in requirements and not UNBLOCKER_RE.search(content):
        missing.append("next_actor_or_unblocker")

    if not missing:
        return EvaluationResult("blocked", message=content)

    return EvaluationResult(
        "needs_action",
        missing_evidence=tuple(missing),
        message="Blocked work must state the concrete blocker and what operator/admin action would unblock it.",
    )


def validate_completion(contract: dict | None, evidence: dict | None, content: str) -> dict:
    return DEFAULT_EVALUATOR.validate_completion(contract, evidence, content)
