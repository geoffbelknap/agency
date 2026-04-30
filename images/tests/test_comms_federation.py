"""Tests for comms federation — cache mode, write buffer, ID remap."""

import json
import os
from pathlib import Path
from unittest.mock import AsyncMock, MagicMock, patch

import pytest
import pytest_asyncio

from services.comms.server import create_app, _try_buffer_drain
from services.comms.store import MessageStore
from images.models.comms import ChannelType


@pytest.fixture
def store(tmp_path):
    return MessageStore(tmp_path / "data")


@pytest.fixture
def store_with_channel(store):
    store.create_channel(
        name="general", type=ChannelType.TEAM,
        created_by="alice", members=["alice", "bob"],
    )
    return store


class TestWriteBuffer:
    def test_buffer_message(self, store_with_channel):
        """Buffer a message when upstream is unavailable."""
        entry = store_with_channel.buffer_message(
            channel="general",
            author="alice",
            content="hello from buffer",
        )
        assert entry["id"].startswith("local-")
        assert entry["channel"] == "general"
        assert entry["content"] == "hello from buffer"

    def test_read_buffer(self, store_with_channel):
        """Read buffered messages for a channel."""
        store_with_channel.buffer_message(
            channel="general", author="alice", content="msg1",
        )
        store_with_channel.buffer_message(
            channel="general", author="bob", content="msg2",
        )
        entries = store_with_channel.read_buffer("general")
        assert len(entries) == 2
        assert entries[0]["content"] == "msg1"
        assert entries[1]["content"] == "msg2"

    def test_read_buffer_empty(self, store_with_channel):
        """Empty buffer returns empty list."""
        entries = store_with_channel.read_buffer("general")
        assert entries == []

    def test_remove_buffer_entry(self, store_with_channel):
        """Remove a specific entry from the buffer after drain."""
        entry = store_with_channel.buffer_message(
            channel="general", author="alice", content="msg1",
        )
        store_with_channel.buffer_message(
            channel="general", author="alice", content="msg2",
        )
        store_with_channel.remove_buffer_entry("general", entry["id"])
        remaining = store_with_channel.read_buffer("general")
        assert len(remaining) == 1
        assert remaining[0]["content"] == "msg2"

    def test_buffer_dir_created(self, store_with_channel):
        """Buffer directory is created on first buffer write."""
        store_with_channel.buffer_message(
            channel="general", author="alice", content="test",
        )
        buffer_dir = store_with_channel.data_dir / "buffer"
        assert buffer_dir.exists()

    def test_buffer_channels(self, store_with_channel):
        """List channels that have buffered messages."""
        store_with_channel.buffer_message(
            channel="general", author="alice", content="test",
        )
        assert "general" in store_with_channel.buffer_channels()

    def test_buffer_size(self, store_with_channel):
        """Count buffered messages for a channel."""
        store_with_channel.buffer_message(
            channel="general", author="alice", content="m1",
        )
        store_with_channel.buffer_message(
            channel="general", author="alice", content="m2",
        )
        assert store_with_channel.buffer_size("general") == 2


class TestIdRemap:
    def test_add_remap(self, store_with_channel):
        """Add a local->server ID mapping."""
        store_with_channel.add_id_remap("local-abc123", "srv-xyz789")
        assert store_with_channel.resolve_id("local-abc123") == "srv-xyz789"

    def test_resolve_unknown_id(self, store_with_channel):
        """Unknown IDs pass through unchanged."""
        assert store_with_channel.resolve_id("some-id") == "some-id"

    def test_remap_persisted(self, store_with_channel):
        """Remap table persists across store instances."""
        store_with_channel.add_id_remap("local-abc", "srv-xyz")
        store2 = MessageStore(store_with_channel.data_dir)
        assert store2.resolve_id("local-abc") == "srv-xyz"

    def test_clear_remap(self, store_with_channel):
        """Clearing remap removes all entries."""
        store_with_channel.add_id_remap("local-a", "srv-1")
        store_with_channel.add_id_remap("local-b", "srv-2")
        store_with_channel.clear_id_remap(["local-a", "local-b"])
        assert store_with_channel.resolve_id("local-a") == "local-a"


