"""Knowledge management orchestration.

Memory proposals are created by agent runtimes, but durable memory is owned by
the knowledge service. This module evaluates pending proposals and either
promotes them into approved graph memory, rejects them, or leaves them for
operator review.
"""

from __future__ import annotations

import hashlib
import json
import logging
import re
from dataclasses import dataclass

from images.knowledge.store import KnowledgeStore

logger = logging.getLogger("agency.knowledge.manager")


SECRET_RE = re.compile(
    r"(?i)\b("
    r"api[_-]?key|secret|password|passwd|token|bearer|private[_-]?key|"
    r"sk-[a-z0-9_-]{8,}|ghp_[a-z0-9_]{8,}"
    r")\b"
)
IDENTITY_RE = re.compile(r"(?i)\b(identity|persona|system prompt|always respond|operator prefers|user prefers|preference)\b")


@dataclass
class MemoryDecision:
    action: str
    reason: str
    target_kind: str = ""


class MemoryManager:
    """Evaluate and promote pending long-term memory proposals."""

    def __init__(self, store: KnowledgeStore):
        self.store = store

    def process_pending(self, limit: int = 25) -> dict:
        proposals = self.store.list_memory_proposals(status="pending_review", limit=limit)
        stats = {"processed": 0, "approved": 0, "rejected": 0, "needs_review": 0}
        for proposal in proposals:
            decision = self.evaluate_proposal(proposal)
            stats["processed"] += 1
            if decision.action == "approve":
                promoted_id = self.promote_proposal(proposal, decision)
                self.store.update_memory_proposal_status(
                    proposal["id"],
                    "approved",
                    reason=decision.reason,
                    promoted_node_id=promoted_id,
                )
                stats["approved"] += 1
            elif decision.action == "reject":
                self.store.update_memory_proposal_status(
                    proposal["id"], "rejected", reason=decision.reason,
                )
                stats["rejected"] += 1
            else:
                self.store.update_memory_proposal_status(
                    proposal["id"], "needs_review", reason=decision.reason,
                )
                stats["needs_review"] += 1
        if stats["processed"]:
            logger.info("memory_manager processed proposals: %s", stats)
        return stats

    def evaluate_proposal(self, proposal: dict) -> MemoryDecision:
        props = _props(proposal)
        summary = str(proposal.get("summary", "")).strip()
        memory_type = str(props.get("memory_type", "")).lower()
        confidence = str(props.get("confidence", "low")).lower()

        if not summary:
            return MemoryDecision("reject", "empty memory summary")
        if memory_type not in {"semantic", "episodic", "procedural"}:
            return MemoryDecision("reject", "invalid memory_type")
        if SECRET_RE.search(summary) or SECRET_RE.search(json.dumps(props)):
            return MemoryDecision("reject", "proposal appears to contain secret material")
        if confidence != "high":
            return MemoryDecision("review", f"confidence is {confidence or 'missing'}")
        if memory_type == "semantic" and IDENTITY_RE.search(summary):
            return MemoryDecision("review", "identity or preference-affecting semantic memory")

        return MemoryDecision(
            "approve",
            "high-confidence low-risk memory proposal",
            target_kind=_target_kind(memory_type),
        )

    def promote_proposal(self, proposal: dict, decision: MemoryDecision) -> str:
        props = _props(proposal)
        memory_type = str(props.get("memory_type", "")).lower()
        target_kind = decision.target_kind or _target_kind(memory_type)
        summary = str(proposal.get("summary", "")).strip()
        source_channels = _json_list(proposal.get("source_channels"))
        proposal_id = proposal["id"]
        promoted_props = {
            "memory_type": memory_type,
            "promoted_from": proposal_id,
            "promotion_reason": decision.reason,
            "confidence": props.get("confidence", ""),
            "evidence_message_ids": props.get("evidence_message_ids", []),
            "entities": props.get("entities", []),
            "agent": props.get("agent", ""),
            "task_id": props.get("task_id", ""),
            "channel": props.get("channel", ""),
            "participant": props.get("participant", ""),
            "approved_by": "knowledge_manager",
        }
        label = _memory_label(memory_type, summary)
        node_id = self.store.add_node(
            label=label,
            kind=target_kind,
            summary=summary,
            properties=promoted_props,
            source_type="manager",
            source_channels=source_channels,
        )
        self.store.log_curation(
            "memory_promoted",
            node_id,
            {"proposal_id": proposal_id, "memory_type": memory_type, "kind": target_kind},
        )
        return node_id

    def review_proposal(self, proposal_id: str, action: str, reason: str = "") -> dict | None:
        """Apply an operator decision to a memory proposal."""
        proposal = self.store.get_node(proposal_id)
        if not proposal or proposal.get("kind") != "memory_proposal":
            return None

        review_reason = reason.strip() or "operator reviewed memory proposal"
        if action == "approve":
            props = _props(proposal)
            memory_type = str(props.get("memory_type", "")).lower()
            decision = MemoryDecision(
                "approve",
                review_reason,
                target_kind=_target_kind(memory_type),
            )
            promoted_id = self.promote_proposal(proposal, decision)
            self.store.update_memory_proposal_status(
                proposal_id,
                "approved",
                reason=review_reason,
                promoted_node_id=promoted_id,
            )
            return {"proposal_id": proposal_id, "action": action, "promoted_node_id": promoted_id}

        self.store.update_memory_proposal_status(proposal_id, "rejected", reason=review_reason)
        return {"proposal_id": proposal_id, "action": action}


class KnowledgeManager:
    """Thin coordinator for knowledge-service management tasks."""

    def __init__(self, store: KnowledgeStore, memory_manager: MemoryManager | None = None):
        self.store = store
        self.memory_manager = memory_manager or MemoryManager(store)

    def process_cycle(self) -> dict:
        return {"memory": self.memory_manager.process_pending()}


def _props(node: dict) -> dict:
    props = node.get("properties") or {}
    if isinstance(props, str):
        try:
            parsed = json.loads(props)
            return parsed if isinstance(parsed, dict) else {}
        except json.JSONDecodeError:
            return {}
    return props if isinstance(props, dict) else {}


def _json_list(value) -> list:
    if isinstance(value, list):
        return value
    if isinstance(value, str):
        try:
            parsed = json.loads(value)
            return parsed if isinstance(parsed, list) else []
        except json.JSONDecodeError:
            return []
    return []


def _target_kind(memory_type: str) -> str:
    if memory_type == "procedural":
        return "procedure"
    if memory_type == "episodic":
        return "episode"
    return "fact"


def _memory_label(memory_type: str, summary: str) -> str:
    digest = hashlib.sha256(summary.encode()).hexdigest()[:10]
    words = re.sub(r"[^a-zA-Z0-9]+", "-", summary.lower()).strip("-")
    return f"memory:{memory_type}:{words[:48]}:{digest}"
