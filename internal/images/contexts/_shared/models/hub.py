"""Pydantic models for hub configuration and provenance tracking."""

from typing import Literal

from pydantic import BaseModel, ConfigDict, field_validator


class HubSource(BaseModel):
    model_config = ConfigDict(extra="forbid")

    name: str
    type: Literal["git"] = "git"
    url: str
    branch: str = "main"


class HubConfig(BaseModel):
    model_config = ConfigDict(extra="forbid")

    sources: list[HubSource] = []

    @field_validator("sources", mode="before")
    @classmethod
    def coerce_none_to_list(cls, v: object) -> object:
        return v if v is not None else []


class AgencyConfig(BaseModel):
    model_config = ConfigDict(extra="forbid")

    hub: HubConfig = HubConfig()


class HubInstalledEntry(BaseModel):
    model_config = ConfigDict(extra="forbid")

    component: str
    kind: str
    source: str
    commit_sha: str
    installed_at: str
