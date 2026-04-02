"""Tests for routing configuration models."""

import pytest
import yaml

from images.models.routing import (
    ModelConfig,
    ProviderConfig,
    RoutingConfig,
    RoutingSettings,
)


class TestProviderConfig:
    def test_defaults(self):
        p = ProviderConfig(api_base="https://api.example.com/v1/")
        assert p.auth_env == ""
        assert p.auth_header == ""
        assert p.auth_prefix == ""

    def test_full_config(self):
        p = ProviderConfig(
            api_base="https://api.anthropic.com/v1/",
            auth_env="ANTHROPIC_API_KEY",
            auth_header="x-api-key",
        )
        assert p.api_base == "https://api.anthropic.com/v1/"
        assert p.auth_env == "ANTHROPIC_API_KEY"


class TestModelConfig:
    def test_cost_defaults(self):
        m = ModelConfig(provider="anthropic", provider_model="claude-sonnet-4-20250514")
        assert m.cost_per_mtok_in == 0.0
        assert m.cost_per_mtok_out == 0.0

    def test_with_costs(self):
        m = ModelConfig(
            provider="anthropic",
            provider_model="claude-sonnet-4-20250514",
            cost_per_mtok_in=3.0,
            cost_per_mtok_out=15.0,
        )
        assert m.cost_per_mtok_in == 3.0
        assert m.cost_per_mtok_out == 15.0


class TestRoutingConfig:
    @pytest.fixture
    def sample_config(self):
        return RoutingConfig(
            providers={
                "anthropic": ProviderConfig(
                    api_base="https://api.anthropic.com/v1/",
                    auth_env="ANTHROPIC_API_KEY",
                    auth_header="x-api-key",
                ),
                "openai": ProviderConfig(
                    api_base="https://api.openai.com/v1/",
                    auth_env="OPENAI_API_KEY",
                    auth_header="Authorization",
                    auth_prefix="Bearer ",
                ),
            },
            models={
                "claude-sonnet": ModelConfig(
                    provider="anthropic",
                    provider_model="claude-sonnet-4-20250514",
                    cost_per_mtok_in=3.0,
                    cost_per_mtok_out=15.0,
                ),
                "gpt-4o-mini": ModelConfig(
                    provider="openai",
                    provider_model="gpt-4o-mini",
                    cost_per_mtok_in=0.15,
                    cost_per_mtok_out=0.60,
                ),
            },
        )

    def test_resolve_model_found(self, sample_config):
        result = sample_config.resolve_model("claude-sonnet")
        assert result is not None
        provider, model = result
        assert provider.api_base == "https://api.anthropic.com/v1/"
        assert model.provider_model == "claude-sonnet-4-20250514"

    def test_resolve_model_not_found(self, sample_config):
        assert sample_config.resolve_model("nonexistent") is None

    def test_resolve_model_missing_provider(self):
        config = RoutingConfig(
            models={
                "broken": ModelConfig(provider="missing", provider_model="x"),
            },
        )
        assert config.resolve_model("broken") is None

    def test_from_yaml(self, tmp_path):
        yaml_content = {
            "version": "0.1",
            "providers": {
                "anthropic": {
                    "api_base": "https://api.anthropic.com/v1/",
                    "auth_env": "ANTHROPIC_API_KEY",
                    "auth_header": "x-api-key",
                },
            },
            "models": {
                "claude-sonnet": {
                    "provider": "anthropic",
                    "provider_model": "claude-sonnet-4-20250514",
                    "cost_per_mtok_in": 3.0,
                    "cost_per_mtok_out": 15.0,
                },
            },
            "settings": {
                "xpia_scan": True,
                "default_timeout": 300,
            },
        }
        config_file = tmp_path / "routing.yaml"
        config_file.write_text(yaml.dump(yaml_content))

        data = yaml.safe_load(config_file.read_text())
        config = RoutingConfig(**data)
        assert len(config.providers) == 1
        assert len(config.models) == 1
        result = config.resolve_model("claude-sonnet")
        assert result is not None
        assert result[1].provider_model == "claude-sonnet-4-20250514"

    def test_settings_defaults(self):
        config = RoutingConfig()
        assert config.settings.xpia_scan is True
        assert config.settings.default_timeout == 300


