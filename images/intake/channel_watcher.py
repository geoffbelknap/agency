"""Channel-watch source — monitor comms channels for matching messages."""

import logging
import re
import sqlite3
from datetime import datetime, timezone
from pathlib import Path
from typing import Optional

logger = logging.getLogger("intake.channel_watcher")


def matches_pattern(content: str, pattern: str) -> bool:
    """Check if message content matches a regex pattern.

    Returns False for invalid regex patterns rather than raising.
    """
    try:
        return re.search(pattern, content) is not None
    except re.error:
        return False


class ChannelWatchStateStore:
    """SQLite-backed state for channel-watch connectors."""

    def __init__(self, data_dir: Path):
        data_dir.mkdir(parents=True, exist_ok=True)
        self.db_path = data_dir / "channel_watch_state.db"
        self._init_db()

    def _init_db(self) -> None:
        with sqlite3.connect(str(self.db_path)) as conn:
            conn.execute("""
                CREATE TABLE IF NOT EXISTS channel_watch_state (
                    connector TEXT PRIMARY KEY,
                    last_seen TEXT NOT NULL
                )
            """)

    def get_last_seen(self, connector: str) -> Optional[datetime]:
        with sqlite3.connect(str(self.db_path)) as conn:
            row = conn.execute(
                "SELECT last_seen FROM channel_watch_state WHERE connector = ?",
                (connector,),
            ).fetchone()
        return datetime.fromisoformat(row[0]) if row else None

    def set_last_seen(self, connector: str, last_seen: datetime) -> None:
        with sqlite3.connect(str(self.db_path)) as conn:
            conn.execute("""
                INSERT INTO channel_watch_state (connector, last_seen) VALUES (?, ?)
                ON CONFLICT(connector) DO UPDATE SET last_seen = ?
            """, (connector, last_seen.isoformat(), last_seen.isoformat()))