@pytest.fixture
def platform_write_app(tmp_path):
    return create_app(data_dir=tmp_path / "data", agents_dir=tmp_path / "agents")


@pytest.mark.asyncio
class TestPlatformWrite:
    async def test_platform_write_channel_rejects_without_header(
        self, aiohttp_client, platform_write_app
    ):
        """Posts to platform-write channels require X-Agency-Platform header."""
        client = await aiohttp_client(platform_write_app)
        await client.post("/channels", json={
            "name": "_team-a-activity",
            "type": "team",
            "created_by": "system",
            "visibility": "platform-write",
            "members": ["alice"],
        })
        resp = await client.post("/channels/_team-a-activity/messages", json={
            "author": "alice",
            "content": "agent trying to write",
        })
        assert resp.status == 403

    async def test_platform_write_channel_accepts_with_header(
        self, aiohttp_client, platform_write_app
    ):
        """Posts with X-Agency-Platform header succeed on platform-write channels."""
        client = await aiohttp_client(platform_write_app)
        await client.post("/channels", json={
            "name": "_team-b-activity",
            "type": "team",
            "created_by": "system",
            "visibility": "platform-write",
            "members": ["alice"],
        })
        resp = await client.post(
            "/channels/_team-b-activity/messages",
            json={"author": "alice", "content": "infra update"},
            headers={"X-Agency-Platform": "true"},
        )
        assert resp.status == 201

    async def test_normal_channel_ignores_platform_header(
        self, aiohttp_client, platform_write_app
    ):
        """Normal channels don't require X-Agency-Platform."""
        client = await aiohttp_client(platform_write_app)
        await client.post("/channels", json={
            "name": "general",
            "type": "team",
            "created_by": "alice",
            "members": ["alice"],
        })
        resp = await client.post("/channels/general/messages", json={
            "author": "alice",
            "content": "hello",
        })
        assert resp.status == 201


@pytest.fixture
def cache_app(tmp_path):
    """Create app in cache mode with a mock upstream."""
    with patch.dict(os.environ, {
        "COMMS_MODE": "cache",
        "COMMS_UPSTREAM": "http://manager:18091",
    }):
        app = create_app(
            data_dir=tmp_path / "data",
            agents_dir=tmp_path / "agents",
        )
    return app


@pytest.mark.asyncio
class TestCacheModeReads:
    """Cache mode: read requests proxy to upstream, fall back to local cache."""

    async def test_health_reports_cache_mode(self, aiohttp_client, cache_app):
        """Health endpoint reports cache mode."""
        client = await aiohttp_client(cache_app)
        resp = await client.get("/health")
        data = await resp.json()
        assert data["mode"] == "cache"

    async def test_read_messages_upstream_failure_returns_stale(self, aiohttp_client, cache_app):
        """When upstream is unreachable, serve from local cache."""
        client = await aiohttp_client(cache_app)
        store = client.app["store"]
        store.create_channel(
            name="general", type=ChannelType.TEAM,
            created_by="alice", members=["alice"],
        )
        store.post_message(
            channel="general", author="alice", content="cached msg",
        )
        resp = await client.get("/channels/general/messages?reader=alice")
        assert resp.status == 200
        data = await resp.json()
        assert len(data) >= 1
        assert data[0]["content"] == "cached msg"

    async def test_list_channels_upstream_failure_returns_local(self, aiohttp_client, cache_app):
        """When upstream fails, list channels returns local cache."""
        client = await aiohttp_client(cache_app)
        store = client.app["store"]
        store.create_channel(
            name="cached-ch", type=ChannelType.TEAM,
            created_by="alice",
        )
        resp = await client.get("/channels")
        assert resp.status == 200
        data = await resp.json()
        assert any(ch["name"] == "cached-ch" for ch in data)


