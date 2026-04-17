package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/geoffbelknap/agency/internal/budget"
	"github.com/geoffbelknap/agency/internal/capabilities"
	"github.com/geoffbelknap/agency/internal/credstore"
	"github.com/geoffbelknap/agency/internal/hub"
	"github.com/geoffbelknap/agency/internal/models"
	"github.com/geoffbelknap/agency/internal/routing"
)

func (d *mcpDeps) budgetConfig() models.PlatformBudgetConfig {
	return models.DefaultPlatformBudgetConfig()
}

func (d *mcpDeps) budgetStore() *budget.Store {
	return budget.NewStore(filepath.Join(d.cfg.Home, "budget"))
}

// loadRoutingConfig reads routing.yaml (hub-managed) and routing.local.yaml
// (operator overrides), merging them. Local overlay wins on conflicts.
func loadRoutingConfig(home string) *models.RoutingConfig {
	infraDir := filepath.Join(home, "infrastructure")

	var rc models.RoutingConfig
	if data, err := os.ReadFile(filepath.Join(infraDir, "routing.yaml")); err == nil {
		yaml.Unmarshal(data, &rc)
	}

	if data, err := os.ReadFile(filepath.Join(infraDir, "routing.local.yaml")); err == nil {
		var local models.RoutingConfig
		if yaml.Unmarshal(data, &local) == nil {
			if rc.Providers == nil {
				rc.Providers = local.Providers
			} else {
				for k, v := range local.Providers {
					rc.Providers[k] = v
				}
			}
			if rc.Models == nil {
				rc.Models = local.Models
			} else {
				for k, v := range local.Models {
					rc.Models[k] = v
				}
			}
		}
	}

	return &rc
}

// loadModelCosts extracts pricing from the merged routing config.
func loadModelCosts(home string) map[string]routing.ModelCost {
	rc := loadRoutingConfig(home)
	if rc == nil || len(rc.Models) == 0 {
		return nil
	}
	costs := make(map[string]routing.ModelCost, len(rc.Models))
	for alias, m := range rc.Models {
		costs[alias] = routing.ModelCost{
			CostPerMTokIn:     m.CostPerMTokIn,
			CostPerMTokOut:    m.CostPerMTokOut,
			CostPerMTokCached: m.CostPerMTokCached,
		}
	}
	if len(costs) == 0 {
		return nil
	}
	return costs
}

// nestedStr extracts a string from a nested map: m[key1][key2]...
func nestedStr(m map[string]interface{}, keys ...string) (string, bool) {
	for i, k := range keys {
		v, ok := m[k]
		if !ok {
			return "", false
		}
		if i == len(keys)-1 {
			s, ok := v.(string)
			return s, ok
		}
		m, ok = v.(map[string]interface{})
		if !ok {
			return "", false
		}
	}
	return "", false
}

// regenerateSwapConfig rebuilds credential-swaps.yaml from the credential store,
// merging with legacy hub-generated entries.
func (d *mcpDeps) regenerateSwapConfig() {
	if d.credStore == nil {
		hub.WriteSwapConfig(d.cfg.Home)
		return
	}
	data, err := d.credStore.GenerateSwapConfig()
	if err != nil {
		d.log.Warn("failed to generate swap config from store", "err", err)
		hub.WriteSwapConfig(d.cfg.Home)
		return
	}
	if len(data) == 0 {
		hub.WriteSwapConfig(d.cfg.Home)
		return
	}
	legacyData, legacyErr := hub.GenerateSwapConfig(d.cfg.Home)
	swapPath := filepath.Join(d.cfg.Home, "infrastructure", "credential-swaps.yaml")
	os.MkdirAll(filepath.Dir(swapPath), 0755)
	if legacyErr == nil && len(legacyData) > 0 {
		var legacy hub.SwapConfigFile
		var store hub.SwapConfigFile
		if yaml.Unmarshal(legacyData, &legacy) == nil && yaml.Unmarshal(data, &store) == nil {
			if legacy.Swaps == nil {
				legacy.Swaps = map[string]hub.SwapEntry{}
			}
			for k, v := range store.Swaps {
				legacy.Swaps[k] = v
			}
			if merged, err := yaml.Marshal(legacy); err == nil {
				os.WriteFile(swapPath, merged, 0644)
				return
			}
		}
	}
	os.WriteFile(swapPath, data, 0644)
}

func mcpManagedRuntimeAgents(home string) ([]string, error) {
	agentsDir := filepath.Join(home, "agents")
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	agents := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			agents = append(agents, entry.Name())
		}
	}
	sort.Strings(agents)
	return agents, nil
}

func (d *mcpDeps) runningAgentNames(ctx context.Context) ([]string, error) {
	if d != nil && d.agents != nil {
		agents, err := d.agents.List(ctx)
		if err != nil {
			return nil, err
		}
		running := make([]string, 0, len(agents))
		for _, agent := range agents {
			if agent.Status == "running" {
				running = append(running, agent.Name)
			}
		}
		sort.Strings(running)
		return running, nil
	}
	if d == nil || d.dc == nil {
		return nil, fmt.Errorf("runtime agent listing unavailable")
	}
	agents, err := d.dc.ListAgents(ctx)
	if err != nil {
		return nil, err
	}
	running := make([]string, 0, len(agents))
	for _, agent := range agents {
		if agent.Status == "running" {
			running = append(running, agent.Name)
		}
	}
	sort.Strings(running)
	return running, nil
}

func (d *mcpDeps) reloadAgentEnforcer(ctx context.Context, agentName string) error {
	if d != nil && d.agents != nil && d.agents.Runtime != nil {
		return d.agents.Runtime.ReloadEnforcer(ctx, agentName)
	}
	if d == nil || d.dc == nil {
		return fmt.Errorf("enforcer reload unavailable")
	}
	enforcerName := fmt.Sprintf("agency-%s-enforcer", agentName)
	return d.dc.RawClient().ContainerKill(ctx, enforcerName, "SIGHUP")
}

