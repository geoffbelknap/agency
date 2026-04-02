"""Audit log retention — rotation, cleanup, and export.

Configurable retention policies for audit logs. Logs older than the
retention period are archived or deleted. Export produces a single
file suitable for regulator review.

Retention config stored at ~/.agency/audit/retention.yaml.
"""

import gzip
import json
import logging
import shutil
from dataclasses import dataclass
from datetime import datetime, timedelta, timezone
from pathlib import Path

import yaml
from typing import Optional

log = logging.getLogger(__name__)


@dataclass
class RetentionPolicy:
    """Audit log retention configuration."""
    retain_days: int = 90  # keep logs for N days
    archive_days: int = 30  # compress logs older than N days
    max_size_mb: int = 500  # max total log size before forced cleanup
    export_format: str = "jsonl"  # jsonl or json

    @classmethod
    def from_dict(cls, data: dict) -> "RetentionPolicy":
        return cls(
            retain_days=data.get("retain_days", 90),
            archive_days=data.get("archive_days", 30),
            max_size_mb=data.get("max_size_mb", 500),
            export_format=data.get("export_format", "jsonl"),
        )

    def to_dict(self) -> dict:
        return {
            "retain_days": self.retain_days,
            "archive_days": self.archive_days,
            "max_size_mb": self.max_size_mb,
            "export_format": self.export_format,
        }


