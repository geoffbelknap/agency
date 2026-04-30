"""Tests for knowledge graph push integration via gateway event bus."""

from unittest.mock import MagicMock, patch

import pytest

from services.knowledge.server import publish_knowledge_update
from services.knowledge.gateway_client import GatewayClient
from images.models.comms import Channel, ChannelType


class TestPublishKnowledgeUpdate:
    """Tests for publish_knowledge_update function (gateway delegation)."""

    def test_delegates_to_gateway_client(self):
        """publish_knowledge_update delegates to GatewayClient.publish_knowledge_update."""
        gateway = MagicMock(spec=GatewayClient)

        publish_knowledge_update(
            gateway=gateway,
            node_summary="agent alice knows about security",
            metadata={"node_id": "abc123", "kind": "agent", "topic": "alice", "contributed_by": "rule"},
        )

        gateway.publish_knowledge_update.assert_called_once_with(
            "agent alice knows about security",
            {"node_id": "abc123", "kind": "agent", "topic": "alice", "contributed_by": "rule"},
        )

class TestGatewayClient:
    """Tests for GatewayClient.publish_knowledge_update."""

    def test_publishes_event_to_gateway(self):
        """GatewayClient posts to /api/v1/events/publish with correct payload."""
        with patch("services.knowledge.gateway_client.httpx.Client") as mock_client_cls:
            mock_client = MagicMock()
            mock_resp = MagicMock()
            mock_resp.status_code = 200
            mock_client.post.return_value = mock_resp
            mock_client_cls.return_value = mock_client

            gw = GatewayClient(base_url="http://gateway:8200", token="test-token")
            gw.publish_knowledge_update(
                "deploy pipeline updated",
                {"node_id": "xyz789", "kind": "concept", "topic": "deploy", "contributed_by": "llm"},
            )

            mock_client.post.assert_called_once()
            call_args = mock_client.post.call_args
            assert call_args.args[0] == "http://gateway:8200/api/v1/events/publish"
            body = call_args.kwargs["json"]
            assert body["source_type"] == "platform"
            assert body["source_name"] == "knowledge"
            assert body["event_type"] == "knowledge_update"
            assert body["data"]["summary"] == "deploy pipeline updated"
            assert body["data"]["channel"] == "_knowledge-updates"
            assert body["data"]["node_id"] == "xyz789"
            assert body["data"]["kind"] == "concept"

    def test_sends_auth_header(self):
        """GatewayClient includes Authorization header when token is set."""
        with patch("services.knowledge.gateway_client.httpx.Client") as mock_client_cls:
            mock_client = MagicMock()
            mock_resp = MagicMock()
            mock_resp.status_code = 200
            mock_client.post.return_value = mock_resp
            mock_client_cls.return_value = mock_client

            gw = GatewayClient(base_url="http://gateway:8200", token="my-token")
            gw.publish_knowledge_update("test", {})

            call_args = mock_client.post.call_args
            headers = call_args.kwargs["headers"]
            assert headers["Authorization"] == "Bearer my-token"

    def test_does_not_raise_on_failure(self):
        """GatewayClient swallows exceptions and logs a warning."""
        with patch("services.knowledge.gateway_client.httpx.Client") as mock_client_cls:
            mock_client = MagicMock()
            mock_client.post.side_effect = Exception("connection refused")
            mock_client_cls.return_value = mock_client

            gw = GatewayClient(base_url="http://gateway:8200", token="")
            # Must not raise
            gw.publish_knowledge_update("test node", {"node_id": "n1"})


class TestKnowledgeUpdatesChannelModel:
    """Tests that the _knowledge-updates channel spec validates correctly."""

    def test_channel_validates_successfully(self):
        """_knowledge-updates channel spec passes Channel model validation."""
        channel = Channel(
            name="_knowledge-updates",
            type=ChannelType.SYSTEM,
            created_by="_platform",
            visibility="platform-write",
        )
        assert channel.name == "_knowledge-updates"
        assert channel.type == ChannelType.SYSTEM
        assert channel.created_by == "_platform"
        assert channel.visibility == "platform-write"

    def test_channel_type_is_system(self):
        """The channel type must be 'system'."""
        channel = Channel(
            name="_knowledge-updates",
            type="system",
            created_by="_platform",
            visibility="platform-write",
        )
        assert channel.type == ChannelType.SYSTEM

    def test_channel_visibility_platform_write(self):
        """platform-write visibility is valid for system channels."""
        channel = Channel(
            name="_knowledge-updates",
            type=ChannelType.SYSTEM,
            created_by="_platform",
            visibility="platform-write",
        )
        assert channel.visibility == "platform-write"

    def test_channel_rejects_invalid_visibility(self):
        """Invalid visibility values are rejected by the Channel model."""
        from pydantic import ValidationError
        with pytest.raises(ValidationError):
            Channel(
                name="_knowledge-updates",
                type=ChannelType.SYSTEM,
                created_by="_platform",
                visibility="agent-write",
            )

    def test_channel_name_pattern_accepts_underscore_prefix(self):
        """Channel names starting with _ are valid (system channels use this convention)."""
        channel = Channel(
            name="_knowledge-updates",
            type=ChannelType.SYSTEM,
            created_by="_platform",
        )
        assert channel.name == "_knowledge-updates"
