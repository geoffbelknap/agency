"""Pydantic models for pack schema — declarative team composition."""

from pydantic import BaseModel, ConfigDict, Field, field_validator, model_validator
from typing import Optional, Literal


class PackAgent(BaseModel):
    model_config = ConfigDict(extra="forbid")

    name: str
    preset: str
    workspace: Optional[str] = None
    role: Literal["standard", "coordinator", "function"] = "standard"
    agent_type: Optional[str] = None
    host: Optional[str] = None
    skills: list[str] = Field(default_factory=list)
    connectors: list[str] = Field(default_factory=list)


class PackChannel(BaseModel):
    model_config = ConfigDict(extra="forbid")

    name: str
    topic: str = ""
    private: bool = False


class PackCredential(BaseModel):
    model_config = ConfigDict(extra="forbid")

    name: str
    description: str = ""
    required: bool = True


class PackMissionAssignment(BaseModel):
    model_config = ConfigDict(extra="forbid")

    mission: str
    agent: str


class PackRequires(BaseModel):
    model_config = ConfigDict(extra="forbid")

    connectors: list[str] = Field(default_factory=list)
    presets: list[str] = Field(default_factory=list)
    services: list[str] = Field(default_factory=list)
    skills: list[str] = Field(default_factory=list)
    workspaces: list[str] = Field(default_factory=list)
    policies: list[str] = Field(default_factory=list)


class PackTeam(BaseModel):
    model_config = ConfigDict(extra="forbid")

    name: str
    agents: list[PackAgent]
    channels: list[PackChannel] = Field(default_factory=list)

    @field_validator("agents")
    @classmethod
    def agents_not_empty(cls, v: list[PackAgent]) -> list[PackAgent]:
        if not v:
            raise ValueError("Pack must define at least one agent")
        return v

    @model_validator(mode="after")
    def no_duplicate_names(self) -> "PackTeam":
        agent_names = [a.name for a in self.agents]
        if len(agent_names) != len(set(agent_names)):
            dupes = [n for n in agent_names if agent_names.count(n) > 1]
            raise ValueError(f"Duplicate agent names: {set(dupes)}")
        channel_names = [c.name for c in self.channels]
        if len(channel_names) != len(set(channel_names)):
            dupes = [n for n in channel_names if channel_names.count(n) > 1]
            raise ValueError(f"Duplicate channel names: {set(dupes)}")
        return self


class PackConfig(BaseModel):
    model_config = ConfigDict(extra="forbid")

    kind: Literal["pack"] = "pack"
    name: str
    version: str = "1.0.0"
    description: str = ""
    author: str = ""
    license: str = ""
    requires: PackRequires = Field(default_factory=PackRequires)
    team: PackTeam
    credentials: list[PackCredential] = Field(default_factory=list)
    policy: Optional[dict] = None
    recommended_connectors: list[str] = Field(default_factory=list)
    mission_assignments: list[PackMissionAssignment] = Field(default_factory=list)
