"""Poll source — periodic API fetching with hash-based change detection."""

import hashlib
import json
import logging
import re
import sqlite3
from pathlib import Path

logger = logging.getLogger("intake.poller")

_INTERVAL_PATTERN = re.compile(r"^(\d+)([smhd])$")


def hash_blob(data: dict | list) -> str:
    """Deterministic SHA-256 hash of JSON-serializable data."""
    raw = json.dumps(data, sort_keys=True, separators=(",", ":"))
    return hashlib.sha256(raw.encode()).hexdigest()[:16]


def hash_items(items: list[dict]) -> list[str]:
    """Hash each item in a list individually."""
    return [hash_blob(item) for item in items]


def extract_items(data: dict | list, response_key: str) -> list[dict] | None:
    """Extract a list from data using a simple JSON path.

    Supports '$' (root is the list) and '$.key' (one level deep).
    Returns None if extraction fails or result is not a list.
    """
    if response_key == "$":
        return data if isinstance(data, list) else None

    if response_key.startswith("$."):
        key = response_key[2:]
        if isinstance(data, dict) and key in data:
            val = data[key]
            return val if isinstance(val, list) else None
    return None


def parse_interval(interval: str) -> int:
    """Parse interval string (e.g. '5m') to seconds."""
    m = _INTERVAL_PATTERN.match(interval)
    if not m:
        raise ValueError(f"Invalid interval: {interval}")
    value, unit = int(m.group(1)), m.group(2)
    multipliers = {"s": 1, "m": 60, "h": 3600, "d": 86400}
    return value * multipliers[unit]


class PollStateStore:
    """SQLite-backed state for poll connectors."""

    def __init__(self, data_dir: Path):
        data_dir.mkdir(parents=True, exist_ok=True)
        self.db_path = data_dir / "poll_state.db"
        self._init_db()

    def _init_db(self) -> None:
        with sqlite3.connect(str(self.db_path)) as conn:
            conn.execute("""
                CREATE TABLE IF NOT EXISTS poll_hashes (
                    connector TEXT NOT NULL,
                    hash TEXT NOT NULL,
                    PRIMARY KEY (connector, hash)
                )
            """)
            conn.execute("""
                CREATE TABLE IF NOT EXISTS poll_failures (
                    connector TEXT PRIMARY KEY,
                    count INTEGER NOT NULL DEFAULT 0
                )
            """)

    def get_hashes(self, connector: str) -> set[str]:
        with sqlite3.connect(str(self.db_path)) as conn:
            rows = conn.execute(
                "SELECT hash FROM poll_hashes WHERE connector = ?", (connector,)
            ).fetchall()
        return {r[0] for r in rows}

    def set_hashes(self, connector: str, hashes: set[str]) -> None:
        with sqlite3.connect(str(self.db_path)) as conn:
            conn.execute("DELETE FROM poll_hashes WHERE connector = ?", (connector,))
            for h in hashes:
                conn.execute(
                    "INSERT INTO poll_hashes (connector, hash) VALUES (?, ?)",
                    (connector, h),
                )

    def get_failure_count(self, connector: str) -> int:
        with sqlite3.connect(str(self.db_path)) as conn:
            row = conn.execute(
                "SELECT count FROM poll_failures WHERE connector = ?", (connector,)
            ).fetchone()
        return row[0] if row else 0

    def increment_failure_count(self, connector: str) -> None:
        with sqlite3.connect(str(self.db_path)) as conn:
            conn.execute("""
                INSERT INTO poll_failures (connector, count) VALUES (?, 1)
                ON CONFLICT(connector) DO UPDATE SET count = count + 1
            """, (connector,))

    def reset_failure_count(self, connector: str) -> None:
        with sqlite3.connect(str(self.db_path)) as conn:
            conn.execute(
                "DELETE FROM poll_failures WHERE connector = ?", (connector,)
            )
