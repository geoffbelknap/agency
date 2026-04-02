"""Swarm cluster models — config, host nodes, placement, manifests."""

from __future__ import annotations

from typing import Literal

from pydantic import BaseModel, ConfigDict, Field

from agency_core.models.host import ComponentLimits


class SwarmConfig(BaseModel):
    """Persisted swarm configuration (~/.agency/swarm/config.yaml)."""

    model_config = ConfigDict(extra="forbid")

    role: Literal["manager", "worker"]
    manager_ip: str
    cluster_name: str
    host_name: str | None = None
    join_token_hash: str | None = None
    signing_pubkey: str | None = None
    cross_host_teams: bool = False


class HostNode(BaseModel):
    """A host in the swarm cluster."""

    model_config = ConfigDict(extra="forbid")

    name: str
    overlay_ip: str
    role: Literal["manager", "worker"]
    status: Literal["ready", "draining", "drained", "left", "removed"]
    agent_count: int = 0
    cpu_count: int = 1
    total_ram_mb: int = 2048
    available_disk_gb: int = 10
    agency_version: str = "0.1.0"
    os_reserve_mb: int = 512
    component_limits: ComponentLimits = Field(default_factory=ComponentLimits)

    @property
    def available_ram_mb(self) -> int:
        """RAM available for new agents."""
        used = (
            self.component_limits.shared_infra_mb
            + self.os_reserve_mb
            + self.component_limits.per_agent_mb * self.agent_count
        )
        return max(0, self.total_ram_mb - used)


class PlacementRule(BaseModel):
    """Agent placement preferences for the scheduler."""

    model_config = ConfigDict(extra="forbid")

    host: str | None = None
    require_colocation: bool = True


class StartManifest(BaseModel):
    """Signed start manifest dispatched to a worker for local execution.

    Config fields use dict (not typed models) intentionally: the worker's
    start sequence validates contents against actual Pydantic models when
    processing. Typing here would create import coupling that breaks lazy
    loading. Signature verification ensures integrity in transit.
    """

    model_config = ConfigDict(extra="forbid")

    schema_version: int = 1
    agent_name: str
    agent_config: dict
    constraints: dict
    policy: dict
    workspace_config: dict = Field(default_factory=dict)
    services: dict = Field(default_factory=dict)
    mounts: list[dict] = Field(default_factory=list)
    target_host: str
    signature: str
    issued_at: str | None = None
    vault_token: str | None = None
    manifest_id: str | None = None
