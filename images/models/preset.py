"""Pydantic model for external preset YAML files."""

from typing import Literal, Optional

from pydantic import BaseModel, field_validator

from images.models.routing import VALID_TIERS


class PresetResponsivenessConfig(BaseModel):
    """Channel-scoped responsiveness for presets."""
    default: Literal["silent", "mention-only", "active"] = "mention-only"
    channels: dict[str, Literal["silent", "mention-only", "active"]] = {}


class PresetExpertiseConfig(BaseModel):
    """Base expertise profile for presets."""
    description: str = ""
    keywords: list[str] = []


class TriageConfig(BaseModel):
    """Domain-aware triage configuration for notification classification."""
    domains: list[str]
    prompt: str


class IdentityConfig(BaseModel):
    """Agent identity block."""
    purpose: str
    body: str


class HardLimit(BaseModel):
    rule: str
    reason: str


class EscalationConfig(BaseModel):
    always_escalate: list[str] = []
    flag_before_proceeding: list[str] = []


class PresetConfig(BaseModel):
    """A complete agent preset definition."""
    name: str
    type: str
    description: str
    model: Optional[str] = None
    model_tier: str = "standard"
    tools: list[str]
    capabilities: list[str]
    identity: IdentityConfig
    hard_limits: list[HardLimit]
    escalation: EscalationConfig
    triage: Optional[TriageConfig] = None
    responsiveness: PresetResponsivenessConfig = PresetResponsivenessConfig()
    expertise: PresetExpertiseConfig = PresetExpertiseConfig()

    @field_validator("type")
    @classmethod
    def validate_type(cls, v: str) -> str:
        allowed = {"standard", "coordinator", "function"}
        if v not in allowed:
            raise ValueError(f"type must be one of {allowed}, got '{v}'")
        return v

    @field_validator("model_tier")
    @classmethod
    def validate_model_tier(cls, v: str) -> str:
        if v not in VALID_TIERS:
            raise ValueError(f"model_tier must be one of {VALID_TIERS}, got '{v}'")
        return v
