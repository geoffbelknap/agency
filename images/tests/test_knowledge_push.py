"""Tests for knowledge graph push integration to comms channel."""

from unittest.mock import MagicMock, patch

import pytest

from images.knowledge.server import publish_knowledge_update
from images.models.comms import Channel, ChannelType


class TestPublishKnowledgeUpdate:
    """Tests for publish_knowledge_update function."""

    def test_calls_httpx_post_with_correct_url(self):
        """publish_knowledge_update posts to the _knowledge-updates channel endpoint."""
        comms_url = "http://comms:18091"
        with patch("images.knowledge.server.httpx.Client") as mock_client_cls:
            mock_client = MagicMock()
            mock_client_cls.return_value = mock_client

            publish_knowledge_update(
                comms_url=comms_url,
                node_summary="agent alice knows about security",
                metadata={"node_id": "abc123", "kind": "agent", "topic": "alice", "contributed_by": "rule"},
            )

            mock_client_cls.assert_called_once_with(timeout=5)
            mock_client.post.assert_called_once_with(
                f"{comms_url}/channels/_knowledge-updates/messages",
                json={
                    "author": "_knowledge-service",
                    "content": "agent alice knows about security",
                    "metadata": {
                        "node_id": "abc123",
                        "kind": "agent",
                        "topic": "alice",
                        "contributed_by": "rule",
                    },
                },
                headers={"X-Agency-Platform": "true"},
            )

    def test_calls_httpx_post_with_correct_body(self):
        """publish_knowledge_update sends author, content, and metadata fields."""
        with patch("images.knowledge.server.httpx.Client") as mock_client_cls:
            mock_client = MagicMock()
            mock_client_cls.return_value = mock_client

            publish_knowledge_update(
                comms_url="http://localhost:18091",
                node_summary="deploy pipeline updated",
                metadata={"node_id": "xyz789", "kind": "concept", "topic": "deploy", "contributed_by": "llm"},
            )

            call_kwargs = mock_client.post.call_args
            body = call_kwargs.kwargs["json"]
            assert body["author"] == "_knowledge-service"
            assert body["content"] == "deploy pipeline updated"
            assert body["metadata"]["node_id"] == "xyz789"
            assert body["metadata"]["kind"] == "concept"
            assert body["metadata"]["topic"] == "deploy"
            assert body["metadata"]["contributed_by"] == "llm"

    def test_does_not_raise_on_httpx_failure(self):
        """publish_knowledge_update swallows exceptions and logs a warning."""
        with patch("images.knowledge.server.httpx.Client") as mock_client_cls:
            mock_client = MagicMock()
            mock_client.post.side_effect = Exception("connection refused")
            mock_client_cls.return_value = mock_client

            # Must not raise
            publish_knowledge_update(
                comms_url="http://comms:18091",
                node_summary="test node",
                metadata={"node_id": "n1", "kind": "concept", "topic": "test", "contributed_by": "rule"},
            )

    def test_platform_header_sent(self):
        """publish_knowledge_update always sends X-Agency-Platform header."""
        with patch("images.knowledge.server.httpx.Client") as mock_client_cls:
            mock_client = MagicMock()
            mock_client_cls.return_value = mock_client

            publish_knowledge_update(
                comms_url="http://comms:18091",
                node_summary="some node",
                metadata={},
            )

            call_kwargs = mock_client.post.call_args
            headers = call_kwargs.kwargs["headers"]
            assert headers.get("X-Agency-Platform") == "true"


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
