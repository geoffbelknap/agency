"""Work item lifecycle and SQLite storage."""

import json
import sqlite3
from dataclasses import dataclass
from datetime import datetime, timezone, timedelta
from pathlib import Path
from uuid import uuid4
from typing import Optional


@dataclass
class WorkItem:
    id: str
    connector: str
    payload: dict
    status: str
    target_type: Optional[str] = None
    target_name: Optional[str] = None
    route_index: Optional[int] = None
    priority: str = "normal"
    sla_deadline: Optional[datetime] = None
    task_content: Optional[str] = None
    created_at: datetime = None
    updated_at: datetime = None
    resolved_at: Optional[datetime] = None


class WorkItemStore:
    """SQLite-backed work item storage."""

    def __init__(self, data_dir: Path):
        self.data_dir = data_dir
        self.data_dir.mkdir(parents=True, exist_ok=True)
        self.db_path = self.data_dir / "work_items.db"
        self._init_db()

    def _init_db(self) -> None:
        with self._connect() as conn:
            conn.execute("""
                CREATE TABLE IF NOT EXISTS work_items (
                    id TEXT PRIMARY KEY,
                    connector TEXT NOT NULL,
                    payload TEXT NOT NULL,
                    status TEXT NOT NULL DEFAULT 'received',
                    target_type TEXT,
                    target_name TEXT,
                    route_index INTEGER,
                    priority TEXT DEFAULT 'normal',
                    sla_deadline TEXT,
                    task_content TEXT,
                    created_at TEXT NOT NULL,
                    updated_at TEXT NOT NULL,
                    resolved_at TEXT
                )
            """)
            conn.execute("""
                CREATE INDEX IF NOT EXISTS idx_work_items_connector_status
                ON work_items(connector, status)
            """)
            conn.execute("""
                CREATE INDEX IF NOT EXISTS idx_work_items_sla
                ON work_items(sla_deadline)
                WHERE resolved_at IS NULL
            """)

    def _connect(self) -> sqlite3.Connection:
        return sqlite3.connect(str(self.db_path))

    def _row_to_item(self, row: tuple) -> WorkItem:
        return WorkItem(
            id=row[0],
            connector=row[1],
            payload=json.loads(row[2]),
            status=row[3],
            target_type=row[4],
            target_name=row[5],
            route_index=row[6],
            priority=row[7],
            sla_deadline=datetime.fromisoformat(row[8]) if row[8] else None,
            task_content=row[9],
            created_at=datetime.fromisoformat(row[10]),
            updated_at=datetime.fromisoformat(row[11]),
            resolved_at=datetime.fromisoformat(row[12]) if row[12] else None,
        )

    def create(self, connector: str, payload: dict) -> WorkItem:
        now = datetime.now(timezone.utc)
        item_id = f"wi-{now.strftime('%Y%m%d')}-{uuid4().hex[:8]}"
        with self._connect() as conn:
            conn.execute(
                """INSERT INTO work_items
                   (id, connector, payload, status, created_at, updated_at)
                   VALUES (?, ?, ?, 'received', ?, ?)""",
                (item_id, connector, json.dumps(payload), now.isoformat(), now.isoformat()),
            )
        return WorkItem(
            id=item_id,
            connector=connector,
            payload=payload,
            status="received",
            created_at=now,
            updated_at=now,
        )

    def get(self, item_id: str) -> Optional[WorkItem]:
        with self._connect() as conn:
            row = conn.execute(
                "SELECT * FROM work_items WHERE id = ?", (item_id,)
            ).fetchone()
        return self._row_to_item(row) if row else None

    def update_status(
        self,
        item_id: str,
        status: str,
        target_type: Optional[str] = None,
        target_name: Optional[str] = None,
        route_index: Optional[int] = None,
        sla_deadline: Optional[datetime] = None,
        task_content: Optional[str] = None,
        priority: Optional[str] = None,
    ) -> None:
        now = datetime.now(timezone.utc)
        sets = ["status = ?", "updated_at = ?"]
        params: list = [status, now.isoformat()]

        if target_type is not None:
            sets.append("target_type = ?")
            params.append(target_type)
        if target_name is not None:
            sets.append("target_name = ?")
            params.append(target_name)
        if route_index is not None:
            sets.append("route_index = ?")
            params.append(route_index)
        if sla_deadline is not None:
            sets.append("sla_deadline = ?")
            params.append(sla_deadline.isoformat())
        if task_content is not None:
            sets.append("task_content = ?")
            params.append(task_content)
        if priority is not None:
            sets.append("priority = ?")
            params.append(priority)
        if status == "resolved":
            sets.append("resolved_at = ?")
            params.append(now.isoformat())

        params.append(item_id)
        with self._connect() as conn:
            conn.execute(
                f"UPDATE work_items SET {', '.join(sets)} WHERE id = ?",
                params,
            )

    def list_items(
        self,
        connector: Optional[str] = None,
        status: Optional[str] = None,
        limit: int = 100,
    ) -> list[WorkItem]:
        clauses = []
        params: list = []
        if connector:
            clauses.append("connector = ?")
            params.append(connector)
        if status:
            clauses.append("status = ?")
            params.append(status)
        where = f"WHERE {' AND '.join(clauses)}" if clauses else ""
        params.append(limit)
        with self._connect() as conn:
            rows = conn.execute(
                f"SELECT * FROM work_items {where} ORDER BY created_at DESC LIMIT ?",
                params,
            ).fetchall()
        return [self._row_to_item(r) for r in rows]

    def list_sla_breached(self) -> list[WorkItem]:
        now = datetime.now(timezone.utc).isoformat()
        with self._connect() as conn:
            rows = conn.execute(
                """SELECT * FROM work_items
                   WHERE sla_deadline IS NOT NULL
                   AND sla_deadline < ?
                   AND resolved_at IS NULL
                   ORDER BY sla_deadline ASC""",
                (now,),
            ).fetchall()
        return [self._row_to_item(r) for r in rows]

    def count_by_status(self, connector: Optional[str] = None) -> dict[str, int]:
        if connector:
            clause = "WHERE connector = ?"
            params = (connector,)
        else:
            clause = ""
            params = ()
        with self._connect() as conn:
            rows = conn.execute(
                f"SELECT status, COUNT(*) FROM work_items {clause} GROUP BY status",
                params,
            ).fetchall()
        return {row[0]: row[1] for row in rows}

    def count_concurrent(self, connector: str) -> int:
        with self._connect() as conn:
            row = conn.execute(
                "SELECT COUNT(*) FROM work_items WHERE connector = ? AND status = 'assigned'",
                (connector,),
            ).fetchone()
        return row[0] if row else 0

    def count_per_hour(self, connector: str) -> int:
        one_hour_ago = (datetime.now(timezone.utc) - timedelta(hours=1)).isoformat()
        with self._connect() as conn:
            row = conn.execute(
                "SELECT COUNT(*) FROM work_items WHERE connector = ? AND created_at > ?",
                (connector, one_hour_ago),
            ).fetchone()
        return row[0] if row else 0

    def stats(self, connector: Optional[str] = None) -> dict:
        by_status = self.count_by_status(connector=connector)
        total = sum(by_status.values())
        by_connector: dict[str, int] = {}
        with self._connect() as conn:
            rows = conn.execute(
                "SELECT connector, COUNT(*) FROM work_items GROUP BY connector"
            ).fetchall()
        for row in rows:
            by_connector[row[0]] = row[1]
        return {
            "total": total,
            "by_status": by_status,
            "by_connector": by_connector,
        }
