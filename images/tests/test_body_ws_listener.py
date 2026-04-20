"""Tests for the WSListener background thread."""

import json
import queue
import threading
import time
from pathlib import Path
from unittest.mock import AsyncMock, MagicMock, patch

import pytest

from images.body.ws_listener import (
    WSListener,
    _backoff_delay,
    _messages_url,
    read_context_fallback,
)


# ---------------------------------------------------------------------------
# Unit tests: _backoff_delay
# ---------------------------------------------------------------------------


class TestBackoffDelay:
    def test_attempt_0_returns_1(self):
        assert _backoff_delay(0) == 1

    def test_attempt_1_returns_2(self):
        assert _backoff_delay(1) == 2

    def test_attempt_2_returns_4(self):
        assert _backoff_delay(2) == 4

    def test_attempt_3_returns_8(self):
        assert _backoff_delay(3) == 8

    def test_attempt_10_capped_at_30(self):
        assert _backoff_delay(10) == 30

    def test_attempt_5_capped_at_30(self):
        # 2**5 = 32, should be capped
        assert _backoff_delay(5) == 30

    def test_large_attempt_capped_at_30(self):
        assert _backoff_delay(100) == 30


# ---------------------------------------------------------------------------
# Unit tests: read_context_fallback
# ---------------------------------------------------------------------------


class TestReadContextFallback:
    def test_valid_task_returned(self, tmp_path):
        ctx_file = tmp_path / "session-context.json"
        task = {"id": "task-1", "description": "Do something"}
        ctx_file.write_text(json.dumps({"current_task": task}), encoding="utf-8")
        result = read_context_fallback(ctx_file)
        assert result == task

    def test_missing_file_returns_none(self, tmp_path):
        ctx_file = tmp_path / "nonexistent.json"
        result = read_context_fallback(ctx_file)
        assert result is None

    def test_empty_file_returns_none(self, tmp_path):
        ctx_file = tmp_path / "session-context.json"
        ctx_file.write_text("", encoding="utf-8")
        result = read_context_fallback(ctx_file)
        assert result is None

    def test_whitespace_only_returns_none(self, tmp_path):
        ctx_file = tmp_path / "session-context.json"
        ctx_file.write_text("   \n  ", encoding="utf-8")
        result = read_context_fallback(ctx_file)
        assert result is None

    def test_invalid_json_returns_none(self, tmp_path):
        ctx_file = tmp_path / "session-context.json"
        ctx_file.write_text("{not valid json}", encoding="utf-8")
        result = read_context_fallback(ctx_file)
        assert result is None

    def test_valid_json_missing_current_task_returns_none(self, tmp_path):
        ctx_file = tmp_path / "session-context.json"
        ctx_file.write_text(json.dumps({"agent": "scout"}), encoding="utf-8")
        result = read_context_fallback(ctx_file)
        assert result is None

    def test_none_path_returns_none(self):
        result = read_context_fallback(None)
        assert result is None

    def test_current_task_none_returns_none(self, tmp_path):
        ctx_file = tmp_path / "session-context.json"
        ctx_file.write_text(json.dumps({"current_task": None}), encoding="utf-8")
        result = read_context_fallback(ctx_file)
        assert result is None


# ---------------------------------------------------------------------------
# Unit tests: WSListener construction
# ---------------------------------------------------------------------------


class TestWSListenerInit:
    def test_initial_state(self):
        q = queue.Queue()
        listener = WSListener(
            comms_url="http://agency-comms:18091",
            agent_name="scout",
            event_queue=q,
        )
        assert listener.comms_url == "http://agency-comms:18091"
        assert listener.agent_name == "scout"
        assert listener.event_queue is q
        assert listener.context_file is None
        assert listener.connected is False
        assert not listener._stop_event.is_set()

    def test_context_file_stored(self, tmp_path):
        q = queue.Queue()
        ctx = tmp_path / "session-context.json"
        listener = WSListener(
            comms_url="http://agency-comms:18091",
            agent_name="scout",
            event_queue=q,
            context_file=ctx,
        )
        assert listener.context_file is ctx


# ---------------------------------------------------------------------------
# Unit tests: URL conversion
# ---------------------------------------------------------------------------


class TestHttpToWs:
    def test_http_becomes_ws(self):
        assert WSListener._http_to_ws("http://host:18091") == "ws://host:18091"

    def test_https_becomes_wss(self):
        assert WSListener._http_to_ws("https://host:18091") == "wss://host:18091"

    def test_already_ws_unchanged(self):
        assert WSListener._http_to_ws("ws://host:18091") == "ws://host:18091"

    def test_already_wss_unchanged(self):
        assert WSListener._http_to_ws("wss://host:18091") == "wss://host:18091"


