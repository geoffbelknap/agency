"""Host profile model -- machine resources and container limits."""

from __future__ import annotations

from pydantic import BaseModel, model_validator


class ComponentLimits(BaseModel):
    """Container resource limits per component, in MB and millicpus."""

    # Per-agent components
    enforcer_mem_mb: int = 32
    enforcer_cpu_milli: int = 500
    gateway_mem_mb: int = 64
    gateway_cpu_milli: int = 500
    workspace_mem_mb: int = 512
    workspace_body_mem_mb: int = 256
    workspace_mem_reservation_mb: int = 256
    workspace_cpu_milli: int = 1000

    # Shared infrastructure
    egress_mem_mb: int = 256
    comms_mem_mb: int = 128
    knowledge_mem_mb: int = 128
    intake_mem_mb: int = 128
    vault_mem_mb: int = 128
    vault_sidecar_mem_mb: int = 64

    # Headroom multipliers (applied to image baselines when available)
    enforcer_headroom: float = 4.0
    gateway_headroom: float = 4.0
    workspace_headroom: float = 16.0
    egress_headroom: float = 4.0

    @property
    def per_agent_mb(self) -> int:
        """Total memory per agent (enforcer + gateway + workspace body)."""
        return self.enforcer_mem_mb + self.gateway_mem_mb + self.workspace_body_mem_mb

    @property
    def shared_infra_mb(self) -> int:
        """Total memory for shared infrastructure (egress + comms + swarm vault)."""
        return (
            self.egress_mem_mb + self.comms_mem_mb
            + self.knowledge_mem_mb + self.intake_mem_mb
            + self.vault_mem_mb + self.vault_sidecar_mem_mb
        )


class HostProfile(BaseModel):
    """Detected host resources and derived capacity limits."""

    cpu_count: int
    total_ram_mb: int
    available_disk_gb: int
    os_reserve_mb: int = 512
    component_limits: ComponentLimits = ComponentLimits()
    max_agents: int = 1
    image_baselines: dict[str, int] = {}

    @model_validator(mode="after")
    def _compute_max_agents(self) -> "HostProfile":
        limits = self.component_limits
        available = self.total_ram_mb - limits.shared_infra_mb - self.os_reserve_mb
        if limits.per_agent_mb > 0:
            computed = max(1, available // limits.per_agent_mb)
        else:
            computed = 1
        self.max_agents = computed
        return self

    def apply_baselines(self) -> None:
        """Recompute component limits from image baselines if available."""
        limits = self.component_limits
        mapping = {
            "enforcer": ("enforcer_mem_mb", limits.enforcer_headroom),
            "workspace": ("workspace_mem_mb", limits.workspace_headroom),
            "egress": ("egress_mem_mb", limits.egress_headroom),
        }
        for image, (field_name, headroom) in mapping.items():
            baseline = self.image_baselines.get(image)
            if baseline and baseline > 0:
                setattr(limits, field_name, int(baseline * headroom))
        self._compute_max_agents()
