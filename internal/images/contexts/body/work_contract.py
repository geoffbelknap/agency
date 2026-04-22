"""Lightweight body-runtime work contracts.

The body uses this as external working discipline around the model: classify
inbound work, attach required evidence, and gate completion when the response
does not satisfy the contract.
"""

from __future__ import annotations

import re
from dataclasses import dataclass, field
from datetime import datetime, timezone


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


@dataclass(frozen=True)
class EvidenceView:
    tool_results: tuple[dict, ...] = ()
    observed: frozenset[str] = frozenset()
    source_urls: tuple[str, ...] = ()
    artifact_paths: tuple[str, ...] = ()

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
        return cls(
            tool_results=tool_results,
            observed=observed,
            source_urls=tuple(source_urls),
            artifact_paths=tuple(artifact_paths),
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
        return ledger

    def entries(self) -> list[EvidenceEntry]:
        return list(self._entries)

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

    def to_dict(self) -> dict:
        return {
            "tool_results": self.tool_results(),
            "observed": self.observed(),
            "source_urls": self.source_urls(),
            "artifact_paths": self.artifact_paths(),
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


def validate_completion(contract: dict | None, evidence: dict | None, content: str) -> dict:
    return DEFAULT_EVALUATOR.validate_completion(contract, evidence, content)
