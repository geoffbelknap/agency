"""Knowledge graph curator — automated near-duplicate detection and merge.

Runs post-ingestion checks to find and merge near-duplicate nodes,
plus periodic curation sweeps for orphan detection, burst flagging,
and fuzzy-match cleanup.
"""

import asyncio
import json
import logging
import os
import re
from datetime import datetime, timezone
from difflib import SequenceMatcher

logger = logging.getLogger("agency.knowledge.curator")

from agency_core.images.knowledge.store import KnowledgeStore, STRUCTURAL_KINDS, _SOURCE_PRIORITY


def _normalize_label(label: str) -> str:
    """Collapse punctuation and whitespace for fuzzy comparison."""
    # Replace punctuation with spaces, then collapse whitespace
    normalized = re.sub(r"[^\w]", " ", label)
    return re.sub(r"\s+", " ", normalized).strip().lower()


def _similarity(a: str, b: str) -> float:
    """Compute similarity ratio between two normalized labels."""
    return SequenceMatcher(None, a, b).ratio()


def _token_overlap(a: str, b: str) -> float:
    """Jaccard similarity on whitespace/punctuation tokens."""
    tokens_a = set(re.split(r"[\s\W]+", a.lower()))
    tokens_b = set(re.split(r"[\s\W]+", b.lower()))
    tokens_a.discard("")
    tokens_b.discard("")
    if not tokens_a and not tokens_b:
        return 1.0
    if not tokens_a or not tokens_b:
        return 0.0
    intersection = tokens_a & tokens_b
    union = tokens_a | tokens_b
    return len(intersection) / len(union)


