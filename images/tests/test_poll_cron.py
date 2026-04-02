"""Tests for poll source cron, transform, and auth fields."""
import pytest
from images.models.connector import ConnectorSource
from images.intake.poller import apply_transform


class TestPollCronField:
    def test_poll_with_cron_valid(self):
        """Poll source accepts cron instead of interval."""
        src = ConnectorSource(
            type="poll",
            url="https://api.example.com/events",
            cron="*/5 * * * *",
        )
        assert src.cron == "*/5 * * * *"
        assert src.interval is None

    def test_poll_with_interval_still_works(self):
        """Existing interval-based poll still works."""
        src = ConnectorSource(
            type="poll",
            url="https://api.example.com/events",
            interval="5m",
        )
        assert src.interval == "5m"
        assert src.cron is None

    def test_poll_cron_and_interval_mutual_exclusion(self):
        """Cannot set both cron and interval."""
        with pytest.raises(ValueError, match="mutually exclusive"):
            ConnectorSource(
                type="poll",
                url="https://api.example.com/events",
                cron="*/5 * * * *",
                interval="5m",
            )

    def test_poll_requires_cron_or_interval(self):
        """Poll source must have exactly one of cron or interval."""
        with pytest.raises(ValueError):
            ConnectorSource(
                type="poll",
                url="https://api.example.com/events",
            )

    def test_poll_transform_field(self):
        """Poll source accepts transform dot-path."""
        src = ConnectorSource(
            type="poll",
            url="https://api.example.com/events",
            interval="5m",
            transform="$.data.results",
        )
        assert src.transform == "$.data.results"

    def test_poll_auth_field(self):
        """Poll source accepts auth grant name."""
        src = ConnectorSource(
            type="poll",
            url="https://api.example.com/events",
            interval="5m",
            auth="splunk-api",
        )
        assert src.auth == "splunk-api"


class TestApplyTransform:
    def test_transform_nested_path(self):
        items = [
            {"data": {"name": "alice", "score": 10}},
            {"data": {"name": "bob", "score": 20}},
        ]
        result = apply_transform(items, "$.data")
        assert result == [{"name": "alice", "score": 10}, {"name": "bob", "score": 20}]

    def test_transform_deep_path(self):
        items = [{"a": {"b": {"c": "value"}}}]
        result = apply_transform(items, "$.a.b")
        assert result == [{"c": "value"}]

    def test_transform_missing_key(self):
        items = [{"data": {"name": "alice"}}, {"other": "field"}]
        result = apply_transform(items, "$.data")
        assert result[0] == {"name": "alice"}
        assert result[1] == {}

    def test_transform_none_passthrough(self):
        items = [{"a": 1}, {"b": 2}]
        result = apply_transform(items, None)
        assert result == items

    def test_transform_root(self):
        items = [{"a": 1}]
        result = apply_transform(items, "$")
        assert result == items
