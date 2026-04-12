"""Pydantic model for mission YAML files."""

import re

from pydantic import BaseModel, ConfigDict, Field, field_validator


class MissionTrigger(BaseModel):
    model_config = ConfigDict(extra="forbid")

    source: str = ""
    connector: str = ""
    channel: str = ""
    event_type: str = ""
    match: str = ""
    name: str = ""
    cron: str = ""


class MissionRequires(BaseModel):
    model_config = ConfigDict(extra="forbid")

    capabilities: list[str] = Field(default_factory=list)
    channels: list[str] = Field(default_factory=list)


class MissionBudget(BaseModel):
    model_config = ConfigDict(extra="forbid")

    daily: float = 0
    monthly: float = 0
    per_task: float = 0


class Mission(BaseModel):
    model_config = ConfigDict(extra="forbid")

    id: str = ""
    name: str
    description: str
    version: int = 0
    status: str = ""
    assigned_to: str = ""
    assigned_type: str = ""
    instructions: str
    triggers: list[MissionTrigger] = Field(default_factory=list)
    requires: MissionRequires | None = None
    budget: MissionBudget | None = None
    cost_mode: str = ""
    min_task_tier: str = ""

    @field_validator("name")
    @classmethod
    def validate_name(cls, value: str) -> str:
        if not value:
            raise ValueError("name must not be empty")
        if len(value) < 2 or len(value) > 63:
            raise ValueError("name must be between 2 and 63 characters")
        if not re.fullmatch(r"^[a-z0-9]([a-z0-9-]*[a-z0-9])?$", value):
            raise ValueError(
                "name must be lowercase alphanumeric with hyphens and cannot start or end with a hyphen"
            )
        return value

    @field_validator("description", "instructions")
    @classmethod
    def validate_required_text(cls, value: str) -> str:
        if not value:
            raise ValueError("must not be empty")
        return value

    @field_validator("status")
    @classmethod
    def validate_status(cls, value: str) -> str:
        allowed = {"", "unassigned", "active", "paused", "completed"}
        if value not in allowed:
            raise ValueError("status must be one of unassigned, active, paused, completed")
        return value

    @field_validator("triggers")
    @classmethod
    def validate_triggers(cls, value: list[MissionTrigger]) -> list[MissionTrigger]:
        allowed_sources = {"", "connector", "channel", "schedule", "webhook", "platform"}
        for i, trigger in enumerate(value):
            if trigger.source not in allowed_sources:
                raise ValueError(
                    f"trigger[{i}].source must be one of connector, channel, schedule, webhook, platform"
                )
        return value