class AuditRetention:
    """Manages audit log retention, archival, and export."""

    def __init__(self, agency_home: Optional[Path] = None):
        self.home = agency_home or Path.home() / ".agency"
        self.audit_dir = self.home / "audit"

    def load_policy(self) -> RetentionPolicy:
        """Load retention policy from config, or return defaults."""
        config_file = self.audit_dir / "retention.yaml"
        if config_file.exists():
            try:
                data = yaml.safe_load(config_file.read_text())
                if data:
                    return RetentionPolicy.from_dict(data)
            except Exception as e:
                log.warning("Failed to load retention policy: %s", e)
        return RetentionPolicy()

    def save_policy(self, policy: RetentionPolicy) -> None:
        """Save retention policy to config."""
        self.audit_dir.mkdir(parents=True, exist_ok=True)
        config_file = self.audit_dir / "retention.yaml"
        config_file.write_text(yaml.dump(policy.to_dict(), default_flow_style=False))

    def apply(self, policy: Optional[RetentionPolicy] = None) -> dict:
        """Apply retention policy: archive old logs, delete expired logs.

        Returns summary: {archived, deleted, total_size_mb}.
        """
        policy = policy or self.load_policy()
        now = datetime.now(timezone.utc)
        archive_cutoff = now - timedelta(days=policy.archive_days)
        delete_cutoff = now - timedelta(days=policy.retain_days)

        archived = 0
        deleted = 0

        for log_file in self._find_log_files():
            file_date = self._parse_log_date(log_file)
            if not file_date:
                continue

            if file_date < delete_cutoff:
                log_file.unlink()
                deleted += 1
            elif file_date < archive_cutoff and log_file.suffix == ".jsonl":
                self._compress(log_file)
                archived += 1

        total_size = self._total_size_mb()

        # Force cleanup if over size limit
        if total_size > policy.max_size_mb:
            extra_deleted = self._force_cleanup(policy.max_size_mb)
            deleted += extra_deleted
            total_size = self._total_size_mb()

        return {
            "archived": archived,
            "deleted": deleted,
            "total_size_mb": round(total_size, 2),
        }

    def export(
        self,
        output_path: Path,
        agent: Optional[str] = None,
        since: Optional[str] = None,
        until: Optional[str] = None,
        format: str = "jsonl",
    ) -> dict:
        """Export audit logs to a single file for regulator review.

        Returns {events_exported, file_path, size_bytes}.
        """
        events = []

        search_dirs = []
        if agent:
            agent_dir = self.audit_dir / agent
            if agent_dir.exists():
                search_dirs.append(agent_dir)
        else:
            if self.audit_dir.exists():
                for d in sorted(self.audit_dir.iterdir()):
                    if d.is_dir():
                        search_dirs.append(d)

        for search_dir in search_dirs:
            for log_file in sorted(search_dir.glob("*.jsonl")):
                events.extend(self._read_events(log_file, since, until))
            for gz_file in sorted(search_dir.glob("*.jsonl.gz")):
                events.extend(self._read_compressed_events(gz_file, since, until))

        # Sort by timestamp
        events.sort(key=lambda e: e.get("ts", ""))

        # Write output
        output_path.parent.mkdir(parents=True, exist_ok=True)
        if format == "json":
            output_path.write_text(json.dumps(events, indent=2) + "\n")
        else:
            with open(output_path, "w") as f:
                for event in events:
                    f.write(json.dumps(event) + "\n")

        return {
            "events_exported": len(events),
            "file_path": str(output_path),
            "size_bytes": output_path.stat().st_size,
        }

    def stats(self) -> dict:
        """Get audit log statistics."""
        if not self.audit_dir.exists():
            return {"agents": 0, "total_files": 0, "total_size_mb": 0, "oldest": None}

        agents = 0
        total_files = 0
        oldest = None

        for d in self.audit_dir.iterdir():
            if not d.is_dir() or d.name == "system":
                continue
            agents += 1
            for f in d.iterdir():
                if f.suffix in (".jsonl", ".gz"):
                    total_files += 1
                    file_date = self._parse_log_date(f)
                    if file_date and (oldest is None or file_date < oldest):
                        oldest = file_date

        return {
            "agents": agents,
            "total_files": total_files,
            "total_size_mb": round(self._total_size_mb(), 2),
            "oldest": oldest.strftime("%Y-%m-%d") if oldest else None,
        }

    def _find_log_files(self) -> list[Path]:
        """Find all audit log files (jsonl and gz)."""
        if not self.audit_dir.exists():
            return []
        files = []
        for d in self.audit_dir.iterdir():
            if d.is_dir():
                files.extend(d.glob("*.jsonl"))
                files.extend(d.glob("*.jsonl.gz"))
        return sorted(files)

    def _parse_log_date(self, path: Path) -> Optional[datetime]:
        """Parse date from log filename (YYYY-MM-DD.jsonl)."""
        name = path.name.replace(".jsonl.gz", "").replace(".jsonl", "")
        try:
            return datetime.strptime(name, "%Y-%m-%d").replace(tzinfo=timezone.utc)
        except ValueError:
            return None

    def _compress(self, log_file: Path) -> None:
        """Compress a log file to .gz."""
        gz_path = log_file.with_suffix(log_file.suffix + ".gz")
        with open(log_file, "rb") as f_in:
            with gzip.open(gz_path, "wb") as f_out:
                shutil.copyfileobj(f_in, f_out)
        log_file.unlink()

    def _total_size_mb(self) -> float:
        """Calculate total size of all audit files in MB."""
        total = 0
        for f in self._find_log_files():
            total += f.stat().st_size
        return total / (1024 * 1024)

    def _force_cleanup(self, max_mb: float) -> int:
        """Delete oldest files until under size limit."""
        deleted = 0
        files = self._find_log_files()
        for f in files:
            if self._total_size_mb() <= max_mb:
                break
            f.unlink()
            deleted += 1
        return deleted

    def _read_events(
        self, path: Path, since: Optional[str], until: Optional[str]
    ) -> list[dict]:
        """Read events from a JSONL file with optional time filter."""
        events = []
        try:
            with open(path) as f:
                for line in f:
                    line = line.strip()
                    if not line:
                        continue
                    event = json.loads(line)
                    ts = event.get("ts", "")
                    if since and ts < since:
                        continue
                    if until and ts > until:
                        continue
                    events.append(event)
        except Exception:
            pass
        return events

    def _read_compressed_events(
        self, path: Path, since: Optional[str], until: Optional[str]
    ) -> list[dict]:
        """Read events from a gzipped JSONL file."""
        events = []
        try:
            with gzip.open(path, "rt") as f:
                for line in f:
                    line = line.strip()
                    if not line:
                        continue
                    event = json.loads(line)
                    ts = event.get("ts", "")
                    if since and ts < since:
                        continue
                    if until and ts > until:
                        continue
                    events.append(event)
        except Exception:
            pass
        return events
