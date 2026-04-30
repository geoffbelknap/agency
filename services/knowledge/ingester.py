"""Rule-based knowledge graph ingester.

Extracts structural graph data from comms messages, tasks,
synthesis records, trust signals, and audit events. No LLM calls.
Runs inside the knowledge container.
"""

import logging
from typing import Optional
from services.knowledge.store import KnowledgeStore

logger = logging.getLogger("agency.knowledge.ingester")


class RuleIngester:
    """Extracts graph structure from platform data sources."""

    def __init__(self, store: KnowledgeStore, curator=None):
        self.store = store
        self.curator = curator
        self._node_cache: dict[tuple[str, str], str] = {}
        self._processed_messages: set[str] = set()
        self._edge_cache: set[tuple[str, str, str]] = set()

    def _ensure_node(self, label: str, kind: str, **kwargs) -> str:
        cache_key = (kind, label)
        if cache_key in self._node_cache:
            return self._node_cache[cache_key]
        existing = self.store.find_nodes_by_kind(kind)
        for node in existing:
            if node["label"] == label:
                self._node_cache[cache_key] = node["id"]
                return node["id"]
        node_id = self.store.add_node(label=label, kind=kind, **kwargs)
        self._node_cache[cache_key] = node_id
        return node_id

    def _check_curation(self, node_id: str) -> None:
        """Run post-ingestion curator check if curator is available."""
        if self.curator is None:
            return
        try:
            self.curator.post_ingestion_check(node_id)
        except Exception as e:
            logger.warning("Curator post-ingestion check failed for %s: %s", node_id, e)

    def _ensure_edge(
        self, source_id: str, target_id: str, relation: str, **kwargs
    ) -> Optional[str]:
        cache_key = (source_id, target_id, relation)
        if cache_key in self._edge_cache:
            return None
        self._edge_cache.add(cache_key)
        return self.store.add_edge(
            source_id=source_id,
            target_id=target_id,
            relation=relation,
            provenance="EXTRACTED",
            **kwargs,
        )

    def ingest_message(self, msg: dict) -> None:
        msg_id = msg.get("id", "")
        if msg_id in self._processed_messages:
            return
        self._processed_messages.add(msg_id)

        channel = msg.get("channel", "")
        author = msg.get("author", "")
        content = msg.get("content", "")
        flags = msg.get("flags") or {}
        reply_to = msg.get("reply_to")

        agent_id = self._ensure_node(author, "agent", source_type="rule")
        channel_id = self._ensure_node(channel, "channel", source_type="rule")
        self._ensure_edge(agent_id, channel_id, "member_of", source_channel=channel)

        if flags.get("decision"):
            summary = content[:200] if content else "Decision"
            decision_id = self.store.add_node(
                label=f"decision-{msg_id}",
                kind="decision",
                summary=summary,
                source_type="rule",
                source_channels=[channel],
                properties={"message_id": msg_id},
            )
            self._check_curation(decision_id)
            self.store.add_edge(
                source_id=agent_id,
                target_id=decision_id,
                relation="decided",
                provenance="EXTRACTED",
                source_channel=channel,
                provenance_id=msg_id,
            )

        if flags.get("blocker"):
            blocker_id = self.store.add_node(
                label=f"blocker-{msg_id}",
                kind="blocker",
                summary=content[:200] if content else "Blocker",
                source_type="rule",
                source_channels=[channel],
                properties={"message_id": msg_id},
            )
            self._check_curation(blocker_id)
            self.store.add_edge(
                source_id=agent_id,
                target_id=blocker_id,
                relation="raised",
                provenance="EXTRACTED",
                source_channel=channel,
                provenance_id=msg_id,
            )

        if reply_to and reply_to in self._processed_messages:
            self.store.add_edge(
                source_id=agent_id,
                target_id=agent_id,
                relation="replied_to",
                provenance="EXTRACTED",
                source_channel=channel,
                provenance_id=msg_id,
                properties={"reply_to": reply_to, "message_id": msg_id},
            )

    def ingest_task(self, task: dict) -> None:
        task_id = task.get("task_id", task.get("subtask_id", ""))
        assignee = task.get("assignee", "")
        coordinator = task.get("coordinator", "")
        description = task.get("description", "")

        task_node_id = self._ensure_node(
            f"task-{task_id}", "task",
            summary=description[:200],
            source_type="rule",
        )

        if assignee:
            agent_id = self._ensure_node(assignee, "agent", source_type="rule")
            self._ensure_edge(agent_id, task_node_id, "assigned_to")

        if coordinator:
            coord_id = self._ensure_node(coordinator, "agent", source_type="rule")
            self._ensure_edge(coord_id, task_node_id, "delegated")

        for dep in task.get("depends_on", []):
            dep_node_id = self._ensure_node(f"task-{dep}", "task", source_type="rule")
            self._ensure_edge(task_node_id, dep_node_id, "depends_on")

    def ingest_trust_signal(self, agent_name: str, signal: dict) -> None:
        agent_id = self._ensure_node(agent_name, "agent", source_type="rule")
        signal_type = signal.get("signal_type", "unknown")
        weight = signal.get("weight", 0)
        self.store.add_edge(
            source_id=agent_id,
            target_id=agent_id,
            relation="trust_signal",
            provenance="EXTRACTED",
            weight=weight,
            properties={"signal_type": signal_type},
            provenance_id=signal.get("timestamp", ""),
        )
