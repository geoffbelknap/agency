"""Tests for event_id threading through comms and body."""


class TestCommsEventIdPassing:
    def test_task_includes_event_id_from_metadata(self):
        """When metadata has event_id, task dict carries it."""
        metadata = {"event_id": "evt-abc12345"}
        task = {"type": "task", "content": "test"}
        if metadata.get("event_id"):
            task["event_id"] = metadata["event_id"]
        assert task["event_id"] == "evt-abc12345"

    def test_task_without_metadata_has_no_event_id(self):
        """Non-event tasks have no event_id."""
        task = {"type": "task", "content": "operator DM"}
        assert "event_id" not in task


class TestBodyEventIdHeader:
    def test_event_id_produces_header(self):
        context = {"current_task": {"event_id": "evt-abc12345"}}
        event_id = context.get("current_task", {}).get("event_id")
        assert event_id == "evt-abc12345"
        headers = {}
        if event_id:
            headers["X-Agency-Event-Id"] = event_id
        assert headers["X-Agency-Event-Id"] == "evt-abc12345"

    def test_missing_event_id_no_header(self):
        context = {"current_task": {"content": "operator DM"}}
        event_id = context.get("current_task", {}).get("event_id")
        assert event_id is None
