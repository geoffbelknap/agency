"""Tests for lifecycle_id injection in AuditLog."""

import json
from pathlib import Path

from images.tests.support.audit.log import AuditLog


def test_audit_log_injects_lifecycle_id(tmp_path):
    audit = AuditLog("testbot", log_dir=tmp_path, lifecycle_id="test-uuid-456")
    audit.record("agent_signal_error", {"category": "llm.call_failed"})

    log_files = list(tmp_path.glob("*.jsonl"))
    assert len(log_files) == 1
    line = log_files[0].read_text().strip()
    event = json.loads(line)
    assert event["lifecycle_id"] == "test-uuid-456"


def test_audit_log_no_lifecycle_id_when_not_set(tmp_path):
    audit = AuditLog("testbot", log_dir=tmp_path)
    audit.record("test_event", {})

    log_files = list(tmp_path.glob("*.jsonl"))
    line = log_files[0].read_text().strip()
    event = json.loads(line)
    assert "lifecycle_id" not in event


def test_lifecycle_id_cannot_be_forged_via_data(tmp_path):
    audit = AuditLog("testbot", log_dir=tmp_path, lifecycle_id="real-uuid")
    audit.record("test_event", {"lifecycle_id": "forged-uuid"})

    log_files = list(tmp_path.glob("*.jsonl"))
    line = log_files[0].read_text().strip()
    event = json.loads(line)
    assert event["lifecycle_id"] == "real-uuid"
