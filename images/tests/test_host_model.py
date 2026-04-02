"""Tests for host profile model."""

import pytest
from images.models.host import HostProfile, ComponentLimits


def test_host_profile_defaults():
    hp = HostProfile(cpu_count=4, total_ram_mb=16384, available_disk_gb=50)
    assert hp.cpu_count == 4
    assert hp.total_ram_mb == 16384
    assert hp.max_agents >= 1


def test_host_profile_computes_max_agents():
    hp = HostProfile(cpu_count=4, total_ram_mb=16384, available_disk_gb=50)
    assert hp.max_agents > 1


def test_host_profile_small_machine():
    hp = HostProfile(cpu_count=2, total_ram_mb=4096, available_disk_gb=20)
    assert hp.max_agents >= 1


def test_host_profile_serialization(tmp_path):
    import yaml
    hp = HostProfile(cpu_count=4, total_ram_mb=16384, available_disk_gb=50)
    path = tmp_path / "host.yaml"
    path.write_text(yaml.dump(hp.model_dump(), default_flow_style=False))
    loaded = yaml.safe_load(path.read_text())
    hp2 = HostProfile(**loaded)
    assert hp2.max_agents == hp.max_agents
    assert hp2.component_limits == hp.component_limits


def test_component_limits_defaults():
    cl = ComponentLimits()
    assert cl.enforcer_mem_mb > 0
    assert cl.gateway_mem_mb > 0
    assert cl.workspace_mem_mb > 0


def test_host_profile_floor():
    hp = HostProfile(cpu_count=1, total_ram_mb=1024, available_disk_gb=5)
    assert hp.max_agents == 1


def test_host_profile_capacity_math():
    hp = HostProfile(cpu_count=2, total_ram_mb=4096, available_disk_gb=20)
    # Shared: egress 256 + analysis 128 + comms 128 + knowledge 128 + intake 128
    #         + vault 128 + vault-sidecar 64 = 960MB. OS reserve: 512MB. Available: 2624MB.
    # Per-agent (body): 32 (enforcer) + 64 (gateway) + 256 (workspace_body) = 352MB -> 7 agents
    assert hp.max_agents >= 7


def test_apply_baselines():
    hp = HostProfile(cpu_count=4, total_ram_mb=16384, available_disk_gb=50,
                     image_baselines={"enforcer": 30, "workspace": 28})
    hp.apply_baselines()
    assert hp.component_limits.enforcer_mem_mb == 120  # 30 * 4.0
    assert hp.component_limits.workspace_mem_mb == 448  # 28 * 16.0
