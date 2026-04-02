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

from typing import Optional, Union
from .store import KnowledgeStore, STRUCTURAL_KINDS, _SOURCE_PRIORITY


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
        post_ingestion_threshold: Optional[float] = None,
        fuzzy_merge_threshold: Optional[float] = None,
        fuzzy_flag_threshold: Optional[float] = None,
        orphan_age_hours: Optional[int] = None,
        recovery_days: Optional[int] = None,
        burst_multiplier: Optional[float] = None,
        mode: Optional[str] = None,
        observe_hours: Optional[int] = None,
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
        self._observe_start: Optional[str] = None
        self._last_scan_time: Optional[str] = None
        self._transitioned_to_active: bool = False
        self._ontology: Optional[dict] = None

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

    def post_ingestion_check(self, node_id: str) -> Optional[str]:
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

    def _upsert_candidate(
        self,
        candidate_type: str,
        value: str,
        occurrence_count: int,
        source_count: int,
        example_labels: list,
        now: str,
    ) -> None:
        """Insert or update an OntologyCandidate node.

        For new candidates: calls store.add_node() to create with full properties.
        For existing candidates: updates occurrence_count, source_count, last_updated
        via direct SQL. Rejected candidates are re-surfaced only when
        occurrence_count > 2 * rejection_count_at.
        """
        label = f"candidate:{value}"
        existing_rows = self.store._db.execute(
            "SELECT * FROM nodes WHERE kind = 'OntologyCandidate' AND label = ?",
            (label,),
        ).fetchall()

        if existing_rows:
            existing = dict(existing_rows[0])
            props = json.loads(existing.get("properties") or "{}")
            status = props.get("status", "candidate")
            rejection_count_at = props.get("rejection_count_at")

            if status == "rejected" and rejection_count_at is not None:
                # Only re-surface if occurrence_count > 2 * rejection_count_at
                if occurrence_count <= 2 * rejection_count_at:
                    # Update counts but keep rejected status
                    props["occurrence_count"] = occurrence_count
                    props["source_count"] = source_count
                    props["last_updated"] = now
                    self.store._db.execute(
                        "UPDATE nodes SET properties=?, updated_at=? WHERE id=?",
                        (json.dumps(props), now, existing["id"]),
                    )
                    self.store._db.commit()
                    return
                else:
                    # Re-surface: reset to candidate
                    props["status"] = "candidate"
                    props["rejection_count_at"] = None

            props["occurrence_count"] = occurrence_count
            props["source_count"] = source_count
            props["last_updated"] = now
            self.store._db.execute(
                "UPDATE nodes SET properties=?, updated_at=? WHERE id=?",
                (json.dumps(props), now, existing["id"]),
            )
            self.store._db.commit()
        else:
            properties = {
                "candidate_type": candidate_type,
                "value": value,
                "occurrence_count": occurrence_count,
                "source_count": source_count,
                "example_labels": example_labels[:5],
                "first_seen": now,
                "last_updated": now,
                "status": "candidate",
                "rejection_count_at": None,
            }
            self.store.add_node(
                label=label,
                kind="OntologyCandidate",
                source_type="rule",
                properties=properties,
            )

    def emergence_scan(self) -> dict:
        """Scan the knowledge graph for novel kind and relation values that appear
        frequently enough to be candidates for ontology promotion.

        Upserts OntologyCandidate nodes into the graph for qualifying values.
        Thresholds are read from environment variables:
          - ONTOLOGY_CANDIDATE_NODE_THRESHOLD (default 10)
          - ONTOLOGY_CANDIDATE_EDGE_THRESHOLD (default 10)
          - ONTOLOGY_CANDIDATE_MIN_SOURCES (default 3)

        Filters out values already in the loaded ontology (self._ontology).
        Skips re-surfacing rejected candidates unless occurrence_count > 2 * rejection_count_at.

        Returns dict with kind_candidates and relation_candidates counts.
        """
        node_threshold = int(os.environ.get("ONTOLOGY_CANDIDATE_NODE_THRESHOLD", "10"))
        edge_threshold = int(os.environ.get("ONTOLOGY_CANDIDATE_EDGE_THRESHOLD", "10"))
        min_sources = int(os.environ.get("ONTOLOGY_CANDIDATE_MIN_SOURCES", "3"))

        # Determine known entity types and relationship types from loaded ontology
        known_entity_types: set = set()
        known_relation_types: set = set()
        if self._ontology:
            known_entity_types = set(self._ontology.get("entity_types") or [])
            known_relation_types = set(self._ontology.get("relationship_types") or [])

        now = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
        kind_candidates_count = 0
        relation_candidates_count = 0

        # --- Kind candidates ---
        # source_channels is a JSON array, so use json_each() to count distinct channels
        kind_rows = self.store._db.execute(
            """
            SELECT kind, COUNT(*) as cnt, COUNT(DISTINCT jc.value) as src_cnt
            FROM nodes, json_each(nodes.source_channels) as jc
            WHERE kind != 'OntologyCandidate'
            GROUP BY kind
            HAVING cnt >= :node_threshold AND src_cnt >= :min_sources
            """,
            {"node_threshold": node_threshold, "min_sources": min_sources},
        ).fetchall()

        for row in kind_rows:
            kind = row[0]
            cnt = row[1]
            src_cnt = row[2]

            # Skip kinds already in ontology
            if kind in known_entity_types:
                continue

            # Gather up to 5 example labels for this kind
            example_rows = self.store._db.execute(
                "SELECT label FROM nodes WHERE kind = ? LIMIT 5",
                (kind,),
            ).fetchall()
            example_labels = [r[0] for r in example_rows]

            self._upsert_candidate(
                candidate_type="kind",
                value=kind,
                occurrence_count=cnt,
                source_count=src_cnt,
                example_labels=example_labels,
                now=now,
            )
            kind_candidates_count += 1

        # --- Relation candidates ---
        # source_channel on edges is a plain string
        rel_rows = self.store._db.execute(
            """
            SELECT relation, COUNT(*) as cnt, COUNT(DISTINCT source_channel) as src_cnt
            FROM edges
            GROUP BY relation
            HAVING cnt >= :edge_threshold AND src_cnt >= :min_sources
            """,
            {"edge_threshold": edge_threshold, "min_sources": min_sources},
        ).fetchall()

        for row in rel_rows:
            relation = row[0]
            cnt = row[1]
            src_cnt = row[2]

            # Skip relations already in ontology
            if relation in known_relation_types:
                continue

            self._upsert_candidate(
                candidate_type="relation",
                value=relation,
                occurrence_count=cnt,
                source_count=src_cnt,
                example_labels=[],
                now=now,
            )
            relation_candidates_count += 1

        # Log completion
        self.store.log_curation(
            "emergence_scan",
            "__system__",
            {
                "kind_candidates": kind_candidates_count,
                "relation_candidates": relation_candidates_count,
                "node_threshold": node_threshold,
                "edge_threshold": edge_threshold,
                "min_sources": min_sources,
                "timestamp": now,
            },
        )

        logger.info(
            "Emergence scan complete: %d kind candidates, %d relation candidates",
            kind_candidates_count,
            relation_candidates_count,
        )

        return {
            "kind_candidates": kind_candidates_count,
            "relation_candidates": relation_candidates_count,
        }

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

    # ── Relationship Inference ──────────────────────────────────────────

    # Properties to exclude from overlap matching (non-semantic metadata)
    _TRIVIAL_PROPERTIES = {
        "source", "source_type", "_provenance_connector", "_provenance_work_item",
        "_ontology_version", "_migrated", "_original_kind", "true", "false", "",
    }

    def relationship_inference(self) -> dict:
        """Three-tier relationship inference.

        Tier 1: explicit rules from inference-rules.yaml — auto-create edges
        Tier 2: automatic property overlap — propose RelationshipCandidate nodes
        Tier 3: semantic similarity via embeddings — propose candidates
        """
        stats = {"tier1_created": 0, "tier2_proposed": 0, "tier3_proposed": 0}

        # Tier 1: explicit rules
        rules = self._load_inference_rules()
        for rule in rules:
            stats["tier1_created"] += self._apply_inference_rule(rule)

        # Tier 2: property overlap for orphan nodes
        stats["tier2_proposed"] += self._scan_property_overlap()

        # Tier 3: semantic similarity (if embeddings available)
        if self.store._vec_available:
            stats["tier3_proposed"] += self._scan_semantic_similarity()

        logger.info(
            "relationship_inference: tier1=%d created, tier2=%d proposed, tier3=%d proposed",
            stats["tier1_created"], stats["tier2_proposed"], stats["tier3_proposed"],
        )
        return stats

    def _load_inference_rules(self) -> list:
        """Load explicit inference rules from inference-rules.yaml."""
        import yaml as _yaml

        paths = [
            os.path.join(self.store.data_dir, "inference-rules.yaml"),
            os.path.join(os.environ.get("KNOWLEDGE_DATA_DIR", "/app/data"), "inference-rules.yaml"),
            os.path.join(os.environ.get("KNOWLEDGE_ONTOLOGY_DIR", "/app"), "inference-rules.yaml"),
        ]
        for path in paths:
            if os.path.exists(path):
                try:
                    with open(path) as f:
                        data = _yaml.safe_load(f)
                    return data.get("inference_rules", []) if data else []
                except Exception as e:
                    logger.warning("Failed to load inference rules from %s: %s", path, e)
        return []

    def _apply_inference_rule(self, rule: dict) -> int:
        """Apply a single explicit inference rule. Returns count of edges created."""
        match_prop = rule.get("match_property")
        from_kinds = rule.get("from_kinds", [])
        to_kinds = rule.get("to_kinds", [])
        relation = rule.get("relation", "ASSOCIATED_WITH")
        match_to_prop = rule.get("match_to_property", match_prop)
        if not match_prop or not from_kinds or not to_kinds:
            return 0

        created = 0
        from_placeholders = ",".join("?" for _ in from_kinds)
        to_placeholders = ",".join("?" for _ in to_kinds)

        # Find source nodes that have the match property
        source_rows = self.store._db.execute(
            f"SELECT id, label, kind, properties FROM nodes "
            f"WHERE kind IN ({from_placeholders}) "
            f"AND (merged_into IS NULL OR merged_into = '') "
            f"AND curation_status IS NULL",
            from_kinds,
        ).fetchall()

        for src_row in source_rows:
            src = dict(src_row)
            try:
                props = json.loads(src["properties"]) if src["properties"] else {}
            except (json.JSONDecodeError, TypeError):
                continue
            value = props.get(match_prop)
            if not value or str(value).strip() == "":
                continue

            # Find target nodes with matching property value
            # Use match_to_property if specified (allows cross-property matching)
            if match_to_prop == "label":
                # Match against node label
                targets = self.store._db.execute(
                    f"SELECT id, label, kind FROM nodes "
                    f"WHERE kind IN ({to_placeholders}) AND label = ? "
                    f"AND id != ? AND (merged_into IS NULL OR merged_into = '')",
                    (*to_kinds, str(value), src["id"]),
                ).fetchall()
            else:
                # Match against a property value via json_extract
                targets = self.store._db.execute(
                    f"SELECT id, label, kind FROM nodes "
                    f"WHERE kind IN ({to_placeholders}) "
                    f"AND json_extract(properties, '$.{match_to_prop}') = ? "
                    f"AND id != ? AND (merged_into IS NULL OR merged_into = '')",
                    (*to_kinds, str(value), src["id"]),
                ).fetchall()

            for tgt in targets:
                tgt = dict(tgt)
                # Check if edge already exists
                existing = self.store._db.execute(
                    "SELECT 1 FROM edges WHERE source_id = ? AND target_id = ? AND relation = ?",
                    (src["id"], tgt["id"], relation),
                ).fetchone()
                if existing:
                    continue

                if self.is_observe_only:
                    self.store.log_curation("observe_infer_edge", src["id"], {
                        "target_id": tgt["id"], "relation": relation,
                        "match_property": match_prop, "match_value": str(value),
                    })
                else:
                    self.store.add_edge(
                        source_id=src["id"], target_id=tgt["id"],
                        relation=relation, properties={"source_type": "inferred"},
                    )
                    self.store.log_curation("infer_edge", src["id"], {
                        "target_id": tgt["id"], "relation": relation,
                        "match_property": match_prop, "match_value": str(value),
                    })
                created += 1
                if created >= 100:
                    return created

        return created

    def _scan_property_overlap(self) -> int:
        """Tier 2: find orphan nodes with 2+ matching property values on other nodes."""
        proposed = 0

        # Get orphan nodes (no edges, not structural, not already curated)
        structural_placeholders = ",".join("?" for _ in STRUCTURAL_KINDS)
        orphans = self.store._db.execute(
            f"SELECT id, label, kind, properties FROM nodes "
            f"WHERE kind NOT IN ({structural_placeholders}, 'OntologyCandidate', 'RelationshipCandidate') "
            f"AND (merged_into IS NULL OR merged_into = '') "
            f"AND curation_status IS NULL "
            f"AND id NOT IN (SELECT source_id FROM edges) "
            f"AND id NOT IN (SELECT target_id FROM edges)",
            tuple(STRUCTURAL_KINDS),
        ).fetchall()

        # Build property index for non-orphan nodes
        connected = self.store._db.execute(
            f"SELECT id, label, kind, properties FROM nodes "
            f"WHERE (merged_into IS NULL OR merged_into = '') "
            f"AND (id IN (SELECT source_id FROM edges) OR id IN (SELECT target_id FROM edges))",
        ).fetchall()

        # Index: property_value -> [(node_id, kind, label, property_name)]
        prop_index: dict[str, list[tuple[str, str, str, str]]] = {}
        for row in connected:
            node = dict(row)
            try:
                props = json.loads(node["properties"]) if node["properties"] else {}
            except (json.JSONDecodeError, TypeError):
                continue
            for k, v in props.items():
                v_str = str(v).strip()
                if k in self._TRIVIAL_PROPERTIES or v_str in self._TRIVIAL_PROPERTIES or len(v_str) < 3:
                    continue
                prop_index.setdefault(v_str, []).append((node["id"], node["kind"], node["label"], k))

        # Check suppression list
        suppressed = set()
        sup_rows = self.store._db.execute(
            "SELECT label FROM nodes WHERE kind = 'RelationshipCandidate' "
            "AND curation_status = 'soft_deleted'"
        ).fetchall()
        for r in sup_rows:
            suppressed.add(dict(r)["label"])

        for row in orphans:
            orphan = dict(row)
            try:
                oprops = json.loads(orphan["properties"]) if orphan["properties"] else {}
            except (json.JSONDecodeError, TypeError):
                continue

            # Find connected nodes that share property values
            matches: dict[str, list[str]] = {}  # target_id -> [matching_property_names]
            for k, v in oprops.items():
                v_str = str(v).strip()
                if k in self._TRIVIAL_PROPERTIES or v_str in self._TRIVIAL_PROPERTIES or len(v_str) < 3:
                    continue
                for (tid, tkind, tlabel, tprop) in prop_index.get(v_str, []):
                    if tid == orphan["id"] or tkind == orphan["kind"]:
                        continue
                    matches.setdefault(tid, []).append(f"{k}={v_str}")

            # Propose candidates with 2+ matching properties
            for tid, matching in matches.items():
                if len(matching) < 2:
                    continue
                tnode = next((dict(r) for r in connected if dict(r)["id"] == tid), None)
                if not tnode:
                    continue

                candidate_label = f"{orphan['kind']}:{orphan['label']} ↔ {tnode['kind']}:{tnode['label']}"
                if candidate_label in suppressed:
                    continue

                # Check if candidate already exists
                existing = self.store._db.execute(
                    "SELECT 1 FROM nodes WHERE kind = 'RelationshipCandidate' AND label = ?",
                    (candidate_label,),
                ).fetchone()
                if existing:
                    continue

                self.store.add_node(
                    label=candidate_label,
                    kind="RelationshipCandidate",
                    source_type="rule",
                    properties={
                        "from_id": orphan["id"],
                        "from_label": orphan["label"],
                        "from_kind": orphan["kind"],
                        "to_id": tid,
                        "to_label": tnode["label"],
                        "to_kind": tnode["kind"],
                        "matching_properties": ", ".join(matching),
                        "suggested_relation": "ASSOCIATED_WITH",
                        "status": "candidate",
                    },
                )
                proposed += 1
                if proposed >= 50:
                    return proposed

        return proposed

    def _scan_semantic_similarity(self) -> int:
        """Tier 3: find orphan nodes with high vector similarity to connected nodes."""
        proposed = 0

        # Get orphan node IDs
        orphan_rows = self.store._db.execute(
            "SELECT id, label, kind FROM nodes "
            "WHERE (merged_into IS NULL OR merged_into = '') "
            "AND curation_status IS NULL "
            "AND kind NOT IN ('OntologyCandidate', 'RelationshipCandidate') "
            "AND id NOT IN (SELECT source_id FROM edges) "
            "AND id NOT IN (SELECT target_id FROM edges)"
        ).fetchall()

        for row in orphan_rows:
            orphan = dict(row)
            try:
                similar = self.store.find_similar(orphan["id"], limit=3)
            except Exception:
                continue

            for sim in similar:
                if sim.get("kind") == orphan["kind"]:
                    continue  # Skip same-kind matches
                # Similarity is in the result — check threshold
                # find_similar returns nodes sorted by similarity, so top results are most similar

                candidate_label = f"{orphan['kind']}:{orphan['label']} ~ {sim['kind']}:{sim['label']}"

                existing = self.store._db.execute(
                    "SELECT 1 FROM nodes WHERE kind = 'RelationshipCandidate' AND label = ?",
                    (candidate_label,),
                ).fetchone()
                if existing:
                    continue

                self.store.add_node(
                    label=candidate_label,
                    kind="RelationshipCandidate",
                    source_type="rule",
                    properties={
                        "from_id": orphan["id"],
                        "from_label": orphan["label"],
                        "from_kind": orphan["kind"],
                        "to_id": sim.get("id", ""),
                        "to_label": sim["label"],
                        "to_kind": sim["kind"],
                        "suggested_relation": "SIMILAR_TO",
                        "status": "candidate",
                        "inference_tier": "semantic",
                    },
                )
                proposed += 1
                if proposed >= 20:
                    return proposed

        return proposed


class CurationLoop:
    """Async background loop that runs periodic curation operations."""

    def __init__(self, curator: Curator, interval_seconds: Union[int, float] = 600):
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
            ("emergence_scan", self.curator.emergence_scan),
            ("relationship_inference", self.curator.relationship_inference),
        ]
        for name, op in operations:
            try:
                op()
            except Exception as e:
                logger.error("Curation operation %s failed: %s", name, e)
