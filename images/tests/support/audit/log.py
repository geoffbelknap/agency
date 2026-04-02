"""JSONL audit logger for Agency events.

Logs are written by infrastructure, never by the agent (tenet 2).
"""

from __future__ import annotations

import fcntl
import json
from datetime import datetime, timezone
from pathlib import Path
from uuid import uuid4

# Reserved keys that cannot be overridden by caller-supplied data.
# Prevents audit log forgery via agent signals or other untrusted input.
_AGENT_RESERVED_KEYS = frozenset({"ts", "type", "agent", "session_id", "lifecycle_id"})
# SystemAuditLog has no agent/session_id fields — only protect ts and type.
_SYSTEM_RESERVED_KEYS = frozenset({"ts", "type"})


class AuditLog:
    """Structured JSONL audit logger."""

    def __init__(self, agent: str, log_dir: Path | None = None,
                 session_id: str | None = None, lifecycle_id: str | None = None):
        self.agent = agent
        self.lifecycle_id = lifecycle_id
        self.session_id = session_id or f"sess-{uuid4().hex[:12]}"
        self.log_dir = log_dir or (Path.home() / ".agency" / "audit" / agent)
        self.log_dir.mkdir(parents=True, exist_ok=True)
        self.log_dir.chmod(0o700)
        self._log_file = self.log_dir / f"{datetime.now(timezone.utc).strftime('%Y-%m-%d')}.jsonl"

    def record(self, event_type: str, data: dict | None = None) -> dict:
        """Record an event to the audit log. Returns the event dict."""
        if data:
            # Filter reserved keys from caller data to prevent forgery
            safe_data = {k: v for k, v in data.items() if k not in _AGENT_RESERVED_KEYS}
        else:
            safe_data = None

        event = {
            "ts": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
            "type": event_type,
            "agent": self.agent,
            "session_id": self.session_id,
        }
        if self.lifecycle_id:
            event["lifecycle_id"] = self.lifecycle_id
        if safe_data:
            event.update(safe_data)

        with open(self._log_file, "a") as f:
            fcntl.flock(f, fcntl.LOCK_EX)
            f.write(json.dumps(event) + "\n")
            f.flush()
            fcntl.flock(f, fcntl.LOCK_UN)

        return event

    def read_events(self, since: str | None = None) -> list[dict]:
        """Read events from the audit log."""
        events = []
        for log_file in sorted(self.log_dir.glob("*.jsonl")):
            with open(log_file) as f:
                for line in f:
                    line = line.strip()
                    if line:
                        event = json.loads(line)
                        if since and event.get("ts", "") < since:
                            continue
                        events.append(event)
        return events


class SystemAuditLog:
    """System-level audit log (not agent-specific)."""

    def __init__(self, log_dir: Path | None = None):
        self.log_dir = log_dir or (Path.home() / ".agency" / "audit" / "system")
        self.log_dir.mkdir(parents=True, exist_ok=True)
        self.log_dir.chmod(0o700)
        self._log_file = self.log_dir / f"{datetime.now(timezone.utc).strftime('%Y-%m-%d')}.jsonl"

    def record(self, event_type: str, data: dict | None = None) -> dict:
        if data:
            safe_data = {k: v for k, v in data.items() if k not in _SYSTEM_RESERVED_KEYS}
        else:
            safe_data = None

        event = {
            "ts": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
            "type": event_type,
        }
        if safe_data:
            event.update(safe_data)

        with open(self._log_file, "a") as f:
            fcntl.flock(f, fcntl.LOCK_EX)
            f.write(json.dumps(event) + "\n")
            f.flush()
            fcntl.flock(f, fcntl.LOCK_UN)

        return event
