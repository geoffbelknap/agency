package orchestrate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestComputeCapacity_16GB(t *testing.T) {
	cfg := ComputeCapacity(16384, 8, true)
	// reserve = 16384/5 = 3276, infra = 4336, available = 8772, agents = 8772/640 = 13
	if cfg.MaxAgents != 13 {
		t.Fatalf("expected 13 agents, got %d", cfg.MaxAgents)
	}
	if cfg.MaxConcurrentMeesks != 13 {
		t.Fatalf("expected 13 meeseeks, got %d", cfg.MaxConcurrentMeesks)
	}
}

func TestComputeCapacity_16GB_NoEmbeddings(t *testing.T) {
	cfg := ComputeCapacity(16384, 8, false)
	// reserve = 3276, infra = 1264, available = 11844, agents = 11844/640 = 18
	if cfg.MaxAgents != 18 {
		t.Fatalf("expected 18 agents, got %d", cfg.MaxAgents)
	}
}

func TestComputeCapacity_FirecrackerMicroVM(t *testing.T) {
	cfg := ComputeCapacityForRuntime(16384, 8, false, "firecracker", map[string]string{
		"enforcement_mode": "microvm",
	})
	// reserve = 3276, infra = 1264, available = 11844, agents = 11844/(512*2) = 11
	if cfg.AgentSlotMB != 1024 {
		t.Fatalf("expected 1024 MB agent slot, got %d", cfg.AgentSlotMB)
	}
	if cfg.MaxAgents != 11 {
		t.Fatalf("expected 11 agents, got %d", cfg.MaxAgents)
	}
	if cfg.EnforcementMode != "microvm" {
		t.Fatalf("expected microvm enforcement mode, got %q", cfg.EnforcementMode)
	}
}

func TestComputeCapacity_FirecrackerConfiguredMemory(t *testing.T) {
	cfg := ComputeCapacityForRuntime(16384, 8, false, "firecracker", map[string]string{
		"enforcement_mode": "microvm",
		"memory_mib":       "768",
	})
	if cfg.AgentSlotMB != 1536 {
		t.Fatalf("expected 1536 MB agent slot, got %d", cfg.AgentSlotMB)
	}
	if cfg.MaxAgents != 7 {
		t.Fatalf("expected 7 agents, got %d", cfg.MaxAgents)
	}
}

func TestApplyRuntimeCapacityProfile_RecomputesLegacyFile(t *testing.T) {
	cfg := CapacityConfig{
		HostMemoryMB:    16384,
		HostCPUCores:    8,
		SystemReserveMB: 3276,
		InfraOverheadMB: 1264,
		MaxAgents:       18,
		AgentSlotMB:     640,
		MeeseeksSlotMB:  640,
	}
	cfg = ApplyRuntimeCapacityProfile(cfg, "firecracker", map[string]string{"enforcement_mode": "microvm"})
	if cfg.AgentSlotMB != 1024 || cfg.MaxAgents != 11 {
		t.Fatalf("capacity = %#v, want firecracker microvm slot and max", cfg)
	}
}

func TestComputeCapacity_8GB(t *testing.T) {
	cfg := ComputeCapacity(8192, 4, true)
	// reserve = max(8192/5=1638, 2048) = 2048, infra = 4336, available = 1808, agents = 1808/640 = 2
	if cfg.MaxAgents != 2 {
		t.Fatalf("expected 2 agents, got %d", cfg.MaxAgents)
	}
}

func TestComputeCapacity_4GB_TooSmall(t *testing.T) {
	cfg := ComputeCapacity(4096, 2, true)
	// reserve = max(819, 2048) = 2048, infra = 4336, available = max(4096-2048-4336, 0) = 0
	if cfg.MaxAgents != 0 {
		t.Fatalf("expected 0 agents, got %d", cfg.MaxAgents)
	}
}

func TestComputeCapacity_MinReserve(t *testing.T) {
	// For a small host, 1/5 of memory < minReserveMB so reserve should be minReserveMB.
	cfg := ComputeCapacity(8192, 4, false)
	// 8192/5 = 1638 < 2048, so reserve = 2048
	if cfg.SystemReserveMB != 2048 {
		t.Fatalf("expected reserve 2048, got %d", cfg.SystemReserveMB)
	}
}

func TestLoadCapacity_Missing(t *testing.T) {
	_, err := LoadCapacity(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadOrProfileCapacity_MissingCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "capacity.yaml")

	cfg, err := LoadOrProfileCapacity(path, false, "microagent", nil)
	if err != nil {
		t.Fatalf("LoadOrProfileCapacity returned error: %v", err)
	}
	if cfg.MaxAgents <= 0 {
		t.Fatalf("MaxAgents = %d, want > 0", cfg.MaxAgents)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected capacity file to be written: %v", err)
	}
}

func TestSaveAndLoadCapacity(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "capacity.yaml")

	orig := CapacityConfig{
		HostMemoryMB:          16384,
		HostCPUCores:          8,
		SystemReserveMB:       3276,
		InfraOverheadMB:       4336,
		MaxAgents:             13,
		MaxConcurrentMeesks:   13,
		AgentSlotMB:           640,
		MeeseeksSlotMB:        640,
		NetworkPoolConfigured: true,
	}

	if err := SaveCapacity(path, orig); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := LoadCapacity(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if loaded.MaxAgents != orig.MaxAgents {
		t.Errorf("MaxAgents: got %d, want %d", loaded.MaxAgents, orig.MaxAgents)
	}
	if loaded.NetworkPoolConfigured != orig.NetworkPoolConfigured {
		t.Errorf("NetworkPoolConfigured: got %v, want %v", loaded.NetworkPoolConfigured, orig.NetworkPoolConfigured)
	}
}

func TestCheckSlotAvailable_HasRoom(t *testing.T) {
	cfg := CapacityConfig{MaxAgents: 10}
	if err := CheckSlotAvailable(cfg, 3, 2); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCheckSlotAvailable_Full(t *testing.T) {
	cfg := CapacityConfig{MaxAgents: 5}
	if err := CheckSlotAvailable(cfg, 3, 2); err == nil {
		t.Fatal("expected error when full")
	}
}

func TestCheckSlotAvailable_ZeroConfig(t *testing.T) {
	cfg := CapacityConfig{MaxAgents: 0}
	if err := CheckSlotAvailable(cfg, 0, 0); err == nil {
		t.Fatal("expected error for zero config")
	}
}

func TestCheckMeeseeksSlotAvailable_AtCap(t *testing.T) {
	cfg := CapacityConfig{MaxAgents: 10, MaxConcurrentMeesks: 3}
	// 2 agents + 3 meeseeks = 5 total (room), but meeseeks at cap
	if err := CheckMeeseeksSlotAvailable(cfg, 2, 3); err == nil {
		t.Fatal("expected error when meeseeks at cap")
	}
}

func TestCapacityChanged_Same(t *testing.T) {
	a := CapacityConfig{MaxAgents: 10, MaxConcurrentMeesks: 10}
	b := CapacityConfig{MaxAgents: 10, MaxConcurrentMeesks: 10}
	if CapacityChanged(a, b) {
		t.Fatal("expected no change")
	}
}

func TestCapacityChanged_Different(t *testing.T) {
	a := CapacityConfig{MaxAgents: 10, MaxConcurrentMeesks: 10}
	b := CapacityConfig{MaxAgents: 12, MaxConcurrentMeesks: 10}
	if !CapacityChanged(a, b) {
		t.Fatal("expected change")
	}
}
