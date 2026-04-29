package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"

	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/egresspolicy"
	"github.com/geoffbelknap/agency/internal/hostadapter"
	hostruntimebackend "github.com/geoffbelknap/agency/internal/hostadapter/runtimebackend"
	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
	"github.com/geoffbelknap/agency/internal/knowledge"
	"github.com/geoffbelknap/agency/internal/logs"
	"github.com/geoffbelknap/agency/internal/orchestrate"
	runtimecontract "github.com/geoffbelknap/agency/internal/runtime/contract"
	agencysecurity "github.com/geoffbelknap/agency/internal/security"
)

type doctorCheckResult = agencysecurity.Finding

type doctorScopeInfo struct {
	Agent    string   `json:"agent"`
	Service  string   `json:"service"`
	Required []string `json:"required,omitempty"`
	Optional []string `json:"optional,omitempty"`
}

type doctorReport struct {
	AllPassed       bool                `json:"all_passed"`
	Backend         string              `json:"backend,omitempty"`
	BackendEndpoint string              `json:"backend_endpoint,omitempty"`
	BackendMode     string              `json:"backend_mode,omitempty"`
	TestedAgents    []string            `json:"tested_agents"`
	Checks          []doctorCheckResult `json:"checks"`
	RuntimeChecks   []doctorCheckResult `json:"runtime_checks,omitempty"`
	BackendChecks   []doctorCheckResult `json:"backend_checks,omitempty"`
	Scopes          []doctorScopeInfo   `json:"scopes,omitempty"`
	Unscoped        []string            `json:"unscoped_agents,omitempty"`
}

type checkResult = doctorCheckResult
type scopeInfo = doctorScopeInfo

var appleContainerStatus = runtimehost.AppleContainerStatus
var appleContainerHelperStatus = runtimehost.AppleContainerHelperStatus
var appleContainerWaitHelperStatus = runtimehost.AppleContainerWaitHelperStatus

func backendConnectionDetails(cfg *config.Config) (string, string) {
	if cfg == nil {
		return "", ""
	}
	backend := configuredRuntimeBackend(cfg)
	if !runtimehost.IsContainerBackend(backend) {
		return "", ""
	}
	return runtimehost.ResolvedBackendEndpoint(backend, cfg.Hub.DeploymentBackendConfig),
		runtimehost.ResolvedBackendMode(backend, cfg.Hub.DeploymentBackendConfig)
}

func configuredRuntimeBackend(cfg *config.Config) string {
	if cfg != nil && strings.TrimSpace(cfg.Hub.DeploymentBackend) != "" {
		return strings.TrimSpace(cfg.Hub.DeploymentBackend)
	}
	if goruntime.GOOS == "darwin" {
		return hostruntimebackend.BackendAppleVFMicroVM
	}
	return hostruntimebackend.BackendFirecracker
}

func configuredRuntimeBackendConfig(cfg *config.Config) map[string]string {
	if cfg == nil {
		return nil
	}
	return cfg.Hub.DeploymentBackendConfig
}

func isBackendSpecificDoctorCheck(name, backend string) bool {
	switch backend {
	case runtimehost.BackendDocker, runtimehost.BackendPodman, runtimehost.BackendContainerd, runtimehost.BackendAppleContainer:
		// Prefix handling is kept only as a compatibility fallback for older
		// checks. New Doctor checks should use backend-neutral names plus
		// scope/backend metadata.
		return name == "docker_connectivity" ||
			strings.HasPrefix(name, "docker_") ||
			strings.HasPrefix(name, backend+"_")
	default:
		return false
	}
}

func splitDoctorChecks(checks []doctorCheckResult, backend string) ([]doctorCheckResult, []doctorCheckResult) {
	var runtimeChecks []doctorCheckResult
	var backendChecks []doctorCheckResult
	for _, check := range checks {
		if check.Scope == "backend" || strings.TrimSpace(check.Backend) != "" || isBackendSpecificDoctorCheck(check.Name, backend) {
			backendChecks = append(backendChecks, check)
			continue
		}
		runtimeChecks = append(runtimeChecks, check)
	}
	return runtimeChecks, backendChecks
}

func appendBackendDiagnosticChecks(report *doctorReport, checks []hostadapter.DiagnosticCheck) {
	for _, check := range checks {
		if check.Status != string(agencysecurity.FindingPass) {
			report.AllPassed = false
		}
		report.Checks = append(report.Checks, doctorCheckResult{
			Name:    check.Name,
			Agent:   check.Agent,
			Scope:   check.Scope,
			Backend: check.Backend,
			Status:  agencysecurity.FindingStatus(check.Status),
			Detail:  check.Detail,
			Fix:     check.Fix,
		})
	}
}

func (h *handler) backendAdapter() hostadapter.Adapter {
	if h.deps.Host != nil {
		return h.deps.Host
	}
	backend := configuredRuntimeBackend(h.deps.Config)
	if runtimehost.IsContainerBackend(backend) && h.deps.Runtime != nil {
		return hostadapter.NewAdapter(backend, h.deps.Runtime, h.deps.Logger)
	}
	return nil
}

func (h *handler) backendRunningAgents(ctx context.Context) ([]string, error) {
	adapter := h.backendAdapter()
	if adapter == nil {
		return nil, fmt.Errorf("runtime backend adapter is unavailable")
	}
	return adapter.ListRunningAgents(ctx)
}

func (h *handler) backendRuntimeChecks(ctx context.Context, runningAgents []string) []hostadapter.DiagnosticCheck {
	adapter := h.backendAdapter()
	if adapter != nil {
		return adapter.RuntimeDiagnostics(ctx, runningAgents)
	}
	return nil
}

func (h *handler) backendDiagnosticChecks(ctx context.Context, runningAgents []string) []hostadapter.DiagnosticCheck {
	adapter := h.backendAdapter()
	if adapter != nil {
		return adapter.BackendDiagnostics(ctx, runningAgents)
	}
	return nil
}

