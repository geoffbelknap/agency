"""Tests for channel-watch source — pattern matching and message tracking."""

import re
import sqlite3
from datetime import datetime, timezone
from pathlib import Path

import pytest

from images.intake.channel_watcher import (
    matches_pattern,
    ChannelWatchStateStore,
)


class TestMatchesPattern:
    def test_simple_match(self):
        assert matches_pattern("hello world", "hello") is True

    def test_no_match(self):
        assert matches_pattern("hello world", "^goodbye") is False

    def test_regex_anchor(self):
        assert matches_pattern("/request fix the build", "^/request\\s+") is True
        assert matches_pattern("please /request fix", "^/request\\s+") is False

    def test_case_sensitive(self):
        assert matches_pattern("Hello", "^hello$") is False

    def test_complex_pattern(self):
        assert matches_pattern("BUG-1234: crash on login", "^BUG-\\d+:") is True
        assert matches_pattern("FEAT-99: new button", "^BUG-\\d+:") is False

    def test_invalid_regex_returns_false(self):
        assert matches_pattern("test", "[invalid") is False


class TestChannelWatchStateStore:
    @pytest.fixture
    def store(self, tmp_path):
        return ChannelWatchStateStore(tmp_path)

    def test_no_previous_state(self, store):
        assert store.get_last_seen("my-connector") is None

    def test_store_and_retrieve(self, store):
        ts = datetime(2026, 3, 11, 9, 0, 0, tzinfo=timezone.utc)
        store.set_last_seen("my-connector", ts)
        assert store.get_last_seen("my-connector") == ts

    def test_update(self, store):
        t1 = datetime(2026, 3, 11, 9, 0, 0, tzinfo=timezone.utc)
        t2 = datetime(2026, 3, 11, 10, 0, 0, tzinfo=timezone.utc)
        store.set_last_seen("c1", t1)
        store.set_last_seen("c1", t2)
        assert store.get_last_seen("c1") == t2

    def test_separate_connectors(self, store):
        t1 = datetime(2026, 3, 11, 9, 0, 0, tzinfo=timezone.utc)
        t2 = datetime(2026, 3, 11, 10, 0, 0, tzinfo=timezone.utc)
        store.set_last_seen("c1", t1)
        store.set_last_seen("c2", t2)
        assert store.get_last_seen("c1") == t1
        assert store.get_last_seen("c2") == t2