class TestProviderValidation:
    """Security validation tests for ProviderConfig."""

    def test_rejects_file_scheme(self):
        with pytest.raises(Exception, match="http:// or https://"):
            ProviderConfig(api_base="file:///etc/passwd")

    def test_rejects_ftp_scheme(self):
        with pytest.raises(Exception, match="http:// or https://"):
            ProviderConfig(api_base="ftp://evil.com/")

    def test_rejects_cloud_metadata(self):
        with pytest.raises(Exception, match="blocked host"):
            ProviderConfig(api_base="http://169.254.169.254/latest/meta-data/")

    def test_rejects_raw_ip_for_https(self):
        with pytest.raises(Exception, match="raw IP"):
            ProviderConfig(api_base="https://1.2.3.4/v1/")

    def test_allows_raw_ip_for_http(self):
        # Ollama-style local providers use http with IPs
        p = ProviderConfig(api_base="http://192.168.1.100:11434/v1/")
        assert "192.168.1.100" in p.api_base

    def test_rejects_empty_api_base(self):
        with pytest.raises(Exception, match="must not be empty"):
            ProviderConfig(api_base="")

    def test_rejects_arbitrary_auth_env(self):
        with pytest.raises(Exception, match="credential variable"):
            ProviderConfig(api_base="https://example.com/v1/", auth_env="HOME")

    def test_rejects_path_auth_env(self):
        with pytest.raises(Exception, match="credential variable"):
            ProviderConfig(api_base="https://example.com/v1/", auth_env="PATH")

    def test_allows_valid_auth_env(self):
        p = ProviderConfig(api_base="https://example.com/v1/", auth_env="ANTHROPIC_API_KEY")
        assert p.auth_env == "ANTHROPIC_API_KEY"

    def test_allows_empty_auth_env(self):
        p = ProviderConfig(api_base="http://localhost:11434/v1/", auth_env="")
        assert p.auth_env == ""

    def test_allows_token_auth_env(self):
        p = ProviderConfig(api_base="https://example.com/v1/", auth_env="GITHUB_TOKEN")
        assert p.auth_env == "GITHUB_TOKEN"


class TestModelConfigValidation:
    def test_rejects_negative_cost_in(self):
        with pytest.raises(Exception):
            ModelConfig(provider="x", provider_model="y", cost_per_mtok_in=-1.0)

    def test_rejects_negative_cost_out(self):
        with pytest.raises(Exception):
            ModelConfig(provider="x", provider_model="y", cost_per_mtok_out=-0.01)

    def test_allows_zero_cost(self):
        m = ModelConfig(provider="x", provider_model="y", cost_per_mtok_in=0.0, cost_per_mtok_out=0.0)
        assert m.cost_per_mtok_in == 0.0


class TestRoutingSettingsValidation:
    def test_rejects_zero_timeout(self):
        with pytest.raises(Exception):
            RoutingSettings(default_timeout=0)

    def test_rejects_huge_timeout(self):
        with pytest.raises(Exception):
            RoutingSettings(default_timeout=9999)

    def test_allows_valid_timeout(self):
        s = RoutingSettings(default_timeout=60)
        assert s.default_timeout == 60


class TestBudgetConfigValidation:
    def test_rejects_negative_daily(self):
        from images.models.constraints import BudgetConfig
        with pytest.raises(Exception):
            BudgetConfig(max_daily_usd=-1.0)

    def test_rejects_negative_session(self):
        from images.models.constraints import BudgetConfig
        with pytest.raises(Exception):
            BudgetConfig(max_session_usd=-0.5)

    def test_rejects_warning_pct_zero(self):
        from images.models.constraints import BudgetConfig
        with pytest.raises(Exception):
            BudgetConfig(warning_threshold_pct=0)

    def test_rejects_warning_pct_over_100(self):
        from images.models.constraints import BudgetConfig
        with pytest.raises(Exception):
            BudgetConfig(warning_threshold_pct=101)

    def test_allows_valid_budget(self):
        from images.models.constraints import BudgetConfig
        b = BudgetConfig(max_daily_usd=10.0, warning_threshold_pct=90)
        assert b.max_daily_usd == 10.0
