import pytest
import yaml
from unittest.mock import MagicMock

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


class TestUnifiedSwapAddon:
    def _setup_addon(self, tmp_path, swaps: dict) -> CredentialSwapAddon:
        swap_file = tmp_path / "credential-swaps.yaml"
        swap_file.write_text(yaml.dump({"swaps": swaps}))
        keys_file = tmp_path / ".service-keys.env"
        keys_file.write_text(
            "nextdns-api=abc123\n"
            "ANTHROPIC_API_KEY=sk-ant-xyz\n"
            "BRAVE_API_KEY=brave-key\n"
        )
        return CredentialSwapAddon(
            swap_config_path=str(swap_file),
            swap_local_path=str(tmp_path / "credential-swaps.local.yaml"),
            service_keys_path=str(keys_file),
        )

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
        keys_file = tmp_path / ".service-keys.env"
        keys_file.write_text("nextdns-api=abc123\nBRAVE_API_KEY=brave-key\n")
        addon = CredentialSwapAddon(
            swap_config_path=str(swap_file),
            swap_local_path=str(local_file),
            service_keys_path=str(keys_file),
        )
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
        keys_file = tmp_path / ".service-keys.env"
        keys_file.write_text("nextdns-api=abc123\n")
        addon = CredentialSwapAddon(
            swap_config_path=str(swap_file),
            swap_local_path=str(tmp_path / "credential-swaps.local.yaml"),
            service_keys_path=str(keys_file),
        )

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
