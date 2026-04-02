import os
import pytest
import yaml
from unittest.mock import MagicMock, patch

import sys
mock_http = MagicMock()
sys.modules.setdefault("mitmproxy", MagicMock())
sys.modules.setdefault("mitmproxy.http", mock_http)

from images.egress.credential_swap import CredentialSwapAddon


def _make_flow(host: str, headers: dict = None) -> MagicMock:
    flow = MagicMock()
    flow.request.pretty_host = host
    flow.request.headers = dict(headers or {})
    flow.request.pretty_url = f"https://{host}/test"
    return flow


class _FakeResolver:
    """In-memory resolver for tests, replacing the real SocketKeyResolver."""

    def __init__(self, keys: dict):
        self._keys = keys

    def resolve(self, key_ref: str):
        return self._keys.get(key_ref)

    def reload(self):
        pass


class TestUnifiedSwapAddon:
    def _setup_addon(self, tmp_path, swaps: dict, keys: dict = None) -> CredentialSwapAddon:
        swap_file = tmp_path / "credential-swaps.yaml"
        swap_file.write_text(yaml.dump({"swaps": swaps}))
        # Create a fake socket file so the existence check passes
        socket_file = tmp_path / "gateway.sock"
        socket_file.write_text("")
        with patch.dict(os.environ, {"GATEWAY_SOCKET": str(socket_file)}):
            addon = CredentialSwapAddon(
                swap_config_path=str(swap_file),
                swap_local_path=str(tmp_path / "credential-swaps.local.yaml"),
            )
        if keys is None:
            keys = {
                "nextdns-api": "abc123",
                "ANTHROPIC_API_KEY": "sk-ant-xyz",
                "BRAVE_API_KEY": "brave-key",
            }
        addon._resolver = _FakeResolver(keys)
        return addon

    def test_api_key_injected_by_domain(self, tmp_path):
        addon = self._setup_addon(tmp_path, {
            "nextdns-api": {
                "type": "api-key",
                "domains": ["api.nextdns.io"],
                "header": "X-Api-Key",
                "key_ref": "nextdns-api",
            }
        })
        flow = _make_flow("api.nextdns.io")
        addon.request(flow)
        assert flow.request.headers["X-Api-Key"] == "abc123"

    def test_no_match_no_injection(self, tmp_path):
        addon = self._setup_addon(tmp_path, {
            "nextdns-api": {
                "type": "api-key",
                "domains": ["api.nextdns.io"],
                "header": "X-Api-Key",
                "key_ref": "nextdns-api",
            }
        })
        flow = _make_flow("api.unknown.com")
        addon.request(flow)
        assert "X-Api-Key" not in flow.request.headers

    def test_formatted_key(self, tmp_path):
        addon = self._setup_addon(tmp_path, {
            "anthropic": {
                "type": "api-key",
                "domains": ["api.anthropic.com"],
                "header": "x-api-key",
                "key_ref": "ANTHROPIC_API_KEY",
            }
        })
        flow = _make_flow("api.anthropic.com")
        addon.request(flow)
        assert flow.request.headers["x-api-key"] == "sk-ant-xyz"

    def test_local_overrides_generated(self, tmp_path):
        swap_file = tmp_path / "credential-swaps.yaml"
        swap_file.write_text(yaml.dump({"swaps": {
            "nextdns-api": {
                "type": "api-key",
                "domains": ["api.nextdns.io"],
                "header": "X-Api-Key",
                "key_ref": "nextdns-api",
            }
        }}))
        local_file = tmp_path / "credential-swaps.local.yaml"
        local_file.write_text(yaml.dump({"swaps": {
            "custom": {
                "type": "api-key",
                "domains": ["api.nextdns.io"],
                "header": "X-Api-Key",
                "key_ref": "BRAVE_API_KEY",
            }
        }}))
        socket_file = tmp_path / "gateway.sock"
        socket_file.write_text("")
        with patch.dict(os.environ, {"GATEWAY_SOCKET": str(socket_file)}):
            addon = CredentialSwapAddon(
                swap_config_path=str(swap_file),
                swap_local_path=str(local_file),
            )
        addon._resolver = _FakeResolver({
            "nextdns-api": "abc123",
            "BRAVE_API_KEY": "brave-key",
        })
        flow = _make_flow("api.nextdns.io")
        addon.request(flow)
        assert flow.request.headers["X-Api-Key"] == "brave-key"

    def test_x_agency_service_header_swap(self, tmp_path):
        addon = self._setup_addon(tmp_path, {
            "brave-search": {
                "type": "api-key",
                "domains": ["api.search.brave.com"],
                "header": "X-Subscription-Token",
                "key_ref": "BRAVE_API_KEY",
            }
        })
        flow = _make_flow("api.search.brave.com", {"x-agency-service": "brave-search"})
        addon.request(flow)
        assert flow.request.headers["X-Subscription-Token"] == "brave-key"
        assert "x-agency-service" not in flow.request.headers

    def test_reload_picks_up_changes(self, tmp_path):
        swap_file = tmp_path / "credential-swaps.yaml"
        swap_file.write_text(yaml.dump({"swaps": {}}))
        socket_file = tmp_path / "gateway.sock"
        socket_file.write_text("")
        with patch.dict(os.environ, {"GATEWAY_SOCKET": str(socket_file)}):
            addon = CredentialSwapAddon(
                swap_config_path=str(swap_file),
                swap_local_path=str(tmp_path / "credential-swaps.local.yaml"),
            )
        addon._resolver = _FakeResolver({"nextdns-api": "abc123"})

        flow = _make_flow("api.nextdns.io")
        addon.request(flow)
        assert "X-Api-Key" not in flow.request.headers

        swap_file.write_text(yaml.dump({"swaps": {
            "nextdns-api": {
                "type": "api-key",
                "domains": ["api.nextdns.io"],
                "header": "X-Api-Key",
                "key_ref": "nextdns-api",
            }
        }}))
        addon.reload()
        flow2 = _make_flow("api.nextdns.io")
        addon.request(flow2)
        assert flow2.request.headers["X-Api-Key"] == "abc123"

    def test_missing_gateway_socket_raises(self, tmp_path):
        swap_file = tmp_path / "credential-swaps.yaml"
        swap_file.write_text(yaml.dump({"swaps": {}}))
        with patch.dict(os.environ, {"GATEWAY_SOCKET": ""}, clear=False):
            with pytest.raises(RuntimeError, match="GATEWAY_SOCKET not set"):
                CredentialSwapAddon(swap_config_path=str(swap_file))

    def test_nonexistent_gateway_socket_raises(self, tmp_path):
        swap_file = tmp_path / "credential-swaps.yaml"
        swap_file.write_text(yaml.dump({"swaps": {}}))
        with patch.dict(os.environ, {"GATEWAY_SOCKET": "/nonexistent/path"}, clear=False):
            with pytest.raises(RuntimeError, match="GATEWAY_SOCKET not set"):
                CredentialSwapAddon(swap_config_path=str(swap_file))
