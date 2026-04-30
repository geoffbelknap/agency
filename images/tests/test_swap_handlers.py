import pytest
from unittest.mock import MagicMock
from services.egress.swap_handlers import handle_api_key, handle_jwt_exchange


class _FakeResolver:
    """In-memory resolver for tests."""

    def __init__(self, keys: dict):
        self._keys = keys

    def resolve(self, key_ref: str):
        return self._keys.get(key_ref)

    def reload(self):
        pass


def _make_flow(host: str, headers=None) -> MagicMock:
    flow = MagicMock()
    flow.request.pretty_host = host
    flow.request.headers = dict(headers or {})
    flow.request.pretty_url = f"https://{host}/test"
    return flow


class TestApiKeyHandler:
    def test_injects_raw_key(self):
        resolver = _FakeResolver({"nextdns-api": "abc123"})
        flow = _make_flow("api.nextdns.io")
        config = {
            "type": "api-key",
            "domains": ["api.nextdns.io"],
            "header": "X-Api-Key",
            "key_ref": "nextdns-api",
        }
        handle_api_key(flow, config, resolver)
        assert flow.request.headers["X-Api-Key"] == "abc123"

    def test_injects_formatted_key(self):
        resolver = _FakeResolver({"PROVIDER_B_API_KEY": "sk-xyz"})
        flow = _make_flow("provider-b.example.com")
        config = {
            "type": "api-key",
            "domains": ["provider-b.example.com"],
            "header": "Authorization",
            "format": "Bearer {key}",
            "key_ref": "PROVIDER_B_API_KEY",
        }
        handle_api_key(flow, config, resolver)
        assert flow.request.headers["Authorization"] == "Bearer sk-xyz"

    def test_missing_key_logs_warning(self):
        resolver = _FakeResolver({})
        flow = _make_flow("api.nextdns.io")
        config = {
            "type": "api-key",
            "domains": ["api.nextdns.io"],
            "header": "X-Api-Key",
            "key_ref": "nonexistent",
        }
        handle_api_key(flow, config, resolver)
        assert "X-Api-Key" not in flow.request.headers


class TestJWTExchangeHandler:
    def test_token_exchange_and_injection(self, monkeypatch):
        resolver = _FakeResolver({"lc-api": "raw-secret"})
        flow = _make_flow("api.limacharlie.io")
        config = {
            "type": "jwt-exchange",
            "domains": ["api.limacharlie.io"],
            "token_url": "https://jwt.example.com/token",
            "token_params": {"secret": "${credential}", "oid": "org1"},
            "token_response_field": "jwt",
            "token_ttl_seconds": 3600,
            "inject_header": "Authorization",
            "inject_format": "Bearer {token}",
            "key_ref": "lc-api",
        }

        mock_resp = MagicMock()
        mock_resp.status_code = 200
        mock_resp.raise_for_status = MagicMock()
        mock_resp.json.return_value = {"jwt": "eyJtoken123"}

        import services.egress.swap_handlers as sh
        monkeypatch.setattr(sh.requests, "post", MagicMock(return_value=mock_resp))

        handle_jwt_exchange(flow, config, resolver, _state_cache={})
        assert flow.request.headers["Authorization"] == "Bearer eyJtoken123"

    def test_cached_token_reused(self, monkeypatch):
        resolver = _FakeResolver({"lc-api": "raw-secret"})
        config = {
            "type": "jwt-exchange",
            "domains": ["api.limacharlie.io"],
            "token_url": "https://jwt.example.com/token",
            "token_params": {"secret": "${credential}"},
            "token_response_field": "jwt",
            "token_ttl_seconds": 3600,
            "inject_header": "Authorization",
            "inject_format": "Bearer {token}",
            "key_ref": "lc-api",
        }

        mock_resp = MagicMock()
        mock_resp.status_code = 200
        mock_resp.raise_for_status = MagicMock()
        mock_resp.json.return_value = {"jwt": "eyJtoken123"}

        import services.egress.swap_handlers as sh
        mock_post = MagicMock(return_value=mock_resp)
        monkeypatch.setattr(sh.requests, "post", mock_post)

        cache = {}
        flow1 = _make_flow("api.limacharlie.io")
        handle_jwt_exchange(flow1, config, resolver, _state_cache=cache)
        flow2 = _make_flow("api.limacharlie.io")
        handle_jwt_exchange(flow2, config, resolver, _state_cache=cache)

        assert mock_post.call_count == 1
        assert flow2.request.headers["Authorization"] == "Bearer eyJtoken123"