class Curator:
    """Automated knowledge graph curation.

    Operates in two modes:
      - active: performs merges, flags, and soft-deletes
      - observe: logs what it would do without making changes

    Configurable via constructor args or environment variables
    (KNOWLEDGE_CURATOR_MODE, KNOWLEDGE_CURATOR_FUZZY_THRESHOLD, etc.).
    """

    def __init__(
        self,
        store: KnowledgeStore,
        *,
        post_ingestion_threshold: float | None = None,
        fuzzy_merge_threshold: float | None = None,
        fuzzy_flag_threshold: float | None = None,
        orphan_age_hours: int | None = None,
        recovery_days: int | None = None,
        burst_multiplier: float | None = None,
        mode: str | None = None,
        observe_hours: int | None = None,
    ):
        self.store = store
        self.post_ingestion_threshold = post_ingestion_threshold or float(
            os.environ.get("KNOWLEDGE_CURATOR_POST_INGESTION_THRESHOLD", "0.90")
        )
        self.fuzzy_merge_threshold = fuzzy_merge_threshold or float(
            os.environ.get("KNOWLEDGE_CURATOR_FUZZY_THRESHOLD", "0.85")
        )
        self.fuzzy_flag_threshold = fuzzy_flag_threshold or float(
            os.environ.get("KNOWLEDGE_CURATOR_FLAG_THRESHOLD", "0.70")
        )
        self.orphan_age_hours = (
            orphan_age_hours if orphan_age_hours is not None
            else int(os.environ.get("KNOWLEDGE_CURATOR_ORPHAN_AGE_HOURS", "24"))
        )
        self.recovery_days = (
            recovery_days if recovery_days is not None
            else int(os.environ.get("KNOWLEDGE_CURATOR_RECOVERY_DAYS", "7"))
        )
        self.burst_multiplier = (
            burst_multiplier if burst_multiplier is not None
            else float(os.environ.get("KNOWLEDGE_CURATOR_BURST_MULTIPLIER", "3.0"))
        )
        self._mode = mode or os.environ.get("KNOWLEDGE_CURATOR_MODE", "auto")
        self.observe_hours = observe_hours or int(
            os.environ.get("KNOWLEDGE_CURATOR_OBSERVE_HOURS", "48")
        )
        self._observe_start: str | None = None
        self._last_scan_time: str | None = None
        self._transitioned_to_active: bool = False

    @property
    def is_observe_only(self) -> bool:
        """Check if curator is in observe-only mode.

        Returns True if mode is 'observe', or if mode is 'auto'
        and the observe period has not elapsed.
        """
        if self._mode == "observe":
            return True
        if self._mode in ("active", "disabled"):
            return False
        # auto mode: observe for observe_hours then switch to active
        if self._observe_start is None:
            self._observe_start = datetime.now(timezone.utc).strftime(
                "%Y-%m-%dT%H:%M:%SZ"
            )
            return True
        from datetime import timedelta

        start = datetime.strptime(self._observe_start, "%Y-%m-%dT%H:%M:%SZ").replace(
            tzinfo=timezone.utc
        )
        elapsed = datetime.now(timezone.utc) - start
        if elapsed >= timedelta(hours=self.observe_hours):
            if not self._transitioned_to_active:
                self._transitioned_to_active = True
                self.store.log_curation(
                    "mode_change", "__system__",
                    {"from": "observe", "to": "active",
                     "observe_hours": self.observe_hours},
                )
                logger.warning(
                    "Curator transitioning from observe to active mode "
                    "after %s hours", self.observe_hours
                )
            return False
        return True

    def post_ingestion_check(self, node_id: str) -> str | None:
        """Check a newly-ingested node for near-duplicates.

        Queries same-kind nodes (max 20), normalizes labels, compares
        similarity. If a match exceeds post_ingestion_threshold:
          - In active mode: merges the new node into the existing one
          - In observe mode: logs what would happen without merging

        Returns the surviving node ID if a merge occurred, None otherwise.
        """
        node = self.store.get_node(node_id)
        if not node:
            return None

        kind = node["kind"]
        normalized_new = _normalize_label(node["label"])

        # Get candidate nodes of the same kind
        candidates = self.store.find_nodes_by_kind(kind, limit=20)

        best_match = None
        best_similarity = 0.0

        for candidate in candidates:
            if candidate["id"] == node_id:
                continue
            # Skip nodes that are already merged or soft-deleted
            if candidate.get("curation_status") in ("merged", "soft_deleted"):
                continue

            normalized_candidate = _normalize_label(candidate["label"])
            sim = _similarity(normalized_new, normalized_candidate)

            if sim > best_similarity and sim >= self.post_ingestion_threshold:
                best_similarity = sim
                best_match = candidate

        if best_match is None:
            return None

        if self.is_observe_only:
            # Log what would happen without actually merging
            self.store.log_curation(
                "observe_merge",
                node_id,
                {
                    "would_merge_into": best_match["id"],
                    "similarity": round(best_similarity, 4),
                    "absorbed_label": node["label"],
                    "surviving_label": best_match["label"],
                },
            )
            return None

        # Active mode: perform the merge
        self._merge_nodes(node_id, best_match["id"], best_similarity)
        return best_match["id"]

    def _merge_nodes(
        self, absorbed_id: str, surviving_id: str, similarity: float
    ) -> None:
        """Merge absorbed node into surviving node.

        - Summary: higher _SOURCE_PRIORITY wins
        - Properties: surviving overwrites, then absorbed fills gaps
        - source_channels: union
        - Edges: repoint from absorbed to surviving, deduplicate parallel edges
        - Mark absorbed as merged
        - Log to curation_log
        """
        absorbed = self.store.get_node(absorbed_id)
        surviving = self.store.get_node(surviving_id)
        if not absorbed or not surviving:
            return

        # -- Merge summary: higher source priority wins --
        absorbed_priority = _SOURCE_PRIORITY.get(absorbed.get("source_type", "rule"), 0)
        surviving_priority = _SOURCE_PRIORITY.get(
            surviving.get("source_type", "rule"), 0
        )

        if absorbed_priority > surviving_priority and absorbed["summary"]:
            merged_summary = absorbed["summary"]
        else:
            merged_summary = surviving["summary"]

        # -- Merge properties: surviving base, absorbed fills gaps --
        surviving_props = json.loads(surviving.get("properties") or "{}")
        absorbed_props = json.loads(absorbed.get("properties") or "{}")
        merged_props = {**absorbed_props, **surviving_props}

        # -- Union source_channels --
        surviving_channels = set(
            json.loads(surviving.get("source_channels") or "[]")
        )
        absorbed_channels = set(
            json.loads(absorbed.get("source_channels") or "[]")
        )
        merged_channels = list(surviving_channels | absorbed_channels)

        # Update surviving node
        self.store.update_node(
            surviving_id,
            summary=merged_summary,
            properties=merged_props,
            source_channels=merged_channels,
        )

        # -- Repoint edges from absorbed to surviving --
        absorbed_edges = self.store.get_edges(absorbed_id, direction="both")

        # Collect existing edges on surviving to detect duplicates
        surviving_edges = self.store.get_edges(surviving_id, direction="both")
        surviving_edge_keys = set()
        for e in surviving_edges:
            key = (e["source_id"], e["target_id"], e["relation"])
            surviving_edge_keys.add(key)

        for edge in absorbed_edges:
            new_source = (
                surviving_id if edge["source_id"] == absorbed_id else edge["source_id"]
            )
            new_target = (
                surviving_id if edge["target_id"] == absorbed_id else edge["target_id"]
            )

            # Skip self-loops that would result from repointing
            if new_source == new_target:
                # Delete the original edge
                self.store._db.execute(
                    "DELETE FROM edges WHERE id = ?", (edge["id"],)
                )
                continue

            # Check for parallel edge (same source, target, relation)
            edge_key = (new_source, new_target, edge["relation"])
            if edge_key in surviving_edge_keys:
                # Duplicate — delete the absorbed edge
                self.store._db.execute(
                    "DELETE FROM edges WHERE id = ?", (edge["id"],)
                )
                continue

            # Repoint the edge
            self.store._db.execute(
                "UPDATE edges SET source_id = ?, target_id = ? WHERE id = ?",
                (new_source, new_target, edge["id"]),
            )
            surviving_edge_keys.add(edge_key)

        # -- Mark absorbed as merged --
        now = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
        self.store._db.execute(
            "UPDATE nodes SET curation_status = 'merged', "
            "curation_reason = 'near_duplicate', "
            "curation_at = ?, merged_into = ? WHERE id = ?",
            (now, surviving_id, absorbed_id),
        )

        # Single commit for entire merge (atomic)
        self.store._db.commit()

        # -- Log to curation_log --
        self.store.log_curation(
            "merge",
            absorbed_id,
            {
                "merged_into": surviving_id,
                "similarity": round(similarity, 4),
                "absorbed_label": absorbed["label"],
                "surviving_label": surviving["label"],
            },
        )

    def _channels_overlap(self, node_a: dict, node_b: dict) -> bool:
        """Check if two nodes' source_channels overlap, or either is empty."""
        channels_a = set(json.loads(node_a.get("source_channels") or "[]"))
        channels_b = set(json.loads(node_b.get("source_channels") or "[]"))
        if not channels_a or not channels_b:
            return True
        return bool(channels_a & channels_b)

    def _flag_node(self, node_id: str, reason: str) -> None:
        """Set curation_status='flagged' and log to curation_log."""
        now = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
        self.store._db.execute(
            "UPDATE nodes SET curation_status = 'flagged', "
            "curation_reason = ?, curation_at = ? WHERE id = ?",
            (reason, now, node_id),
        )
        self.store._db.commit()
        self.store.log_curation("flag", node_id, {"reason": reason})

    def fuzzy_duplicate_scan(self) -> dict:
        """Scan for fuzzy duplicates across all kinds.

        Uses token overlap as primary metric, SequenceMatcher as secondary
        for short labels (<=3 tokens). Thresholds:
          - >0.85: auto-merge (if channels overlap, else flag for Tenet 12)
          - 0.70-0.85: flag for review
        Skips pairs where both nodes predate _last_scan_time.
        """
        stats = {"scanned": 0, "merged": 0, "flagged": 0}

        # Get all distinct kinds
        rows = self.store._db.execute(
            "SELECT DISTINCT kind FROM nodes WHERE curation_status IS NULL"
        ).fetchall()
        kinds = [r[0] for r in rows]

        scan_start = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")

        for kind in kinds:
            nodes = self.store.find_nodes_by_kind(kind, limit=500)
            # Filter to active nodes only
            nodes = [
                n for n in nodes
                if n.get("curation_status") not in ("merged", "soft_deleted")
            ]
            stats["scanned"] += len(nodes)

            for i in range(len(nodes)):
                for j in range(i + 1, len(nodes)):
                    node_a = nodes[i]
                    node_b = nodes[j]

                    # Skip if either was already handled this scan
                    if node_a.get("curation_status") in ("merged", "soft_deleted"):
                        continue
                    if node_b.get("curation_status") in ("merged", "soft_deleted"):
                        continue

                    # Skip pairs where both predate last scan
                    if self._last_scan_time:
                        a_updated = node_a.get("updated_at", "")
                        b_updated = node_b.get("updated_at", "")
                        if a_updated < self._last_scan_time and b_updated < self._last_scan_time:
                            continue

                    label_a = _normalize_label(node_a["label"])
                    label_b = _normalize_label(node_b["label"])

                    # Primary metric: token overlap (Jaccard)
                    tokens_a = re.split(r"[\s\W]+", label_a)
                    tokens_a = [t for t in tokens_a if t]
                    sim = _token_overlap(label_a, label_b)

                    # Secondary metric for short labels: SequenceMatcher
                    if len(tokens_a) <= 3:
                        seq_sim = _similarity(label_a, label_b)
                        sim = max(sim, seq_sim)

                    if sim > self.fuzzy_merge_threshold:
                        if self._channels_overlap(node_a, node_b):
                            # Auto-merge
                            if self.is_observe_only:
                                self.store.log_curation(
                                    "observe_merge", node_b["id"],
                                    {"would_merge_into": node_a["id"],
                                     "similarity": round(sim, 4)},
                                )
                            else:
                                # Refresh node state before merge
                                fresh_b = self.store.get_node(node_b["id"])
                                if fresh_b and fresh_b.get("curation_status") not in ("merged", "soft_deleted"):
                                    self._merge_nodes(node_b["id"], node_a["id"], sim)
                                    node_b["curation_status"] = "merged"
                            stats["merged"] += 1
                        else:
                            # Cross-channel: flag for Tenet 12
                            if self.is_observe_only:
                                self.store.log_curation(
                                    "observe_flag", node_b["id"],
                                    {"reason": "cross_channel_duplicate_tenet_12",
                                     "similarity": round(sim, 4)},
                                )
                            else:
                                self._flag_node(
                                    node_b["id"],
                                    "cross_channel_duplicate_tenet_12",
                                )
                                node_b["curation_status"] = "flagged"
                            stats["flagged"] += 1
                    elif sim >= self.fuzzy_flag_threshold:
                        if self.is_observe_only:
                            self.store.log_curation(
                                "observe_flag", node_b["id"],
                                {"reason": "fuzzy_duplicate",
                                 "similarity": round(sim, 4)},
                            )
                        else:
                            self._flag_node(node_b["id"], "fuzzy_duplicate")
                            node_b["curation_status"] = "flagged"
                        stats["flagged"] += 1

        self._last_scan_time = scan_start
        return stats

    def _soft_delete_node(self, node_id: str, reason: str) -> None:
        """Soft-delete a node by setting curation_status='soft_deleted'."""
        now = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
        self.store._db.execute(
            "UPDATE nodes SET curation_status = 'soft_deleted', "
            "curation_reason = ?, curation_at = ? WHERE id = ?",
            (reason, now, node_id),
        )
        self.store._db.commit()
        self.store.log_curation("soft_delete", node_id, {"reason": reason})

    def orphan_pruning(self) -> dict:
        """Find and soft-delete orphan nodes.

        Orphans are nodes with zero edges, older than orphan_age_hours,
        with curation_status IS NULL. STRUCTURAL_KINDS are exempt.
        """
        from datetime import timedelta

        stats = {"pruned": 0, "exempt": 0}
        now = datetime.now(timezone.utc)
        cutoff = (now - timedelta(hours=self.orphan_age_hours)).strftime(
            "%Y-%m-%dT%H:%M:%SZ"
        )

        # Find nodes with no edges, not structural, not already curated
        structural_placeholders = ", ".join("?" for _ in STRUCTURAL_KINDS)
        rows = self.store._db.execute(
            f"SELECT n.* FROM nodes n "
            f"WHERE n.curation_status IS NULL "
            f"AND n.kind NOT IN ({structural_placeholders}) "
            f"AND n.created_at <= ? "
            f"AND n.id NOT IN (SELECT source_id FROM edges) "
            f"AND n.id NOT IN (SELECT target_id FROM edges)",
            (*STRUCTURAL_KINDS, cutoff),
        ).fetchall()

        for row in rows:
            node = dict(row)
            if self.is_observe_only:
                self.store.log_curation(
                    "observe_soft_delete", node["id"],
                    {"reason": "orphan", "label": node["label"]},
                )
            else:
                self._soft_delete_node(node["id"], "orphan")
            stats["pruned"] += 1

        return stats

    def cluster_analysis(self) -> dict:
        """Analyze node distribution across kinds.

        Flags any kind that exceeds 40% of the total node count.
        Returns dict with over_concentrated list and distribution.
        """
        rows = self.store._db.execute(
            "SELECT kind, COUNT(*) as cnt FROM nodes "
            "WHERE curation_status IS NULL OR curation_status = 'flagged' "
            "GROUP BY kind"
        ).fetchall()

        distribution = {r[0]: r[1] for r in rows}
        total = sum(distribution.values())

        over_concentrated = []
        if total > 0:
            for kind, count in distribution.items():
                if count / total > 0.40:
                    over_concentrated.append(kind)

        return {
            "over_concentrated": over_concentrated,
            "distribution": distribution,
            "total": total,
        }

    def anomaly_detection(self) -> dict:
        """Detect burst and dominance anomalies from agents.

        Burst: agent's recent contributions exceed burst_multiplier * historical avg.
        Dominance: agent contributed >50% of all new nodes in the last hour.
        Agents with <4 hours history are exempt from burst.
        Windows with <5 total nodes are exempt from dominance.
        """
        from datetime import timedelta

        stats = {"checked": 0, "flagged": 0, "alerts": []}
        now = datetime.now(timezone.utc)
        one_hour_ago = (now - timedelta(hours=1)).strftime("%Y-%m-%dT%H:%M:%SZ")
        four_hours_ago = (now - timedelta(hours=4)).strftime("%Y-%m-%dT%H:%M:%SZ")

        # Get all agent nodes
        agents = self.store.find_nodes_by_kind("agent", limit=500)
        agents = [a for a in agents if a.get("curation_status") not in ("merged", "soft_deleted")]

        # Count total recent nodes (last hour) for dominance check
        total_recent = self.store._db.execute(
            "SELECT COUNT(*) FROM nodes WHERE created_at >= ?",
            (one_hour_ago,),
        ).fetchone()[0]

        for agent in agents:
            stats["checked"] += 1
            agent_id = agent["id"]

            # Count contributions via edges
            total_contributions = self.store._db.execute(
                "SELECT COUNT(*) FROM edges WHERE source_id = ? AND relation = 'contributed'",
                (agent_id,),
            ).fetchone()[0]

            recent_contributions = self.store._db.execute(
                "SELECT COUNT(*) FROM edges e "
                "JOIN nodes n ON e.target_id = n.id "
                "WHERE e.source_id = ? AND e.relation = 'contributed' "
                "AND n.created_at >= ?",
                (agent_id, one_hour_ago),
            ).fetchone()[0]

            # Check agent history length (exempt if <4 hours)
            has_old_contributions = self.store._db.execute(
                "SELECT COUNT(*) FROM edges e "
                "JOIN nodes n ON e.target_id = n.id "
                "WHERE e.source_id = ? AND e.relation = 'contributed' "
                "AND n.created_at < ?",
                (agent_id, four_hours_ago),
            ).fetchone()[0]

            # Burst detection
            if has_old_contributions > 0 and total_contributions > 0:
                old_contributions = total_contributions - recent_contributions
                if old_contributions > 0:
                    avg_rate = old_contributions  # simplified historical avg
                    if recent_contributions > self.burst_multiplier * avg_rate:
                        if self.is_observe_only:
                            self.store.log_curation(
                                "observe_flag", agent_id,
                                {"reason": "burst_anomaly",
                                 "recent": recent_contributions,
                                 "historical": old_contributions},
                            )
                        else:
                            self._flag_node(agent_id, "burst_anomaly")
                        stats["flagged"] += 1
                        stats["alerts"].append({
                            "agent": agent["label"],
                            "type": "burst",
                            "recent": recent_contributions,
                            "historical_avg": avg_rate,
                        })

            # Dominance detection
            if total_recent >= 5 and recent_contributions > 0:
                dominance_ratio = recent_contributions / total_recent
                if dominance_ratio > 0.50:
                    if self.is_observe_only:
                        self.store.log_curation(
                            "observe_flag", agent_id,
                            {"reason": "dominance_anomaly",
                             "ratio": round(dominance_ratio, 2)},
                        )
                    else:
                        # Only flag if not already flagged
                        fresh = self.store.get_node(agent_id)
                        if fresh and fresh.get("curation_status") != "flagged":
                            self._flag_node(agent_id, "dominance_anomaly")
                    stats["flagged"] += 1
                    stats["alerts"].append({
                        "agent": agent["label"],
                        "type": "dominance",
                        "ratio": round(dominance_ratio, 2),
                    })

        return stats

    def compute_health_metrics(self) -> dict:
        """Compute knowledge graph health metrics.

        Returns orphan_ratio, duplicate_density, cluster_distribution,
        cluster_gini, flagged_count, soft_deleted_count, total_nodes,
        total_edges, last_cycle. Stores in curation_log as 'metrics' action.
        """
        total_nodes = self.store._db.execute(
            "SELECT COUNT(*) FROM nodes WHERE curation_status IS NULL "
            "OR curation_status = 'flagged'"
        ).fetchone()[0]

        total_edges = self.store._db.execute(
            "SELECT COUNT(*) FROM edges"
        ).fetchone()[0]

        # Orphan count (nodes with no edges, excluding structural)
        structural_placeholders = ", ".join("?" for _ in STRUCTURAL_KINDS)
        orphan_count = self.store._db.execute(
            f"SELECT COUNT(*) FROM nodes n "
            f"WHERE (n.curation_status IS NULL OR n.curation_status = 'flagged') "
            f"AND n.kind NOT IN ({structural_placeholders}) "
            f"AND n.id NOT IN (SELECT source_id FROM edges) "
            f"AND n.id NOT IN (SELECT target_id FROM edges)",
            (*STRUCTURAL_KINDS,),
        ).fetchone()[0]

        orphan_ratio = orphan_count / total_nodes if total_nodes > 0 else 0.0

        # Duplicate density (merges in last 24h / total nodes)
        from datetime import timedelta
        twenty_four_ago = (datetime.now(timezone.utc) - timedelta(hours=24)).strftime(
            "%Y-%m-%dT%H:%M:%SZ"
        )
        recent_merges = self.store._db.execute(
            "SELECT COUNT(*) FROM curation_log WHERE action = 'merge' AND timestamp >= ?",
            (twenty_four_ago,),
        ).fetchone()[0]
        duplicate_density = recent_merges / total_nodes if total_nodes > 0 else 0.0

        # Cluster distribution
        rows = self.store._db.execute(
            "SELECT kind, COUNT(*) as cnt FROM nodes "
            "WHERE curation_status IS NULL OR curation_status = 'flagged' "
            "GROUP BY kind"
        ).fetchall()
        cluster_distribution = {r[0]: r[1] for r in rows}

        # Gini coefficient for cluster distribution
        counts = sorted(cluster_distribution.values())
        n = len(counts)
        if n == 0 or sum(counts) == 0:
            cluster_gini = 0.0
        else:
            total_sum = sum(counts)
            cumulative = 0.0
            weighted_sum = 0.0
            for i, c in enumerate(counts):
                cumulative += c
                weighted_sum += (2 * (i + 1) - n - 1) * c
            cluster_gini = weighted_sum / (n * total_sum)

        flagged_count = self.store._db.execute(
            "SELECT COUNT(*) FROM nodes WHERE curation_status = 'flagged'"
        ).fetchone()[0]

        soft_deleted_count = self.store._db.execute(
            "SELECT COUNT(*) FROM nodes WHERE curation_status = 'soft_deleted'"
        ).fetchone()[0]

        now = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")

        metrics = {
            "orphan_ratio": round(orphan_ratio, 4),
            "duplicate_density": round(duplicate_density, 4),
            "cluster_distribution": cluster_distribution,
            "cluster_gini": round(cluster_gini, 4),
            "flagged_count": flagged_count,
            "soft_deleted_count": soft_deleted_count,
            "total_nodes": total_nodes,
            "total_edges": total_edges,
            "last_cycle": now,
        }

        # Log metrics to curation_log
        self.store.log_curation("metrics", "__system__", metrics)

        return metrics

    def hard_delete_cleanup(self) -> dict:
        """Hard-delete nodes that have been soft-deleted past recovery_days.

        For each expired node: logs hard_delete, deletes edges,
        clears merged_into references, deletes node and FTS entry.
        """
        from datetime import timedelta

        stats = {"hard_deleted": 0}
        now = datetime.now(timezone.utc)
        cutoff = (now - timedelta(days=self.recovery_days)).strftime(
            "%Y-%m-%dT%H:%M:%SZ"
        )

        rows = self.store._db.execute(
            "SELECT * FROM nodes WHERE curation_status = 'soft_deleted' "
            "AND curation_at <= ?",
            (cutoff,),
        ).fetchall()

        for row in rows:
            node = dict(row)
            node_id = node["id"]

            # Log before deletion
            self.store.log_curation(
                "hard_delete", node_id,
                {"label": node["label"], "kind": node["kind"]},
            )

            # Delete edges referencing this node
            self.store._db.execute(
                "DELETE FROM edges WHERE source_id = ? OR target_id = ?",
                (node_id, node_id),
            )

            # Clear merged_into references pointing to this node
            self.store._db.execute(
                "UPDATE nodes SET merged_into = NULL WHERE merged_into = ?",
                (node_id,),
            )

            # Delete FTS entry
            self.store._db.execute(
                "DELETE FROM nodes_fts WHERE id = ?", (node_id,)
            )

            # Delete the node
            self.store._db.execute(
                "DELETE FROM nodes WHERE id = ?", (node_id,)
            )

            stats["hard_deleted"] += 1

        self.store._db.commit()
        return stats


class CurationLoop:
    """Async background loop that runs periodic curation operations."""

    def __init__(self, curator: Curator, interval_seconds: int | float = 600):
        self.curator = curator
        self.interval = interval_seconds

    async def run(self) -> None:
        logger.info("Curation loop started (interval=%ss, mode=%s)", self.interval, self.curator._mode)
        while True:
            try:
                self._run_cycle()
            except asyncio.CancelledError:
                raise
            except Exception as e:
                logger.error("Curation cycle error: %s", e)
            await asyncio.sleep(self.interval)

    def _run_cycle(self) -> None:
        operations = [
            ("fuzzy_duplicate_scan", self.curator.fuzzy_duplicate_scan),
            ("orphan_pruning", self.curator.orphan_pruning),
            ("cluster_analysis", self.curator.cluster_analysis),
            ("anomaly_detection", self.curator.anomaly_detection),
            ("hard_delete_cleanup", self.curator.hard_delete_cleanup),
            ("compute_health_metrics", self.curator.compute_health_metrics),
        ]
        for name, op in operations:
            try:
                op()
            except Exception as e:
                logger.error("Curation operation %s failed: %s", name, e)
