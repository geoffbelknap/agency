"""Tests for poll source — hashing and change detection."""

import hashlib
import json
import sqlite3
from pathlib import Path

import pytest

from images.intake.poller import (
    hash_blob,
    hash_items,
    extract_items,
    parse_interval,
    PollStateStore,
)


class TestHashBlob:
    def test_deterministic(self):
        data = {"key": "value", "num": 42}
        assert hash_blob(data) == hash_blob(data)

    def test_different_data_different_hash(self):
        assert hash_blob({"a": 1}) != hash_blob({"a": 2})

    def test_key_order_irrelevant(self):
        assert hash_blob({"a": 1, "b": 2}) == hash_blob({"b": 2, "a": 1})


class TestHashItems:
    def test_list_of_dicts(self):
        items = [{"id": 1, "title": "A"}, {"id": 2, "title": "B"}]
        hashes = hash_items(items)
        assert len(hashes) == 2
        assert all(isinstance(h, str) for h in hashes)

    def test_deterministic(self):
        items = [{"id": 1}]
        assert hash_items(items) == hash_items(items)


class TestExtractItems:
    def test_root_list(self):
        data = [{"id": 1}, {"id": 2}]
        assert extract_items(data, "$") == [{"id": 1}, {"id": 2}]

    def test_nested_key(self):
        data = {"results": [{"id": 1}], "total": 1}
        assert extract_items(data, "$.results") == [{"id": 1}]

    def test_root_dollar_on_dict(self):
        data = {"id": 1}
        result = extract_items(data, "$")
        assert result is None

    def test_missing_key(self):
        data = {"other": [1, 2]}
        result = extract_items(data, "$.items")
        assert result is None

    def test_non_list_value(self):
        data = {"items": "not a list"}
        result = extract_items(data, "$.items")
        assert result is None


class TestParseInterval:
    def test_seconds(self):
        assert parse_interval("30s") == 30

    def test_minutes(self):
        assert parse_interval("5m") == 300

    def test_hours(self):
        assert parse_interval("1h") == 3600

    def test_days(self):
        assert parse_interval("2d") == 172800


class TestPollStateStore:
    @pytest.fixture
    def store(self, tmp_path):
        return PollStateStore(tmp_path)

    def test_no_previous_state(self, store):
        assert store.get_hashes("my-connector") == set()

    def test_store_and_retrieve(self, store):
        hashes = {"abc123", "def456"}
        store.set_hashes("my-connector", hashes)
        assert store.get_hashes("my-connector") == hashes

    def test_replace_hashes(self, store):
        store.set_hashes("c1", {"old"})
        store.set_hashes("c1", {"new1", "new2"})
        assert store.get_hashes("c1") == {"new1", "new2"}

    def test_separate_connectors(self, store):
        store.set_hashes("c1", {"a"})
        store.set_hashes("c2", {"b"})
        assert store.get_hashes("c1") == {"a"}
        assert store.get_hashes("c2") == {"b"}

    def test_get_failure_count(self, store):
        assert store.get_failure_count("c1") == 0

    def test_increment_failure_count(self, store):
        store.increment_failure_count("c1")
        store.increment_failure_count("c1")
        assert store.get_failure_count("c1") == 2

    def test_reset_failure_count(self, store):
        store.increment_failure_count("c1")
        store.reset_failure_count("c1")
        assert store.get_failure_count("c1") == 0
