"""Principal registry — UUID-based identity for all principals.

Provides a unified registry for operators, agents, teams, roles, and channels.
Each principal gets a stable UUID that persists across sessions. Shares the
same SQLite database as KnowledgeStore but is a separate class.
"""

import json
import logging
import sqlite3
import uuid
from datetime import datetime, timezone
from typing import Optional

logger = logging.getLogger("agency.knowledge.principal_registry")


class PrincipalRegistry:
    """UUID-based identity registry for all principals."""

    VALID_TYPES = ("operator", "agent", "team", "role", "channel")

    def __init__(self, db: sqlite3.Connection):
        self._db = db
        self._db.row_factory = sqlite3.Row
        self._init_schema()

    def _init_schema(self):
        self._db.executescript("""
            CREATE TABLE IF NOT EXISTS principal_registry (
                uuid TEXT PRIMARY KEY,
                type TEXT NOT NULL,
                name TEXT NOT NULL,
                created_at TEXT NOT NULL,
                metadata TEXT DEFAULT '{}'
            );
            CREATE INDEX IF NOT EXISTS idx_principal_type_name
                ON principal_registry(type, name);
        """)

    def register(self, principal_type: str, name: str, metadata: Optional[dict] = None) -> str:
        """Register a principal. Returns UUID. Idempotent for type+name pairs."""
        if principal_type not in self.VALID_TYPES:
            raise ValueError(
                f"Invalid principal type '{principal_type}'. "
                f"Must be one of: {', '.join(self.VALID_TYPES)}"
            )

        # Check for existing registration
        row = self._db.execute(
            "SELECT uuid FROM principal_registry WHERE type = ? AND name = ?",
            (principal_type, name),
        ).fetchone()
        if row:
            return row["uuid"]

        # Create new registration
        new_uuid = str(uuid.uuid4())
        now = datetime.now(timezone.utc).isoformat()
        meta_json = json.dumps(metadata or {})

        self._db.execute(
            "INSERT INTO principal_registry (uuid, type, name, created_at, metadata) "
            "VALUES (?, ?, ?, ?, ?)",
            (new_uuid, principal_type, name, now, meta_json),
        )
        self._db.commit()
        logger.debug("Registered %s '%s' as %s", principal_type, name, new_uuid)
        return new_uuid

    def resolve(self, principal_uuid: str) -> Optional[dict]:
        """Resolve a UUID to its principal record."""
        row = self._db.execute(
            "SELECT uuid, type, name, created_at, metadata FROM principal_registry WHERE uuid = ?",
            (principal_uuid,),
        ).fetchone()
        if not row:
            return None
        return self._row_to_dict(row)

    def resolve_name(self, principal_type: str, name: str) -> Optional[str]:
        """Resolve a type+name pair to its UUID."""
        row = self._db.execute(
            "SELECT uuid FROM principal_registry WHERE type = ? AND name = ?",
            (principal_type, name),
        ).fetchone()
        return row["uuid"] if row else None

    def list_by_type(self, principal_type: str) -> list[dict]:
        """List all principals of a given type."""
        rows = self._db.execute(
            "SELECT uuid, type, name, created_at, metadata FROM principal_registry WHERE type = ?",
            (principal_type,),
        ).fetchall()
        return [self._row_to_dict(r) for r in rows]

    def list_all(self) -> list[dict]:
        """List all registered principals."""
        rows = self._db.execute(
            "SELECT uuid, type, name, created_at, metadata FROM principal_registry"
        ).fetchall()
        return [self._row_to_dict(r) for r in rows]

    @staticmethod
    def format_id(principal_type: str, principal_uuid: str) -> str:
        """Format a principal ID as 'type:uuid'."""
        return f"{principal_type}:{principal_uuid}"

    def parse_id(self, principal_id: str) -> tuple[str, str]:
        """Parse a principal ID string.

        Accepts 'type:uuid' or legacy 'type:name' format.
        For legacy format, resolves name to UUID if possible;
        returns name as-is if not resolvable.
        """
        if ":" not in principal_id:
            raise ValueError(
                f"Invalid principal_id format: '{principal_id}'. Expected 'type:value'."
            )

        ptype, pval = principal_id.split(":", 1)

        # Check if pval is already a UUID
        try:
            uuid.UUID(pval, version=4)
            return ptype, pval
        except ValueError:
            pass

        # Legacy name format — try to resolve
        resolved = self.resolve_name(ptype, pval)
        if resolved:
            return ptype, resolved
        return ptype, pval

    @staticmethod
    def _row_to_dict(row: sqlite3.Row) -> dict:
        """Convert a database row to a principal dict."""
        return {
            "uuid": row["uuid"],
            "type": row["type"],
            "name": row["name"],
            "created_at": row["created_at"],
            "metadata": json.loads(row["metadata"]),
        }