def test_messages_url_encodes_utc_offset_plus():
    url = _messages_url(
        "http://agency-comms:18091",
        "dm-henry",
        "2026-04-19T01:39:35.833388+00:00",
        "henry",
    )
    assert url == (
        "http://agency-comms:18091/channels/dm-henry/messages"
        "?since=2026-04-19T01%3A39%3A35.833388%2B00%3A00&reader=henry"
    )


# ---------------------------------------------------------------------------
# Integration test: WSListener with aiohttp test server
# ---------------------------------------------------------------------------
# This test uses aiohttp's test utilities to stand up a real WebSocket server
# and verifies that events arrive in the queue.
#
# Note: This involves threads + asyncio, which requires careful teardown.
# The test is marked asyncio and uses the aiohttp_client fixture from
# pytest-aiohttp.


@pytest.mark.asyncio
async def test_ws_listener_receives_events(aiohttp_client, tmp_path):
    """WSListener connects and receives pushed JSON events in event_queue."""
    import asyncio
    from aiohttp import web

    async def ws_handler(request):
        ws = web.WebSocketResponse()
        await ws.prepare(request)
        # Send ack first (matching real comms server behaviour)
        await ws.send_json({"type": "ack", "v": 1, "data": {"agent": "scout", "channels": [], "unreads": {}}})
        # Push a test event
        await ws.send_json({"type": "message", "channel": "dev", "message": {"content": "hello"}})
        # Wait for close
        async for msg in ws:
            pass
        return ws

    app = web.Application()
    app.router.add_get("/ws", ws_handler)
    client = await aiohttp_client(app)

    # Build the listener pointing at the test server
    base_url = f"http://127.0.0.1:{client.port}"
    q: queue.Queue = queue.Queue()
    listener = WSListener(
        comms_url=base_url,
        agent_name="scout",
        event_queue=q,
    )
    listener.start()

    # Keep the async context alive while the listener's thread connects and
    # collects events.  Poll the queue while yielding to the event loop.
    received = []
    deadline = time.monotonic() + 5.0
    while time.monotonic() < deadline:
        await asyncio.sleep(0.05)
        while True:
            try:
                received.append(q.get_nowait())
            except queue.Empty:
                break
        if len(received) >= 2:
            break

    listener.stop()

    # Should have received the ack and the message push
    types = [e.get("type") for e in received]
    assert "ack" in types
    assert "message" in types


@pytest.mark.asyncio
@pytest.mark.skip(reason="Flaky: async timing issue — missed_task not always received in time")
async def test_ws_listener_context_fallback_on_connect(aiohttp_client, tmp_path):
    """WSListener enqueues missed_task event from context file on connect."""
    import asyncio
    from aiohttp import web

    async def ws_handler(request):
        ws = web.WebSocketResponse()
        await ws.prepare(request)
        await ws.send_json({"type": "ack", "v": 1, "data": {"agent": "scout", "channels": [], "unreads": {}}})
        async for msg in ws:
            pass
        return ws

    app = web.Application()
    app.router.add_get("/ws", ws_handler)
    client = await aiohttp_client(app)

    # Write a context file with a current task
    ctx_file = tmp_path / "session-context.json"
    task_data = {"id": "task-42", "description": "Catch up on missed work"}
    ctx_file.write_text(json.dumps({"current_task": task_data}), encoding="utf-8")

    base_url = f"http://127.0.0.1:{client.port}"
    q: queue.Queue = queue.Queue()
    listener = WSListener(
        comms_url=base_url,
        agent_name="scout",
        event_queue=q,
        context_file=ctx_file,
    )
    listener.start()

    received = []
    deadline = time.monotonic() + 5.0
    while time.monotonic() < deadline:
        await asyncio.sleep(0.05)
        while True:
            try:
                received.append(q.get_nowait())
            except queue.Empty:
                break
        if len(received) >= 2:
            break

    listener.stop()

    types = [e.get("type") for e in received]
    assert "missed_task" in types
    missed = next(e for e in received if e.get("type") == "missed_task")
    assert missed["current_task"] == task_data


@pytest.mark.asyncio
async def test_ws_listener_stop_cleanly(aiohttp_client):
    """WSListener.stop() causes the thread to exit without hanging."""
    import asyncio
    from aiohttp import web

    async def ws_handler(request):
        ws = web.WebSocketResponse()
        await ws.prepare(request)
        await ws.send_json({"type": "ack", "v": 1, "data": {"agent": "scout", "channels": [], "unreads": {}}})
        async for msg in ws:
            pass
        return ws

    app = web.Application()
    app.router.add_get("/ws", ws_handler)
    client = await aiohttp_client(app)

    base_url = f"http://127.0.0.1:{client.port}"
    q: queue.Queue = queue.Queue()
    listener = WSListener(comms_url=base_url, agent_name="scout", event_queue=q)
    listener.start()

    # Yield to the event loop so the server can accept the WS connection
    # before we signal stop.
    await asyncio.sleep(0.5)
    listener.stop()

    assert listener._thread is not None
    assert not listener._thread.is_alive()