@pytest.mark.asyncio
class TestCacheModeWrites:
    """Cache mode: writes forward to upstream, buffer on failure."""

    async def test_write_buffers_when_upstream_down(self, aiohttp_client, cache_app):
        """When upstream is unreachable, messages are buffered."""
        client = await aiohttp_client(cache_app)
        store = client.app["store"]
        store.create_channel(
            name="general", type=ChannelType.TEAM,
            created_by="alice", members=["alice"],
        )
        resp = await client.post("/channels/general/messages", json={
            "author": "alice",
            "content": "buffered message",
        })
        assert resp.status == 201
        data = await resp.json()
        assert data["id"].startswith("local-")

        # Verify it's in the buffer
        entries = store.read_buffer("general")
        assert len(entries) == 1
        assert entries[0]["content"] == "buffered message"

    async def test_write_returns_sent_status(self, aiohttp_client, cache_app):
        """Write response always looks successful to the agent."""
        client = await aiohttp_client(cache_app)
        store = client.app["store"]
        store.create_channel(
            name="general", type=ChannelType.TEAM,
            created_by="alice", members=["alice"],
        )
        resp = await client.post("/channels/general/messages", json={
            "author": "alice",
            "content": "test",
        })
        assert resp.status == 201


class TestCacheRelayValidation:
    """Primary mode: validate X-Agency-Cache-Relay header on forwarded writes."""

    @pytest.fixture
    def primary_app(self, tmp_path):
        """Primary mode app with hosts registry."""
        agents_dir = tmp_path / "agents"
        agents_dir.mkdir()
        app = create_app(data_dir=tmp_path / "data", agents_dir=agents_dir)
        # Set up a mock hosts registry
        app["hosts_registry"] = {"worker-1", "worker-2"}
        return app

    @pytest_asyncio.fixture
    async def primary_client(self, primary_app, aiohttp_client):
        return await aiohttp_client(primary_app)

    @pytest.mark.asyncio
    async def test_relay_header_from_known_host_accepted(self, primary_client):
        """Forwarded write from a known host is accepted."""
        store = primary_client.app["store"]
        store.create_channel(
            name="general", type=ChannelType.TEAM,
            created_by="alice", members=["alice"],
        )
        resp = await primary_client.post(
            "/channels/general/messages",
            json={"author": "alice", "content": "relayed msg"},
            headers={"X-Agency-Cache-Relay": "worker-1"},
        )
        assert resp.status == 201

    @pytest.mark.asyncio
    async def test_relay_header_from_unknown_host_rejected(self, primary_client):
        """Forwarded write from an unknown host is rejected."""
        store = primary_client.app["store"]
        store.create_channel(
            name="general", type=ChannelType.TEAM,
            created_by="alice", members=["alice"],
        )
        resp = await primary_client.post(
            "/channels/general/messages",
            json={"author": "alice", "content": "spoofed msg"},
            headers={"X-Agency-Cache-Relay": "evil-host"},
        )
        assert resp.status == 403

    @pytest.mark.asyncio
    async def test_no_relay_header_accepted(self, primary_client):
        """Direct writes (no relay header) are accepted normally."""
        store = primary_client.app["store"]
        store.create_channel(
            name="general", type=ChannelType.TEAM,
            created_by="alice", members=["alice"],
        )
        resp = await primary_client.post(
            "/channels/general/messages",
            json={"author": "alice", "content": "direct msg"},
        )
        assert resp.status == 201


