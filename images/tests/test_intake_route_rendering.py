"""Tests for intake route rendering helpers."""

import os
import sys
import types

import pytest
from images.models.connector import ConnectorRoute


# Intake server imports scheduler at module import time; stub croniter so these
# focused helper tests do not depend on the optional scheduler dependency.
croniter_stub = types.ModuleType("croniter")
croniter_stub.croniter = type("Croniter", (), {"match": staticmethod(lambda expr, dt: False)})
sys.modules.setdefault("croniter", croniter_stub)

from images.intake.server import _build_channel_text, _build_route_task_text, _expand_route_target, _execute_relay  # noqa: E402


class TestIntakeRouteRendering:
    def test_expand_route_target_uses_env(self, monkeypatch):
        monkeypatch.setenv("TARGET_AGENT", "slack-bridge")
        assert _expand_route_target("${TARGET_AGENT}") == "slack-bridge"

    def test_build_route_task_text_prefers_brief(self):
        route = ConnectorRoute(
            match={"command": "/agency"},
            target={"agent": "${TARGET_AGENT}"},
            brief="Slack slash command from {{ user_id }} in {{ channel_id }}: {{ text }}",
        )
        payload = {
            "user_id": "U123",
            "channel_id": "C123",
            "text": "summarize this thread",
        }
        rendered = _build_route_task_text(route, payload)
        assert "U123" in rendered
        assert "C123" in rendered
        assert "summarize this thread" in rendered

    def test_build_channel_text_prefers_brief(self):
        route = ConnectorRoute(
            match={"type": "event_callback"},
            target={"channel": "dm-slack-bridge"},
            brief="Slack message from {{ event.user }} in {{ event.channel }}: {{ event.text }}",
        )
        payload = {
            "event": {
                "user": "U123",
                "channel": "C123",
                "text": "hello from slack",
            }
        }
        rendered = _build_channel_text(route, payload, "connector:slack-events")
        assert rendered == "Slack message from U123 in C123: hello from slack"

    @pytest.mark.asyncio
    async def test_execute_relay_prunes_empty_json_fields(self, monkeypatch):
        relay = types.SimpleNamespace(
            url="https://slack.com/api/chat.postMessage",
            method="POST",
            headers={"Content-Type": "application/json"},
            body='{"channel":"C123","text":"hello","thread_ts":""}',
            content_type="application/json",
        )
        captured = {}

        class FakeResponse:
            status = 200

            async def text(self):
                return ""

        class FakeRequestContext:
            async def __aenter__(self):
                return FakeResponse()

            async def __aexit__(self, exc_type, exc, tb):
                return False

        class FakeSession:
            async def __aenter__(self):
                return self

            async def __aexit__(self, exc_type, exc, tb):
                return False

            def request(self, method, url, **kwargs):
                captured["method"] = method
                captured["url"] = url
                captured["kwargs"] = kwargs
                return FakeRequestContext()

        monkeypatch.setattr("images.intake.server.ClientSession", lambda: FakeSession())
        ok = await _execute_relay(relay, {}, "test-relay")
        assert ok is True
        assert captured["kwargs"]["json"] == {"channel": "C123", "text": "hello"}