func (h *handler) adminDoctorAppleContainer(ctx context.Context, report doctorReport) doctorReport {
	backendConfig := map[string]string(nil)
	if h.deps.Config != nil {
		backendConfig = h.deps.Config.Hub.DeploymentBackendConfig
	}
	if err := appleContainerStatus(ctx, backendConfig); err != nil {
		report.AllPassed = false
		report.Checks = append(report.Checks, doctorCheckResult{
			Name:    "apple_container_service",
			Scope:   "backend",
			Backend: runtimehost.BackendAppleContainer,
			Status:  agencysecurity.FindingFail,
			Detail:  err.Error(),
			Fix:     "Install and start Apple container, or select a different deployment backend.",
		})
	} else {
		report.Checks = append(report.Checks, doctorCheckResult{
			Name:    "apple_container_service",
			Scope:   "backend",
			Backend: runtimehost.BackendAppleContainer,
			Status:  agencysecurity.FindingPass,
			Detail:  "`container system status` succeeded",
		})
	}
	health, helperErr := appleContainerHelperStatus(ctx, backendConfig)
	if helperErr != nil {
		report.Checks = append(report.Checks, doctorCheckResult{
			Name:    "apple_container_helper",
			Scope:   "backend",
			Backend: runtimehost.BackendAppleContainer,
			Status:  agencysecurity.FindingWarn,
			Detail:  helperErr.Error(),
			Fix:     "Build agency-apple-container-helper and set hub.deployment_backend_config.helper_binary or AGENCY_APPLE_CONTAINER_HELPER_BIN.",
		})
	} else {
		report.Checks = append(report.Checks, doctorCheckResult{
			Name:    "apple_container_helper",
			Scope:   "backend",
			Backend: runtimehost.BackendAppleContainer,
			Status:  agencysecurity.FindingPass,
			Detail:  "Apple container helper health check succeeded",
		})
	}
	waitHealth, waitHelperErr := appleContainerWaitHelperStatus(ctx, backendConfig)
	if waitHelperErr != nil {
		report.Checks = append(report.Checks, doctorCheckResult{
			Name:    "apple_container_wait_helper",
			Scope:   "backend",
			Backend: runtimehost.BackendAppleContainer,
			Status:  agencysecurity.FindingWarn,
			Detail:  waitHelperErr.Error(),
			Fix:     "Build agency-apple-container-wait-helper and set hub.deployment_backend_config.wait_helper_binary or AGENCY_APPLE_CONTAINER_WAIT_HELPER_BIN.",
		})
	} else {
		report.Checks = append(report.Checks, doctorCheckResult{
			Name:    "apple_container_wait_helper",
			Scope:   "backend",
			Backend: runtimehost.BackendAppleContainer,
			Status:  agencysecurity.FindingPass,
			Detail:  "Apple container wait helper health check succeeded",
		})
	}
	status := agencysecurity.FindingWarn
	detail := "Apple container helper is available but does not yet emit lifecycle exit events."
	if strings.TrimSpace(health.EventSupport) != "" && health.EventSupport != "none" {
		status = agencysecurity.FindingPass
		detail = "Apple container helper reports lifecycle event support: " + health.EventSupport
	} else if waitHelperErr == nil && strings.TrimSpace(waitHealth.EventSupport) != "" && waitHealth.EventSupport != "none" {
		status = agencysecurity.FindingPass
		detail = "Apple container wait helper reports lifecycle event support: " + waitHealth.EventSupport
	} else if waitHelperErr != nil {
		detail = "Apple container lifecycle exit events require the wait helper: " + waitHelperErr.Error()
	}
	report.Checks = append(report.Checks, doctorCheckResult{
		Name:    "apple_container_helper_events",
		Scope:   "backend",
		Backend: runtimehost.BackendAppleContainer,
		Status:  status,
		Detail:  detail,
	})
	report.RuntimeChecks, report.BackendChecks = splitDoctorChecks(report.Checks, report.Backend)
	return report
}

func isSyntheticReadinessAgent(name string) bool {
	name = strings.TrimSpace(strings.ToLower(name))
	return strings.HasPrefix(name, "podman-readiness-") ||
		strings.HasPrefix(name, "containerd-readiness-") ||
		strings.HasPrefix(name, "docker-readiness-")
}

func managedRuntimeAgents(home string) ([]string, error) {
	agentsDir := filepath.Join(home, "agents")
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var agents []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		manifestPath := filepath.Join(agentsDir, entry.Name(), "runtime", "manifest.yaml")
		if _, err := os.Stat(manifestPath); err == nil {
			agents = append(agents, entry.Name())
		}
	}
	return agents, nil
}