class TestCacheModeChannelOps:
    @pytest.fixture
    def cache_app(self, tmp_path):
        with patch.dict(os.environ, {
            "COMMS_MODE": "cache",
            "COMMS_UPSTREAM": "http://manager:18091",
        }):
            app = create_app(
                data_dir=tmp_path / "data",
                agents_dir=tmp_path / "agents",
            )
        return app

    @pytest_asyncio.fixture
    async def cache_client(self, cache_app, aiohttp_client):
        return await aiohttp_client(cache_app)

    @pytest.mark.asyncio
    async def test_create_channel_fails_when_upstream_down(self, cache_client):
        """Cannot create channels when manager is unreachable."""
        resp = await cache_client.post("/channels", json={
            "name": "new-channel",
            "type": "team",
            "created_by": "alice",
        })
        assert resp.status == 503

    @pytest.mark.asyncio
    async def test_list_channels_falls_back_to_local(self, cache_client):
        """List channels serves from local cache when upstream is down."""
        store = cache_client.app["store"]
        store.create_channel(
            name="cached", type=ChannelType.TEAM, created_by="alice",
        )
        resp = await cache_client.get("/channels")
        assert resp.status == 200
        data = await resp.json()
        assert any(ch["name"] == "cached" for ch in data)


class TestCacheModeJoinAndSearch:
    @pytest.fixture
    def cache_app(self, tmp_path):
        with patch.dict(os.environ, {
            "COMMS_MODE": "cache",
            "COMMS_UPSTREAM": "http://manager:18091",
        }):
            app = create_app(
                data_dir=tmp_path / "data",
                agents_dir=tmp_path / "agents",
            )
        return app

    @pytest_asyncio.fixture
    async def cache_client(self, cache_app, aiohttp_client):
        return await aiohttp_client(cache_app)

    @pytest.mark.asyncio
    async def test_join_channel_fails_when_upstream_down(self, cache_client):
        """Cannot join channels when manager is unreachable."""
        store = cache_client.app["store"]
        store.create_channel(
            name="general", type=ChannelType.TEAM, created_by="alice",
        )
        resp = await cache_client.post("/channels/general/join", json={
            "participant": "bob",
        })
        assert resp.status == 503

    @pytest.mark.asyncio
    async def test_search_falls_back_to_local(self, cache_client):
        """Search serves from local FTS when upstream is down."""
        store = cache_client.app["store"]
        store.create_channel(
            name="general", type=ChannelType.TEAM,
            created_by="alice", members=["alice"],
        )
        store.post_message(
            channel="general", author="alice", content="important topic",
        )
        resp = await cache_client.get("/search?q=important")
        assert resp.status == 200
        data = await resp.json()
        assert len(data) >= 1