func (d *mcpDeps) runtimeDoctorSummary(ctx context.Context) (string, bool) {
	backend := mcpConfiguredRuntimeBackend(d)
	if d == nil || d.agents == nil || d.agents.Runtime == nil {
		return fmt.Sprintf("Security guarantees (FAILURES)\n  Runtime supervisor unavailable for backend %s", backend), true
	}
	agents, err := mcpManagedRuntimeAgents(d.cfg.Home)
	if err != nil {
		return fmt.Sprintf("Security guarantees (FAILURES)\n  Cannot enumerate managed agents: %s", err), true
	}
	if len(agents) == 0 {
		return fmt.Sprintf("Security guarantees (ALL PASS)\n  No managed agents to check (backend: %s)", backend), false
	}

	lines := make([]string, 0, len(agents)+1)
	allPassed := true
	for _, agentName := range agents {
		status, err := d.agents.Runtime.Get(ctx, agentName)
		if err != nil {
			allPassed = false
			lines = append(lines, fmt.Sprintf("  [FAIL] %s runtime status unavailable: %s", agentName, err))
			continue
		}
		if err := d.agents.Runtime.Validate(ctx, agentName); err != nil {
			allPassed = false
			lines = append(lines, fmt.Sprintf("  [FAIL] %s runtime validate failed: %s", agentName, err))
			continue
		}
		if status.Healthy {
			lines = append(lines, fmt.Sprintf("  [PASS] %s runtime healthy: phase=%s backend=%s", agentName, status.Phase, status.Backend))
			continue
		}
		allPassed = false
		lines = append(lines, fmt.Sprintf("  [FAIL] %s runtime unhealthy: phase=%s backend=%s", agentName, status.Phase, status.Backend))
	}

	header := "Security guarantees (ALL PASS)"
	if !allPassed {
		header = "Security guarantees (FAILURES)"
	}
	return header + "\n" + strings.Join(lines, "\n"), !allPassed
}

// reloadCapabilitiesForRunningAgents regenerates manifests and signals enforcers
// for all running agents after a capability change.
func (d *mcpDeps) reloadCapabilitiesForRunningAgents(capName string) {
	ctx := context.Background()
	agents, err := d.runningAgentNames(ctx)
	if err != nil {
		d.log.Warn("capability reload: failed to list agents", "err", err)
		return
	}

	reg := capabilities.NewRegistry(d.cfg.Home)
	allCaps := reg.List()
	enabledServices := map[string]capabilities.Entry{}
	for _, cap := range allCaps {
		if cap.Kind == "service" && cap.State != "disabled" {
			enabledServices[cap.Name] = cap
		}
	}

	for _, name := range agents {
		agentDir := filepath.Join(d.cfg.Home, "agents", name)

		if err := d.generateAgentManifest(name); err != nil {
			d.log.Warn("capability reload: failed to generate manifest", "agent", name, "err", err)
			continue
		}

		var manifest struct {
			Services []map[string]interface{} `json:"services"`
		}
		if mdata, err := os.ReadFile(filepath.Join(agentDir, "services-manifest.json")); err == nil {
			json.Unmarshal(mdata, &manifest)
		}

		entries := map[string]string{}
		for _, svcMap := range manifest.Services {
			svcName, _ := svcMap["service"].(string)
			scopedToken, _ := svcMap["scoped_token"].(string)
			envVar, _ := nestedStr(svcMap, "credential", "env_var")
			if scopedToken == "" || envVar == "" {
				continue
			}
			capKeyName := strings.ToUpper(strings.ReplaceAll(svcName, "-", "_")) + "_KEY"
			var realKey string
			if d.credStore != nil {
				if entry, err := d.credStore.Get(capKeyName); err == nil {
					realKey = entry.Value
				}
			}
			if realKey == "" {
				continue
			}
			entries[scopedToken] = realKey
			entries[envVar] = realKey
		}

		if d.credStore != nil && len(entries) > 0 {
			now := time.Now().UTC().Format(time.RFC3339)
			for k, v := range entries {
				_ = d.credStore.Put(credstore.Entry{
					Name:  k,
					Value: v,
					Metadata: credstore.Metadata{
						Kind:      "service",
						Scope:     "platform",
						Protocol:  "api-key",
						Source:    "capability-reload",
						CreatedAt: now,
						RotatedAt: now,
					},
				})
			}
			d.regenerateSwapConfig()
		}

		hostServicesDir := filepath.Join(d.cfg.Home, "services")
		os.MkdirAll(hostServicesDir, 0755)
		for svcName := range enabledServices {
			src := filepath.Join(d.cfg.Home, "registry", "services", svcName+".yaml")
			dst := filepath.Join(hostServicesDir, svcName+".yaml")
			if data, err := os.ReadFile(src); err == nil {
				os.WriteFile(dst, data, 0644)
			}
		}

		if err := d.reloadAgentEnforcer(ctx, name); err != nil {
			d.log.Debug("capability reload: enforcer reload failed", "agent", name, "err", err)
		}

		d.audit.Write(name, "capability_reload", map[string]interface{}{
			"capability": capName,
			"services":   len(manifest.Services),
		})
		d.log.Info("capability reloaded for agent", "agent", name, "capability", capName, "services", len(manifest.Services))
	}
}

// serviceGet makes a GET request to an infra service via its localhost port.
func serviceGet(ctx context.Context, port, path string) ([]byte, error) {
	url := "http://localhost:" + port + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return out, fmt.Errorf("service (port %s) returned %d", port, resp.StatusCode)
	}
	return out, nil
}
