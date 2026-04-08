package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"

	"github.com/geoffbelknap/agency/internal/logs"
	"github.com/geoffbelknap/agency/internal/orchestrate"
)

// ── Admin ───────────────────────────────────────────────────────────────────

func (h *handler) adminDoctor(w http.ResponseWriter, r *http.Request) {
	type checkResult struct {
		Name   string `json:"name"`
		Agent  string `json:"agent"`
		Status string `json:"status"`
		Detail string `json:"detail,omitempty"`
	}
	type scopeInfo struct {
		Agent    string   `json:"agent"`
		Service  string   `json:"service"`
		Required []string `json:"required,omitempty"`
		Optional []string `json:"optional,omitempty"`
	}
	type doctorReport struct {
		AllPassed    bool          `json:"all_passed"`
		TestedAgents []string      `json:"tested_agents"`
		Checks       []checkResult `json:"checks"`
		Scopes       []scopeInfo   `json:"scopes,omitempty"`
		Unscoped     []string      `json:"unscoped_agents,omitempty"`
	}

	ctx := r.Context()
	report := doctorReport{AllPassed: true}

	// Find running agents via docker (workspace containers)
	agents, err := h.deps.DC.ListAgentWorkspaces(ctx)
	if err != nil {
		report.AllPassed = false
		report.Checks = append(report.Checks, checkResult{
			Name: "docker_connectivity", Status: "fail",
			Detail: "Cannot list containers: " + err.Error(),
		})
		writeJSON(w, 200, report)
		return
	}
	report.TestedAgents = agents

	if len(agents) == 0 {
		report.Checks = append(report.Checks, checkResult{
			Name: "no_running_agents", Status: "pass",
			Detail: "No running agents to check",
		})
		// Still run infra-level Docker checks even with no agents
		dockerChecks := h.runDockerChecks(ctx, nil)
		for _, dc := range dockerChecks {
			if dc.Status != "pass" {
				report.AllPassed = false
			}
			report.Checks = append(report.Checks, checkResult{
				Name:   dc.Name,
				Agent:  dc.Agent,
				Status: dc.Status,
				Detail: dc.Detail,
			})
		}
		writeJSON(w, 200, report)
		return
	}

	// Run the 7 security guarantee checks for each agent
	for _, agentName := range agents {
		wsName := "agency-" + agentName + "-workspace"
		enfName := "agency-" + agentName + "-enforcer"

		// ── 1. LLM credentials isolated ───────────────────────────────
		func() {
			ws, err := h.deps.DC.InspectContainer(ctx, wsName)
			if err != nil {
				report.AllPassed = false
				report.Checks = append(report.Checks, checkResult{
					Name: "credentials_isolated", Agent: agentName, Status: "fail",
					Detail: "Cannot inspect workspace: " + err.Error(),
				})
				return
			}
			// Real provider keys must never be in the workspace.
			// OPENAI_API_KEY is allowed only if it holds an agency-scoped token
			// (prefix "agency-scoped--"). Real provider keys (sk-*, aip-*, etc.) are violations.
			realKeyPrefixes := []string{"ANTHROPIC_API_KEY", "GOOGLE_API_KEY", "GEMINI_API_KEY", "AWS_SECRET_ACCESS_KEY"}
			var leaked []string
			for _, env := range ws.Env {
				for _, key := range realKeyPrefixes {
					if strings.HasPrefix(env, key+"=") {
						parts := strings.SplitN(env, "=", 2)
						if len(parts) == 2 && parts[1] != "" {
							leaked = append(leaked, key)
						}
					}
				}
				// OPENAI_API_KEY: scoped tokens start with "agency-scoped--"
				if strings.HasPrefix(env, "OPENAI_API_KEY=") {
					parts := strings.SplitN(env, "=", 2)
					if len(parts) == 2 && parts[1] != "" && !strings.HasPrefix(parts[1], "agency-scoped--") {
						leaked = append(leaked, "OPENAI_API_KEY (not an agency-scoped token)")
					}
				}
			}
			if len(leaked) > 0 {
				report.AllPassed = false
				report.Checks = append(report.Checks, checkResult{
					Name: "credentials_isolated", Agent: agentName, Status: "fail",
					Detail: "LLM credentials visible in workspace env: " + strings.Join(leaked, ", "),
				})
			} else {
				report.Checks = append(report.Checks, checkResult{
					Name: "credentials_isolated", Agent: agentName, Status: "pass",
					Detail: "No LLM API keys in workspace environment",
				})
			}
		}()

		// ── 2. Network mediation complete ─────────────────────────────
		func() {
			ws, err := h.deps.DC.InspectContainer(ctx, wsName)
			if err != nil {
				report.AllPassed = false
				report.Checks = append(report.Checks, checkResult{
					Name: "network_mediation", Agent: agentName, Status: "fail",
					Detail: "Cannot inspect workspace: " + err.Error(),
				})
				return
			}
			var forbidden []string
			for _, net := range ws.Networks {
				if strings.Contains(net, "egress") || net == "agency-gateway" {
					forbidden = append(forbidden, net)
				}
			}
			if len(forbidden) > 0 {
				report.AllPassed = false
				report.Checks = append(report.Checks, checkResult{
					Name: "network_mediation", Agent: agentName, Status: "fail",
					Detail: "Workspace on forbidden network(s): " + strings.Join(forbidden, ", "),
				})
			} else {
				report.Checks = append(report.Checks, checkResult{
					Name: "network_mediation", Agent: agentName, Status: "pass",
					Detail: "Workspace on internal network(s) only: " + strings.Join(ws.Networks, ", "),
				})
			}
		}()

		// ── 3. Constraints read-only ──────────────────────────────────
		func() {
			ws, err := h.deps.DC.InspectContainer(ctx, wsName)
			if err != nil {
				report.AllPassed = false
				report.Checks = append(report.Checks, checkResult{
					Name: "constraints_readonly", Agent: agentName, Status: "fail",
					Detail: "Cannot inspect workspace: " + err.Error(),
				})
				return
			}
			found := false
			for _, m := range ws.Mounts {
				if strings.Contains(m.Destination, "constraints.yaml") {
					found = true
					if m.RW {
						report.AllPassed = false
						report.Checks = append(report.Checks, checkResult{
							Name: "constraints_readonly", Agent: agentName, Status: "fail",
							Detail: "constraints.yaml mounted read-write at " + m.Destination,
						})
						return
					}
				}
			}
			if found {
				report.Checks = append(report.Checks, checkResult{
					Name: "constraints_readonly", Agent: agentName, Status: "pass",
					Detail: "constraints.yaml mounted read-only",
				})
			} else {
				// Mount not found — not necessarily a failure, but worth noting
				report.Checks = append(report.Checks, checkResult{
					Name: "constraints_readonly", Agent: agentName, Status: "pass",
					Detail: "constraints.yaml mount not found (may be embedded in image)",
				})
			}
		}()

		// ── 4. Enforcer audit active ──────────────────────────────────
		func() {
			enf, err := h.deps.DC.InspectContainer(ctx, enfName)
			if err != nil {
				report.AllPassed = false
				report.Checks = append(report.Checks, checkResult{
					Name: "enforcer_audit", Agent: agentName, Status: "fail",
					Detail: "Enforcer container not found: " + err.Error(),
				})
				return
			}
			if enf.State != "running" {
				report.AllPassed = false
				report.Checks = append(report.Checks, checkResult{
					Name: "enforcer_audit", Agent: agentName, Status: "fail",
					Detail: "Enforcer status: " + enf.State,
				})
			} else {
				detail := "Enforcer running"
				if enf.Health != "none" && enf.Health != "" {
					detail += ", health: " + enf.Health
				}
				report.Checks = append(report.Checks, checkResult{
					Name: "enforcer_audit", Agent: agentName, Status: "pass",
					Detail: detail,
				})
			}
		}()

		// ── 5. Audit log not writable by agent ────────────────────────
		func() {
			ws, err := h.deps.DC.InspectContainer(ctx, wsName)
			if err != nil {
				report.AllPassed = false
				report.Checks = append(report.Checks, checkResult{
					Name: "audit_not_writable", Agent: agentName, Status: "fail",
					Detail: "Cannot inspect workspace: " + err.Error(),
				})
				return
			}
			for _, m := range ws.Mounts {
				if strings.Contains(m.Destination, "audit") {
					if m.RW {
						report.AllPassed = false
						report.Checks = append(report.Checks, checkResult{
							Name: "audit_not_writable", Agent: agentName, Status: "fail",
							Detail: "Audit directory mounted read-write at " + m.Destination,
						})
						return
					}
				}
			}
			// Either mounted read-only or not mounted at all — both are safe
			report.Checks = append(report.Checks, checkResult{
				Name: "audit_not_writable", Agent: agentName, Status: "pass",
				Detail: "Audit directory not writable by agent",
			})
		}()

		// ── 6. Halt functional ────────────────────────────────────────
		func() {
			ws, err := h.deps.DC.InspectContainer(ctx, wsName)
			if err != nil {
				report.AllPassed = false
				report.Checks = append(report.Checks, checkResult{
					Name: "halt_functional", Agent: agentName, Status: "fail",
					Detail: "Cannot inspect workspace: " + err.Error(),
				})
				return
			}
			// Container must be running to be pauseable (docker pause requirement)
			if ws.State == "running" {
				report.Checks = append(report.Checks, checkResult{
					Name: "halt_functional", Agent: agentName, Status: "pass",
					Detail: "Workspace container is running and pauseable",
				})
			} else {
				report.AllPassed = false
				report.Checks = append(report.Checks, checkResult{
					Name: "halt_functional", Agent: agentName, Status: "fail",
					Detail: "Workspace state '" + ws.State + "' — cannot pause",
				})
			}
		}()

		// ── 7. Operator override available ────────────────────────────
		func() {
			enf, err := h.deps.DC.InspectContainer(ctx, enfName)
			if err != nil {
				report.AllPassed = false
				report.Checks = append(report.Checks, checkResult{
					Name: "operator_override", Agent: agentName, Status: "fail",
					Detail: "Cannot inspect enforcer: " + err.Error(),
				})
				return
			}
			onMediation := false
			for _, net := range enf.Networks {
				if strings.Contains(net, "mediation") {
					onMediation = true
					break
				}
			}
			if onMediation {
				report.Checks = append(report.Checks, checkResult{
					Name: "operator_override", Agent: agentName, Status: "pass",
					Detail: "Enforcer reachable on mediation network",
				})
			} else {
				report.AllPassed = false
				report.Checks = append(report.Checks, checkResult{
					Name: "operator_override", Agent: agentName, Status: "fail",
					Detail: "Enforcer not on mediation network: " + strings.Join(enf.Networks, ", "),
				})
			}
		}()
	}

	// ── Infrastructure Docker hygiene checks ──────────────────────────────────
	dockerChecks := h.runDockerChecks(ctx, agents)
	for _, dc := range dockerChecks {
		if dc.Status != "pass" {
			report.AllPassed = false
		}
		report.Checks = append(report.Checks, checkResult{
			Name:   dc.Name,
			Agent:  dc.Agent,
			Status: dc.Status,
			Detail: dc.Detail,
		})
	}

	// Scope audit: collect per-agent scope declarations from presets
	agentsDir := filepath.Join(h.deps.Config.Home, "agents")
	agentEntries, _ := os.ReadDir(agentsDir)
	for _, ae := range agentEntries {
		if !ae.IsDir() {
			continue
		}
		agentName := ae.Name()
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
		var agentCfg struct{ Preset string `yaml:"preset"` }
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
		agentCount := len(agents)
		meeseeksCount := 0
		// Count meeseeks containers
		if mks, err := h.deps.DC.RawClient().ContainerList(ctx, container.ListOptions{
			Filters: filters.NewArgs(
				filters.Arg("label", "agency.role=meeseeks"),
				filters.Arg("status", "running"),
			),
		}); err == nil {
			meeseeksCount = len(mks)
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

	// Network pool check
	if capErr == nil {
		if capCfg.NetworkPoolConfigured {
			report.Checks = append(report.Checks, checkResult{
				Name: "network_pool", Status: "pass",
				Detail: "Docker network pool configured for /24 subnets",
			})
		} else {
			report.Checks = append(report.Checks, checkResult{
				Name: "network_pool", Status: "warn",
				Detail: "Default Docker pool (limited to ~15 networks) — run agency setup to configure",
			})
		}
	}

	writeJSON(w, 200, report)
}

func (h *handler) adminDestroy(w http.ResponseWriter, r *http.Request) {
	// Stop all agents
	agents, _ := h.deps.AgentManager.List(r.Context())
	for _, a := range agents {
		h.deps.AgentManager.StopContainers(r.Context(), a.Name)
		h.deps.AgentManager.Delete(r.Context(), a.Name)
	}

	// Tear down infrastructure
	if h.deps.Infra != nil {
		h.deps.Infra.Teardown(r.Context())
	}

	// Prune dangling agency images
	if h.deps.DC != nil {
		h.pruneDanglingImages(r.Context())
	}

	h.deps.Logger.Info("admin destroy completed")
	writeJSON(w, 200, map[string]string{"status": "destroyed"})
}

// pruneDanglingImages removes agency-prefixed images that are not tagged :latest.
func (h *handler) pruneDanglingImages(ctx context.Context) {
	imgs, err := h.deps.DC.ListAgencyImages(ctx)
	if err != nil {
		h.deps.Logger.Warn("prune images: list failed", "err", err)
		return
	}
	for _, img := range imgs {
		for _, tag := range img.RepoTags {
			if !strings.HasSuffix(tag, ":latest") {
				if _, err := h.deps.DC.RemoveImage(ctx, tag); err != nil {
					h.deps.Logger.Debug("prune image skip", "tag", tag, "err", err)
				} else {
					h.deps.Logger.Info("pruned dangling image", "tag", tag)
				}
			}
		}
		// Remove untagged images
		if len(img.RepoTags) == 0 {
			if _, err := h.deps.DC.RemoveImage(ctx, img.ID); err != nil {
				h.deps.Logger.Debug("prune untagged image skip", "id", img.ID, "err", err)
			}
		}
	}
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

	// export / retention / default: require agent
	if agent == "" {
		writeJSON(w, 400, map[string]string{"error": "agent query parameter required"})
		return
	}

	events, err := reader.ReadAgentLog(agent, since, until)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "no audit logs for agent"})
		return
	}
	writeJSON(w, 200, events)
}

func (h *handler) adminEgress(w http.ResponseWriter, r *http.Request) {
	agent := r.URL.Query().Get("agent")
	if _, ok := requireName(w, agent); !ok {
		return
	}

	// Read egress config for the agent
	egressPath := filepath.Join(h.deps.Config.Home, "agents", agent, "egress.yaml")
	var egress map[string]interface{}
	if data, err := os.ReadFile(egressPath); err == nil {
		yaml.Unmarshal(data, &egress)
	}
	if egress == nil {
		egress = map[string]interface{}{"agent": agent, "domains": []string{}}
	}
	writeJSON(w, 200, egress)
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
		raw, err = kp.Get(ctx, "/ontology/candidates")
	case "ontology_promote":
		val := args["value"]
		if val == "" {
			writeJSON(w, 400, map[string]string{"error": "value is required"})
			return
		}
		raw, err = kp.Post(ctx, "/ontology/promote", map[string]string{"value": val})
	case "ontology_reject":
		val := args["value"]
		if val == "" {
			writeJSON(w, 400, map[string]string{"error": "value is required"})
			return
		}
		raw, err = kp.Post(ctx, "/ontology/reject", map[string]string{"value": val})
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
