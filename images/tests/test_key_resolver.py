import pytest
from images.egress.key_resolver import SocketKeyResolver


class TestSocketKeyResolver:
    def test_resolve_caches_result(self):
        resolver = SocketKeyResolver("/nonexistent/socket")
        # Pre-populate cache to test cache hit path
        resolver._cache["MY_KEY"] = "cached-value"
        assert resolver.resolve("MY_KEY") == "cached-value"

    def test_resolve_missing_socket_returns_none(self):
        resolver = SocketKeyResolver("/nonexistent/socket")
        # No socket exists, so resolve should fail gracefully
        assert resolver.resolve("MY_KEY") is None

    def test_reload_clears_cache(self):
        resolver = SocketKeyResolver("/nonexistent/socket")
        resolver._cache["KEY"] = "value"
        resolver.reload()
        assert resolver._cache == {}
