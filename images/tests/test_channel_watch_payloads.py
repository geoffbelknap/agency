"""Tests for channel-watch payload construction."""

import sys
import types
from datetime import datetime, timezone
from types import SimpleNamespace
from unittest.mock import AsyncMock, MagicMock, patch

import pytest

try:
    import croniter  # noqa: F401
except ModuleNotFoundError:
    croniter_stub = types.ModuleType("croniter")
    croniter_stub.croniter = type("Croniter", (), {"match": staticmethod(lambda expr, dt: False)})
    sys.modules.setdefault("croniter", croniter_stub)

from images.intake.server import _channel_watch_once

pytestmark = pytest.mark.asyncio


class TestChannelWatchPayloads:
    async def test_channel_watch_forwards_author_reply_to_and_metadata(self):
        connector = SimpleNamespace(
            name="comms-to-slack",
            source=SimpleNamespace(
                channel="general",
                pattern=".*",
            ),
        )
        watch_state = MagicMock()
        watch_state.get_last_seen.return_value = None
        watch_state.set_last_seen = MagicMock()

        gateway = MagicMock()

        message = {
            "id": "msg-1",
            "author": "slack-bridge",
            "content": "hello from agency",
            "timestamp": datetime.now(timezone.utc).isoformat(),
            "reply_to": "msg-parent",
            "metadata": {"slack": {"channel": "C123", "thread_ts": "1712860000.1234"}},
            "flags": {"decision": True},
        }

        with patch("images.intake.server._fetch_channel_messages", new_callable=AsyncMock) as mock_fetch, patch(
            "images.intake.server._route_and_deliver", new_callable=AsyncMock
        ) as mock_route:
            mock_fetch.return_value = [message]
            created = await _channel_watch_once(
                connector=connector,
                store=MagicMock(),
                watch_state=watch_state,
                gateway=gateway,
        )

        assert created == 1
        call_args, _ = mock_route.call_args
        payload = call_args[2]
        assert payload["author"] == "slack-bridge"
        assert payload["reply_to"] == "msg-parent"
        assert payload["metadata"] == {"slack": {"channel": "C123", "thread_ts": "1712860000.1234"}}
        assert payload["flags"] == {"decision": True}
