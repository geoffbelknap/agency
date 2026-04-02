"""Tests for comms message and channel models."""

import pytest
from pydantic import ValidationError

from images.models.comms import Channel, ChannelType, Message, MessageFlags


class TestMessage:
    def test_create_message(self):
        msg = Message(
            channel="chefhub-beta",
            author="scout",
            content="Integration design is ready.",
        )
        assert msg.id is not None
        assert msg.channel == "chefhub-beta"
        assert msg.author == "scout"
        assert msg.content == "Integration design is ready."
        assert msg.reply_to is None
        assert msg.flags.decision is False
        assert msg.flags.question is False
        assert msg.flags.blocker is False
        assert msg.timestamp is not None

    def test_message_with_flags(self):
        msg = Message(
            channel="strategy",
            author="pm",
            content="We should launch API first.",
            flags=MessageFlags(decision=True),
        )
        assert msg.flags.decision is True

    def test_message_with_reply(self):
        parent = Message(channel="test", author="scout", content="Question?")
        reply = Message(
            channel="test",
            author="pm",
            content="Answer.",
            reply_to=parent.id,
        )
        assert reply.reply_to == parent.id

    def test_message_serialization_roundtrip(self):
        msg = Message(channel="test", author="scout", content="hello")
        data = msg.model_dump()
        restored = Message.model_validate(data)
        assert restored.id == msg.id
        assert restored.content == msg.content
        assert restored.timestamp == msg.timestamp

    def test_message_content_max_length(self):
        with pytest.raises(Exception):
            Message(channel="test", author="scout", content="x" * 10001)

    def test_message_content_not_empty(self):
        with pytest.raises(Exception):
            Message(channel="test", author="scout", content="")

    def test_message_content_not_whitespace(self):
        with pytest.raises(Exception):
            Message(channel="test", author="scout", content="   ")

    def test_message_unique_ids(self):
        m1 = Message(channel="test", author="scout", content="one")
        m2 = Message(channel="test", author="scout", content="two")
        assert m1.id != m2.id


class TestChannel:
    def test_create_team_channel(self):
        ch = Channel(
            name="chefhub-beta",
            type=ChannelType.TEAM,
            created_by="operator",
            topic="ChefHub beta readiness",
        )
        assert ch.name == "chefhub-beta"
        assert ch.type == ChannelType.TEAM
        assert ch.members == []

    def test_create_direct_channel(self):
        ch = Channel(
            name="dm-scout-pm",
            type=ChannelType.DIRECT,
            created_by="system",
            members=["scout", "pm"],
        )
        assert ch.type == ChannelType.DIRECT
        assert "scout" in ch.members

    def test_channel_name_validation_rejects_uppercase(self):
        with pytest.raises(Exception):
            Channel(
                name="Invalid",
                type=ChannelType.TEAM,
                created_by="operator",
            )

    def test_channel_name_validation_rejects_spaces(self):
        with pytest.raises(Exception):
            Channel(
                name="bad name",
                type=ChannelType.TEAM,
                created_by="operator",
            )

    def test_channel_name_single_char(self):
        ch = Channel(name="x", type=ChannelType.TEAM, created_by="operator")
        assert ch.name == "x"

    def test_channel_serialization_roundtrip(self):
        ch = Channel(
            name="test-channel",
            type=ChannelType.TEAM,
            created_by="operator",
            topic="Testing",
            members=["scout", "pm"],
        )
        data = ch.model_dump()
        restored = Channel.model_validate(data)
        assert restored.name == ch.name
        assert restored.members == ch.members


class TestChannelVisibility:
    def test_default_visibility_is_open(self):
        ch = Channel(name="general", type=ChannelType.TEAM, created_by="operator")
        assert ch.visibility == "open"

    def test_private_visibility(self):
        ch = Channel(name="secret", type=ChannelType.TEAM, created_by="operator", visibility="private")
        assert ch.visibility == "private"

    def test_invalid_visibility_rejected(self):
        with pytest.raises(ValidationError):
            Channel(name="bad", type=ChannelType.TEAM, created_by="operator", visibility="hidden")

    def test_visibility_serializes(self):
        ch = Channel(name="ops", type=ChannelType.TEAM, created_by="operator", visibility="private")
        data = ch.model_dump()
        assert data["visibility"] == "private"

    def test_channel_platform_write_visibility(self):
        """platform-write is a valid visibility mode with underscore-prefixed name."""
        ch = Channel(
            name="_team-alpha-activity",
            type=ChannelType.TEAM,
            created_by="system",
            visibility="platform-write",
        )
        assert ch.visibility == "platform-write"
        assert ch.name == "_team-alpha-activity"

    def test_channel_underscore_prefix_name(self):
        """Channel names starting with _ are valid (internal/platform channels)."""
        ch = Channel(
            name="_internal-ops",
            type=ChannelType.TEAM,
            created_by="system",
            visibility="platform-write",
        )
        assert ch.name == "_internal-ops"

    def test_channel_underscore_single_segment(self):
        """Single-segment underscore names like _x are valid."""
        ch = Channel(
            name="_x",
            type=ChannelType.SYSTEM,
            created_by="system",
        )
        assert ch.name == "_x"
