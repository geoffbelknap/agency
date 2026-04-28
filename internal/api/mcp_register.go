package api

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/credstore"
	"github.com/geoffbelknap/agency/internal/features"
	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
	"github.com/geoffbelknap/agency/internal/infratier"
	"github.com/geoffbelknap/agency/internal/logs"
	"github.com/geoffbelknap/agency/internal/orchestrate"
	"github.com/geoffbelknap/agency/internal/routing"
	"gopkg.in/yaml.v3"
)

var mcpAgentNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*[a-z0-9]$`)

func mcpValidateAgentName(name string) error {
	if len(name) < 2 || !mcpAgentNameRE.MatchString(name) {
		return fmt.Errorf("invalid agent name '%s': must be lowercase alphanumeric with hyphens, min 2 chars", name)
	}
	return nil
}

func registerMCPTools(reg *MCPToolRegistry) {
	registerInfraTools(reg)
	registerAgentTools(reg)
	registerCommsTools(reg)
	registerObservabilityTools(reg)
	registerAdminTools(reg)
	registerPolicyTools(reg)
	registerCredentialTools(reg)
	reg.WithTier(string(features.TierExperimental), func() {
		registerCapabilityTools(reg)
		registerTeamTools(reg)
		registerDeployTools(reg)
		registerConnectorTools(reg)
		registerIntakeTools(reg)
		registerHubTools(reg)
		registerMissionTools(reg)
		registerMeeseeksTools(reg)
		registerEventTools(reg)
		registerNotificationTools(reg)
		registerProfileTools(reg)
	})
}

func mcpConfiguredRuntimeBackend(d *mcpDeps) string {
	if d != nil && d.cfg != nil && strings.TrimSpace(d.cfg.Hub.DeploymentBackend) != "" {
		return strings.TrimSpace(d.cfg.Hub.DeploymentBackend)
	}
	return runtimehost.BackendDocker
}

func mcpContainerInfraUnavailable(d *mcpDeps) (string, bool) {
	backend := mcpConfiguredRuntimeBackend(d)
	if !runtimehost.IsContainerBackend(backend) {
		return fmt.Sprintf("Infrastructure container lifecycle is only available for container backends. Current backend: %s.", backend), true
	}
	if d == nil || d.dc == nil {
		return fmt.Sprintf("Infrastructure manager is unavailable: %s client is not initialized.", runtimehost.NormalizeContainerBackend(backend)), true
	}
	return "", false
}

// ── Infrastructure (6 tools) ────────────────────────────────────────────────

func registerInfraTools(reg *MCPToolRegistry) {
	reg.Register(
		"agency_infra_status",
		"Show health for the shared Agency runtime services that support agent work.",
		nil,
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			if msg, unavailable := mcpContainerInfraUnavailable(d); unavailable {
				return msg, true
			}
			status, err := d.dc.InfraStatus(context.Background())
			if err != nil {
				return "Error: " + err.Error(), true
			}
			if len(status) > 0 {
				lines := []string{"Infrastructure:"}
				for _, comp := range status {
					icon := "OK"
					if comp.Health != "healthy" && comp.State != "running" {
						icon = "FAIL"
					} else if comp.Health != "" && comp.Health != "healthy" && comp.Health != "none" {
						icon = "FAIL"
					}
					lines = append(lines, fmt.Sprintf("  [%s] %s: state=%s health=%s", icon, comp.Name, comp.State, comp.Health))
				}
				return strings.Join(lines, "\n"), false
			}
			data, _ := json.Marshal(status)
			return string(data), false
		},
	)

	reg.Register(
		"agency_infra_up",
		"Start the shared Agency runtime services needed before agents can run. Use after agency_setup.",
		nil,
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			if msg, unavailable := mcpContainerInfraUnavailable(d); unavailable {
				return msg, true
			}
			if d.infra == nil {
				return "Error: infrastructure manager not initialized", true
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			if err := d.infra.EnsureRunning(ctx); err != nil {
				return "Error: " + err.Error(), true
			}
			return "Infrastructure started.", false
		},
	)

	reg.Register(
		"agency_infra_down",
		"Stop the shared Agency runtime services.",
		nil,
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			if msg, unavailable := mcpContainerInfraUnavailable(d); unavailable {
				return msg, true
			}
			if d.infra == nil {
				return "Error: infrastructure manager not initialized", true
			}
			if err := d.infra.Teardown(context.Background()); err != nil {
				return "Error: " + err.Error(), true
			}
			return "Infrastructure stopped.", false
		},
	)

	reg.Register(
		"agency_infra_rebuild",
		"Rebuild and restart one shared runtime component.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"component": map[string]interface{}{
					"type":        "string",
					"enum":        infratier.RebuildComponents(),
					"description": "Component to rebuild",
				},
			},
			"required": []string{"component"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			component, _ := args["component"].(string)
			if component == "" {
				return "Error: component is required", true
			}
			if msg, unavailable := mcpContainerInfraUnavailable(d); unavailable {
				return msg, true
			}
			if d.infra == nil {
				return "Error: infrastructure manager not initialized", true
			}
			if err := d.infra.RestartComponent(context.Background(), component); err != nil {
				return "Error: " + err.Error(), true
			}
			return fmt.Sprintf("Component '%s' rebuilt.", component), false
		},
	)

	reg.Register(
		"agency_infra_reload",
		"Reload shared runtime and enforcer configuration without a full restart.",
		nil,
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			if msg, unavailable := mcpContainerInfraUnavailable(d); unavailable {
				return msg, true
			}
			if d.infra == nil {
				return "Error: infrastructure manager not initialized", true
			}
			// Regenerate credential-swaps.yaml before reloading
			d.regenerateSwapConfig()

			components := infratier.ReloadComponents()
			var reloaded []string
			for _, comp := range components {
				if err := d.infra.RestartComponent(context.Background(), comp); err != nil {
					continue
				}
				reloaded = append(reloaded, comp)
			}
			if len(reloaded) == 0 {
				return "No components reloaded.", false
			}
			return fmt.Sprintf("Configuration reloaded: %s", strings.Join(reloaded, ", ")), false
		},
	)

	reg.Register(
		"agency_setup",
		"Bootstrap Agency on a fresh host. Creates ~/.agency, writes base config, and verifies the configured container backend. Run this before agency_infra_up and agent creation.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"operator": map[string]interface{}{"type": "string", "description": "Operator name (default: current user)"},
				"force":    map[string]interface{}{"type": "boolean", "description": "Reinitialize if already exists", "default": false},
			},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			operator, _ := args["operator"].(string)
			force, _ := args["force"].(bool)
			opts := config.InitOptions{
				Operator:     operator,
				Force:        force,
				ProviderKeys: config.ProviderKeysFromEnv(),
			}

			// Check for existing keys before init
			home, _ := os.UserHomeDir()
			agencyHome := filepath.Join(home, ".agency")
			existingProviders := config.ReadExistingKeys(agencyHome)

			pendingKeys, err := config.RunInit(opts)
			if err != nil {
				return "Error: " + err.Error(), true
			}

			// Store any new API keys in the credential store
			for _, key := range pendingKeys {
				if d.credStore != nil {
					now := time.Now().UTC().Format(time.RFC3339)
					d.credStore.Put(credstore.Entry{ //nolint:errcheck
						Name:  key.EnvVar,
						Value: key.Key,
						Metadata: credstore.Metadata{
							Kind:      "provider",
							Scope:     "platform",
							Protocol:  "api-key",
							Source:    "setup",
							CreatedAt: now,
							RotatedAt: now,
						},
					})
				}
			}

			msg := "Agency initialized successfully."
			if len(existingProviders) > 0 && len(opts.ProviderKeys) == 0 {
				msg += fmt.Sprintf("\nUsing existing API keys from .env: %s", strings.Join(existingProviders, ", "))
			}
			return msg, false
		},
	)
}

// ── Agent Lifecycle (11 tools) ──────────────────────────────────────────────

func registerAgentTools(reg *MCPToolRegistry) {
	// agency_list
	reg.Register(
		"agency_list",
		"List all agents with their current status (running, stopped, etc). Shows name and state for every configured agent.",
		nil,
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			if d.agents == nil {
				return "Error: agent manager not initialized", true
			}
			agents, err := d.agents.List(context.Background())
			if err != nil {
				return "Error: " + err.Error(), true
			}
			// Convert []AgentDetail to []map[string]interface{} for formatting.
			maps := make([]map[string]interface{}, len(agents))
			for i, a := range agents {
				maps[i] = map[string]interface{}{"name": a.Name, "status": a.Status}
			}
			return fmtAgentList(maps), false
		},
	)

	// agency_create
	reg.Register(
		"agency_create",
		"Create a new agent definition with config files. Requires: agency_setup completed. Next: agency_start to launch the agent.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name":   map[string]interface{}{"type": "string", "description": "Agent name (alphanumeric, hyphens, underscores)"},
				"preset": map[string]interface{}{"type": "string", "enum": []string{"minimal", "generalist", "engineer", "researcher", "writer", "ops", "analyst", "reviewer", "coordinator", "function", "security-reviewer", "compliance-auditor", "privacy-monitor", "code-reviewer", "ops-monitor"}, "default": "generalist"},
			},
			"required": []string{"name"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			name := mapStr(args, "name")
			if err := mcpValidateAgentName(name); err != nil {
				return fmt.Sprintf("Error: %s", err), true
			}
			if d.agents == nil {
				return "Error: agent manager not initialized", true
			}
			preset := mapStr(args, "preset")
			if preset == "" {
				preset = "generalist"
			}
			if err := d.agents.Create(context.Background(), name, preset); err != nil {
				return "Error: " + err.Error(), true
			}
			d.audit.Write(name, "agent_created", map[string]interface{}{"preset": preset})
			return fmt.Sprintf("Agent '%s' created.", name), false
		},
	)

	// agency_show
	reg.Register(
		"agency_show",
		"Show the current agent summary: identity, preset, model, trust, granted access, task state, and runtime status.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"agent": map[string]interface{}{"type": "string", "description": "Agent name"},
			},
			"required": []string{"agent"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			name := mapStr(args, "agent")
			if err := mcpValidateAgentName(name); err != nil {
				return fmt.Sprintf("Error: %s", err), true
			}
			if d.agents == nil {
				return "Error: agent manager not initialized", true
			}
			detail, err := d.agents.Show(context.Background(), name)
			if err != nil {
				return "Error: " + err.Error(), true
			}

			// Header block: identity and configuration
			lines := []string{fmt.Sprintf("Agent: %s", detail.Name)}
			if detail.Preset != "" {
				lines = append(lines, fmt.Sprintf("Preset: %s", detail.Preset))
			}
			if detail.Role != "" {
				lines = append(lines, fmt.Sprintf("Role: %s", detail.Role))
			}
			if detail.Model != "" {
				model := detail.Model
				if detail.ModelTier != "" {
					model = fmt.Sprintf("%s (%s)", model, detail.ModelTier)
				}
				lines = append(lines, fmt.Sprintf("Model: %s", model))
			}

			// Trust — always shown; default is "standard" when no trust.yaml level is set
			trustLabels := map[int]string{1: "minimal", 2: "low", 3: "standard", 4: "high", 5: "elevated"}
			trustLevel := detail.TrustLevel
			trustLabel := trustLabels[trustLevel]
			if trustLabel == "" {
				trustLabel = "standard"
			}
			lines = append(lines, fmt.Sprintf("Trust: %s", trustLabel))

			if detail.Team != "" {
				lines = append(lines, fmt.Sprintf("Team: %s", detail.Team))
			}
			if len(detail.GrantedServices) > 0 {
				lines = append(lines, fmt.Sprintf("Services: %s", strings.Join(detail.GrantedServices, ", ")))
			}
			if len(detail.GrantedCaps) > 0 {
				lines = append(lines, fmt.Sprintf("Capabilities: %s", strings.Join(detail.GrantedCaps, ", ")))
			}
			if len(detail.UnknownKeys) > 0 {
				lines = append(lines, fmt.Sprintf("WARNING: agent.yaml has unknown keys: %s", strings.Join(detail.UnknownKeys, ", ")))
			}

			// Container state
			lines = append(lines, fmt.Sprintf("Workspace: %s", detail.Status))
			lines = append(lines, fmt.Sprintf("Enforcer: %s", detail.Enforcer))

			// Active task — most recent task from session context
			if detail.CurrentTask != nil {
				task := detail.CurrentTask
				taskLine := fmt.Sprintf("Active task: %s", task.Content)
				if task.Timestamp != "" {
					taskLine = fmt.Sprintf("Active task [%s]: %s", task.Timestamp, task.Content)
				}
				lines = append(lines, taskLine)
			}

			return strings.Join(lines, "\n"), false
		},
	)

	// agency_start — uses the same StartSequence as the REST handler
	reg.Register(
		"agency_start",
		"Start an agent through the seven-phase start sequence (verify, enforcement, constraints, workspace, identity, body, session). Requires: agent created, infrastructure running (agency_infra_up). Next: agency_send to deliver a task via DM channel.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"agent": map[string]interface{}{"type": "string", "description": "Agent name"},
			},
			"required": []string{"agent"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			name := mapStr(args, "agent")
			if err := mcpValidateAgentName(name); err != nil {
				return fmt.Sprintf("Error: %s", err), true
			}
			if d.agents == nil {
				return "Error: agent manager not initialized", true
			}

			// Ensure agent exists
			if _, err := d.agents.Show(context.Background(), name); err != nil {
				return "Error: " + err.Error(), true
			}

			ss := &orchestrate.StartSequence{
				AgentName: name,
				Home:      d.cfg.Home,
				Version:   d.cfg.Version,
				SourceDir: d.cfg.SourceDir,
				Backend:   d.dc,
				Comms:     d.dc,
				Log:       d.log,
				CredStore: d.credStore,
			}

			result, err := ss.Run(context.Background(), func(phase int, phaseName, desc string) {
				d.log.Info("start phase", "agent", name, "phase", phase, "name", phaseName)
				d.audit.Write(name, "start_phase", map[string]interface{}{"phase": phase, "phase_name": phaseName})
			})
			if err != nil {
				d.audit.Write(name, "start_failed", map[string]interface{}{"error": err.Error()})
				return "Error: " + err.Error(), true
			}

			// Wire WebSocket client to enforcer for constraint delivery.
			d.registerEnforcerWSClient(name)
			d.audit.Write(name, "agent_started", nil)

			_ = result
			return fmt.Sprintf("Agent '%s' started.", name), false
		},
	)

	// agency_stop
	reg.Register(
		"agency_stop",
		"Stop a running agent. Three tiers: supervised (graceful), immediate (SIGTERM), emergency (SIGKILL). After stopping, use agency_resume to restart.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"agent":     map[string]interface{}{"type": "string", "description": "Agent name"},
				"halt_type": map[string]interface{}{"type": "string", "enum": []string{"supervised", "immediate", "emergency"}, "default": "supervised"},
				"reason":    map[string]interface{}{"type": "string", "description": "Reason for stopping"},
			},
			"required": []string{"agent"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			name := mapStr(args, "agent")
			if err := mcpValidateAgentName(name); err != nil {
				return fmt.Sprintf("Error: %s", err), true
			}
			if d.halt == nil {
				return "Error: halt controller not initialized", true
			}
			haltType := mapStr(args, "halt_type")
			if haltType == "" {
				haltType = "supervised"
			}
			reason := mapStr(args, "reason")
			if haltType == "emergency" && reason == "" {
				return "Error: emergency halt requires a reason (ASK Tenet 2)", true
			}
			d.unregisterEnforcerWSClient(name)
			_, err := d.halt.Halt(context.Background(), name, haltType, reason, "operator")
			if err != nil {
				return "Error: " + err.Error(), true
			}
			d.audit.Write(name, "agent_halted", map[string]interface{}{"type": haltType, "reason": reason, "initiator": "operator"})
			return fmt.Sprintf("Agent '%s' stopped.", name), false
		},
	)

	// agency_restart — stop then start with key rotation
	reg.Register(
		"agency_restart",
		"Restart a running agent (teardown + full start sequence). Equivalent to stop then start.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"agent": map[string]interface{}{"type": "string", "description": "Agent name"},
			},
			"required": []string{"agent"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			name := mapStr(args, "agent")
			if err := mcpValidateAgentName(name); err != nil {
				return fmt.Sprintf("Error: %s", err), true
			}
			if d.agents == nil {
				return "Error: agent manager not initialized", true
			}

			// Ensure agent exists and load detail for lifecycle_id wiring
			detail, err := d.agents.Show(context.Background(), name)
			if err != nil {
				return "Error: " + err.Error(), true
			}

			// Wire lifecycle_id into audit writer so all subsequent events carry it.
			d.audit.SetLifecycleID(name, detail.LifecycleID)

			// Stop existing containers and close old WS client
			d.unregisterEnforcerWSClient(name)
			d.agents.StopContainers(context.Background(), name)

			// Start with key rotation (ASK tenet 4: least privilege)
			ss := &orchestrate.StartSequence{
				AgentName:   name,
				Home:        d.cfg.Home,
				Version:     d.cfg.Version,
				SourceDir:   d.cfg.SourceDir,
				BuildID:     d.cfg.BuildID,
				Backend:     d.dc,
				Comms:       d.dc,
				Log:         d.log,
				KeyRotation: true,
				CredStore:   d.credStore,
			}

			result, err := ss.Run(context.Background(), func(phase int, phaseName, desc string) {
				d.log.Info("restart phase", "agent", name, "phase", phase, "name", phaseName)
				d.audit.Write(name, "start_phase", map[string]interface{}{
					"phase":       phase,
					"phase_name":  phaseName,
					"trigger":     "restart",
					"instance_id": d.runtimeInstanceID(context.Background(), name, "enforcer"),
					"build_id":    d.cfg.BuildID,
				})
			})
			if err != nil {
				d.audit.Write(name, "restart_failed", map[string]interface{}{"error": err.Error(), "build_id": d.cfg.BuildID})
				return "Error: " + err.Error(), true
			}

			// Re-wire WebSocket client to enforcer after restart.
			d.registerEnforcerWSClient(name)
			d.audit.Write(name, "agent_restarted", map[string]interface{}{
				"instance_id": d.runtimeInstanceID(context.Background(), name, "workspace"),
				"build_id":    d.cfg.BuildID,
			})

			_ = result
			return fmt.Sprintf("Agent '%s' restarted.", name), false
		},
	)

	// agency_resume
	reg.Register(
		"agency_resume",
		"Resume a previously halted agent. Restores state and reloads constraints. Requires: agent was previously halted. After resuming, use agency_send to deliver a new task via DM channel.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"agent": map[string]interface{}{"type": "string", "description": "Agent name"},
			},
			"required": []string{"agent"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			name := mapStr(args, "agent")
			if err := mcpValidateAgentName(name); err != nil {
				return fmt.Sprintf("Error: %s", err), true
			}
			if d.halt == nil {
				return "Error: halt controller not initialized", true
			}
			if err := d.halt.Resume(context.Background(), name, "operator"); err != nil {
				return "Error: " + err.Error(), true
			}
			d.registerEnforcerWSClient(name)
			d.audit.Write(name, "agent_resumed", map[string]interface{}{"initiator": "operator"})
			return fmt.Sprintf("Agent '%s' resumed.", name), false
		},
	)

	// agency_delete
	reg.Register(
		"agency_delete",
		"Delete an agent definition and archive its audit logs. Cannot be undone.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"agent": map[string]interface{}{"type": "string", "description": "Agent name"},
			},
			"required": []string{"agent"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			name := mapStr(args, "agent")
			if err := mcpValidateAgentName(name); err != nil {
				return fmt.Sprintf("Error: %s", err), true
			}
			if d.agents == nil {
				return "Error: agent manager not initialized", true
			}
			if err := d.agents.Delete(context.Background(), name); err != nil {
				return "Error: " + err.Error(), true
			}
			d.audit.WriteSystem("agent_deleted", map[string]interface{}{"agent": name})
			return fmt.Sprintf("Agent '%s' deleted.", name), false
		},
	)

	// agency_grant
	reg.Register(
		"agency_grant",
		"Grant a governed service capability to an agent.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"agent":   map[string]interface{}{"type": "string", "description": "Agent name"},
				"service": map[string]interface{}{"type": "string", "description": "Service name (e.g. github, brave-search)"},
				"key":     map[string]interface{}{"type": "string", "description": "API key for the service (optional if already configured)"},
			},
			"required": []string{"agent", "service"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			name := mapStr(args, "agent")
			if err := mcpValidateAgentName(name); err != nil {
				return fmt.Sprintf("Error: %s", err), true
			}
			if d.agents == nil {
				return "Error: agent manager not initialized", true
			}
			service := mapStr(args, "service")
			if service == "" {
				return "Error: service is required", true
			}

			// Verify agent exists
			if _, err := d.agents.Show(context.Background(), name); err != nil {
				return "Error: " + err.Error(), true
			}

			// Write grant to agent's constraints
			constraintsPath := filepath.Join(d.cfg.Home, "agents", name, "constraints.yaml")
			var constraints map[string]interface{}
			if data, err := os.ReadFile(constraintsPath); err == nil {
				yaml.Unmarshal(data, &constraints)
			}
			if constraints == nil {
				constraints = map[string]interface{}{}
			}

			grants, _ := constraints["granted_capabilities"].([]interface{})
			grants = append(grants, service)
			constraints["granted_capabilities"] = grants

			data, _ := yaml.Marshal(constraints)
			os.WriteFile(constraintsPath, data, 0644)

			d.log.Info("capability granted", "agent", name, "capability", service)
			d.audit.Write(name, "capability_granted", map[string]interface{}{"capability": service})
			// Hot-reload: regenerate manifest and signal enforcer
			go d.reloadCapabilitiesForRunningAgents(service)
			return fmt.Sprintf("Granted '%s' to agent '%s'.", service, name), false
		},
	)

	// agency_revoke
	reg.Register(
		"agency_revoke",
		"Revoke a governed service capability from an agent.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"agent":   map[string]interface{}{"type": "string", "description": "Agent name"},
				"service": map[string]interface{}{"type": "string", "description": "Service name"},
			},
			"required": []string{"agent", "service"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			name := mapStr(args, "agent")
			if err := mcpValidateAgentName(name); err != nil {
				return fmt.Sprintf("Error: %s", err), true
			}
			if d.agents == nil {
				return "Error: agent manager not initialized", true
			}
			service := mapStr(args, "service")
			if service == "" {
				return "Error: service is required", true
			}

			// Verify agent exists
			if _, err := d.agents.Show(context.Background(), name); err != nil {
				return "Error: " + err.Error(), true
			}

			// Remove grant from agent's constraints
			constraintsPath := filepath.Join(d.cfg.Home, "agents", name, "constraints.yaml")
			var constraints map[string]interface{}
			if data, err := os.ReadFile(constraintsPath); err == nil {
				yaml.Unmarshal(data, &constraints)
			}
			if constraints != nil {
				if grantsList, ok := constraints["granted_capabilities"].([]interface{}); ok {
					var filtered []interface{}
					for _, g := range grantsList {
						if s, ok := g.(string); ok && s != service {
							filtered = append(filtered, g)
						}
					}
					constraints["granted_capabilities"] = filtered
					data, _ := yaml.Marshal(constraints)
					os.WriteFile(constraintsPath, data, 0644)
				}
			}

			d.log.Info("capability revoked", "agent", name, "capability", service)
			d.audit.Write(name, "capability_revoked", map[string]interface{}{"capability": service})
			return fmt.Sprintf("Revoked '%s' from agent '%s'.", service, name), false
		},
	)

}

// ── Comms (7 tools) ─────────────────────────────────────────────────────────

func registerCommsTools(reg *MCPToolRegistry) {
	// agency_channel_create
	reg.Register(
		"agency_channel_create",
		"Create a communication channel for agent coordination. Requires: infrastructure running. Create channels before starting agents that need to communicate.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name":    map[string]interface{}{"type": "string", "description": "Channel name (alphanumeric, hyphens)"},
				"topic":   map[string]interface{}{"type": "string", "description": "Channel topic/purpose", "default": ""},
				"members": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Initial member names", "default": []string{}},
				"private": map[string]interface{}{"type": "boolean", "description": "Private channel (members only)", "default": false},
			},
			"required": []string{"name"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			name := mapStr(args, "name")
			if name == "" {
				return "Error: name is required", true
			}
			if !requireNameStr(name) {
				return `{"error":"invalid name"}`, false
			}
			body := map[string]interface{}{
				"name":       name,
				"type":       "team",
				"created_by": "_operator",
				"members":    []string{"_operator"},
			}
			if topic := mapStr(args, "topic"); topic != "" {
				body["topic"] = topic
			}
			if members := mapSlice(args, "members"); len(members) > 0 {
				body["members"] = members
			}
			visibility := "open"
			if mapBool(args, "private") {
				visibility = "private"
			}
			body["visibility"] = visibility

			_, err := d.dc.CommsRequest(context.Background(), "POST", "/channels", body)
			if err != nil {
				return "Error: " + err.Error(), true
			}
			return fmt.Sprintf("Channel #%s created", name), false
		},
	)

	// agency_channel_list
	reg.Register(
		"agency_channel_list",
		"List communication channels visible to the operator. Set include_archived=true to also show archived channels.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"include_archived": map[string]interface{}{"type": "boolean", "description": "Include archived channels in the listing", "default": false},
			},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			data, err := d.dc.CommsRequest(context.Background(), "GET", "/channels?member=_operator", nil)
			if err != nil {
				return "Error: " + err.Error(), true
			}
			return fmtChannelList(data), false
		},
	)

	// agency_channel_read
	reg.Register(
		"agency_channel_read",
		"Read messages from a communication channel.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"channel": map[string]interface{}{"type": "string", "description": "Channel name"},
				"limit":   map[string]interface{}{"type": "integer", "description": "Number of messages to return", "default": 20},
			},
			"required": []string{"channel"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			channel := mapStr(args, "channel")
			if channel == "" {
				return "Error: channel is required", true
			}
			limit := mapInt(args, "limit", 20)
			path := fmt.Sprintf("/channels/%s/messages?limit=%d&reader=_operator", channel, limit)
			data, err := d.dc.CommsRequest(context.Background(), "GET", path, nil)
			if err != nil {
				return "Error: " + err.Error(), true
			}
			return fmtMessages(channel, data), false
		},
	)

	// agency_channel_send
	reg.Register(
		"agency_channel_send",
		"Post a message to a communication channel as the operator. Requires: channel exists.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"channel": map[string]interface{}{"type": "string", "description": "Channel name"},
				"content": map[string]interface{}{"type": "string", "description": "Message content"},
			},
			"required": []string{"channel", "content"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			channel := mapStr(args, "channel")
			if channel == "" {
				return "Error: channel is required", true
			}
			content := mapStr(args, "content")
			if content == "" {
				return "Error: content is required", true
			}
			body := map[string]interface{}{
				"content": content,
				"author":  "_operator",
			}
			path := "/channels/" + channel + "/messages"
			_, err := d.dc.CommsRequest(context.Background(), "POST", path, body)
			if err != nil {
				return "Error: " + err.Error(), true
			}
			return fmt.Sprintf("Message sent to #%s", channel), false
		},
	)

	// agency_channel_search
	reg.Register(
		"agency_channel_search",
		"Search message history across channels.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query":   map[string]interface{}{"type": "string", "description": "Search query"},
				"channel": map[string]interface{}{"type": "string", "description": "Limit search to this channel"},
			},
			"required": []string{"query"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			query := mapStr(args, "query")
			if query == "" {
				return "Error: query is required", true
			}
			path := "/search?q=" + query + "&reader=_operator"
			if channel := mapStr(args, "channel"); channel != "" {
				path += "&channel=" + channel
			}
			data, err := d.dc.CommsRequest(context.Background(), "GET", path, nil)
			if err != nil {
				return "Error: " + err.Error(), true
			}
			return fmtSearchResults(query, data), false
		},
	)

	// agency_channel_archive
	reg.Register(
		"agency_channel_archive",
		"Archive a communication channel. Channel data is preserved but marked inactive. Useful for post-deployment review.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{"type": "string", "description": "Channel name to archive"},
			},
			"required": []string{"name"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			name := mapStr(args, "name")
			if name == "" {
				return "Error: name is required", true
			}
			if !requireNameStr(name) {
				return `{"error":"invalid name"}`, false
			}
			_, err := d.dc.CommsRequest(context.Background(), "POST", "/channels/"+name+"/archive", nil)
			if err != nil {
				return "Error: " + err.Error(), true
			}
			return fmt.Sprintf("Channel #%s archived", name), false
		},
	)

	// agency_channel_grant_access
	reg.Register(
		"agency_channel_grant_access",
		"Grant an agent access to a channel (including archived channels). Useful for post-mortem review of deployment channels.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"channel": map[string]interface{}{"type": "string", "description": "Channel name"},
				"agent":   map[string]interface{}{"type": "string", "description": "Agent name to grant access"},
			},
			"required": []string{"channel", "agent"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			channel := mapStr(args, "channel")
			agent := mapStr(args, "agent")
			if channel == "" || agent == "" {
				return "Error: channel and agent are required", true
			}
			if !requireNameStr(agent) {
				return `{"error":"invalid agent name"}`, false
			}
			body := map[string]interface{}{"agent": agent}
			_, err := d.dc.CommsRequest(context.Background(), "POST", "/channels/"+channel+"/grant-access", body)
			if err != nil {
				return "Error: " + err.Error(), true
			}
			return fmt.Sprintf("Granted %s access to #%s", agent, channel), false
		},
	)
}

// ── Observability (2 tools) ─────────────────────────────────────────────────

func registerObservabilityTools(reg *MCPToolRegistry) {
	// agency_log
	reg.Register(
		"agency_log",
		"View agent audit log events. Default: compact one-line-per-agent summary. Use verbose=true for full event log with tail/types filtering.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"agent":   map[string]interface{}{"type": "string", "description": "Agent name (omit for all agents)"},
				"since":   map[string]interface{}{"type": "string", "description": "Start time (ISO 8601)"},
				"until":   map[string]interface{}{"type": "string", "description": "End time (ISO 8601)"},
				"verbose": map[string]interface{}{"type": "boolean", "description": "Show full event log instead of summary (default: false)"},
				"tail":    map[string]interface{}{"type": "integer", "description": "Number of most recent events to show in verbose mode (default: 10)"},
				"types":   map[string]interface{}{"type": "string", "description": "Comma-separated event types to filter in verbose mode (e.g. task_delivered,halt_initiated)"},
			},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			agent := mapStr(args, "agent")
			since := mapStr(args, "since")
			until := mapStr(args, "until")

			reader := logs.NewReader(d.cfg.Home)
			var events []logs.Event
			var err error

			if agent != "" {
				if verr := mcpValidateAgentName(agent); verr != nil {
					return fmt.Sprintf("Error: %s", verr), true
				}
				events, err = reader.ReadAgentLog(agent, since, until)
			} else {
				events, err = reader.ReadAllLogs(since, until)
			}

			if err != nil {
				return "Error: " + err.Error(), true
			}

			// Limit to last 500
			if len(events) > 500 {
				events = events[len(events)-500:]
			}

			if len(events) == 0 {
				return "No events found.", false
			}

			// Convert logs.Event (map[string]interface{}) to typed slice for formatting.
			typed := make([]map[string]interface{}, len(events))
			for i, e := range events {
				typed[i] = map[string]interface{}(e)
			}

			verbose := mapBool(args, "verbose")
			if verbose {
				return fmtLogVerbose(typed, args), false
			}
			return fmtLogSummary(typed), false
		},
	)

	// agency_status
	reg.Register(
		"agency_status",
		"Show overall Agency health: infrastructure, agents, and security guarantees.",
		nil,
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			var parts []string

			// Infrastructure health
			status, err := d.dc.InfraStatus(context.Background())
			if err != nil {
				parts = append(parts, "Infrastructure: "+err.Error())
			} else if len(status) > 0 {
				var healthy, unhealthy []string
				for _, comp := range status {
					if comp.Health == "healthy" || (comp.State == "running" && (comp.Health == "" || comp.Health == "none")) {
						healthy = append(healthy, comp.Name)
					} else {
						unhealthy = append(unhealthy, fmt.Sprintf("%s (%s/%s)", comp.Name, comp.State, comp.Health))
					}
				}
				if len(unhealthy) > 0 {
					parts = append(parts, fmt.Sprintf("Infrastructure: %s FAILING. Healthy: %s.", strings.Join(unhealthy, ", "), strings.Join(healthy, ", ")))
				} else {
					parts = append(parts, fmt.Sprintf("Infrastructure: all healthy (%s).", strings.Join(healthy, ", ")))
				}
			} else {
				parts = append(parts, "Infrastructure: no components found.")
			}

			// Agents
			if d.agents != nil {
				agents, err := d.agents.List(context.Background())
				if err == nil && len(agents) > 0 {
					maps := make([]map[string]interface{}, len(agents))
					for i, a := range agents {
						maps[i] = map[string]interface{}{"name": a.Name, "status": a.Status}
					}
					parts = append(parts, fmtAgentList(maps))
				} else {
					parts = append(parts, "No agents defined.")
				}
			}

			return strings.Join(parts, "\n"), false
		},
	)

	// agency_budget_show
	reg.Register(
		"agency_budget_show",
		"Show agent budget usage — daily, monthly, and current task cost in USD.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"agent_name": map[string]interface{}{"type": "string", "description": "Agent name"},
			},
			"required": []string{"agent_name"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			name := mapStr(args, "agent_name")
			if name == "" {
				return "Error: agent_name is required", true
			}
			if err := mcpValidateAgentName(name); err != nil {
				return fmt.Sprintf("Error: %s", err), true
			}

			limits := d.budgetConfig()
			costs := loadModelCosts(d.cfg.Home)

			now := time.Now().UTC()
			todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
			monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

			todayMetrics, _ := routing.CollectWithCosts(d.cfg.Home, routing.MetricsQuery{
				Agent: name, Since: todayStart.Format(time.RFC3339),
			}, costs)
			monthMetrics, _ := routing.CollectWithCosts(d.cfg.Home, routing.MetricsQuery{
				Agent: name, Since: monthStart.Format(time.RFC3339),
			}, costs)

			dailyUsed := 0.0
			monthlyUsed := 0.0
			if todayMetrics != nil {
				dailyUsed = todayMetrics.Totals.EstCostUSD
			}
			if monthMetrics != nil {
				monthlyUsed = monthMetrics.Totals.EstCostUSD
			}

			dailyPct := 0.0
			if limits.AgentDaily > 0 {
				dailyPct = dailyUsed / limits.AgentDaily * 100
			}
			monthlyPct := 0.0
			if limits.AgentMonthly > 0 {
				monthlyPct = monthlyUsed / limits.AgentMonthly * 100
			}

			result := fmt.Sprintf("Budget for %s:\n", name)
			result += fmt.Sprintf("  Today:      $%.2f / $%.2f (%.0f%%)\n", dailyUsed, limits.AgentDaily, dailyPct)
			result += fmt.Sprintf("  This month: $%.2f / $%.2f (%.0f%%)\n", monthlyUsed, limits.AgentMonthly, monthlyPct)
			result += fmt.Sprintf("  Per-task limit: $%.2f\n", limits.PerTask)
			result += fmt.Sprintf("  Daily remaining: $%.2f\n", limits.AgentDaily-dailyUsed)
			result += fmt.Sprintf("  Monthly remaining: $%.2f\n", limits.AgentMonthly-monthlyUsed)
			return result, false
		},
	)
}
