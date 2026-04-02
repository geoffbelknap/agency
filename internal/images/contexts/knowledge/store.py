"""Knowledge graph store with flexible schema.

SQLite backend with adjacency tables for nodes and edges.
Free-form kind/relation labels — the LLM decides what entity
types and relationship types to use. FTS5 for text search on
node labels and summaries.

Schema is designed for clean export to external graph DBs.
"""

import json
import sqlite3
import uuid
from datetime import datetime, timezone
from pathlib import Path


STRUCTURAL_KINDS = {"agent", "channel", "task"}

_SOURCE_PRIORITY = {"agent": 3, "llm": 2, "local": 1, "rule": 1}


class KnowledgeStore:
    def __init__(self, data_dir: Path):
        self.data_dir = data_dir
        self.data_dir.mkdir(parents=True, exist_ok=True)
        db_path = data_dir / "knowledge.db"
        self._db = sqlite3.connect(str(db_path))
        self._db.execute("PRAGMA journal_mode=WAL")
        self._db.row_factory = sqlite3.Row
        self._db.execute("PRAGMA foreign_keys = ON")
        self._init_schema()

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
        self._db.commit()

    def add_node(
        self,
        label: str,
        kind: str,
        summary: str = "",
        properties: dict | None = None,
        source_type: str = "rule",
        source_channels: list[str] | None = None,
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
        return node_id

    def get_node(self, node_id: str) -> dict | None:
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
        self._db.commit()

    def find_nodes(
        self,
        query: str,
        visible_channels: list[str] | None = None,
        limit: int = 50,
    ) -> list[dict]:
        # Quote each token individually to prevent FTS5 column interpretation
        # while preserving multi-term AND semantics. Wrapping the whole query
        # in one phrase literal (the previous approach) caused zero matches for
        # any multi-word query like "juice shop vulnerabilities".
        tokens = query.split()
        safe_query = " ".join('"' + t.replace('"', '""') + '"' for t in tokens) if tokens else '""'
        rows = self._db.execute(
            "SELECT n.* FROM nodes_fts f JOIN nodes n ON f.id = n.id "
            "WHERE nodes_fts MATCH ? "
            "AND (n.curation_status IS NULL OR n.curation_status = 'flagged') "
            "LIMIT ?",
            (safe_query, limit),
        ).fetchall()
        results = [dict(r) for r in rows]
        if visible_channels is not None:
            results = self._filter_by_channels(results, visible_channels)
        return results

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
        properties: dict | None = None,
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
        relation: str | None = None,
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

    def get_subgraph(
        self,
        node_id: str,
        max_hops: int = 1,
        visible_channels: list[str] | None = None,
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

    def export_jsonl(self, since: str | None = None) -> list[str]:
        lines = []
        sql = "SELECT * FROM nodes"
        params = []
        if since:
            sql += " WHERE updated_at >= ?"
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
        relation: str | None = None,
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
    ) -> dict | None:
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
        node_count = self._db.execute("SELECT COUNT(*) FROM nodes").fetchone()[0]
        edge_count = self._db.execute("SELECT COUNT(*) FROM edges").fetchone()[0]
        kinds = self._db.execute(
            "SELECT kind, COUNT(*) as cnt FROM nodes GROUP BY kind ORDER BY cnt DESC"
        ).fetchall()
        relations = self._db.execute(
            "SELECT relation, COUNT(*) as cnt FROM edges GROUP BY relation ORDER BY cnt DESC LIMIT 10"
        ).fetchall()
        top_connected = self._db.execute("""
            SELECT n.label, n.kind, COUNT(e.id) as edge_count
            FROM nodes n
            LEFT JOIN edges e ON e.source_id = n.id OR e.target_id = n.id
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

    def export_cypher(self, since: str | None = None) -> str:
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

    def export_dot(self, since: str | None = None) -> str:
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
        node_id: str | None = None,
        action: str | None = None,
        since: str | None = None,
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

    def get_cached_query(self, query_key: str) -> dict | None:
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
        properties: dict | None = None,
        source_type: str = "rule",
        source_channels: list[str] | None = None,
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