func (h *handler) adminDoctorRuntimeContract(ctx context.Context) doctorReport {
	endpoint, mode := backendConnectionDetails(h.deps.Config)
	report := doctorReport{AllPassed: true, Backend: configuredRuntimeBackend(h.deps.Config), BackendEndpoint: endpoint, BackendMode: mode}
	if h.deps.AgentManager == nil || h.deps.AgentManager.Runtime == nil {
		report.AllPassed = false
		report.Checks = append(report.Checks, doctorCheckResult{
			Name:   "runtime_supervisor",
			Status: "fail",
			Detail: "Runtime supervisor is not initialized",
		})
		report.RuntimeChecks, report.BackendChecks = splitDoctorChecks(report.Checks, report.Backend)
		return report
	}
	if err := h.deps.AgentManager.Runtime.RuntimeAvailable(ctx); err != nil {
		report.AllPassed = false
		report.Checks = append(report.Checks, doctorCheckResult{
			Name:   "runtime_backend_available",
			Status: "fail",
			Detail: err.Error(),
		})
		report.RuntimeChecks, report.BackendChecks = splitDoctorChecks(report.Checks, report.Backend)
		return report
	}
	report.Checks = append(report.Checks, doctorCheckResult{
		Name:   "runtime_backend_available",
		Status: "pass",
		Detail: fmt.Sprintf("Runtime backend %q is available", report.Backend),
	})

	home := ""
	if h.deps.Config != nil {
		home = h.deps.Config.Home
	}
	if home == "" && h.deps.AgentManager != nil {
		home = h.deps.AgentManager.Home
	}
	agents, err := managedRuntimeAgents(home)
	if err != nil {
		report.AllPassed = false
		report.Checks = append(report.Checks, doctorCheckResult{
			Name:   "runtime_agent_discovery",
			Status: "fail",
			Detail: err.Error(),
		})
		report.RuntimeChecks, report.BackendChecks = splitDoctorChecks(report.Checks, report.Backend)
		return report
	}
	report.TestedAgents = agents
	if len(agents) == 0 {
		report.Checks = append(report.Checks, doctorCheckResult{
			Name:   "no_running_agents",
			Status: "pass",
			Detail: "No managed runtime manifests to check",
		})
		report.RuntimeChecks, report.BackendChecks = splitDoctorChecks(report.Checks, report.Backend)
		return report
	}
	for _, agentName := range agents {
		if _, err := h.deps.AgentManager.Runtime.Manifest(agentName); err != nil {
			report.AllPassed = false
			report.Checks = append(report.Checks, doctorCheckResult{
				Name:   "runtime_manifest",
				Agent:  agentName,
				Status: "fail",
				Detail: err.Error(),
			})
			continue
		}
		report.Checks = append(report.Checks, doctorCheckResult{
			Name:   "runtime_manifest",
			Agent:  agentName,
			Status: "pass",
			Detail: "Persisted runtime manifest is present",
		})
		status, err := h.deps.AgentManager.Runtime.Get(ctx, agentName)
		if err != nil {
			report.AllPassed = false
			report.Checks = append(report.Checks, doctorCheckResult{
				Name:   "runtime_status",
				Agent:  agentName,
				Status: "fail",
				Detail: err.Error(),
			})
		} else {
			report.Checks = append(report.Checks, doctorCheckResult{
				Name:   "runtime_status",
				Agent:  agentName,
				Status: "pass",
				Detail: fmt.Sprintf("Runtime phase=%s healthy=%t backend=%s", status.Phase, status.Healthy, status.Backend),
			})
		}
		if err := h.deps.AgentManager.Runtime.Validate(ctx, agentName); err != nil {
			report.AllPassed = false
			report.Checks = append(report.Checks, doctorCheckResult{
				Name:   "runtime_validate",
				Agent:  agentName,
				Status: "fail",
				Detail: err.Error(),
			})
			continue
		}
		report.Checks = append(report.Checks, doctorCheckResult{
			Name:   "runtime_validate",
			Agent:  agentName,
			Status: "pass",
			Detail: "Runtime contract validates cleanly",
		})
	}
	report.RuntimeChecks, report.BackendChecks = splitDoctorChecks(report.Checks, report.Backend)
	return report
}

// ── Admin ───────────────────────────────────────────────────────────────────

