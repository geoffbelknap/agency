"""Integration tests for advanced source types creating work items."""

import asyncio
import json
from datetime import datetime, timezone
from pathlib import Path
from unittest.mock import AsyncMock, patch, MagicMock

import pytest
import yaml

from images.intake.server import create_app
from images.intake.work_items import WorkItemStore

pytestmark = pytest.mark.asyncio


@pytest.fixture
def data_dir(tmp_path):
    d = tmp_path / "data"
    d.mkdir()
    return d


class TestPollIntegration:
    @pytest.fixture
    def poll_connector_dir(self, tmp_path):
        d = tmp_path / "connectors"
        d.mkdir()
        (d / "github-issues.yaml").write_text(yaml.dump({
            "kind": "connector",
            "name": "github-issues",
            "source": {
                "type": "poll",
                "url": "https://api.github.com/repos/test/test/issues",
                "interval": "5m",
                "response_key": "$",
            },
            "routes": [
                {"match": {"state": "*"}, "target": {"agent": "dev"}},
            ],
        }))
        return d

    async def test_poll_creates_work_items_for_new_data(self, poll_connector_dir, data_dir):
        """Simulate one poll tick: fetch returns new items, work items created."""
        from images.intake.poller import PollStateStore
        from images.intake.server import _poll_once

        app = create_app(connectors_dir=poll_connector_dir, data_dir=data_dir)
        store = app["store"]
        poll_state = PollStateStore(data_dir)

        api_response = [
            {"id": 1, "title": "Bug report", "state": "open"},
            {"id": 2, "title": "Feature request", "state": "open"},
        ]

        with patch("images.intake.server._deliver_task", new_callable=AsyncMock) as mock_deliver:
            mock_deliver.return_value = True
            with patch("images.intake.server._fetch_url", new_callable=AsyncMock) as mock_fetch:
                mock_fetch.return_value = api_response
                created = await _poll_once(
                    connector=app["connectors"]["github-issues"],
                    store=store,
                    poll_state=poll_state,
                    gateway=app["gateway"],
                )
                assert created == 2
                assert mock_deliver.call_count == 2

    async def test_poll_skips_already_seen_items(self, poll_connector_dir, data_dir):
        """Second poll tick with same data creates no new work items."""
        from images.intake.poller import PollStateStore
        from images.intake.server import _poll_once

        app = create_app(connectors_dir=poll_connector_dir, data_dir=data_dir)
        store = app["store"]
        poll_state = PollStateStore(data_dir)

        api_response = [{"id": 1, "title": "Bug", "state": "open"}]

        with patch("images.intake.server._deliver_task", new_callable=AsyncMock) as mock_deliver:
            mock_deliver.return_value = True
            with patch("images.intake.server._fetch_url", new_callable=AsyncMock) as mock_fetch:
                mock_fetch.return_value = api_response
                created1 = await _poll_once(
                    connector=app["connectors"]["github-issues"],
                    store=store,
                    poll_state=poll_state,
                    gateway=app["gateway"],
                )
                created2 = await _poll_once(
                    connector=app["connectors"]["github-issues"],
                    store=store,
                    poll_state=poll_state,
                    gateway=app["gateway"],
                )
                assert created1 == 1
                assert created2 == 0


class TestScheduleIntegration:
    @pytest.fixture
    def schedule_connector_dir(self, tmp_path):
        d = tmp_path / "connectors"
        d.mkdir()
        (d / "daily-standup.yaml").write_text(yaml.dump({
            "kind": "connector",
            "name": "daily-standup",
            "source": {"type": "schedule", "cron": "* * * * *"},
            "routes": [
                {"match": {"connector": "*"}, "target": {"agent": "lead"}},
            ],
        }))
        return d

    async def test_schedule_creates_work_item_on_match(self, schedule_connector_dir, data_dir):
        from images.intake.scheduler import ScheduleStateStore
        from images.intake.server import _schedule_once

        app = create_app(connectors_dir=schedule_connector_dir, data_dir=data_dir)
        store = app["store"]
        schedule_state = ScheduleStateStore(data_dir)

        with patch("images.intake.server._deliver_task", new_callable=AsyncMock) as mock_deliver:
            mock_deliver.return_value = True
            fired = await _schedule_once(
                connectors=app["connectors"],
                store=store,
                schedule_state=schedule_state,
                gateway=app["gateway"],
            )
            assert fired == 1
            mock_deliver.assert_called_once()

    async def test_schedule_prevents_double_fire(self, schedule_connector_dir, data_dir):
        from images.intake.scheduler import ScheduleStateStore
        from images.intake.server import _schedule_once

        app = create_app(connectors_dir=schedule_connector_dir, data_dir=data_dir)
        store = app["store"]
        schedule_state = ScheduleStateStore(data_dir)

        with patch("images.intake.server._deliver_task", new_callable=AsyncMock) as mock_deliver:
            mock_deliver.return_value = True
            fired1 = await _schedule_once(
                connectors=app["connectors"],
                store=store,
                schedule_state=schedule_state,
                gateway=app["gateway"],
            )
            fired2 = await _schedule_once(
                connectors=app["connectors"],
                store=store,
                schedule_state=schedule_state,
                gateway=app["gateway"],
            )
            assert fired1 == 1
            assert fired2 == 0


