"""Routing configuration schema for LLM provider routing.

Defines routing.yaml that the enforcer uses to route LLM requests
to providers (Anthropic, OpenAI, etc.).
"""

import re
from urllib.parse import urlparse

from pydantic import BaseModel, ConfigDict, Field, field_validator
from typing import Optional


# Cloud metadata and link-local IP ranges that must never be used as api_base
_BLOCKED_HOSTS = frozenset({
    "169.254.169.254",   # AWS/GCP metadata
    "metadata.google.internal",
    "100.100.100.200",   # Alibaba metadata
    "0.0.0.0",           # Unspecified
})


class ProviderConfig(BaseModel):
    """LLM provider connection details."""

    model_config = ConfigDict(extra="forbid")

    api_base: str
    auth_env: str = ""
    auth_header: str = ""
    auth_prefix: str = ""
    caching: bool = True

    @field_validator("api_base")
    @classmethod
    def validate_api_base(cls, v: str) -> str:
        v = v.strip()
        if not v:
            raise ValueError("api_base must not be empty")
        parsed = urlparse(v)
        if parsed.scheme not in ("http", "https"):
            raise ValueError(
                f"api_base must use http:// or https:// scheme, got {parsed.scheme}://"
            )
        host = parsed.hostname or ""
        if host in _BLOCKED_HOSTS:
            raise ValueError(f"api_base must not target blocked host: {host}")
        # Block raw IPs for HTTPS providers (allow for local like ollama)
        if parsed.scheme == "https" and re.match(r"^\d+\.\d+\.\d+\.\d+$", host):
            raise ValueError("api_base must use a domain name, not a raw IP, for HTTPS")
        return v

    @field_validator("auth_env")
    @classmethod
    def validate_auth_env(cls, v: str) -> str:
        if not v:
            return v
        if not re.match(r"^[A-Z][A-Z0-9_]*_(API_KEY|TOKEN|SECRET|KEY)$", v):
            raise ValueError(
                f"auth_env must reference a credential variable "
                f"(pattern: *_API_KEY, *_TOKEN, *_SECRET, *_KEY), got: {v}"
            )
        return v


class ModelConfig(BaseModel):
    """Model alias -> provider mapping with cost information."""

    model_config = ConfigDict(extra="forbid")

    provider: str
    provider_model: str
    cost_per_mtok_in: float = Field(default=0.0, ge=0.0)
    cost_per_mtok_out: float = Field(default=0.0, ge=0.0)
    cost_per_mtok_cached: float = Field(default=0.0, ge=0.0)


class TierEntry(BaseModel):
    """A single model entry within a tier, ranked by preference."""

    model_config = ConfigDict(extra="forbid")

    model: str  # references a key in RoutingConfig.models
    preference: int = Field(default=0, ge=0)  # lower = preferred


VALID_TIERS = ("frontier", "standard", "fast", "mini", "nano")


class TierConfig(BaseModel):
    """Maps a tier name to an ordered list of model candidates.

    The routing layer picks the first model whose provider has credentials
    configured. Operators can reorder or prune to control cost/quality.
    """

    model_config = ConfigDict(extra="forbid")

    frontier: list[TierEntry] = []
    standard: list[TierEntry] = []
    fast: list[TierEntry] = []
    mini: list[TierEntry] = []
    nano: list[TierEntry] = []


class RoutingSettings(BaseModel):
    """Global routing settings."""

    model_config = ConfigDict(extra="forbid")

    xpia_scan: bool = True
    default_timeout: int = Field(default=300, ge=1, le=3600)
    default_tier: str = Field(default="standard")

    @field_validator("default_tier")
    @classmethod
    def validate_default_tier(cls, v: str) -> str:
        if v not in VALID_TIERS:
            raise ValueError(f"default_tier must be one of {VALID_TIERS}, got '{v}'")
        return v


class RoutingConfig(BaseModel):
    """Schema for routing.yaml — org-level LLM routing configuration."""

    model_config = ConfigDict(extra="forbid")

    version: str = "0.1"
    providers: dict[str, ProviderConfig] = {}
    models: dict[str, ModelConfig] = {}
    tiers: TierConfig = TierConfig()
    settings: RoutingSettings = RoutingSettings()

    def resolve_model(self, alias: str) -> Optional[tuple[ProviderConfig, ModelConfig]]:
        """Resolve a model alias to its provider config and model config.

        Returns (ProviderConfig, ModelConfig) or None if not found.
        """
        model = self.models.get(alias)
        if model is None:
            return None
        provider = self.providers.get(model.provider)
        if provider is None:
            return None
        return provider, model

    def resolve_tier(
        self, tier: str, extra_env: Optional[dict[str, str]] = None,
    ) -> Optional[tuple[ProviderConfig, ModelConfig, str]]:
        """Resolve a tier to the best available model.

        Walks the tier's model list in preference order, returning the first
        model whose provider has credentials (non-empty auth_env with a
        matching env var, or empty auth_env for local providers).

        Args:
            tier: One of VALID_TIERS.
            extra_env: Additional env vars to check (e.g. from .env file).

        Returns (ProviderConfig, ModelConfig, model_alias) or None.
        """
        import os

        if tier not in VALID_TIERS:
            return None
        entries = getattr(self.tiers, tier, [])
        if not entries:
            return None

        env = extra_env or {}

        sorted_entries = sorted(entries, key=lambda e: e.preference)
        for entry in sorted_entries:
            result = self.resolve_model(entry.model)
            if result is None:
                continue
            provider, model_cfg = result
            # Local providers (no auth_env) are always available
            if not provider.auth_env:
                return provider, model_cfg, entry.model
            # Check env vars and extra_env (e.g. ~/.agency/.env)
            if os.environ.get(provider.auth_env) or env.get(provider.auth_env):
                return provider, model_cfg, entry.model
        return None