func (h *handler) adminDoctor(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	endpoint, mode := backendConnectionDetails(h.deps.Config)
	report := doctorReport{AllPassed: true, Backend: configuredRuntimeBackend(h.deps.Config), BackendEndpoint: endpoint, BackendMode: mode}
	if report.Backend == hostruntimebackend.BackendFirecracker {
		writeJSON(w, 200, h.adminDoctorFirecracker(ctx))
		return
	}
	if report.Backend == hostruntimebackend.BackendAppleVFMicroVM {
		writeJSON(w, 200, h.adminDoctorAppleVF(ctx))
		return
	}
	if !runtimehost.IsContainerBackend(report.Backend) {
		writeJSON(w, 200, h.adminDoctorRuntimeContract(ctx))
		return
	}
	if runtimehost.NormalizeContainerBackend(report.Backend) == runtimehost.BackendAppleContainer {
		writeJSON(w, 200, h.adminDoctorAppleContainer(ctx, report))
		return
	}

	agents, err := h.backendRunningAgents(ctx)
	if err != nil {
		report.AllPassed = false
		report.Checks = append(report.Checks, doctorCheckResult{
			Name: "connectivity", Scope: "backend", Backend: report.Backend, Status: "fail",
			Detail: "Cannot list runtime containers: " + err.Error(),
		})
		report.RuntimeChecks, report.BackendChecks = splitDoctorChecks(report.Checks, report.Backend)
		writeJSON(w, 200, report)
		return
	}
	report.TestedAgents = agents

	if len(agents) == 0 {
		report.Checks = append(report.Checks, doctorCheckResult{
			Name: "no_running_agents", Status: "pass",
			Detail: "No running agents to check",
		})
		appendBackendDiagnosticChecks(&report, h.backendDiagnosticChecks(ctx, nil))
		report.RuntimeChecks, report.BackendChecks = splitDoctorChecks(report.Checks, report.Backend)
		writeJSON(w, 200, report)
		return
	}

	appendBackendDiagnosticChecks(&report, h.backendRuntimeChecks(ctx, agents))
	appendBackendDiagnosticChecks(&report, h.backendDiagnosticChecks(ctx, agents))

	// Scope audit: collect per-agent scope declarations from presets
	agentsDir := filepath.Join(h.deps.Config.Home, "agents")
	agentEntries, _ := os.ReadDir(agentsDir)
	for _, ae := range agentEntries {
		if !ae.IsDir() {
			continue
		}
		agentName := ae.Name()
		if isSyntheticReadinessAgent(agentName) {
			continue
		}
		// Check if agent has any granted services
		var constraints map[string]interface{}
		if data, err := os.ReadFile(filepath.Join(agentsDir, agentName, "constraints.yaml")); err == nil {
			yaml.Unmarshal(data, &constraints)
		}
		grantedList, _ := constraints["granted_capabilities"].([]interface{})
		if len(grantedList) == 0 {
			continue
		}

		scopes := h.loadPresetScopes(agentName)
		if len(scopes) == 0 {
			report.Unscoped = append(report.Unscoped, agentName)
			continue
		}

		// Get required vs optional from preset
		var agentCfg struct {
			Preset string `yaml:"preset"`
		}
		if data, err := os.ReadFile(filepath.Join(agentsDir, agentName, "agent.yaml")); err == nil {
			yaml.Unmarshal(data, &agentCfg)
		}
		presetPaths := []string{
			filepath.Join(h.deps.Config.Home, "hub-cache", "default", "presets", agentCfg.Preset, "preset.yaml"),
			filepath.Join(h.deps.Config.Home, "presets", agentCfg.Preset+".yaml"),
		}
		for _, pp := range presetPaths {
			data, err := os.ReadFile(pp)
			if err != nil {
				continue
			}
			var preset struct {
				Requires struct {
					Credentials []struct {
						GrantName string `yaml:"grant_name"`
						Scopes    struct {
							Required []string `yaml:"required"`
							Optional []string `yaml:"optional"`
						} `yaml:"scopes"`
					} `yaml:"credentials"`
				} `yaml:"requires"`
			}
			if yaml.Unmarshal(data, &preset) == nil {
				for _, cred := range preset.Requires.Credentials {
					if len(cred.Scopes.Required) > 0 || len(cred.Scopes.Optional) > 0 {
						report.Scopes = append(report.Scopes, scopeInfo{
							Agent:    agentName,
							Service:  cred.GrantName,
							Required: cred.Scopes.Required,
							Optional: cred.Scopes.Optional,
						})
					}
				}
			}
			break
		}
	}

	// Host capacity check (ASK Tenet 8: operations are bounded)
	capPath := filepath.Join(h.deps.Config.Home, "capacity.yaml")
	capCfg, capErr := orchestrate.LoadCapacity(capPath)
	if capErr != nil {
		report.AllPassed = false
		report.Checks = append(report.Checks, checkResult{
			Name: "host_capacity", Status: "fail",
			Detail: "Capacity config not found — run agency setup",
		})
	} else {
		capCfg = orchestrate.ApplyRuntimeCapacityProfile(capCfg, configuredRuntimeBackend(h.deps.Config), configuredRuntimeBackendConfig(h.deps.Config))
		agentCount := len(agents)
		meeseeksCount := 0
		if h.deps.Host != nil {
			if count, err := h.deps.Host.CountRunningMeeseeks(ctx); err == nil {
				meeseeksCount = count
			}
		}

		total := agentCount + meeseeksCount
		pct := 0
		if capCfg.MaxAgents > 0 {
			pct = (total * 100) / capCfg.MaxAgents
		}

		if pct >= 80 {
			report.Checks = append(report.Checks, checkResult{
				Name: "host_capacity", Status: "warn",
				Detail: fmt.Sprintf("Approaching capacity: %d/%d slots used (%d%%)", total, capCfg.MaxAgents, pct),
			})
		} else {
			report.Checks = append(report.Checks, checkResult{
				Name: "host_capacity", Status: "pass",
				Detail: fmt.Sprintf("%d/%d slots used (%d available)", total, capCfg.MaxAgents, capCfg.MaxAgents-total),
			})
		}
	}

	report.RuntimeChecks, report.BackendChecks = splitDoctorChecks(report.Checks, report.Backend)
	writeJSON(w, 200, report)
}

func (h *handler) adminDestroy(w http.ResponseWriter, r *http.Request) {
	backend := configuredRuntimeBackend(h.deps.Config)
	if !runtimehost.IsContainerBackend(backend) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error":   fmt.Sprintf("admin destroy is only available for container backends (current: %s)", backend),
			"backend": backend,
		})
		return
	}
	// Stop all agents
	agents, _ := h.deps.AgentManager.List(r.Context())
	for _, a := range agents {
		h.deps.AgentManager.StopContainers(r.Context(), a.Name)
		h.deps.AgentManager.Delete(r.Context(), a.Name)
	}

	// Tear down infrastructure
	if h.deps.Host != nil {
		_ = h.deps.Host.TeardownInfrastructure(r.Context(), h.deps.Infra)
	} else if h.deps.Infra != nil {
		h.deps.Infra.Teardown(r.Context())
	}

	// Prune dangling agency images
	if h.deps.Host != nil {
		_, _, _ = h.deps.Host.PruneDanglingAgencyImages(r.Context())
	} else if h.deps.Runtime != nil {
		_, _ = h.pruneDanglingImages(r.Context())
	}

	h.deps.Logger.Info("admin destroy completed")
	writeJSON(w, 200, map[string]string{"status": "destroyed"})
}

func (h *handler) adminPruneImages(w http.ResponseWriter, r *http.Request) {
	backend := configuredRuntimeBackend(h.deps.Config)
	if !runtimehost.IsContainerBackend(backend) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error":   fmt.Sprintf("image pruning is only available for container backends (current: %s)", backend),
			"backend": backend,
		})
		return
	}
	pruned, skipped := h.pruneDanglingImages(r.Context())
	writeJSON(w, 200, map[string]interface{}{
		"status":  "ok",
		"pruned":  pruned,
		"skipped": skipped,
	})
}

