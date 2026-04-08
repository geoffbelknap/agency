package infra

import (
	"net/http"
	"path/filepath"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"

	"github.com/geoffbelknap/agency/internal/orchestrate"
)

func (h *handler) infraCapacity(w http.ResponseWriter, r *http.Request) {
	capPath := filepath.Join(h.deps.Config.Home, "capacity.yaml")
	cfg, err := orchestrate.LoadCapacity(capPath)
	if err != nil {
		writeJSON(w, 503, map[string]string{
			"error": "capacity config not available: " + err.Error(),
		})
		return
	}

	var runningAgents, runningMeeseeks int

	if h.deps.DC != nil {
		if raw := h.deps.DC.RawClient(); raw != nil {
			ctx := r.Context()

			agentContainers, err := raw.ContainerList(ctx, container.ListOptions{
				Filters: filters.NewArgs(
					filters.Arg("label", "agency.role=workspace"),
					filters.Arg("status", "running"),
				),
			})
			if err == nil {
				runningAgents = len(agentContainers)
			}

			meeseeksContainers, err := raw.ContainerList(ctx, container.ListOptions{
				Filters: filters.NewArgs(
					filters.Arg("label", "agency.role=meeseeks"),
					filters.Arg("status", "running"),
				),
			})
			if err == nil {
				runningMeeseeks = len(meeseeksContainers)
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
