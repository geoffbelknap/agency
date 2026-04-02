"""Tests for work item lifecycle and SQLite store."""

import pytest
from datetime import datetime, timezone, timedelta

from images.intake.work_items import WorkItem, WorkItemStore


@pytest.fixture
def store(tmp_path):
    return WorkItemStore(data_dir=tmp_path)


class TestWorkItemStore:
    def test_create_work_item(self, store):
        wi = store.create(
            connector="splunk-soc",
            payload={"alert_id": "A123", "severity": "critical"},
        )
        assert wi.id.startswith("wi-")
        assert wi.connector == "splunk-soc"
        assert wi.status == "received"
        assert wi.payload == {"alert_id": "A123", "severity": "critical"}

    def test_get_work_item(self, store):
        wi = store.create(connector="test", payload={"x": 1})
        fetched = store.get(wi.id)
        assert fetched is not None
        assert fetched.id == wi.id

    def test_get_nonexistent(self, store):
        assert store.get("wi-nonexistent") is None

    def test_update_status_routed(self, store):
        wi = store.create(connector="test", payload={"x": 1})
        store.update_status(
            wi.id,
            status="routed",
            target_type="agent",
            target_name="analyst",
            route_index=0,
        )
        updated = store.get(wi.id)
        assert updated.status == "routed"
        assert updated.target_name == "analyst"
        assert updated.route_index == 0

    def test_update_status_assigned(self, store):
        wi = store.create(connector="test", payload={"x": 1})
        store.update_status(wi.id, status="routed", target_type="agent", target_name="a")
        store.update_status(wi.id, status="assigned", task_content="Do the thing")
        updated = store.get(wi.id)
        assert updated.status == "assigned"
        assert updated.task_content == "Do the thing"

    def test_update_status_unrouted(self, store):
        wi = store.create(connector="test", payload={"x": 1})
        store.update_status(wi.id, status="unrouted")
        updated = store.get(wi.id)
        assert updated.status == "unrouted"

    def test_set_sla_deadline(self, store):
        wi = store.create(connector="test", payload={"x": 1})
        deadline = datetime.now(timezone.utc) + timedelta(minutes=15)
        store.update_status(wi.id, status="routed", target_type="agent", target_name="a", sla_deadline=deadline)
        updated = store.get(wi.id)
        assert updated.sla_deadline is not None

    def test_list_by_connector(self, store):
        store.create(connector="splunk", payload={"x": 1})
        store.create(connector="splunk", payload={"x": 2})
        store.create(connector="jira", payload={"x": 3})
        items = store.list_items(connector="splunk")
        assert len(items) == 2

    def test_list_by_status(self, store):
        wi1 = store.create(connector="test", payload={"x": 1})
        wi2 = store.create(connector="test", payload={"x": 2})
        store.update_status(wi1.id, status="unrouted")
        items = store.list_items(status="unrouted")
        assert len(items) == 1
        assert items[0].id == wi1.id

    def test_list_sla_breached(self, store):
        wi1 = store.create(connector="test", payload={"x": 1})
        wi2 = store.create(connector="test", payload={"x": 2})
        past = datetime.now(timezone.utc) - timedelta(hours=1)
        future = datetime.now(timezone.utc) + timedelta(hours=1)
        store.update_status(wi1.id, status="assigned", sla_deadline=past, task_content="t")
        store.update_status(wi2.id, status="assigned", sla_deadline=future, task_content="t")
        breached = store.list_sla_breached()
        assert len(breached) == 1
        assert breached[0].id == wi1.id

    def test_count_by_status(self, store):
        wi1 = store.create(connector="test", payload={"x": 1})
        wi2 = store.create(connector="test", payload={"x": 2})
        store.update_status(wi1.id, status="unrouted")
        stats = store.count_by_status()
        assert stats["received"] == 1
        assert stats["unrouted"] == 1

    def test_count_concurrent(self, store):
        wi1 = store.create(connector="test", payload={"x": 1})
        wi2 = store.create(connector="test", payload={"x": 2})
        store.update_status(wi1.id, status="assigned", task_content="t")
        assert store.count_concurrent("test") == 1

    def test_count_per_hour(self, store):
        store.create(connector="test", payload={"x": 1})
        store.create(connector="test", payload={"x": 2})
        assert store.count_per_hour("test") == 2

    def test_stats(self, store):
        store.create(connector="test", payload={"x": 1})
        stats = store.stats()
        assert stats["total"] == 1
        assert "by_status" in stats
        assert "by_connector" in stats