// pruneDanglingImages removes true dangling untagged Agency build images.
func (h *handler) pruneDanglingImages(ctx context.Context) (pruned, skipped int) {
	if h.deps.Host != nil {
		pruned, skipped, err := h.deps.Host.PruneDanglingAgencyImages(ctx)
		if err != nil && h.deps.Logger != nil {
			h.deps.Logger.Warn("prune images: list failed", "err", err)
		}
		return pruned, skipped
	}
	if h.deps.Runtime == nil {
		if h.deps.Logger != nil {
			h.deps.Logger.Warn("prune images: container backend client unavailable")
		}
		return 0, 0
	}
	imgs, err := h.deps.Runtime.ListDanglingAgencyImages(ctx)
	if err != nil {
		h.deps.Logger.Warn("prune images: list failed", "err", err)
		return 0, 0
	}
	for _, img := range imgs {
		if _, err := h.deps.Runtime.RemoveImage(ctx, img.ID); err != nil {
			h.deps.Logger.Debug("prune untagged image skip", "id", img.ID, "err", err)
			skipped++
		} else {
			h.deps.Logger.Info("pruned dangling image", "id", img.ID)
			pruned++
		}
	}
	return pruned, skipped
}

func (h *handler) adminTrust(w http.ResponseWriter, r *http.Request) {
	// Accept flat format: {"action": "show", "agent": "foo", ...}
	// Also accept legacy nested format: {"action": "show", "args": {"agent": "foo"}}
	var body struct {
		Action      string            `json:"action"`
		Agent       string            `json:"agent"`
		Level       string            `json:"level"`
		SignalType  string            `json:"signal_type"`
		Description string            `json:"description"`
		Args        map[string]string `json:"args"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}

	// Resolve agent: flat field takes precedence over nested args
	agent := body.Agent
	if agent == "" {
		agent = body.Args["agent"]
	}

	if agent != "" {
		if !requireNameStr(agent) {
			writeJSON(w, 400, map[string]string{"error": "invalid agent name"})
			return
		}
	}

	// list action does not require agent
	if body.Action == "list" {
		agentsDir := filepath.Join(h.deps.Config.Home, "agents")
		entries, err := os.ReadDir(agentsDir)
		if err != nil {
			writeJSON(w, 200, []interface{}{})
			return
		}
		trustLabels := map[int]string{1: "minimal", 2: "low", 3: "standard", 4: "high", 5: "elevated"}
		var profiles []map[string]interface{}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			trustPath := filepath.Join(agentsDir, name, "trust.yaml")
			var trust map[string]interface{}
			if data, err := os.ReadFile(trustPath); err == nil {
				yaml.Unmarshal(data, &trust)
			}
			if trust == nil {
				trust = map[string]interface{}{"level": 3, "agent": name}
			}
			level, _ := trust["level"].(int)
			if level == 0 {
				if f, ok := trust["level"].(float64); ok {
					level = int(f)
				}
				if level == 0 {
					level = 3
				}
			}
			label := trustLabels[level]
			if label == "" {
				label = "unknown"
			}
			profiles = append(profiles, map[string]interface{}{"agent": name, "level": level, "label": label})
		}
		if profiles == nil {
			profiles = []map[string]interface{}{}
		}
		writeJSON(w, 200, profiles)
		return
	}

	if agent == "" {
		writeJSON(w, 400, map[string]string{"error": "agent required"})
		return
	}
	if !requireNameStr(agent) {
		writeJSON(w, 400, map[string]string{"error": "invalid agent"})
		return
	}

	// Read current trust state from agent config
	trustPath := filepath.Join(h.deps.Config.Home, "agents", agent, "trust.yaml")
	var trust map[string]interface{}
	if data, err := os.ReadFile(trustPath); err == nil {
		yaml.Unmarshal(data, &trust)
	}
	if trust == nil {
		trust = map[string]interface{}{"level": 3, "agent": agent}
	}

	// Helper to read level with int/float64 fallback (yaml unmarshals numbers as float64)
	getLevel := func() int {
		if lvl, ok := trust["level"].(int); ok {
			return lvl
		}
		if f, ok := trust["level"].(float64); ok {
			return int(f)
		}
		return 3
	}

	switch body.Action {
	case "show":
		writeJSON(w, 200, trust)
	case "elevate":
		level := getLevel()
		if level < 5 {
			trust["level"] = level + 1
		}
		data, _ := yaml.Marshal(trust)
		os.WriteFile(trustPath, data, 0644)
		h.deps.Audit.Write(agent, "trust_elevated", map[string]interface{}{"from": level, "to": trust["level"]})
		writeJSON(w, 200, trust)
	case "demote":
		level := getLevel()
		if level > 1 {
			trust["level"] = level - 1
		}
		data, _ := yaml.Marshal(trust)
		os.WriteFile(trustPath, data, 0644)
		h.deps.Audit.Write(agent, "trust_demoted", map[string]interface{}{"from": level, "to": trust["level"]})
		writeJSON(w, 200, trust)
	case "record":
		signalType := body.SignalType
		if signalType == "" {
			signalType = body.Args["signal_type"]
		}
		desc := body.Description
		if desc == "" {
			desc = body.Args["description"]
		}
		h.deps.Audit.Write(agent, "trust_signal", map[string]interface{}{"signal_type": signalType, "description": desc})
		writeJSON(w, 200, map[string]string{"status": "recorded", "agent": agent, "signal_type": signalType})
	default:
		writeJSON(w, 400, map[string]string{"error": "unknown action: " + body.Action})
	}
}

func (h *handler) adminAudit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	action := q.Get("action")
	agent := q.Get("agent")
	if agent != "" && !requireNameStr(agent) {
		writeJSON(w, 400, map[string]string{"error": "invalid agent name"})
		return
	}
	since := q.Get("since")
	until := q.Get("until")

	reader := logs.NewReader(h.deps.Config.Home)

	// stats action: return aggregate stats across all agents (or a single agent if provided)
	if action == "stats" {
		auditDir := filepath.Join(h.deps.Config.Home, "audit")
		entries, err := os.ReadDir(auditDir)
		if err != nil {
			writeJSON(w, 200, map[string]interface{}{
				"agents":        0,
				"total_files":   0,
				"total_size_mb": 0.0,
				"oldest":        "",
			})
			return
		}

		agentCount := 0
		totalFiles := 0
		var totalSize int64
		oldest := ""

		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			// If filtering by a specific agent, skip others
			if agent != "" && e.Name() != agent {
				continue
			}
			agentCount++
			agentDir := filepath.Join(auditDir, e.Name())
			files, _ := os.ReadDir(agentDir)
			for _, f := range files {
				if !strings.HasSuffix(f.Name(), ".jsonl") {
					continue
				}
				totalFiles++
				if info, err := f.Info(); err == nil {
					totalSize += info.Size()
					modTime := info.ModTime().Format(time.RFC3339)
					if oldest == "" || modTime < oldest {
						oldest = modTime
					}
				}
			}
		}

		writeJSON(w, 200, map[string]interface{}{
			"agents":        agentCount,
			"total_files":   totalFiles,
			"total_size_mb": float64(totalSize) / (1024 * 1024),
			"oldest":        oldest,
		})
		return
	}

	var events []logs.Event
	var err error
	if agent == "" || agent == "_all" {
		events, err = reader.ReadAllLogs(since, until)
	} else {
		events, err = reader.ReadAgentLog(agent, since, until)
	}
	if err != nil {
		if agent == "" || agent == "_all" {
			writeJSON(w, 200, []logs.Event{})
			return
		}
		writeJSON(w, 404, map[string]string{"error": "no audit logs for agent"})
		return
	}
	h.annotateAuditResultArtifacts(events)
	writeJSON(w, 200, events)
}

func (h *handler) annotateAuditResultArtifacts(events []logs.Event) {
	if len(events) == 0 {
		return
	}
	taskIDsByAgent := map[string]map[string]struct{}{}
	for _, event := range events {
		agentName, _ := event["agent"].(string)
		taskID, _ := event["task_id"].(string)
		agentName = strings.TrimSpace(agentName)
		taskID = strings.TrimSpace(taskID)
		if agentName == "" || taskID == "" {
			continue
		}
		taskIDs, ok := taskIDsByAgent[agentName]
		if !ok {
			taskIDs = h.auditResultTaskIDs(agentName)
			taskIDsByAgent[agentName] = taskIDs
		}
		if _, exists := taskIDs[taskID]; !exists {
			continue
		}
		event["has_result"] = true
		event["result"] = map[string]interface{}{
			"task_id": taskID,
			"url":     "/api/v1/agents/" + url.PathEscape(agentName) + "/results/" + url.PathEscape(taskID),
		}
	}
}

func (h *handler) auditResultTaskIDs(agentName string) map[string]struct{} {
	ids := map[string]struct{}{}
	if dir, ok := h.auditHostResultsDir(agentName); ok {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return ids
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			taskID := strings.TrimSuffix(entry.Name(), ".md")
			if taskID != "" {
				ids[taskID] = struct{}{}
			}
		}
		return ids
	}
	if h.deps.Runtime == nil {
		return ids
	}
	ref := runtimecontract.InstanceRef{RuntimeID: agentName, Role: runtimecontract.RoleWorkspace}
	out, err := h.deps.Runtime.Exec(context.Background(), ref, []string{
		"sh", "-c", "ls -1 /workspace/.results/*.md 2>/dev/null | while read f; do basename \"$f\" .md; done",
	})
	if err != nil {
		return ids
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		taskID := strings.TrimSpace(filepath.Base(line))
		taskID = strings.TrimSuffix(taskID, ".md")
		if taskID != "" {
			ids[taskID] = struct{}{}
		}
	}
	return ids
}

func (h *handler) auditHostResultsDir(agentName string) (string, bool) {
	if h.deps.AgentManager == nil || h.deps.AgentManager.Runtime == nil {
		return "", false
	}
	manifest, err := h.deps.AgentManager.Runtime.Manifest(agentName)
	if err != nil {
		return "", false
	}
	workspacePath := strings.TrimSpace(manifest.Spec.Storage.WorkspacePath)
	if workspacePath == "" || workspacePath == "/workspace" || !filepath.IsAbs(workspacePath) {
		return "", false
	}
	return filepath.Join(workspacePath, ".results"), true
}

func (h *handler) adminEgress(w http.ResponseWriter, r *http.Request) {
	agent := r.URL.Query().Get("agent")
	if _, ok := requireName(w, agent); !ok {
		return
	}

	egress, err := h.egressPolicy().List(agent)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, egress)
}

func (h *handler) adminEgressApproveDomain(w http.ResponseWriter, r *http.Request) {
	agent, ok := requireName(w, chi.URLParam(r, "agent"))
	if !ok {
		return
	}
	var body struct {
		Domain string `json:"domain"`
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	egress, err := h.egressPolicy().ApproveDomain(agent, body.Domain, body.Reason)
	h.writeEgressMutationResponse(w, err, egress)
}

func (h *handler) adminEgressRevokeDomain(w http.ResponseWriter, r *http.Request) {
	agent, ok := requireName(w, chi.URLParam(r, "agent"))
	if !ok {
		return
	}
	egress, err := h.egressPolicy().RevokeDomain(agent, chi.URLParam(r, "domain"))
	h.writeEgressMutationResponse(w, err, egress)
}

func (h *handler) adminEgressMode(w http.ResponseWriter, r *http.Request) {
	agent, ok := requireName(w, chi.URLParam(r, "agent"))
	if !ok {
		return
	}
	var body struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	egress, err := h.egressPolicy().SetMode(agent, body.Mode)
	h.writeEgressMutationResponse(w, err, egress)
}

func (h *handler) egressPolicy() egresspolicy.Service {
	return egresspolicy.Service{
		Home:  h.deps.Config.Home,
		Audit: h.deps.Audit,
	}
}

func (h *handler) writeEgressMutationResponse(w http.ResponseWriter, err error, result *egresspolicy.MutationResult) {
	if err != nil {
		switch {
		case errors.Is(err, egresspolicy.ErrInvalidDomain):
			writeJSON(w, 400, map[string]string{"error": "invalid domain"})
		case errors.Is(err, egresspolicy.ErrInvalidMode):
			writeJSON(w, 400, map[string]string{"error": "invalid egress mode"})
		default:
			writeJSON(w, 500, map[string]string{"error": err.Error()})
		}
		return
	}
	writeJSON(w, 200, result)
}

func (h *handler) adminKnowledge(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action string            `json:"action"`
		Args   map[string]string `json:"args"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}

	ctx := r.Context()
	kp := h.deps.Knowledge
	args := body.Args
	if args == nil {
		args = map[string]string{}
	}

	var (
		raw []byte
		err error
	)

	switch body.Action {
	case "stats":
		raw, err = kp.Stats(ctx)
	case "health":
		raw, err = kp.Get(ctx, "/health")
	case "flags":
		raw, err = kp.Flags(ctx)
	case "log":
		raw, err = kp.CurationLog(ctx)
	case "query":
		q := args["query"]
		if q == "" {
			writeJSON(w, 400, map[string]string{"error": "query is required"})
			return
		}
		path := "/search?q=" + q
		if kind := args["kind"]; kind != "" {
			path += "&kind=" + kind
		}
		if limit := args["limit"]; limit != "" {
			path += "&limit=" + limit
		}
		raw, err = kp.Get(ctx, path)
	case "neighbors":
		nodeID := args["node_id"]
		if nodeID == "" {
			writeJSON(w, 400, map[string]string{"error": "node_id is required"})
			return
		}
		path := "/neighbors?node_id=" + nodeID
		if dir := args["direction"]; dir != "" {
			path += "&direction=" + dir
		}
		if rel := args["relation"]; rel != "" {
			path += "&relation=" + rel
		}
		raw, err = kp.Get(ctx, path)
	case "path":
		from, to := args["from"], args["to"]
		if from == "" || to == "" {
			writeJSON(w, 400, map[string]string{"error": "from and to are required"})
			return
		}
		path := "/path?from=" + from + "&to=" + to
		if hops := args["max_hops"]; hops != "" {
			path += "&max_hops=" + hops
		}
		raw, err = kp.Get(ctx, path)
	case "changes":
		path := "/changes"
		if since := args["since"]; since != "" {
			path += "?since=" + since
		}
		raw, err = kp.Get(ctx, path)
	case "export":
		path := "/export"
		sep := "?"
		if fmt2 := args["format"]; fmt2 != "" {
			path += sep + "format=" + fmt2
			sep = "&"
		}
		if since := args["since"]; since != "" {
			path += sep + "since=" + since
		}
		raw, err = kp.Get(ctx, path)
	case "graph":
		subj := args["subject"]
		if subj == "" {
			writeJSON(w, 400, map[string]string{"error": "subject is required"})
			return
		}
		path := "/graph?subject=" + subj
		if hops := args["hops"]; hops != "" {
			path += "&hops=" + hops
		}
		raw, err = kp.Get(ctx, path)
	case "reset":
		raw, err = kp.Post(ctx, "/reset", nil)
	case "restore":
		nodeID := args["node_id"]
		if nodeID == "" {
			writeJSON(w, 400, map[string]string{"error": "node_id is required"})
			return
		}
		raw, err = kp.Restore(ctx, nodeID)
	case "unflag":
		nodeID := args["node_id"]
		if nodeID == "" {
			writeJSON(w, 400, map[string]string{"error": "node_id is required"})
			return
		}
		raw, err = kp.Post(ctx, "/curation/unflag", map[string]string{"node_id": nodeID})
	case "curate":
		raw, err = kp.Post(ctx, "/curation/run", nil)
	case "ontology_candidates":
		var candidates []knowledge.OntologyCandidate
		candidates, err = knowledge.ListOntologyCandidates(ctx, kp)
		if err == nil {
			raw, err = json.Marshal(map[string]interface{}{"candidates": candidates})
		}
	case "ontology_promote":
		val := args["value"]
		nodeID := args["node_id"]
		if val == "" && nodeID == "" {
			writeJSON(w, 400, map[string]string{"error": "node_id or value is required"})
			return
		}
		var resolved string
		resolved, err = knowledge.ResolveOntologyCandidateID(ctx, kp, nodeID, val)
		if err == nil {
			raw, err = kp.Post(ctx, "/ontology/promote", map[string]string{"node_id": resolved})
		}
	case "ontology_reject":
		val := args["value"]
		nodeID := args["node_id"]
		if val == "" && nodeID == "" {
			writeJSON(w, 400, map[string]string{"error": "node_id or value is required"})
			return
		}
		var resolved string
		resolved, err = knowledge.ResolveOntologyCandidateID(ctx, kp, nodeID, val)
		if err == nil {
			raw, err = kp.Post(ctx, "/ontology/reject", map[string]string{"node_id": resolved})
		}
	case "ontology_restore":
		val := args["value"]
		nodeID := args["node_id"]
		if val == "" && nodeID == "" {
			writeJSON(w, 400, map[string]string{"error": "node_id or value is required"})
			return
		}
		var resolved string
		resolved, err = knowledge.ResolveOntologyCandidateID(ctx, kp, nodeID, val)
		if err == nil {
			raw, err = kp.Post(ctx, "/ontology/restore", map[string]string{"node_id": resolved})
		}
	case "ontology_seed_kind_candidate":
		kind := args["kind"]
		if !requireNameStr(kind) {
			writeJSON(w, 400, map[string]string{"error": "valid kind is required"})
			return
		}
		seedID := args["seed_id"]
		if !requireNameStr(seedID) {
			writeJSON(w, 400, map[string]string{"error": "valid seed_id is required"})
			return
		}
		count := 10
		if rawCount := args["count"]; rawCount != "" {
			count, err = strconv.Atoi(rawCount)
			if err != nil || count < 3 || count > 100 {
				writeJSON(w, 400, map[string]string{"error": "count must be an integer between 3 and 100"})
				return
			}
		}
		var nodes []map[string]interface{}
		for i := 0; i < count; i++ {
			nodes = append(nodes, map[string]interface{}{
				"label":           fmt.Sprintf("%s-%02d", kind, i+1),
				"kind":            kind,
				"summary":         "Playwright ontology candidate seed",
				"source_type":     "test",
				"source_channels": []string{fmt.Sprintf("%s-source-%d", seedID, (i%3)+1)},
				"properties": map[string]string{
					"e2e_seed": seedID,
				},
			})
		}
		raw, err = kp.Post(ctx, "/ingest/nodes", map[string]interface{}{"nodes": nodes})
	case "delete_by_kind":
		kind := args["kind"]
		filterProp := args["filter_property"]
		filterValue := args["filter_value"]
		if kind == "" || filterProp == "" || filterValue == "" {
			writeJSON(w, 400, map[string]string{"error": "kind, filter_property, and filter_value are required"})
			return
		}
		raw, err = kp.Post(ctx, "/delete-by-kind", map[string]interface{}{
			"kind": kind,
			"filter": map[string]string{
				filterProp: filterValue,
			},
		})
	default:
		writeJSON(w, 400, map[string]string{"error": "unknown action: " + body.Action})
		return
	}

	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(raw)
}

