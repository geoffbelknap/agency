"""Bridge state for chat-oriented connectors."""

import json
import sqlite3
from pathlib import Path
from typing import Optional


class BridgeStateStore:
    """SQLite-backed durable conversation mapping state for bridges."""

    def __init__(self, data_dir: Path):
        data_dir.mkdir(parents=True, exist_ok=True)
        self.db_path = data_dir / "bridge_state.db"
        self._init_db()

    def _init_db(self) -> None:
        with sqlite3.connect(str(self.db_path)) as conn:
            conn.execute(
                """
                CREATE TABLE IF NOT EXISTS bridge_conversations (
                    conversation_key TEXT PRIMARY KEY,
                    platform TEXT NOT NULL,
                    workspace_id TEXT,
                    channel_id TEXT,
                    root_ts TEXT,
                    thread_ts TEXT,
                    conversation_kind TEXT,
                    user_id TEXT,
                    target_agent TEXT,
                    connector_name TEXT,
                    metadata_json TEXT NOT NULL
                )
                """
            )

    def upsert_conversation(
        self,
        conversation_key: str,
        *,
        platform: str,
        workspace_id: Optional[str],
        channel_id: Optional[str],
        root_ts: Optional[str],
        thread_ts: Optional[str],
        conversation_kind: Optional[str],
        user_id: Optional[str],
        target_agent: Optional[str],
        connector_name: str,
        metadata: dict,
    ) -> None:
        with sqlite3.connect(str(self.db_path)) as conn:
            conn.execute(
                """
                INSERT INTO bridge_conversations (
                    conversation_key,
                    platform,
                    workspace_id,
                    channel_id,
                    root_ts,
                    thread_ts,
                    conversation_kind,
                    user_id,
                    target_agent,
                    connector_name,
                    metadata_json
                ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                ON CONFLICT(conversation_key) DO UPDATE SET
                    platform = excluded.platform,
                    workspace_id = excluded.workspace_id,
                    channel_id = excluded.channel_id,
                    root_ts = excluded.root_ts,
                    thread_ts = excluded.thread_ts,
                    conversation_kind = excluded.conversation_kind,
                    user_id = excluded.user_id,
                    target_agent = excluded.target_agent,
                    connector_name = excluded.connector_name,
                    metadata_json = excluded.metadata_json
                """,
                (
                    conversation_key,
                    platform,
                    workspace_id,
                    channel_id,
                    root_ts,
                    thread_ts,
                    conversation_kind,
                    user_id,
                    target_agent,
                    connector_name,
                    json.dumps(metadata, sort_keys=True),
                ),
            )

    def get_conversation(self, conversation_key: str) -> Optional[dict]:
        with sqlite3.connect(str(self.db_path)) as conn:
            row = conn.execute(
                """
                SELECT
                    conversation_key,
                    platform,
                    workspace_id,
                    channel_id,
                    root_ts,
                    thread_ts,
                    conversation_kind,
                    user_id,
                    target_agent,
                    connector_name,
                    metadata_json
                FROM bridge_conversations
                WHERE conversation_key = ?
                """,
                (conversation_key,),
            ).fetchone()
        if row is None:
            return None
        return {
            "conversation_key": row[0],
            "platform": row[1],
            "workspace_id": row[2],
            "channel_id": row[3],
            "root_ts": row[4],
            "thread_ts": row[5],
            "conversation_kind": row[6],
            "user_id": row[7],
            "target_agent": row[8],
            "connector_name": row[9],
            "metadata": json.loads(row[10]),
        }
