"""Tests for audit log retention, archival, and export."""

import gzip
import json
import pytest
from datetime import datetime, timedelta, timezone
from pathlib import Path

from images.tests.support.audit.retention import AuditRetention, RetentionPolicy


@pytest.fixture
def retention_home(tmp_path):
    home = tmp_path / ".agency"
    audit_dir = home / "audit"
    audit_dir.mkdir(parents=True)
    return home


def _create_log_file(audit_dir, agent, date_str, events=None):
    """Create a test log file with optional events."""
    agent_dir = audit_dir / agent
    agent_dir.mkdir(parents=True, exist_ok=True)
    log_file = agent_dir / f"{date_str}.jsonl"
    if events is None:
        events = [{"ts": f"{date_str}T12:00:00Z", "event": "test", "agent": agent}]
    with open(log_file, "w") as f:
        for event in events:
            f.write(json.dumps(event) + "\n")
    return log_file


def _create_gz_file(audit_dir, agent, date_str, events=None):
    """Create a gzipped test log file."""
    agent_dir = audit_dir / agent
    agent_dir.mkdir(parents=True, exist_ok=True)
    gz_file = agent_dir / f"{date_str}.jsonl.gz"
    if events is None:
        events = [{"ts": f"{date_str}T12:00:00Z", "event": "test", "agent": agent}]
    with gzip.open(gz_file, "wt") as f:
        for event in events:
            f.write(json.dumps(event) + "\n")
    return gz_file


class TestRetentionPolicy:
    def test_defaults(self):
        policy = RetentionPolicy()
        assert policy.retain_days == 90
        assert policy.archive_days == 30
        assert policy.max_size_mb == 500

    def test_from_dict(self):
        policy = RetentionPolicy.from_dict({"retain_days": 60, "archive_days": 14})
        assert policy.retain_days == 60
        assert policy.archive_days == 14
        assert policy.max_size_mb == 500  # default

    def test_serialization(self):
        policy = RetentionPolicy(retain_days=45, export_format="json")
        d = policy.to_dict()
        p2 = RetentionPolicy.from_dict(d)
        assert p2.retain_days == 45
        assert p2.export_format == "json"


class TestRetentionPolicySaveLoad:
    def test_save_and_load(self, retention_home):
        retention = AuditRetention(agency_home=retention_home)
        policy = RetentionPolicy(retain_days=60, archive_days=7)
        retention.save_policy(policy)

        loaded = retention.load_policy()
        assert loaded.retain_days == 60
        assert loaded.archive_days == 7

    def test_load_defaults_when_no_file(self, retention_home):
        retention = AuditRetention(agency_home=retention_home)
        policy = retention.load_policy()
        assert policy.retain_days == 90


class TestApply:
    def test_archives_old_logs(self, retention_home):
        audit_dir = retention_home / "audit"
        now = datetime.now(timezone.utc)
        old_date = (now - timedelta(days=35)).strftime("%Y-%m-%d")
        _create_log_file(audit_dir, "test-agent", old_date)

        retention = AuditRetention(agency_home=retention_home)
        policy = RetentionPolicy(archive_days=30, retain_days=90)
        result = retention.apply(policy)

        assert result["archived"] == 1
        # Original should be gone, .gz should exist
        assert not (audit_dir / "test-agent" / f"{old_date}.jsonl").exists()
        assert (audit_dir / "test-agent" / f"{old_date}.jsonl.gz").exists()

    def test_deletes_expired_logs(self, retention_home):
        audit_dir = retention_home / "audit"
        now = datetime.now(timezone.utc)
        expired_date = (now - timedelta(days=100)).strftime("%Y-%m-%d")
        _create_log_file(audit_dir, "test-agent", expired_date)

        retention = AuditRetention(agency_home=retention_home)
        policy = RetentionPolicy(retain_days=90)
        result = retention.apply(policy)

        assert result["deleted"] == 1
        assert not (audit_dir / "test-agent" / f"{expired_date}.jsonl").exists()

    def test_keeps_recent_logs(self, retention_home):
        audit_dir = retention_home / "audit"
        now = datetime.now(timezone.utc)
        recent_date = (now - timedelta(days=5)).strftime("%Y-%m-%d")
        _create_log_file(audit_dir, "test-agent", recent_date)

        retention = AuditRetention(agency_home=retention_home)
        policy = RetentionPolicy(archive_days=30, retain_days=90)
        result = retention.apply(policy)

        assert result["archived"] == 0
        assert result["deleted"] == 0
        assert (audit_dir / "test-agent" / f"{recent_date}.jsonl").exists()

    def test_force_cleanup_on_size_limit(self, retention_home):
        audit_dir = retention_home / "audit"
        now = datetime.now(timezone.utc)

        # Create multiple files (all recent so they won't be archived/deleted by date)
        for i in range(5):
            date_str = (now - timedelta(days=i + 1)).strftime("%Y-%m-%d")
            events = [{"ts": f"{date_str}T12:00:00Z", "data": "x" * 10000}]
            _create_log_file(audit_dir, "test-agent", date_str, events)

        retention = AuditRetention(agency_home=retention_home)
        # Set a very small size limit to trigger forced cleanup
        policy = RetentionPolicy(
            archive_days=90, retain_days=180,
            max_size_mb=0.00001,  # tiny limit
        )
        result = retention.apply(policy)

        # Should have deleted some files to get under the limit
        assert result["deleted"] > 0

    def test_empty_audit_dir(self, retention_home):
        retention = AuditRetention(agency_home=retention_home)
        result = retention.apply()
        assert result["archived"] == 0
        assert result["deleted"] == 0

    def test_deletes_expired_gz(self, retention_home):
        audit_dir = retention_home / "audit"
        now = datetime.now(timezone.utc)
        expired_date = (now - timedelta(days=100)).strftime("%Y-%m-%d")
        _create_gz_file(audit_dir, "test-agent", expired_date)

        retention = AuditRetention(agency_home=retention_home)
        policy = RetentionPolicy(retain_days=90)
        result = retention.apply(policy)

        assert result["deleted"] == 1


