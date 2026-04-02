"""Workspace configuration schema."""

from typing import Optional, Literal

from pydantic import BaseModel, ConfigDict, field_validator


class WorkspaceBase(BaseModel):
    model_config = ConfigDict(extra="forbid")
    image: str
    user: str = "agent"
    filesystem: str = "readonly-root"


class WorkspaceProvides(BaseModel):
    model_config = ConfigDict(extra="forbid")
    tools: list[str] = []
    network: str = "mediated"


class WorkspaceResources(BaseModel):
    model_config = ConfigDict(extra="forbid")
    memory: str = "2GB"
    cpu: str = "1.0"
    tmpfs: str = "512MB"


class WorkspaceSecurity(BaseModel):
    model_config = ConfigDict(extra="forbid")
    capabilities: Literal["none"] = "none"
    seccomp: Literal["default-strict", "default"] = "default-strict"
    no_new_privileges: bool = True


class WorkspaceConfig(BaseModel):
    """Schema for workspace.yaml (workspace template)."""

    model_config = ConfigDict(extra="forbid")

    name: str
    version: str = "1.0"
    base: WorkspaceBase
    provides: WorkspaceProvides = WorkspaceProvides()
    resources: WorkspaceResources = WorkspaceResources()
    security: WorkspaceSecurity = WorkspaceSecurity()


class ExtraMount(BaseModel):
    """An additional read-only bind mount for an agent workspace.

    Extra mounts are always read-only (tenet 4: access matches purpose).
    The operator configures these in workspace.yaml.
    """

    model_config = ConfigDict(extra="forbid")

    source: str  # Host path
    target: str  # Container path

    @field_validator("source")
    @classmethod
    def source_must_be_absolute(cls, v: str) -> str:
        if not v.startswith("/"):
            raise ValueError("source must be an absolute path")
        return v

    @field_validator("target")
    @classmethod
    def target_must_be_absolute(cls, v: str) -> str:
        if not v.startswith("/"):
            raise ValueError("target must be an absolute path")
        return v


class AgentWorkspaceConfig(BaseModel):
    """Schema for agent-level workspace.yaml (references a workspace template)."""

    model_config = ConfigDict(extra="forbid")

    version: str = "0.1"
    agent: str
    workspace_ref: str
    project_dir: Optional[str] = None
    extra_mounts: list[ExtraMount] = []
