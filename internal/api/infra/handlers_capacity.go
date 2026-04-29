package infra

import (
	"net/http"
	"path/filepath"

	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
	"github.com/geoffbelknap/agency/internal/orchestrate"
)

func workspaceCapacityFilters() runtimehost.FilterArgs {
	return runtimehost.NewFilterArgs(
		runtimehost.FilterArg("label", "agency.type=workspace"),
		runtimehost.FilterArg("status", "running"),
	)
}

func meeseeksCapacityFilters() runtimehost.FilterArgs {
	return runtimehost.NewFilterArgs(
		runtimehost.FilterArg("label", "agency.type=meeseeks-workspace"),
		runtimehost.FilterArg("status", "running"),
	)
}

func (h *handler) infraCapacity(w http.ResponseWriter, r *http.Request) {
	capPath := filepath.Join(h.deps.Config.Home, "capacity.yaml")
	cfg, err := orchestrate.LoadCapacity(capPath)
	if err != nil {
		writeJSON(w, 503, map[string]string{
			"error": "capacity config not available: " + err.Error(),
		})
		return
	}
	backend, backendConfig := h.capacityRuntimeConfig()
	cfg = orchestrate.ApplyRuntimeCapacityProfile(cfg, backend, backendConfig)

	var runningAgents, runningMeeseeks int

	if runtimehost.IsContainerBackend(backend) && h.deps.Runtime != nil {
		if raw := h.deps.Runtime.RawClient(); raw != nil {
			ctx := r.Context()

			agentContainers, err := raw.ContainerList(ctx, runtimehost.ListOptions{
				Filters: workspaceCapacityFilters(),
			})
			if err == nil {
				runningAgents = len(agentContainers)
			}

			meeseeksContainers, err := raw.ContainerList(ctx, runtimehost.ListOptions{
				Filters: meeseeksCapacityFilters(),
			})
			if err == nil {
				runningMeeseeks = len(meeseeksContainers)
			}
		}
	} else if h.deps.AgentManager != nil {
		agents, err := h.deps.AgentManager.List(r.Context())
		if err == nil {
			for _, agent := range agents {
				if agent.Status == "running" || agent.Status == "unhealthy" || agent.Status == "starting" {
					runningAgents++
				}
			}
		}
	}

	available := cfg.MaxAgents - runningAgents - runningMeeseeks
	if available < 0 {
		available = 0
	}

	writeJSON(w, 200, map[string]interface{}{
		"host_memory_mb":          cfg.HostMemoryMB,
		"host_cpu_cores":          cfg.HostCPUCores,
		"system_reserve_mb":       cfg.SystemReserveMB,
		"infra_overhead_mb":       cfg.InfraOverheadMB,
		"runtime_backend":         cfg.RuntimeBackend,
		"enforcement_mode":        cfg.EnforcementMode,
		"max_agents":              cfg.MaxAgents,
		"max_concurrent_meesks":   cfg.MaxConcurrentMeesks,
		"agent_slot_mb":           cfg.AgentSlotMB,
		"meeseeks_slot_mb":        cfg.MeeseeksSlotMB,
		"network_pool_configured": cfg.NetworkPoolConfigured,
		"running_agents":          runningAgents,
		"running_meeseeks":        runningMeeseeks,
		"available_slots":         available,
	})
}

func (h *handler) capacityRuntimeConfig() (string, map[string]string) {
	if h.deps.Config == nil {
		return runtimehost.BackendDocker, nil
	}
	backend := h.deps.Config.Hub.DeploymentBackend
	if backend == "" {
		backend = runtimehost.BackendDocker
	}
	return backend, h.deps.Config.Hub.DeploymentBackendConfig
}
