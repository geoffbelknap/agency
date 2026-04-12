"""Tests for bridge conversation state."""

from images.intake.bridge_state import BridgeStateStore


class TestBridgeStateStore:
    def test_upsert_and_get_conversation(self, tmp_path):
        store = BridgeStateStore(tmp_path)
        metadata = {
            "bridge": {
                "platform": "slack",
                "workspace_id": "T123",
                "channel_id": "D123",
                "thread_ts": "1712860000.1234",
                "root_ts": "1712860000.1234",
                "conversation_key": "slack:D123:1712860000.1234",
                "conversation_kind": "dm",
                "user_id": "U123",
            },
            "principal": {
                "platform": "slack",
                "workspace_id": "T123",
                "user_id": "U123",
                "channel_id": "D123",
                "conversation_key": "slack:D123:1712860000.1234",
                "is_dm": True,
            },
        }
        store.upsert_conversation(
            "slack:D123:1712860000.1234",
            platform="slack",
            workspace_id="T123",
            channel_id="D123",
            root_ts="1712860000.1234",
            thread_ts="1712860000.1234",
            conversation_kind="dm",
            user_id="U123",
            target_agent="slack-bridge",
            connector_name="slack-events",
            metadata=metadata,
        )

        record = store.get_conversation("slack:D123:1712860000.1234")
        assert record is not None
        assert record["platform"] == "slack"
        assert record["target_agent"] == "slack-bridge"
        assert record["metadata"]["principal"]["user_id"] == "U123"

    def test_upsert_overwrites_existing_record(self, tmp_path):
        store = BridgeStateStore(tmp_path)
        store.upsert_conversation(
            "slack:C123:1",
            platform="slack",
            workspace_id="T123",
            channel_id="C123",
            root_ts="1",
            thread_ts="1",
            conversation_kind="thread",
            user_id="U123",
            target_agent="slack-bridge",
            connector_name="slack-events",
            metadata={"bridge": {"conversation_key": "slack:C123:1"}},
        )
        store.upsert_conversation(
            "slack:C123:1",
            platform="slack",
            workspace_id="T123",
            channel_id="C123",
            root_ts="1",
            thread_ts="2",
            conversation_kind="thread",
            user_id="U456",
            target_agent="slack-bridge",
            connector_name="slack-events",
            metadata={"bridge": {"conversation_key": "slack:C123:1", "thread_ts": "2"}},
        )

        record = store.get_conversation("slack:C123:1")
        assert record is not None
        assert record["thread_ts"] == "2"
        assert record["user_id"] == "U456"
