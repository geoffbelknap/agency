"""Tests for schedule source — cron matching and double-fire prevention."""

import sqlite3
from datetime import datetime, timezone, timedelta
from pathlib import Path
from unittest.mock import patch

import pytest

from images.intake.scheduler import (
    should_fire,
    ScheduleStateStore,
)


class TestShouldFire:
    def test_matching_cron(self):
        # "every minute" cron should always match
        assert should_fire("* * * * *", None) is True

    def test_non_matching_cron(self):
        # Check a cron that only fires at midnight; test at noon
        now = datetime(2026, 3, 11, 12, 30, 0, tzinfo=timezone.utc)
        assert should_fire("0 0 * * *", None, now=now) is False

    def test_already_fired_this_minute(self):
        now = datetime(2026, 3, 11, 9, 0, 30, tzinfo=timezone.utc)
        last_fired = datetime(2026, 3, 11, 9, 0, 0, tzinfo=timezone.utc)
        assert should_fire("0 9 * * *", last_fired, now=now) is False

    def test_fired_previous_minute(self):
        now = datetime(2026, 3, 11, 9, 0, 30, tzinfo=timezone.utc)
        last_fired = datetime(2026, 3, 11, 8, 0, 0, tzinfo=timezone.utc)
        assert should_fire("0 9 * * *", last_fired, now=now) is True

    def test_weekday_filter(self):
        # 2026-03-11 is Wednesday (3 in cron)
        now = datetime(2026, 3, 11, 9, 0, 0, tzinfo=timezone.utc)
        assert should_fire("0 9 * * 3", None, now=now) is True  # Wednesday
        assert should_fire("0 9 * * 1", None, now=now) is False  # Monday only


class TestScheduleStateStore:
    @pytest.fixture
    def store(self, tmp_path):
        return ScheduleStateStore(tmp_path)

    def test_no_previous_fire(self, store):
        assert store.get_last_fired("cron-job") is None

    def test_record_and_retrieve(self, store):
        now = datetime(2026, 3, 11, 9, 0, 0, tzinfo=timezone.utc)
        store.set_last_fired("cron-job", now)
        result = store.get_last_fired("cron-job")
        assert result == now

    def test_update_last_fired(self, store):
        t1 = datetime(2026, 3, 11, 9, 0, 0, tzinfo=timezone.utc)
        t2 = datetime(2026, 3, 11, 10, 0, 0, tzinfo=timezone.utc)
        store.set_last_fired("cron-job", t1)
        store.set_last_fired("cron-job", t2)
        assert store.get_last_fired("cron-job") == t2

    def test_separate_connectors(self, store):
        t1 = datetime(2026, 3, 11, 9, 0, 0, tzinfo=timezone.utc)
        t2 = datetime(2026, 3, 11, 10, 0, 0, tzinfo=timezone.utc)
        store.set_last_fired("c1", t1)
        store.set_last_fired("c2", t2)
        assert store.get_last_fired("c1") == t1
        assert store.get_last_fired("c2") == t2
