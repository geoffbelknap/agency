"""Knowledge graph store with flexible schema.

SQLite backend with adjacency tables for nodes and edges.
Free-form kind/relation labels — the LLM decides what entity
types and relationship types to use. FTS5 for text search on
node labels and summaries.

Schema is designed for clean export to external graph DBs.
"""

import json
import logging
import sqlite3
import time
import uuid
from datetime import datetime, timezone
from pathlib import Path
from typing import Optional

logger = logging.getLogger("agency.knowledge.store")

STRUCTURAL_KINDS = {"agent", "channel", "task", "OntologyCandidate", "RelationshipCandidate"}

_SOURCE_PRIORITY = {"agent": 3, "llm": 2, "local": 1, "rule": 1}

# Org-structural kinds require operator review before being committed to the
# graph. A compromised agent could inject false leadership/team data that
# propagates to other agents' system prompts via /org-context.
ORG_STRUCTURAL_KINDS = {"team", "department", "escalation-path", "leadership"}


class KnowledgeStore:
    def __init__(self, data_dir: Path):
        self.data_dir = Path(data_dir)
        self.data_dir.mkdir(parents=True, exist_ok=True)
        db_path = self.data_dir / "knowledge.db"
        self._db = sqlite3.connect(str(db_path))
        self._db.execute("PRAGMA journal_mode=WAL")
        self._db.row_factory = sqlite3.Row
        self._db.execute("PRAGMA foreign_keys = ON")
        self._init_schema()

        # --- sqlite-vec: graceful load ---
        self._vec_available = False
        try:
            import sqlite_vec
            self._db.enable_load_extension(True)
            sqlite_vec.load(self._db)
            self._db.enable_load_extension(False)
            self._vec_available = True
        except (ImportError, Exception) as e:
            logger.warning("sqlite-vec not available: %s — vector search disabled", e)

        # --- Embedding provider ---
        from .embedding import create_provider, get_embeddable_kinds
        self._embedding_provider = create_provider()
        self._embeddable_kinds = get_embeddable_kinds()

        # --- Create vec0 virtual table if possible ---
        if self._vec_available and self._embedding_provider.dimensions > 0:
            try:
                dims = self._embedding_provider.dimensions
                self._db.execute(
                    f"CREATE VIRTUAL TABLE IF NOT EXISTS nodes_vec USING vec0("
                    f"id TEXT PRIMARY KEY, embedding float[{dims}])"
                )
                self._db.commit()
            except Exception as e:
                logger.warning("Failed to create nodes_vec table: %s", e)
                self._vec_available = False

    def _init_schema(self) -> None:
        self._db.executescript("""
            CREATE TABLE IF NOT EXISTS nodes (
                id TEXT PRIMARY KEY,
                label TEXT NOT NULL,
                kind TEXT NOT NULL,
                summary TEXT DEFAULT '',
                properties TEXT DEFAULT '{}',
                source_type TEXT DEFAULT 'rule',
                source_channels TEXT DEFAULT '[]',
                created_at TEXT NOT NULL,
                updated_at TEXT NOT NULL
            );

            CREATE TABLE IF NOT EXISTS edges (
                id TEXT PRIMARY KEY,
                source_id TEXT NOT NULL REFERENCES nodes(id),
                target_id TEXT NOT NULL REFERENCES nodes(id),
                relation TEXT NOT NULL,
                weight REAL DEFAULT 1.0,
                properties TEXT DEFAULT '{}',
                source_channel TEXT DEFAULT '',
                provenance_id TEXT DEFAULT '',
                timestamp TEXT NOT NULL
            );

            CREATE INDEX IF NOT EXISTS idx_edges_source ON edges(source_id);
            CREATE INDEX IF NOT EXISTS idx_edges_target ON edges(target_id);
            CREATE INDEX IF NOT EXISTS idx_nodes_kind ON nodes(kind);
            CREATE INDEX IF NOT EXISTS idx_nodes_label ON nodes(label);
        """)
        # FTS5 for text search on labels and summaries
        try:
            self._db.execute(
                "CREATE VIRTUAL TABLE IF NOT EXISTS nodes_fts USING fts5("
                "id, label, summary, tokenize='porter')"
            )
        except sqlite3.OperationalError:
            pass  # Already exists
        # Curation columns on nodes (idempotent ALTER TABLE)
        for col in ("curation_status TEXT", "curation_reason TEXT",
                     "curation_at TEXT", "merged_into TEXT"):
            try:
                self._db.execute(f"ALTER TABLE nodes ADD COLUMN {col}")
            except sqlite3.OperationalError:
                pass  # Column already exists
        # Curation log table
        self._db.execute("""
            CREATE TABLE IF NOT EXISTS curation_log (
                id TEXT PRIMARY KEY,
                action TEXT NOT NULL,
                node_id TEXT NOT NULL,
                detail TEXT DEFAULT '{}',
                timestamp TEXT NOT NULL
            )
        """)
        self._db.execute(
            "CREATE INDEX IF NOT EXISTS idx_curation_log_node ON curation_log(node_id)"
        )
        self._db.execute(
            "CREATE INDEX IF NOT EXISTS idx_curation_log_action ON curation_log(action)"
        )
        # Pending nodes: org-structural contributions held for operator review
        self._db.execute("""
            CREATE TABLE IF NOT EXISTS pending_nodes (
                id TEXT PRIMARY KEY,
                label TEXT NOT NULL,
                kind TEXT NOT NULL,
                summary TEXT DEFAULT '',
                properties TEXT DEFAULT '{}',
                source_agent TEXT DEFAULT '',
                submitted_at TEXT NOT NULL
            )
        """)
        self._db.commit()

    def _generate_embedding(self, node_id: str, kind: str, label: str, summary: str) -> None:
        """Generate and store embedding for a node. Best-effort — never raises."""
        if kind.lower() not in self._embeddable_kinds:
            return
        if self._embedding_provider.dimensions == 0:
            return
        if not self._vec_available:
            return
        try:
            text = f"{label}: {summary}"[:2048]
            vector = self._embedding_provider.embed(text)
            if vector:
                self._db.execute(
                    "INSERT OR REPLACE INTO nodes_vec(id, embedding) VALUES (?, ?)",
                    (node_id, json.dumps(vector)),
                )
                self._db.commit()
        except Exception as e:
            logger.warning("Embedding generation failed for %s: %s", node_id, e)

    def add_node(
        self,
        label: str,
        kind: str,
        summary: str = "",
        properties: Optional[dict] = None,
        source_type: str = "rule",
        source_channels: Optional[list[str]] = None,
    ) -> str:
        """Add or merge a node. Deduplicates by (label, kind) case-insensitively.

        When a node with the same label+kind already exists:
          - Higher source_type priority (agent > llm > rule) wins for summary
          - Properties are merged (new values overwrite existing for same key)
          - source_channels are unioned
        """
        now = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")

        existing_row = self._db.execute(
            "SELECT * FROM nodes WHERE LOWER(label) = LOWER(?) AND kind = ?",
            (label, kind),
        ).fetchone()

        if existing_row:
            existing = dict(existing_row)
            node_id = existing["id"]

            existing_priority = _SOURCE_PRIORITY.get(existing["source_type"], 0)
            new_priority = _SOURCE_PRIORITY.get(source_type, 0)

            # Use new summary if it comes from a higher-priority source, or is
            # longer when priorities are equal and the new one is non-empty.
            if summary and (
                new_priority > existing_priority
                or (new_priority == existing_priority and len(summary) > len(existing["summary"]))
            ):
                merged_summary = summary
            else:
                merged_summary = existing["summary"]

            # Union source_channels
            existing_channels = set(json.loads(existing.get("source_channels") or "[]"))
            merged_channels = list(existing_channels | set(source_channels or []))

            # Merge properties: new values overwrite existing for same key
            merged_props = json.loads(existing.get("properties") or "{}")
            merged_props.update(properties or {})

            merged_source_type = source_type if new_priority >= existing_priority else existing["source_type"]

            self._db.execute(
                "UPDATE nodes SET summary=?, properties=?, source_type=?, "
                "source_channels=?, updated_at=? WHERE id=?",
                (
                    merged_summary,
                    json.dumps(merged_props),
                    merged_source_type,
                    json.dumps(merged_channels),
                    now,
                    node_id,
                ),
            )
            self._db.execute("DELETE FROM nodes_fts WHERE id = ?", (node_id,))
            self._db.execute(
                "INSERT INTO nodes_fts (id, label, summary) VALUES (?, ?, ?)",
                (node_id, existing["label"], merged_summary),
            )
            self._db.commit()
            try:
                self._generate_embedding(node_id, kind, existing["label"], merged_summary)
            except Exception:
                pass
            return node_id

        node_id = uuid.uuid4().hex[:12]
        self._db.execute(
            "INSERT INTO nodes (id, label, kind, summary, properties, "
            "source_type, source_channels, created_at, updated_at) "
            "VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
            (
                node_id,
                label,
                kind,
                summary,
                json.dumps(properties or {}),
                source_type,
                json.dumps(source_channels or []),
                now,
                now,
            ),
        )
        self._db.execute(
            "INSERT INTO nodes_fts (id, label, summary) VALUES (?, ?, ?)",
            (node_id, label, summary),
        )
        self._db.commit()
        try:
            self._generate_embedding(node_id, kind, label, summary)
        except Exception:
            pass
        return node_id

    def get_node(self, node_id: str) -> Optional[dict]:
        row = self._db.execute(
            "SELECT * FROM nodes WHERE id = ?", (node_id,)
        ).fetchone()
        return dict(row) if row else None

    def update_node(self, node_id: str, **kwargs) -> None:
        now = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
        allowed = {"label", "summary", "properties", "source_channels", "kind"}
        updates = {k: v for k, v in kwargs.items() if k in allowed}
        if not updates:
            return
        if "properties" in updates and isinstance(updates["properties"], dict):
            updates["properties"] = json.dumps(updates["properties"])
        if "source_channels" in updates and isinstance(updates["source_channels"], list):
            updates["source_channels"] = json.dumps(updates["source_channels"])
        updates["updated_at"] = now
        set_clause = ", ".join(f"{k} = ?" for k in updates)
        values = list(updates.values()) + [node_id]
        self._db.execute(f"UPDATE nodes SET {set_clause} WHERE id = ?", values)
        # Update FTS if label or summary changed
        if "label" in updates or "summary" in updates:
            node = self.get_node(node_id)
            if node:
                self._db.execute("DELETE FROM nodes_fts WHERE id = ?", (node_id,))
                self._db.execute(
                    "INSERT INTO nodes_fts (id, label, summary) VALUES (?, ?, ?)",
                    (node_id, node["label"], node["summary"]),
                )
                try:
                    self._generate_embedding(node_id, node.get("kind", ""), node["label"], node["summary"])
                except Exception:
                    pass
        self._db.commit()

    def find_nodes(
        self,
        query: str,
        visible_channels: Optional[list[str]] = None,
        limit: int = 50,
        semantic_only: bool = False,
    ) -> list[dict]:
        # Determine whether hybrid retrieval is possible
        can_vector = (
            self._vec_available
            and self._embedding_provider.dimensions > 0
        )

        if semantic_only and can_vector:
            return self._find_nodes_vector(query, visible_channels, limit)

        # --- FTS5 leg ---
        fts_results: list[dict] = []
        if not semantic_only:
            tokens = query.split()
            safe_query = " ".join('"' + t.replace('"', '""') + '"' for t in tokens) if tokens else '""'
            fetch_limit = limit * 2 if can_vector else limit
            rows = self._db.execute(
                "SELECT n.* FROM nodes_fts f JOIN nodes n ON f.id = n.id "
                "WHERE nodes_fts MATCH ? "
                "AND (n.curation_status IS NULL OR n.curation_status = 'flagged') "
                "AND n.kind != 'OntologyCandidate' "
                "LIMIT ?",
                (safe_query, fetch_limit),
            ).fetchall()
            fts_results = [dict(r) for r in rows]

        if not can_vector:
            # FTS only
            if visible_channels is not None:
                fts_results = self._filter_by_channels(fts_results, visible_channels)
            return fts_results[:limit]

        # --- Vector leg ---
        vec_results = self._find_nodes_vector(query, visible_channels, limit * 2)

        # --- RRF merge ---
        return self._rrf_merge(fts_results, vec_results, visible_channels, limit)

    def _find_nodes_vector(
        self,
        query: str,
        visible_channels: Optional[list[str]],
        limit: int,
    ) -> list[dict]:
        """ANN vector search, excluding OntologyCandidate nodes."""
        try:
            query_vec = self._embedding_provider.embed(query[:2048], input_type="query")
            if not query_vec:
                return []
            rows = self._db.execute(
                "SELECT id, distance FROM nodes_vec WHERE embedding MATCH ? "
                "ORDER BY distance LIMIT ?",
                (json.dumps(query_vec), limit),
            ).fetchall()
            results = []
            for row in rows:
                node = self.get_node(row[0] if isinstance(row, (list, tuple)) else row["id"])
                if node and node.get("kind") != "OntologyCandidate":
                    curation = node.get("curation_status")
                    if curation is None or curation == "flagged":
                        results.append(node)
            if visible_channels is not None:
                results = self._filter_by_channels(results, visible_channels)
            return results
        except Exception as e:
            logger.warning("Vector search failed: %s", e)
            return []

    def _rrf_merge(
        self,
        fts_results: list[dict],
        vec_results: list[dict],
        visible_channels: Optional[list[str]],
        limit: int,
    ) -> list[dict]:
        """Reciprocal Rank Fusion merge of FTS and vector results."""
        k = 60  # RRF constant
        scores: dict[str, float] = {}
        node_map: dict[str, dict] = {}

        for rank, node in enumerate(fts_results):
            nid = node["id"]
            scores[nid] = scores.get(nid, 0) + 1.0 / (k + rank)
            node_map[nid] = node

        for rank, node in enumerate(vec_results):
            nid = node["id"]
            scores[nid] = scores.get(nid, 0) + 1.0 / (k + rank)
            node_map[nid] = node

        ranked = sorted(scores.keys(), key=lambda nid: scores[nid], reverse=True)
        results = [node_map[nid] for nid in ranked]

        if visible_channels is not None:
            results = self._filter_by_channels(results, visible_channels)
        return results[:limit]

    def find_nodes_by_kind(self, kind: str, limit: int = 100) -> list[dict]:
        rows = self._db.execute(
            "SELECT * FROM nodes WHERE kind = ? LIMIT ?", (kind, limit)
        ).fetchall()
        return [dict(r) for r in rows]

    def add_edge(
        self,
        source_id: str,
        target_id: str,
        relation: str,
        weight: float = 1.0,
        properties: Optional[dict] = None,
        source_channel: str = "",
        provenance_id: str = "",
    ) -> str:
        now = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
        edge_id = uuid.uuid4().hex[:12]
        self._db.execute(
            "INSERT INTO edges (id, source_id, target_id, relation, weight, "
            "properties, source_channel, provenance_id, timestamp) "
            "VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
            (
                edge_id,
                source_id,
                target_id,
                relation,
                weight,
                json.dumps(properties or {}),
                source_channel,
                provenance_id,
                now,
            ),
        )
        self._db.commit()
        return edge_id

    def get_edges(
        self,
        node_id: str,
        direction: str = "outgoing",
        relation: Optional[str] = None,
    ) -> list[dict]:
        if direction == "outgoing":
            sql = "SELECT * FROM edges WHERE source_id = ?"
        elif direction == "incoming":
            sql = "SELECT * FROM edges WHERE target_id = ?"
        else:
            sql = "SELECT * FROM edges WHERE (source_id = ? OR target_id = ?)"
            params = [node_id, node_id]
            if relation:
                sql += " AND relation = ?"
                params.append(relation)
            return [dict(r) for r in self._db.execute(sql, params).fetchall()]
        params = [node_id]
        if relation:
            sql += " AND relation = ?"
            params.append(relation)
        return [dict(r) for r in self._db.execute(sql, params).fetchall()]

    def filter_nodes_by_property(self, kind: str, property_name: str, value: str, limit: int = 50) -> list[dict]:
        """Find nodes matching kind + JSON property value. Max 50 results."""
        limit = min(limit, 50)
        rows = self._db.execute(
            "SELECT * FROM nodes WHERE kind = ? AND json_extract(properties, '$.' || ?) = ? LIMIT ?",
            (kind, property_name, value, limit),
        ).fetchall()
        return [dict(r) for r in rows]

    def get_neighbors_subgraph(self, node_id: str, relation: Optional[str] = None, limit: int = 50) -> dict:
        """Get neighbor nodes + connecting edges. Max 50 neighbor nodes."""
        limit = min(limit, 50)
        edges = self.get_edges(node_id, direction="both", relation=relation)
        neighbor_ids = set()
        for e in edges:
            if e["source_id"] != node_id:
                neighbor_ids.add(e["source_id"])
            if e["target_id"] != node_id:
                neighbor_ids.add(e["target_id"])
        nodes = []
        for nid in list(neighbor_ids)[:limit]:
            node = self.get_node(nid)
            if node:
                nodes.append(node)
        returned_ids = {n["id"] for n in nodes} | {node_id}
        filtered_edges = [e for e in edges if e["source_id"] in returned_ids and e["target_id"] in returned_ids]
        return {"nodes": nodes, "edges": filtered_edges}

    def get_subgraph(
        self,
        node_id: str,
        max_hops: int = 1,
        visible_channels: Optional[list[str]] = None,
    ) -> dict:
        visited_nodes = set()
        all_edges = []
        frontier = {node_id}
        for _ in range(max_hops + 1):
            if not frontier:
                break
            visited_nodes.update(frontier)
            next_frontier = set()
            for nid in frontier:
                for edge in self.get_edges(nid, direction="both"):
                    all_edges.append(edge)
                    other = edge["target_id"] if edge["source_id"] == nid else edge["source_id"]
                    if other not in visited_nodes:
                        next_frontier.add(other)
            frontier = next_frontier
        nodes = []
        for nid in visited_nodes:
            node = self.get_node(nid)
            if node:
                nodes.append(node)
        if visible_channels is not None:
            nodes = self._filter_by_channels(nodes, visible_channels)
            visible_ids = {n["id"] for n in nodes}
            all_edges = [
                e for e in all_edges
                if e["source_id"] in visible_ids and e["target_id"] in visible_ids
            ]
        # Deduplicate edges
        seen_edges = set()
        unique_edges = []
        for e in all_edges:
            if e["id"] not in seen_edges:
                seen_edges.add(e["id"])
                unique_edges.append(e)
        return {"nodes": nodes, "edges": unique_edges}

    def find_similar(self, node_id: str, limit: int = 10) -> list[dict]:
        """Find nodes similar to the given node via vector search."""
        if not self._vec_available or self._embedding_provider.dimensions == 0:
            return []
        try:
            # Get the node's vector
            row = self._db.execute(
                "SELECT embedding FROM nodes_vec WHERE id = ?", (node_id,)
            ).fetchone()
            if not row:
                return []
            embedding = row[0] if isinstance(row, (list, tuple)) else row["embedding"]
            # ANN search
            rows = self._db.execute(
                "SELECT id, distance FROM nodes_vec WHERE embedding MATCH ? "
                "ORDER BY distance LIMIT ?",
                (embedding, limit + 1),  # +1 to exclude self
            ).fetchall()
            results = []
            for r in rows:
                rid = r[0] if isinstance(r, (list, tuple)) else r["id"]
                if rid == node_id:
                    continue
                node = self.get_node(rid)
                if node and node.get("kind") != "OntologyCandidate":
                    results.append(node)
            return results[:limit]
        except Exception as e:
            logger.warning("find_similar failed for %s: %s", node_id, e)
            return []

    def backfill_embeddings(self) -> int:
        """Backfill embeddings for embeddable nodes missing vectors. Returns count."""
        if not self._vec_available or self._embedding_provider.dimensions == 0:
            return 0
        # Find embeddable nodes not yet in nodes_vec
        kind_placeholders = ",".join("?" for _ in self._embeddable_kinds)
        if not kind_placeholders:
            return 0
        kinds_list = [k for k in self._embeddable_kinds]
        sql = (
            f"SELECT id, kind, label, summary FROM nodes "
            f"WHERE LOWER(kind) IN ({kind_placeholders}) "
            f"AND id NOT IN (SELECT id FROM nodes_vec)"
        )
        rows = self._db.execute(sql, kinds_list).fetchall()
        count = 0
        batch_size = 20
        for i in range(0, len(rows), batch_size):
            batch = rows[i:i + batch_size]
            for row in batch:
                node = dict(row)
                try:
                    self._generate_embedding(
                        node["id"], node["kind"], node["label"], node["summary"]
                    )
                    count += 1
                except Exception as e:
                    logger.warning("Backfill failed for %s: %s", node["id"], e)
            if i + batch_size < len(rows):
                time.sleep(0.1)  # 100ms between batches
        logger.info("Backfill complete: %d embeddings generated", count)
        return count

    def export_jsonl(self, since: Optional[str] = None) -> list[str]:
        lines = []
        sql = "SELECT * FROM nodes WHERE (curation_status IS NULL OR curation_status = 'flagged')"
        params = []
        if since:
            sql += " AND updated_at >= ?"
            params.append(since)
        for row in self._db.execute(sql, params).fetchall():
            node = dict(row)
            lines.append(json.dumps({
                "type": "node",
                "label": node["label"],
                "kind": node["kind"],
                "summary": node["summary"],
                "properties": json.loads(node["properties"]),
                "source_type": node["source_type"],
                "created_at": node["created_at"],
                "updated_at": node["updated_at"],
            }))
        sql = "SELECT * FROM edges"
        params = []
        if since:
            sql += " WHERE timestamp >= ?"
            params.append(since)
        for row in self._db.execute(sql, params).fetchall():
            edge = dict(row)
            # Resolve labels for stable references
            src = self.get_node(edge["source_id"])
            tgt = self.get_node(edge["target_id"])
            lines.append(json.dumps({
                "type": "edge",
                "source": src["label"] if src else edge["source_id"],
                "source_kind": src["kind"] if src else "",
                "target": tgt["label"] if tgt else edge["target_id"],
                "target_kind": tgt["kind"] if tgt else "",
                "relation": edge["relation"],
                "weight": edge["weight"],
                "properties": json.loads(edge["properties"]),
                "timestamp": edge["timestamp"],
            }))
        return lines

    def _filter_by_channels(
        self, nodes: list[dict], visible_channels: list[str]
    ) -> list[dict]:
        visible_set = set(visible_channels)
        filtered = []
        for node in nodes:
            # Structural nodes (agent, channel, task) are always visible
            if node.get("kind") in STRUCTURAL_KINDS:
                filtered.append(node)
                continue
            channels = json.loads(node.get("source_channels", "[]"))
            if not channels or visible_set.intersection(channels):
                filtered.append(node)
        return filtered

    def get_neighbors(
        self,
        node_id: str,
        direction: str = "both",
        relation: Optional[str] = None,
    ) -> dict:
        """Get direct neighbors of a node with edge metadata."""
        edges = self.get_edges(node_id, direction=direction, relation=relation)
        neighbor_ids = set()
        for edge in edges:
            other = edge["target_id"] if edge["source_id"] == node_id else edge["source_id"]
            neighbor_ids.add(other)
        neighbors = [n for nid in neighbor_ids if (n := self.get_node(nid))]
        return {"neighbors": neighbors, "edges": edges}

    def find_path(
        self,
        from_label: str,
        to_label: str,
        max_hops: int = 4,
    ) -> Optional[dict]:
        """BFS shortest path between two nodes identified by label."""
        from_nodes = self.find_nodes(from_label)
        to_nodes = self.find_nodes(to_label)
        if not from_nodes or not to_nodes:
            return None
        start_id = from_nodes[0]["id"]
        target_ids = {n["id"] for n in to_nodes}
        if start_id in target_ids:
            node = self.get_node(start_id)
            return {"nodes": [node] if node else [], "edges": []}
        # BFS: each entry is (current_id, node_id_path, edge_path)
        queue: list[tuple[str, list[str], list[dict]]] = [(start_id, [start_id], [])]
        visited = {start_id}
        while queue:
            current_id, node_path, edge_path = queue.pop(0)
            if len(edge_path) >= max_hops:
                continue
            for edge in self.get_edges(current_id, direction="both"):
                other = edge["target_id"] if edge["source_id"] == current_id else edge["source_id"]
                if other in visited:
                    continue
                visited.add(other)
                new_node_path = node_path + [other]
                new_edge_path = edge_path + [edge]
                if other in target_ids:
                    nodes = [n for nid in new_node_path if (n := self.get_node(nid))]
                    return {"nodes": nodes, "edges": new_edge_path}
                queue.append((other, new_node_path, new_edge_path))
        return None

    def stats(self) -> dict:
        # Exclude soft-deleted and merged nodes from stats
        active_filter = "WHERE curation_status IS NULL OR curation_status = 'flagged'"
        node_count = self._db.execute(f"SELECT COUNT(*) FROM nodes {active_filter}").fetchone()[0]
        edge_count = self._db.execute("SELECT COUNT(*) FROM edges").fetchone()[0]
        kinds = self._db.execute(
            f"SELECT kind, COUNT(*) as cnt FROM nodes {active_filter} GROUP BY kind ORDER BY cnt DESC"
        ).fetchall()
        relations = self._db.execute(
            "SELECT relation, COUNT(*) as cnt FROM edges GROUP BY relation ORDER BY cnt DESC LIMIT 10"
        ).fetchall()
        top_connected = self._db.execute(f"""
            SELECT n.label, n.kind, COUNT(e.id) as edge_count
            FROM nodes n
            LEFT JOIN edges e ON e.source_id = n.id OR e.target_id = n.id
            {active_filter}
            GROUP BY n.id
            ORDER BY edge_count DESC
            LIMIT 10
        """).fetchall()
        return {
            "nodes": node_count,
            "edges": edge_count,
            "kinds": {r[0]: r[1] for r in kinds},
            "relations": {r[0]: r[1] for r in relations},
            "top_connected": [
                {"label": r[0], "kind": r[1], "connections": r[2]}
                for r in top_connected
            ],
        }

    def export_cypher(self, since: Optional[str] = None) -> str:
        """Export graph as Cypher statements for Neo4j import."""
        lines = []
        sql = "SELECT * FROM nodes"
        params = []
        if since:
            sql += " WHERE updated_at >= ?"
            params.append(since)
        for row in self._db.execute(sql, params).fetchall():
            node = dict(row)
            label = node["label"].replace("'", "\\'")
            kind = node["kind"]
            summary = node["summary"].replace("'", "\\'")
            lines.append(
                f"MERGE (n:{kind} {{label: '{label}'}}) "
                f"SET n.summary = '{summary}', n.id = '{node['id']}';"
            )
        sql = "SELECT * FROM edges"
        params = []
        if since:
            sql += " WHERE timestamp >= ?"
            params.append(since)
        for row in self._db.execute(sql, params).fetchall():
            edge = dict(row)
            src = self.get_node(edge["source_id"])
            tgt = self.get_node(edge["target_id"])
            if not src or not tgt:
                continue
            src_label = src["label"].replace("'", "\\'")
            tgt_label = tgt["label"].replace("'", "\\'")
            relation = edge["relation"].upper().replace(" ", "_")
            lines.append(
                f"MATCH (a:{src['kind']} {{label: '{src_label}'}}), "
                f"(b:{tgt['kind']} {{label: '{tgt_label}'}}) "
                f"MERGE (a)-[:{relation}]->(b);"
            )
        return "\n".join(lines)

    def export_dot(self, since: Optional[str] = None) -> str:
        """Export graph as DOT format for Graphviz visualization."""
        lines = ['digraph knowledge {', '  rankdir=LR;', '  node [shape=box];']
        sql = "SELECT * FROM edges"
        params = []
        if since:
            sql += " WHERE timestamp >= ?"
            params.append(since)
        seen_nodes: set[str] = set()
        edge_lines = []
        for row in self._db.execute(sql, params).fetchall():
            edge = dict(row)
            src = self.get_node(edge["source_id"])
            tgt = self.get_node(edge["target_id"])
            if not src or not tgt:
                continue
            src_id = edge["source_id"]
            tgt_id = edge["target_id"]
            if src_id not in seen_nodes:
                seen_nodes.add(src_id)
                label = src["label"].replace('"', '\\"')
                lines.append(f'  "{src_id}" [label="{label} ({src["kind"]})"];')
            if tgt_id not in seen_nodes:
                seen_nodes.add(tgt_id)
                label = tgt["label"].replace('"', '\\"')
                lines.append(f'  "{tgt_id}" [label="{label} ({tgt["kind"]})"];')
            relation = edge["relation"].replace('"', '\\"')
            edge_lines.append(f'  "{src_id}" -> "{tgt_id}" [label="{relation}"];')
        lines.extend(edge_lines)
        lines.append('}')
        return "\n".join(lines)

    # -- Org-structural review gate --

    def is_org_structural(self, kind: str) -> bool:
        """Determine if a knowledge contribution is org-structural.

        Org-structural contributions are held in pending_nodes until an
        operator approves them. This prevents a compromised agent from
        injecting false leadership or team data into the graph.
        """
        return kind.lower() in ORG_STRUCTURAL_KINDS

    def submit_pending(
        self,
        label: str,
        kind: str,
        summary: str = "",
        properties: Optional[dict] = None,
        source_agent: str = "",
    ) -> str:
        """Hold an org-structural contribution for operator review."""
        now = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
        pending_id = uuid.uuid4().hex[:12]
        self._db.execute(
            "INSERT INTO pending_nodes (id, label, kind, summary, properties, source_agent, submitted_at) "
            "VALUES (?, ?, ?, ?, ?, ?, ?)",
            (
                pending_id,
                label,
                kind,
                summary,
                json.dumps(properties or {}),
                source_agent,
                now,
            ),
        )
        self._db.commit()
        return pending_id

    def list_pending(self) -> list[dict]:
        """List all pending org-structural contributions."""
        cursor = self._db.execute(
            "SELECT * FROM pending_nodes ORDER BY submitted_at"
        )
        return [dict(row) for row in cursor.fetchall()]

    def review_pending(self, pending_id: str, action: str) -> bool:
        """Approve or reject a pending contribution.

        On approve: the node is committed to the main graph via add_node.
        On reject: the pending entry is silently discarded.
        Returns True if the pending_id was found, False otherwise.
        """
        cursor = self._db.execute(
            "SELECT * FROM pending_nodes WHERE id = ?", (pending_id,)
        )
        row = cursor.fetchone()
        if not row:
            return False
        node = dict(row)
        if action == "approve":
            self.add_node(
                label=node["label"],
                kind=node["kind"],
                summary=node["summary"],
                properties=json.loads(node["properties"]),
                source_type="agent",
                source_channels=[],
            )
        self.log_curation(
            action=f"review_{action}",
            node_id=pending_id,
            detail={"label": node["label"], "kind": node["kind"], "source_agent": node.get("source_agent", "")},
        )
        self._db.execute("DELETE FROM pending_nodes WHERE id = ?", (pending_id,))
        self._db.commit()
        return True

    # -- Curation log helpers --

    def log_curation(self, action: str, node_id: str, detail: dict) -> str:
        """Record a curation action in the curation log."""
        now = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
        log_id = uuid.uuid4().hex[:12]
        self._db.execute(
            "INSERT INTO curation_log (id, action, node_id, detail, timestamp) "
            "VALUES (?, ?, ?, ?, ?)",
            (log_id, action, node_id, json.dumps(detail), now),
        )
        self._db.commit()
        return log_id

    def get_curation_log(
        self,
        node_id: Optional[str] = None,
        action: Optional[str] = None,
        since: Optional[str] = None,
        limit: int = 100,
        offset: int = 0,
    ) -> list[dict]:
        """Query the curation log with optional filters."""
        sql = "SELECT * FROM curation_log WHERE 1=1"
        params: list = []
        if node_id is not None:
            sql += " AND node_id = ?"
            params.append(node_id)
        if action is not None:
            sql += " AND action = ?"
            params.append(action)
        if since is not None:
            sql += " AND timestamp >= ?"
            params.append(since)
        sql += " ORDER BY timestamp DESC LIMIT ? OFFSET ?"
        params.extend([limit, offset])
        rows = self._db.execute(sql, params).fetchall()
        return [dict(r) for r in rows]

    # -- Query result cache (cache mode) --

    def cache_query(self, query_key: str, result: dict, ttl_seconds: int = 60) -> None:
        """Cache a query result with TTL."""
        import time
        cache_dir = self.data_dir / "cache"
        cache_dir.mkdir(parents=True, exist_ok=True)
        entry = {
            "result": result,
            "cached_at": time.time(),
            "ttl": ttl_seconds,
        }
        cache_file = cache_dir / f"{self._cache_key(query_key)}.json"
        cache_file.write_text(json.dumps(entry))

    def get_cached_query(self, query_key: str) -> Optional[dict]:
        """Retrieve a cached query result. Returns None if expired or missing."""
        import time
        cache_file = self.data_dir / "cache" / f"{self._cache_key(query_key)}.json"
        if not cache_file.exists():
            return None
        entry = json.loads(cache_file.read_text())
        if time.time() - entry["cached_at"] >= entry["ttl"]:
            cache_file.unlink(missing_ok=True)
            return None
        return entry["result"]

    def _cache_key(self, query: str) -> str:
        """Generate a filesystem-safe cache key from a query string."""
        import hashlib
        return hashlib.sha256(query.encode()).hexdigest()[:16]

    # -- Contribute buffer (cache mode) --

    def buffer_contribution(
        self,
        label: str,
        kind: str,
        summary: str = "",
        properties: Optional[dict] = None,
        source_type: str = "rule",
        source_channels: Optional[list[str]] = None,
    ) -> dict:
        """Buffer a knowledge contribution when upstream is unavailable."""
        buffer_dir = self.data_dir / "buffer"
        buffer_dir.mkdir(parents=True, exist_ok=True)

        entry_id = uuid.uuid4().hex[:12]
        entry = {
            "id": entry_id,
            "label": label,
            "kind": kind,
            "summary": summary,
            "properties": properties or {},
            "source_type": source_type,
            "source_channels": source_channels or [],
        }

        buffer_path = buffer_dir / "nodes.jsonl"
        with open(buffer_path, "a") as f:
            f.write(json.dumps(entry) + "\n")

        return entry

    def read_contribution_buffer(self) -> list[dict]:
        """Read all buffered contributions in FIFO order."""
        buffer_path = self.data_dir / "buffer" / "nodes.jsonl"
        if not buffer_path.exists():
            return []
        entries = []
        for line in buffer_path.read_text().strip().splitlines():
            if line.strip():
                entries.append(json.loads(line))
        return entries

    def remove_contribution(self, entry_id: str) -> None:
        """Remove a specific contribution from the buffer."""
        buffer_path = self.data_dir / "buffer" / "nodes.jsonl"
        if not buffer_path.exists():
            return
        lines = buffer_path.read_text().strip().splitlines()
        remaining = []
        for line in lines:
            if line.strip():
                entry = json.loads(line)
                if entry.get("id") != entry_id:
                    remaining.append(line)
        if remaining:
            buffer_path.write_text("\n".join(remaining) + "\n")
        else:
            buffer_path.unlink(missing_ok=True)

    def contribution_buffer_size(self) -> int:
        """Count buffered contributions."""
        return len(self.read_contribution_buffer())

    def get_org_context(self, agent_name: str) -> dict:
        """Return organizational context scoped to an agent's authorization.

        Traverses the graph to find the agent's team membership and builds a
        structured view of the org: team, department, escalation path, peer
        teams, and relevant org history nodes.

        Graph conventions:
          - agent node: kind in ("agent", "system"), label matches agent_name
          - team membership: agent -[member_of]-> team
          - team lead: team -[led_by]-> person/agent
          - department membership: team -[part_of]-> department
          - department lead: department -[led_by]-> person/agent
          - operator node: kind="operator" or label="operator"
        """
        # Find the agent node (kind may be "agent" or "system" after migration)
        agent_row = self._db.execute(
            "SELECT * FROM nodes WHERE LOWER(label) = LOWER(?) AND kind IN ('agent', 'system')",
            (agent_name,),
        ).fetchone()

        empty = {
            "team": {},
            "department": {},
            "escalation_path": [],
            "peer_teams": [],
            "org_history": [],
        }

        if not agent_row:
            return empty

        agent_node = dict(agent_row)
        agent_id = agent_node["id"]

        # Find team via member_of edge (agent -> team)
        team_edges = self._db.execute(
            "SELECT * FROM edges WHERE source_id = ? AND relation = 'member_of'",
            (agent_id,),
        ).fetchall()

        if not team_edges:
            return empty

        # Use the first team found
        team_edge = dict(team_edges[0])
        team_row = self._db.execute(
            "SELECT * FROM nodes WHERE id = ?", (team_edge["target_id"],)
        ).fetchone()

        if not team_row:
            return empty

        team_node = dict(team_row)
        team_id = team_node["id"]

        # Get all team members (other nodes with member_of -> this team)
        member_edges = self._db.execute(
            "SELECT * FROM edges WHERE target_id = ? AND relation = 'member_of'",
            (team_id,),
        ).fetchall()
        team_members = []
        for me in member_edges:
            member = self._db.execute(
                "SELECT id, label, kind FROM nodes WHERE id = ?",
                (dict(me)["source_id"],),
            ).fetchone()
            if member:
                team_members.append({"id": member["id"], "label": member["label"], "kind": member["kind"]})

        # Find team lead (team -[led_by]-> person/agent)
        team_lead_edges = self._db.execute(
            "SELECT * FROM edges WHERE source_id = ? AND relation = 'led_by'",
            (team_id,),
        ).fetchall()
        team_lead = None
        if team_lead_edges:
            lead_row = self._db.execute(
                "SELECT id, label, kind FROM nodes WHERE id = ?",
                (dict(team_lead_edges[0])["target_id"],),
            ).fetchone()
            if lead_row:
                team_lead = {"id": lead_row["id"], "label": lead_row["label"], "kind": lead_row["kind"]}

        team_info = {
            "id": team_id,
            "label": team_node["label"],
            "kind": team_node["kind"],
            "summary": team_node.get("summary", ""),
            "members": team_members,
            "lead": team_lead,
        }

        # Find department (team -[part_of]-> department)
        dept_edges = self._db.execute(
            "SELECT * FROM edges WHERE source_id = ? AND relation = 'part_of'",
            (team_id,),
        ).fetchall()

        dept_info: dict = {}
        peer_teams: list[dict] = []
        escalation_path: list[dict] = []

        if dept_edges:
            dept_row = self._db.execute(
                "SELECT * FROM nodes WHERE id = ?",
                (dict(dept_edges[0])["target_id"],),
            ).fetchone()

            if dept_row:
                dept_node = dict(dept_row)
                dept_id = dept_node["id"]

                # Find department lead
                dept_lead_edges = self._db.execute(
                    "SELECT * FROM edges WHERE source_id = ? AND relation = 'led_by'",
                    (dept_id,),
                ).fetchall()
                dept_lead = None
                if dept_lead_edges:
                    dl_row = self._db.execute(
                        "SELECT id, label, kind FROM nodes WHERE id = ?",
                        (dict(dept_lead_edges[0])["target_id"],),
                    ).fetchone()
                    if dl_row:
                        dept_lead = {"id": dl_row["id"], "label": dl_row["label"], "kind": dl_row["kind"]}

                dept_info = {
                    "id": dept_id,
                    "label": dept_node["label"],
                    "kind": dept_node["kind"],
                    "summary": dept_node.get("summary", ""),
                    "lead": dept_lead,
                }

                # Find peer teams (other teams part_of same department)
                peer_team_edges = self._db.execute(
                    "SELECT * FROM edges WHERE target_id = ? AND relation = 'part_of'",
                    (dept_id,),
                ).fetchall()
                for pte in peer_team_edges:
                    pt_id = dict(pte)["source_id"]
                    if pt_id == team_id:
                        continue
                    pt_row = self._db.execute(
                        "SELECT id, label, kind, summary FROM nodes WHERE id = ?",
                        (pt_id,),
                    ).fetchone()
                    if pt_row:
                        peer_teams.append({
                            "id": pt_row["id"],
                            "label": pt_row["label"],
                            "kind": pt_row["kind"],
                            "summary": pt_row["summary"] or "",
                        })

                # Build escalation path: team_lead -> dept_lead -> operator
                if team_lead:
                    escalation_path.append({"role": "team_lead", **team_lead})
                if dept_lead:
                    escalation_path.append({"role": "dept_lead", **dept_lead})

        # Look for an operator node
        operator_row = self._db.execute(
            "SELECT id, label, kind FROM nodes WHERE kind = 'operator' OR LOWER(label) = 'operator' LIMIT 1"
        ).fetchone()
        if operator_row:
            op = {"id": operator_row["id"], "label": operator_row["label"], "kind": operator_row["kind"]}
            # Only add if not already the same as dept_lead
            if not escalation_path or escalation_path[-1].get("id") != op["id"]:
                escalation_path.append({"role": "operator", **op})

        # Collect org history: nodes of kind "decision", "lesson", "incident" connected
        # to the team or department
        org_history: list[dict] = []
        history_kinds = {"decision", "lesson", "incident", "fact"}
        search_ids = [team_id]
        if dept_info:
            search_ids.append(dept_info["id"])

        seen_history: set[str] = set()
        for sid in search_ids:
            connected_edges = self._db.execute(
                "SELECT * FROM edges WHERE source_id = ? OR target_id = ?",
                (sid, sid),
            ).fetchall()
            for ce in connected_edges:
                ce_dict = dict(ce)
                other_id = ce_dict["target_id"] if ce_dict["source_id"] == sid else ce_dict["source_id"]
                if other_id in seen_history:
                    continue
                other_row = self._db.execute(
                    "SELECT id, label, kind, summary FROM nodes WHERE id = ? AND kind IN ('decision', 'lesson', 'incident', 'fact')",
                    (other_id,),
                ).fetchone()
                if other_row:
                    seen_history.add(other_id)
                    org_history.append({
                        "id": other_row["id"],
                        "label": other_row["label"],
                        "kind": other_row["kind"],
                        "summary": other_row["summary"] or "",
                    })

        return {
            "team": team_info,
            "department": dept_info,
            "escalation_path": escalation_path,
            "peer_teams": peer_teams,
            "org_history": org_history,
        }
