// agency-gateway/internal/models/host.go
package models

// ComponentLimits defines container resource limits per component.
type ComponentLimits struct {
	// Per-agent components
	EnforcerMemMB             int `yaml:"enforcer_mem_mb" default:"32"`
	EnforcerCPUMilli          int `yaml:"enforcer_cpu_milli" default:"500"`
	WorkspaceMemMB            int `yaml:"workspace_mem_mb" default:"512"`
	WorkspaceBodyMemMB        int `yaml:"workspace_body_mem_mb" default:"256"`
	WorkspaceMemReservationMB int `yaml:"workspace_mem_reservation_mb" default:"256"`
	WorkspaceCPUMilli         int `yaml:"workspace_cpu_milli" default:"1000"`

	// Shared infrastructure
	EgressMemMB    int `yaml:"egress_mem_mb" default:"256"`
	CommsMemMB     int `yaml:"comms_mem_mb" default:"128"`
	KnowledgeMemMB int `yaml:"knowledge_mem_mb" default:"128"`
	IntakeMemMB    int `yaml:"intake_mem_mb" default:"128"`

	// Headroom multipliers
	EnforcerHeadroom  float64 `yaml:"enforcer_headroom" default:"4.0"`
	WorkspaceHeadroom float64 `yaml:"workspace_headroom" default:"16.0"`
	EgressHeadroom    float64 `yaml:"egress_headroom" default:"4.0"`
}

// PerAgentMB returns total memory per agent.
func (c *ComponentLimits) PerAgentMB() int {
	return c.EnforcerMemMB + c.WorkspaceBodyMemMB
}

// SharedInfraMB returns total memory for shared infrastructure.
func (c *ComponentLimits) SharedInfraMB() int {
	return c.EgressMemMB + c.CommsMemMB +
		c.KnowledgeMemMB + c.IntakeMemMB
}

// HostProfile holds detected host resources and derived capacity limits.
type HostProfile struct {
	CPUCount        int            `yaml:"cpu_count" validate:"required"`
	TotalRAMMB      int            `yaml:"total_ram_mb" validate:"required"`
	AvailableDiskGB int            `yaml:"available_disk_gb" validate:"required"`
	OSReserveMB     int            `yaml:"os_reserve_mb" default:"512"`
	ComponentLimits ComponentLimits `yaml:"component_limits"`
	MaxAgents       int            `yaml:"max_agents"`
	ImageBaselines  map[string]int `yaml:"image_baselines"`
}

// Validate computes max_agents from host resources.
func (h *HostProfile) Validate() error {
	h.ComputeMaxAgents()
	return nil
}

// ComputeMaxAgents calculates max agents from available RAM.
func (h *HostProfile) ComputeMaxAgents() {
	available := h.TotalRAMMB - h.ComponentLimits.SharedInfraMB() - h.OSReserveMB
	perAgent := h.ComponentLimits.PerAgentMB()
	if perAgent > 0 {
		h.MaxAgents = available / perAgent
		if h.MaxAgents < 1 {
			h.MaxAgents = 1
		}
	} else {
		h.MaxAgents = 1
	}
}

// ApplyBaselines recomputes limits from image baselines.
func (h *HostProfile) ApplyBaselines() {
	mapping := map[string]struct {
		field    *int
		headroom float64
	}{
		"enforcer":  {&h.ComponentLimits.EnforcerMemMB, h.ComponentLimits.EnforcerHeadroom},
		"workspace": {&h.ComponentLimits.WorkspaceMemMB, h.ComponentLimits.WorkspaceHeadroom},
		"egress":         {&h.ComponentLimits.EgressMemMB, h.ComponentLimits.EgressHeadroom},
	}
	for image, m := range mapping {
		if baseline, ok := h.ImageBaselines[image]; ok && baseline > 0 {
			*m.field = int(float64(baseline) * m.headroom)
		}
	}
	h.ComputeMaxAgents()
}