@pytest.mark.asyncio
class TestBufferDrainAudit:
    """Buffer drain emits audit events per spec (ASK T2)."""

    @pytest.fixture
    def store_with_buffer(self, tmp_path):
        store = MessageStore(tmp_path / "data")
        store.create_channel(
            name="general", type=ChannelType.TEAM,
            created_by="alice", members=["alice", "bob"],
        )
        return store

    def _make_app(self, store, http_mock, audit_log):
        app = {}
        app["upstream_state"] = {"ok": True}
        app["upstream_url"] = "http://manager:18091"
        app["http"] = http_mock
        app["audit_log"] = audit_log
        app["store"] = store
        return app

    async def test_drain_rejected_logged(self, store_with_buffer):
        """Rejected buffer entries emit buffer_drain_rejected audit event."""
        store = store_with_buffer
        store.buffer_message(channel="general", author="alice", content="msg1")

        # Mock upstream returning 403
        mock_resp = MagicMock()
        mock_resp.status_code = 403
        mock_resp.text = "forbidden"
        http_mock = AsyncMock()
        http_mock.post = AsyncMock(return_value=mock_resp)

        audit_log = MagicMock()
        app = self._make_app(store, http_mock, audit_log)

        await _try_buffer_drain(app, store, "general")

        # buffer_drain_rejected should have been called once
        calls = [c for c in audit_log.record.call_args_list if c[0][0] == "buffer_drain_rejected"]
        assert len(calls) == 1
        data = calls[0][0][1]
        assert data["agent"] == "alice"
        assert data["channel"] == "general"
        assert "reason" in data

    async def test_drain_complete_emits_gap_marker(self, store_with_buffer):
        """Successful drain emits buffer_drained audit event with count and timestamps."""
        store = store_with_buffer
        store.buffer_message(channel="general", author="alice", content="msg1")
        store.buffer_message(channel="general", author="bob", content="msg2")

        # Mock upstream returning 201
        server_msg = {"id": "srv-001", "channel": "general", "author": "alice", "content": "msg1"}
        server_msg2 = {"id": "srv-002", "channel": "general", "author": "bob", "content": "msg2"}
        mock_resp1 = MagicMock()
        mock_resp1.status_code = 201
        mock_resp1.json = MagicMock(return_value=server_msg)
        mock_resp2 = MagicMock()
        mock_resp2.status_code = 201
        mock_resp2.json = MagicMock(return_value=server_msg2)
        http_mock = AsyncMock()
        http_mock.post = AsyncMock(side_effect=[mock_resp1, mock_resp2])

        audit_log = MagicMock()
        app = self._make_app(store, http_mock, audit_log)

        await _try_buffer_drain(app, store, "general")

        # buffer_drained should have been called once
        calls = [c for c in audit_log.record.call_args_list if c[0][0] == "buffer_drained"]
        assert len(calls) == 1
        data = calls[0][0][1]
        assert data["channel"] == "general"
        assert data["count"] == 2
        assert data["rejected_count"] == 0
        assert "earliest" in data
        assert "latest" in data

    async def test_no_audit_when_buffer_empty(self, store_with_buffer):
        """No audit events are emitted when the buffer is empty."""
        store = store_with_buffer
        http_mock = AsyncMock()
        audit_log = MagicMock()
        app = self._make_app(store, http_mock, audit_log)

        await _try_buffer_drain(app, store, "general")

        audit_log.record.assert_not_called()

    async def test_no_audit_when_upstream_down(self, store_with_buffer):
        """No audit events are emitted when upstream is marked down."""
        store = store_with_buffer
        store.buffer_message(channel="general", author="alice", content="msg1")

        http_mock = AsyncMock()
        audit_log = MagicMock()
        app = self._make_app(store, http_mock, audit_log)
        app["upstream_state"] = {"ok": False}

        await _try_buffer_drain(app, store, "general")

        audit_log.record.assert_not_called()

    async def test_mixed_drain_emits_both_events(self, store_with_buffer):
        """Mix of accepted and rejected entries emits both audit event types."""
        store = store_with_buffer
        store.buffer_message(channel="general", author="alice", content="ok-msg")
        store.buffer_message(channel="general", author="bob", content="bad-msg")

        server_msg = {"id": "srv-001", "channel": "general", "author": "alice", "content": "ok-msg"}
        mock_resp_ok = MagicMock()
        mock_resp_ok.status_code = 201
        mock_resp_ok.json = MagicMock(return_value=server_msg)
        mock_resp_403 = MagicMock()
        mock_resp_403.status_code = 403
        mock_resp_403.text = "policy violation"
        http_mock = AsyncMock()
        http_mock.post = AsyncMock(side_effect=[mock_resp_ok, mock_resp_403])

        audit_log = MagicMock()
        app = self._make_app(store, http_mock, audit_log)

        await _try_buffer_drain(app, store, "general")

        rejected_calls = [c for c in audit_log.record.call_args_list if c[0][0] == "buffer_drain_rejected"]
        drained_calls = [c for c in audit_log.record.call_args_list if c[0][0] == "buffer_drained"]
        assert len(rejected_calls) == 1
        assert len(drained_calls) == 1
        assert drained_calls[0][0][1]["count"] == 1
        assert drained_calls[0][0][1]["rejected_count"] == 1