func (h *handler) adminDepartment(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body struct {
		Action string            `json:"action"`
		Args   map[string]string `json:"args"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}

	deptDir := filepath.Join(h.deps.Config.Home, "departments")
	os.MkdirAll(deptDir, 0755)

	switch body.Action {
	case "list":
		entries, _ := os.ReadDir(deptDir)
		var depts []string
		for _, e := range entries {
			if e.IsDir() {
				depts = append(depts, e.Name())
			}
		}
		writeJSON(w, 200, map[string]interface{}{"departments": depts})
	case "show":
		name, ok := requireName(w, body.Args["name"])
		if !ok {
			return
		}
		policyPath := filepath.Join(deptDir, name, "policy.yaml")
		var policy map[string]interface{}
		if data, err := os.ReadFile(policyPath); err == nil {
			yaml.Unmarshal(data, &policy)
		}
		if policy == nil {
			policy = map[string]interface{}{"name": name}
		}
		writeJSON(w, 200, policy)
	default:
		writeJSON(w, 200, map[string]interface{}{"status": "ok", "action": body.Action})
	}
}

func (h *handler) rebuildAgent(w http.ResponseWriter, r *http.Request) {
	name, ok := requireName(w, chi.URLParam(r, "name"))
	if !ok {
		return
	}

	agentDir := filepath.Join(h.deps.Config.Home, "agents", name)
	if _, err := os.Stat(filepath.Join(agentDir, "agent.yaml")); err != nil {
		writeJSON(w, 404, map[string]string{"error": "agent not found: " + name})
		return
	}

	var regenerated []string
	var errors []string

	// 1. Regenerate services-manifest.json and services.yaml
	if err := h.generateAgentManifest(name); err != nil {
		errors = append(errors, "manifest: "+err.Error())
	} else {
		regenerated = append(regenerated, "services-manifest.json", "services.yaml")
	}

	// 2. Read agent config for type-specific doc generation
	agentType := "standard"
	var agentConfig map[string]interface{}
	if data, err := os.ReadFile(filepath.Join(agentDir, "agent.yaml")); err == nil {
		yaml.Unmarshal(data, &agentConfig)
		if t, ok := agentConfig["type"].(string); ok && t != "" {
			agentType = t
		}
	}

	// 3. Read constraints for AGENTS.md generation
	var constraints map[string]interface{}
	if data, err := os.ReadFile(filepath.Join(agentDir, "constraints.yaml")); err == nil {
		yaml.Unmarshal(data, &constraints)
	}

	// 4. Regenerate AGENTS.md
	if constraints != nil {
		agentsMD := orchestrate.GenerateAgentsMD(constraints, agentType)
		if err := os.WriteFile(filepath.Join(agentDir, "AGENTS.md"), []byte(agentsMD), 0644); err != nil {
			errors = append(errors, "AGENTS.md: "+err.Error())
		} else {
			regenerated = append(regenerated, "AGENTS.md")
		}
	} else {
		errors = append(errors, "AGENTS.md: constraints.yaml not readable")
	}

	// 5. Regenerate FRAMEWORK.md
	frameworkMD := orchestrate.GenerateFrameworkMD(agentType, "standard")
	if err := os.WriteFile(filepath.Join(agentDir, "FRAMEWORK.md"), []byte(frameworkMD), 0644); err != nil {
		errors = append(errors, "FRAMEWORK.md: "+err.Error())
	} else {
		regenerated = append(regenerated, "FRAMEWORK.md")
	}

	// 6. Regenerate PLATFORM.md
	platformMD := orchestrate.GeneratePlatformMD(agentType, nil)
	if err := os.WriteFile(filepath.Join(agentDir, "PLATFORM.md"), []byte(platformMD), 0644); err != nil {
		errors = append(errors, "PLATFORM.md: "+err.Error())
	} else {
		regenerated = append(regenerated, "PLATFORM.md")
	}

	h.deps.Audit.Write(name, "admin_rebuild", map[string]interface{}{
		"regenerated": regenerated,
		"errors":      errors,
	})
	h.deps.Logger.Info("agent rebuild completed", "agent", name,
		"regenerated", len(regenerated), "errors", len(errors))

	status := "rebuilt"
	code := 200
	if len(errors) > 0 && len(regenerated) == 0 {
		status = "failed"
		code = 500
	} else if len(errors) > 0 {
		status = "partial"
	}

	writeJSON(w, code, map[string]interface{}{
		"status":      status,
		"agent":       name,
		"regenerated": regenerated,
		"errors":      errors,
	})
}
