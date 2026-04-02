import pytest
from unittest.mock import MagicMock
from images.egress.key_resolver import FileKeyResolver
from images.egress.swap_handlers import handle_api_key, handle_jwt_exchange


def _make_flow(host: str, headers=None) -> MagicMock:
    flow = MagicMock()
    flow.request.pretty_host = host
    flow.request.headers = dict(headers or {})
    flow.request.pretty_url = f"https://{host}/test"
    return flow


class TestApiKeyHandler:
    def test_injects_raw_key(self, tmp_path):
        keys_file = tmp_path / ".service-keys.env"
        keys_file.write_text("nextdns-api=abc123\n")
        resolver = FileKeyResolver(str(keys_file))
        flow = _make_flow("api.nextdns.io")
        config = {
            "type": "api-key",
            "domains": ["api.nextdns.io"],
            "header": "X-Api-Key",
            "key_ref": "nextdns-api",
        }
        handle_api_key(flow, config, resolver)
        assert flow.request.headers["X-Api-Key"] == "abc123"

    def test_injects_formatted_key(self, tmp_path):
        keys_file = tmp_path / ".service-keys.env"
        keys_file.write_text("OPENAI_API_KEY=sk-xyz\n")
        resolver = FileKeyResolver(str(keys_file))
        flow = _make_flow("api.openai.com")
        config = {
            "type": "api-key",
            "domains": ["api.openai.com"],
            "header": "Authorization",
            "format": "Bearer {key}",
            "key_ref": "OPENAI_API_KEY",
        }
        handle_api_key(flow, config, resolver)
        assert flow.request.headers["Authorization"] == "Bearer sk-xyz"

    def test_missing_key_logs_warning(self, tmp_path):
        keys_file = tmp_path / ".service-keys.env"
        keys_file.write_text("")
        resolver = FileKeyResolver(str(keys_file))
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
    def test_token_exchange_and_injection(self, tmp_path, monkeypatch):
        keys_file = tmp_path / ".service-keys.env"
        keys_file.write_text("lc-api=raw-secret\n")
        resolver = FileKeyResolver(str(keys_file))
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

        import images.egress.swap_handlers as sh
        monkeypatch.setattr(sh.requests, "post", MagicMock(return_value=mock_resp))

        handle_jwt_exchange(flow, config, resolver, _state_cache={})
        assert flow.request.headers["Authorization"] == "Bearer eyJtoken123"

    def test_cached_token_reused(self, tmp_path, monkeypatch):
        keys_file = tmp_path / ".service-keys.env"
        keys_file.write_text("lc-api=raw-secret\n")
        resolver = FileKeyResolver(str(keys_file))
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

        import images.egress.swap_handlers as sh
        mock_post = MagicMock(return_value=mock_resp)
        monkeypatch.setattr(sh.requests, "post", mock_post)

        cache = {}
        flow1 = _make_flow("api.limacharlie.io")
        handle_jwt_exchange(flow1, config, resolver, _state_cache=cache)
        flow2 = _make_flow("api.limacharlie.io")
        handle_jwt_exchange(flow2, config, resolver, _state_cache=cache)

        assert mock_post.call_count == 1
        assert flow2.request.headers["Authorization"] == "Bearer eyJtoken123"
