"""Policy configuration schema."""

from typing import Any

from pydantic import BaseModel, ConfigDict, Field, field_validator


class CommsScanningConfig(BaseModel):
    enabled: bool = True
    rules: list[str] = Field(default_factory=lambda: ["no_credentials"])

    @field_validator("rules")
    @classmethod
    def ensure_no_credentials(cls, v: list[str]) -> list[str]:
        if "no_credentials" not in v:
            v = ["no_credentials"] + v
        return v


class CommsBridgingConfig(BaseModel):
    enabled: bool = False
    allowed_platforms: list[str] = Field(default_factory=list)


class CommunicationPolicy(BaseModel):
    scanning: CommsScanningConfig = Field(default_factory=CommsScanningConfig)
    bridging: CommsBridgingConfig = Field(default_factory=CommsBridgingConfig)


class PolicyConfig(BaseModel):
    """Schema for policy.yaml (org-level or agent-level)."""

    model_config = ConfigDict(extra="forbid")

    version: str = "0.1"
    bundle: str | None = None
    additions: list[str] = []
    restrictions: list[str] = []
    communication: CommunicationPolicy = Field(default_factory=CommunicationPolicy)


class AgentPolicyConfig(BaseModel):
    """Schema for agent-level policy.yaml."""

    model_config = ConfigDict(extra="forbid")

    version: str = "0.1"
    inherits_from: str | None = None
    additions: list[str] = []
    restrictions: list[str] = []
