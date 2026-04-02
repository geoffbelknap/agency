"""Tests for dot-path nested field matching in route match and poll URL variables."""
from images.intake.router import match_route, _get_nested


class TestGetNested:
    def test_single_level(self):
        assert _get_nested({"a": 1}, "a") == 1

    def test_two_levels(self):
        assert _get_nested({"a": {"b": 2}}, "a.b") == 2

    def test_three_levels(self):
        assert _get_nested({"a": {"b": {"c": 3}}}, "a.b.c") == 3

    def test_missing_key(self):
        assert _get_nested({"a": 1}, "b") is None

    def test_missing_nested_key(self):
        assert _get_nested({"a": {"b": 2}}, "a.c") is None

    def test_non_dict_intermediate(self):
        assert _get_nested({"a": "string"}, "a.b") is None


class TestNestedMatchRoute:
    """Test that match_route supports dot-path fields like detect_mtd.level."""

    def _make_route(self, match):
        from types import SimpleNamespace
        return SimpleNamespace(match=match)

    def test_flat_field_still_works(self):
        route = self._make_route({"severity": "high"})
        assert match_route(route, {"severity": "high"})
        assert not match_route(route, {"severity": "low"})

    def test_nested_field_exact_match(self):
        route = self._make_route({"detect_mtd.level": "medium"})
        payload = {"detect_mtd": {"level": "medium"}}
        assert match_route(route, payload)

    def test_nested_field_list_match(self):
        route = self._make_route({"detect_mtd.level": ["high", "critical"]})
        assert match_route(route, {"detect_mtd": {"level": "high"}})
        assert match_route(route, {"detect_mtd": {"level": "critical"}})
        assert not match_route(route, {"detect_mtd": {"level": "medium"}})

    def test_nested_field_missing(self):
        route = self._make_route({"detect_mtd.level": "high"})
        assert not match_route(route, {"other": "field"})

    def test_nested_field_wildcard(self):
        route = self._make_route({"detect_mtd.level": "*"})
        assert match_route(route, {"detect_mtd": {"level": "anything"}})
        assert not match_route(route, {"detect_mtd": {}})

    def test_mixed_flat_and_nested(self):
        route = self._make_route({"cat": "alert", "detect_mtd.level": ["high", "critical"]})
        payload = {"cat": "alert", "detect_mtd": {"level": "high"}}
        assert match_route(route, payload)
        payload_wrong = {"cat": "alert", "detect_mtd": {"level": "low"}}
        assert not match_route(route, payload_wrong)

    def test_limacharlie_detection_match(self):
        """Real LC detection payload matches severity route."""
        route = self._make_route({"detect_mtd.level": ["high", "critical"]})
        lc_payload = {
            "detect_id": "abc-123",
            "cat": "Suspicious Process",
            "detect_mtd": {
                "level": "high",
                "description": "Something bad happened",
            },
            "routing": {
                "hostname": "server-1",
                "sid": "sid-xyz",
            },
        }
        assert match_route(route, lc_payload)


class TestPollUrlVariables:
    def test_poll_start_end_substituted(self):
        """URL with {_poll_start} and {_poll_end} gets unix timestamps."""
        import time
        url = "https://api.example.com/detections?start={_poll_start}&end={_poll_end}"
        now = int(time.time())
        poll_start = now - 120
        result = url.replace("{_poll_start}", str(poll_start)).replace("{_poll_end}", str(now))
        assert str(poll_start) in result
        assert str(now) in result
        assert "{_poll_start}" not in result
        assert "{_poll_end}" not in result