class TestExport:
    def test_export_jsonl(self, retention_home):
        audit_dir = retention_home / "audit"
        _create_log_file(audit_dir, "test-agent", "2026-03-01", [
            {"ts": "2026-03-01T10:00:00Z", "event": "a"},
            {"ts": "2026-03-01T11:00:00Z", "event": "b"},
        ])

        retention = AuditRetention(agency_home=retention_home)
        output = retention_home / "export.jsonl"
        result = retention.export(output)

        assert result["events_exported"] == 2
        assert result["size_bytes"] > 0
        assert output.exists()

        lines = output.read_text().strip().split("\n")
        assert len(lines) == 2

    def test_export_json_format(self, retention_home):
        audit_dir = retention_home / "audit"
        _create_log_file(audit_dir, "test-agent", "2026-03-01", [
            {"ts": "2026-03-01T10:00:00Z", "event": "test"},
        ])

        retention = AuditRetention(agency_home=retention_home)
        output = retention_home / "export.json"
        result = retention.export(output, format="json")

        content = json.loads(output.read_text())
        assert isinstance(content, list)
        assert len(content) == 1

    def test_export_filtered_by_agent(self, retention_home):
        audit_dir = retention_home / "audit"
        _create_log_file(audit_dir, "agent-a", "2026-03-01", [
            {"ts": "2026-03-01T10:00:00Z", "event": "a"},
        ])
        _create_log_file(audit_dir, "agent-b", "2026-03-01", [
            {"ts": "2026-03-01T10:00:00Z", "event": "b"},
        ])

        retention = AuditRetention(agency_home=retention_home)
        output = retention_home / "export.jsonl"
        result = retention.export(output, agent="agent-a")

        assert result["events_exported"] == 1

    def test_export_filtered_by_time(self, retention_home):
        audit_dir = retention_home / "audit"
        _create_log_file(audit_dir, "test-agent", "2026-03-01", [
            {"ts": "2026-03-01T10:00:00Z", "event": "early"},
            {"ts": "2026-03-01T20:00:00Z", "event": "late"},
        ])

        retention = AuditRetention(agency_home=retention_home)
        output = retention_home / "export.jsonl"
        result = retention.export(
            output, since="2026-03-01T15:00:00Z"
        )

        assert result["events_exported"] == 1

    def test_export_includes_compressed(self, retention_home):
        audit_dir = retention_home / "audit"
        _create_gz_file(audit_dir, "test-agent", "2026-02-01", [
            {"ts": "2026-02-01T10:00:00Z", "event": "archived"},
        ])
        _create_log_file(audit_dir, "test-agent", "2026-03-01", [
            {"ts": "2026-03-01T10:00:00Z", "event": "current"},
        ])

        retention = AuditRetention(agency_home=retention_home)
        output = retention_home / "export.jsonl"
        result = retention.export(output)

        assert result["events_exported"] == 2

    def test_export_sorted_by_timestamp(self, retention_home):
        audit_dir = retention_home / "audit"
        _create_log_file(audit_dir, "test-agent", "2026-03-02", [
            {"ts": "2026-03-02T10:00:00Z", "event": "second"},
        ])
        _create_log_file(audit_dir, "test-agent", "2026-03-01", [
            {"ts": "2026-03-01T10:00:00Z", "event": "first"},
        ])

        retention = AuditRetention(agency_home=retention_home)
        output = retention_home / "export.jsonl"
        retention.export(output)

        lines = output.read_text().strip().split("\n")
        events = [json.loads(l) for l in lines]
        assert events[0]["event"] == "first"
        assert events[1]["event"] == "second"

    def test_export_empty(self, retention_home):
        retention = AuditRetention(agency_home=retention_home)
        output = retention_home / "export.jsonl"
        result = retention.export(output)
        assert result["events_exported"] == 0


class TestStats:
    def test_stats_with_logs(self, retention_home):
        audit_dir = retention_home / "audit"
        _create_log_file(audit_dir, "agent-a", "2026-03-01")
        _create_log_file(audit_dir, "agent-a", "2026-03-02")
        _create_log_file(audit_dir, "agent-b", "2026-03-01")

        retention = AuditRetention(agency_home=retention_home)
        stats = retention.stats()

        assert stats["agents"] == 2
        assert stats["total_files"] == 3
        assert stats["oldest"] == "2026-03-01"

    def test_stats_empty(self, tmp_path):
        home = tmp_path / ".agency"
        home.mkdir()
        retention = AuditRetention(agency_home=home)
        stats = retention.stats()

        assert stats["agents"] == 0
        assert stats["total_files"] == 0
        assert stats["oldest"] is None

    def test_stats_skips_system_dir(self, retention_home):
        audit_dir = retention_home / "audit"
        _create_log_file(audit_dir, "test-agent", "2026-03-01")
        # system dir should be skipped
        system_dir = audit_dir / "system"
        system_dir.mkdir()
        (system_dir / "2026-03-01.jsonl").write_text('{"event":"sys"}\n')

        retention = AuditRetention(agency_home=retention_home)
        stats = retention.stats()

        assert stats["agents"] == 1  # only test-agent
