import json

from services.knowledge.manager import MemoryManager
from services.knowledge.store import KnowledgeStore


def _proposal(store, *, memory_type="procedural", confidence="high", summary="Use SEC primary filings first."):
    return store.add_node(
        label=f"memory-proposal:{memory_type}:{confidence}:{summary}",
        kind="memory_proposal",
        summary=summary,
        properties={
            "status": "pending_review",
            "memory_type": memory_type,
            "confidence": confidence,
            "reason": "useful later",
            "entities": ["SEC"],
            "evidence_message_ids": ["m1"],
            "agent": "jarvis",
            "task_id": "t1",
            "channel": "dm-jarvis",
        },
        source_type="agent",
        source_channels=["dm-jarvis"],
    )


def test_high_confidence_procedural_proposal_is_promoted(tmp_path):
    store = KnowledgeStore(tmp_path)
    proposal_id = _proposal(store)

    stats = MemoryManager(store).process_pending()

    assert stats == {"processed": 1, "approved": 1, "rejected": 0, "needs_review": 0}
    proposal = store.get_node(proposal_id)
    proposal_props = json.loads(proposal["properties"])
    assert proposal_props["status"] == "approved"
    assert proposal_props["promoted_node_id"]

    promoted = store.get_node(proposal_props["promoted_node_id"])
    assert promoted["kind"] == "procedure"
    assert promoted["source_type"] == "manager"
    promoted_props = json.loads(promoted["properties"])
    assert promoted_props["memory_type"] == "procedural"
    assert promoted_props["promoted_from"] == proposal_id
    assert promoted_props["evidence_message_ids"] == ["m1"]


def test_low_confidence_proposal_requires_review(tmp_path):
    store = KnowledgeStore(tmp_path)
    proposal_id = _proposal(store, confidence="medium")

    stats = MemoryManager(store).process_pending()

    assert stats["needs_review"] == 1
    props = json.loads(store.get_node(proposal_id)["properties"])
    assert props["status"] == "needs_review"
    assert "confidence is medium" in props["decision_reason"]
    assert store.find_nodes_by_kind("procedure") == []


def test_preference_semantic_proposal_requires_review(tmp_path):
    store = KnowledgeStore(tmp_path)
    proposal_id = _proposal(
        store,
        memory_type="semantic",
        confidence="high",
        summary="Operator prefers primary SEC filings.",
    )

    stats = MemoryManager(store).process_pending()

    assert stats["needs_review"] == 1
    props = json.loads(store.get_node(proposal_id)["properties"])
    assert props["status"] == "needs_review"
    assert "preference" in props["decision_reason"]


def test_preference_procedural_proposal_requires_review(tmp_path):
    store = KnowledgeStore(tmp_path)
    proposal_id = _proposal(
        store,
        memory_type="procedural",
        confidence="high",
        summary="Operator preference: when asked for SEC filings, use SEC EDGAR first.",
    )

    stats = MemoryManager(store).process_pending()

    assert stats["needs_review"] == 1
    props = json.loads(store.get_node(proposal_id)["properties"])
    assert props["status"] == "needs_review"
    assert "preference" in props["decision_reason"]
    assert store.find_nodes_by_kind("procedure") == []


def test_secret_like_proposal_is_rejected(tmp_path):
    store = KnowledgeStore(tmp_path)
    proposal_id = _proposal(
        store,
        memory_type="semantic",
        confidence="high",
        summary="The API token is sk-example-secret.",
    )

    stats = MemoryManager(store).process_pending()

    assert stats["rejected"] == 1
    props = json.loads(store.get_node(proposal_id)["properties"])
    assert props["status"] == "rejected"
    assert "secret" in props["decision_reason"]
    assert store.find_nodes_by_kind("fact") == []


def test_list_memory_proposals_filters_by_status(tmp_path):
    store = KnowledgeStore(tmp_path)
    pending_id = _proposal(store, summary="Pending")
    approved_id = _proposal(store, summary="Approved")
    store.update_memory_proposal_status(approved_id, "approved")

    pending = store.list_memory_proposals(status="pending_review")

    assert [p["id"] for p in pending] == [pending_id]


def test_operator_approve_promotes_reviewed_memory(tmp_path):
    store = KnowledgeStore(tmp_path)
    proposal_id = _proposal(store, confidence="medium")
    manager = MemoryManager(store)
    manager.process_pending()

    result = manager.review_proposal(proposal_id, "approve", "operator confirmed")

    assert result["proposal_id"] == proposal_id
    assert result["action"] == "approve"
    assert result["promoted_node_id"]
    props = json.loads(store.get_node(proposal_id)["properties"])
    assert props["status"] == "approved"
    assert props["decision_reason"] == "operator confirmed"
    promoted = store.get_node(result["promoted_node_id"])
    assert promoted["kind"] == "procedure"


def test_operator_reject_keeps_reviewed_memory_unpromoted(tmp_path):
    store = KnowledgeStore(tmp_path)
    proposal_id = _proposal(store, confidence="medium")
    manager = MemoryManager(store)
    manager.process_pending()

    result = manager.review_proposal(proposal_id, "reject", "not useful")

    assert result == {"proposal_id": proposal_id, "action": "reject"}
    props = json.loads(store.get_node(proposal_id)["properties"])
    assert props["status"] == "rejected"
    assert props["decision_reason"] == "not useful"
    assert store.find_nodes_by_kind("procedure") == []