class TestChannelWatchIntegration:
    @pytest.fixture
    def watch_connector_dir(self, tmp_path):
        d = tmp_path / "connectors"
        d.mkdir()
        (d / "support-watch.yaml").write_text(yaml.dump({
            "kind": "connector",
            "name": "support-watch",
            "source": {
                "type": "channel-watch",
                "channel": "support-requests",
                "pattern": "^/request\\s+",
            },
            "routes": [
                {"match": {"channel": "*"}, "target": {"agent": "support"}},
            ],
        }))
        return d

    async def test_channel_watch_creates_work_items_for_matches(self, watch_connector_dir, data_dir):
        from images.intake.channel_watcher import ChannelWatchStateStore
        from images.intake.server import _channel_watch_once

        app = create_app(connectors_dir=watch_connector_dir, data_dir=data_dir)
        store = app["store"]
        watch_state = ChannelWatchStateStore(data_dir)

        messages = [
            {"id": "m1", "content": "/request fix the login page", "sender": "user1",
             "timestamp": "2026-03-11T09:00:00+00:00", "channel": "support-requests"},
            {"id": "m2", "content": "just chatting", "sender": "user2",
             "timestamp": "2026-03-11T09:01:00+00:00", "channel": "support-requests"},
            {"id": "m3", "content": "/request update the docs", "sender": "user3",
             "timestamp": "2026-03-11T09:02:00+00:00", "channel": "support-requests"},
        ]

        with patch("images.intake.server._deliver_task", new_callable=AsyncMock) as mock_deliver:
            mock_deliver.return_value = True
            with patch("images.intake.server._fetch_channel_messages", new_callable=AsyncMock) as mock_fetch:
                mock_fetch.return_value = messages
                created = await _channel_watch_once(
                    connector=app["connectors"]["support-watch"],
                    store=store,
                    watch_state=watch_state,
                    gateway=app["gateway"],
                )
                assert created == 2
                assert mock_deliver.call_count == 2

    async def test_channel_watch_tracks_last_seen(self, watch_connector_dir, data_dir):
        from images.intake.channel_watcher import ChannelWatchStateStore
        from images.intake.server import _channel_watch_once

        app = create_app(connectors_dir=watch_connector_dir, data_dir=data_dir)
        store = app["store"]
        watch_state = ChannelWatchStateStore(data_dir)

        messages_batch1 = [
            {"id": "m1", "content": "/request fix bug", "sender": "u1",
             "timestamp": "2026-03-11T09:00:00+00:00", "channel": "support-requests"},
        ]
        messages_batch2 = []

        with patch("images.intake.server._deliver_task", new_callable=AsyncMock) as mock_deliver:
            mock_deliver.return_value = True
            with patch("images.intake.server._fetch_channel_messages", new_callable=AsyncMock) as mock_fetch:
                mock_fetch.return_value = messages_batch1
                await _channel_watch_once(
                    connector=app["connectors"]["support-watch"],
                    store=store,
                    watch_state=watch_state,
                    gateway=app["gateway"],
                )
                mock_fetch.return_value = messages_batch2
                created = await _channel_watch_once(
                    connector=app["connectors"]["support-watch"],
                    store=store,
                    watch_state=watch_state,
                    gateway=app["gateway"],
                )
                assert created == 0
