"""End-to-end-ish tests for the Slack bridge v1 message path."""

import os
import sys
from pathlib import Path

import yaml

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))
sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "body"))

from body import Body  # noqa: E402
from images.intake.router import render_template  # noqa: E402
from images.models.connector import ConnectorConfig  # noqa: E402


class _FakeHTTPClient:
    def __init__(self):
        self.calls = []

    def post(self, url, json=None, timeout=None):
        self.calls.append({"url": url, "json": json, "timeout": timeout})


class TestSlackBridgeV1:
    def _load_connector(self, relative_path: str) -> ConnectorConfig:
        repo_root = Path(__file__).resolve().parents[2]
        path = repo_root.parent / "agency-hub" / relative_path
        return ConnectorConfig.model_validate(yaml.safe_load(path.read_text()))

    def test_task_response_posts_bridge_metadata_and_renders_for_slack(self, monkeypatch):
        monkeypatch.setenv("AGENCY_COMMS_URL", "http://comms:18091")

        body = Body.__new__(Body)
        body.agent_name = "slack-bridge"
        body._http_client = _FakeHTTPClient()

        task = {
            "source": "connector:slack-events",
            "task_id": "task-123",
            "metadata": {
                "bridge": {
                    "platform": "slack",
                    "channel_id": "D123",
                    "thread_ts": "1712860000.1234",
                    "conversation_key": "slack:D123:1712860000.1234",
                },
                "principal": {
                    "platform": "slack",
                    "workspace_id": "T123",
                    "user_id": "U123",
                    "channel_id": "D123",
                    "conversation_key": "slack:D123:1712860000.1234",
                    "is_dm": True,
                },
            },
        }

        body._post_task_response(task, "Bridge reply", has_artifact=True)

        assert len(body._http_client.calls) == 1
        call = body._http_client.calls[0]
        assert call["url"] == "http://comms:18091/channels/slack-events/messages"
        payload = call["json"]
        assert payload["author"] == "slack-bridge"
        assert payload["metadata"]["bridge"]["channel_id"] == "D123"
        assert payload["metadata"]["principal"]["user_id"] == "U123"
        assert payload["metadata"]["has_artifact"] is True
        assert payload["metadata"]["attachment_id"] == "task-123"

        config = self._load_connector("connectors/agency-bridge-slack-events-outbound/connector.yaml")
        rendered = render_template(config.routes[0].relay.body, payload)
        assert '"channel": "D123"' in rendered
        assert '"thread_ts": "1712860000.1234"' in rendered
        assert "Attachment: task-123" in rendered
