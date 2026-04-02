"""Schedule source — cron-based task triggering."""

import logging
import sqlite3
from datetime import datetime, timezone
from pathlib import Path

from croniter import croniter
from typing import Optional

logger = logging.getLogger("intake.scheduler")


def should_fire(
    cron_expr: str,
    last_fired: Optional[datetime],
    now: Optional[datetime] = None,
) -> bool:
    """Check if a cron expression should fire now.

    Returns True if cron matches the current minute AND it hasn't already
    fired in this minute window. Does not retroactively fire missed schedules.
    """
    if now is None:
        now = datetime.now(timezone.utc)

    # Truncate to minute boundary for comparison
    now_minute = now.replace(second=0, microsecond=0)

    # Check if cron matches current minute
    if not croniter.match(cron_expr, now_minute):
        return False

    # Check double-fire: if last_fired is within the same minute, skip
    if last_fired is not None:
        last_minute = last_fired.replace(second=0, microsecond=0)
        if last_minute >= now_minute:
            return False

    return True


class ScheduleStateStore:
    """SQLite-backed state for schedule connectors."""

    def __init__(self, data_dir: Path):
        data_dir.mkdir(parents=True, exist_ok=True)
        self.db_path = data_dir / "schedule_state.db"
        self._init_db()

    def _init_db(self) -> None:
        with sqlite3.connect(str(self.db_path)) as conn:
            conn.execute("""
                CREATE TABLE IF NOT EXISTS schedule_state (
                    connector TEXT PRIMARY KEY,
                    last_fired TEXT NOT NULL
                )
            """)

    def get_last_fired(self, connector: str) -> Optional[datetime]:
        with sqlite3.connect(str(self.db_path)) as conn:
            row = conn.execute(
                "SELECT last_fired FROM schedule_state WHERE connector = ?",
                (connector,),
            ).fetchone()
        return datetime.fromisoformat(row[0]) if row else None

    def set_last_fired(self, connector: str, fired_at: datetime) -> None:
        with sqlite3.connect(str(self.db_path)) as conn:
            conn.execute("""
                INSERT INTO schedule_state (connector, last_fired) VALUES (?, ?)
                ON CONFLICT(connector) DO UPDATE SET last_fired = ?
            """, (connector, fired_at.isoformat(), fired_at.isoformat()))
