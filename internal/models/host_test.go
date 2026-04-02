// agency-gateway/internal/models/host_test.go
package models

import (
	"testing"
)

func TestComponentLimits_PerAgentMB(t *testing.T) {
	// Default values: EnforcerMemMB=32, WorkspaceBodyMemMB=256
	c := ComponentLimits{
		EnforcerMemMB:      32,
		WorkspaceBodyMemMB: 256,
	}
	got := c.PerAgentMB()
	want := 288
	if got != want {
		t.Errorf("PerAgentMB() = %d, want %d", got, want)
	}
}

func TestComponentLimits_SharedInfraMB(t *testing.T) {
	// Default values: egress=256, comms=128, knowledge=128, intake=128
	c := ComponentLimits{
		EgressMemMB:    256,
		CommsMemMB:     128,
		KnowledgeMemMB: 128,
		IntakeMemMB:    128,
	}
	got := c.SharedInfraMB()
	want := 640
	if got != want {
		t.Errorf("SharedInfraMB() = %d, want %d", got, want)
	}
}

func TestHostProfile_Validate_ComputesMaxAgents(t *testing.T) {
	// available = 4096 - 640 - 512 = 2944
	// max_agents = 2944 / 288 = 10
	h := HostProfile{
		CPUCount:        4,
		TotalRAMMB:      4096,
		AvailableDiskGB: 50,
		OSReserveMB:     512,
		ComponentLimits: ComponentLimits{
			EnforcerMemMB:      32,
			WorkspaceBodyMemMB: 256,
			EgressMemMB:        256,
			CommsMemMB:         128,
			KnowledgeMemMB:     128,
			IntakeMemMB:        128,
		},
	}
	if err := h.Validate(); err != nil {
		t.Fatalf("Validate() returned error: %v", err)
	}
	want := 10
	if h.MaxAgents != want {
		t.Errorf("MaxAgents = %d, want %d", h.MaxAgents, want)
	}
}

func TestHostProfile_Validate_SmallRAM(t *testing.T) {
	// Small RAM: available goes negative -> max_agents floors to 1
	h := HostProfile{
		CPUCount:        1,
		TotalRAMMB:      1024,
		AvailableDiskGB: 20,
		OSReserveMB:     512,
		ComponentLimits: ComponentLimits{
			EnforcerMemMB:      32,
			WorkspaceBodyMemMB: 256,
			EgressMemMB:        256,
			CommsMemMB:         128,
			KnowledgeMemMB:     128,
			IntakeMemMB:        128,
		},
	}
	if err := h.Validate(); err != nil {
		t.Fatalf("Validate() returned error: %v", err)
	}
	if h.MaxAgents != 1 {
		t.Errorf("MaxAgents = %d, want 1 (floor)", h.MaxAgents)
	}
}

func TestHostProfile_ApplyBaselines(t *testing.T) {
	h := HostProfile{
		CPUCount:        4,
		TotalRAMMB:      8192,
		AvailableDiskGB: 100,
		OSReserveMB:     512,
		ComponentLimits: ComponentLimits{
			EnforcerMemMB:      32,
			EnforcerHeadroom:   4.0,
			WorkspaceMemMB:     512,
			WorkspaceBodyMemMB: 256,
			WorkspaceHeadroom:  16.0,
			EgressMemMB:        256,
			EgressHeadroom:     4.0,
			CommsMemMB:         128,
			KnowledgeMemMB:     128,
			IntakeMemMB:        128,
		},
		ImageBaselines: map[string]int{
			"enforcer": 50,
			"egress":   100,
		},
	}

	h.ApplyBaselines()

	// enforcer baseline=50, headroom=4.0 -> 200
	if h.ComponentLimits.EnforcerMemMB != 200 {
		t.Errorf("EnforcerMemMB = %d, want 200", h.ComponentLimits.EnforcerMemMB)
	}
	// egress baseline=100, headroom=4.0 -> 400
	if h.ComponentLimits.EgressMemMB != 400 {
		t.Errorf("EgressMemMB = %d, want 400", h.ComponentLimits.EgressMemMB)
	}
	// MaxAgents must have been recomputed (non-zero)
	if h.MaxAgents < 1 {
		t.Errorf("MaxAgents = %d, want >= 1", h.MaxAgents)
	}
}

func TestHostProfile_ApplyBaselines_ZeroBaselineIgnored(t *testing.T) {
	h := HostProfile{
		CPUCount:        2,
		TotalRAMMB:      4096,
		AvailableDiskGB: 50,
		OSReserveMB:     512,
		ComponentLimits: ComponentLimits{
			EnforcerMemMB:      32,
			EnforcerHeadroom:   4.0,
			WorkspaceMemMB:     512,
			WorkspaceBodyMemMB: 256,
			WorkspaceHeadroom:  16.0,
			EgressMemMB:        256,
			EgressHeadroom:     4.0,
			CommsMemMB:         128,
			KnowledgeMemMB:     128,
			IntakeMemMB:        128,
		},
		ImageBaselines: map[string]int{
			"enforcer": 0, // zero baseline must be ignored
		},
	}

	h.ApplyBaselines()

	// zero baseline must leave the field unchanged
	if h.ComponentLimits.EnforcerMemMB != 32 {
		t.Errorf("EnforcerMemMB = %d, want 32 (zero baseline ignored)", h.ComponentLimits.EnforcerMemMB)
	}
}
