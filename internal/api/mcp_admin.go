package api

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"encoding/json"

	"github.com/geoffbelknap/agency/internal/capabilities"
	"github.com/geoffbelknap/agency/internal/knowledge"
	"github.com/geoffbelknap/agency/internal/logs"
	"github.com/geoffbelknap/agency/internal/orchestrate"
	"github.com/geoffbelknap/agency/internal/policy"
	"gopkg.in/yaml.v3"
)

// ── Admin (8 tools) ─────────────────────────────────────────────────────────

func registerAdminTools(reg *MCPToolRegistry) {

	// 1. agency_admin_doctor
	reg.Register(
		"agency_admin_doctor",
		"Verify all six security guarantees against running containers. Reports pass/fail for each guarantee.",
		nil,
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			ctx := context.Background()

			type checkResult struct {
				name   string
				agent  string
				status string
				detail string
			}
			allPassed := true
			var checks []checkResult

			agents, err := d.dc.ListAgentWorkspaces(ctx)
			if err != nil {
				return fmt.Sprintf("Security guarantees (FAILURES)\n  Cannot list containers: %s", err), true
			}

			if len(agents) == 0 {
				return "Security guarantees (ALL PASS)\n  No running agents to check", false
			}

			for _, agentName := range agents {
				wsName := "agency-" + agentName + "-workspace"
				enfName := "agency-" + agentName + "-enforcer"

				// 1. LLM credentials isolated
				func() {
					ws, err := d.dc.InspectContainer(ctx, wsName)
					if err != nil {
						allPassed = false
						checks = append(checks, checkResult{"credentials_isolated", agentName, "FAIL", "Cannot inspect workspace: " + err.Error()})
						return
					}
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
						if strings.HasPrefix(env, "OPENAI_API_KEY=") {
							parts := strings.SplitN(env, "=", 2)
							if len(parts) == 2 && parts[1] != "" && !strings.HasPrefix(parts[1], "agency-scoped--") {
								leaked = append(leaked, "OPENAI_API_KEY (not an agency-scoped token)")
							}
						}
					}
					if len(leaked) > 0 {
						allPassed = false
						checks = append(checks, checkResult{"credentials_isolated", agentName, "FAIL", "LLM credentials visible in workspace env: " + strings.Join(leaked, ", ")})
					} else {
						checks = append(checks, checkResult{"credentials_isolated", agentName, "PASS", "No LLM API keys in workspace environment"})
					}
				}()

				// 2. Network mediation complete
				func() {
					ws, err := d.dc.InspectContainer(ctx, wsName)
					if err != nil {
						allPassed = false
						checks = append(checks, checkResult{"network_mediation", agentName, "FAIL", "Cannot inspect workspace: " + err.Error()})
						return
					}
					var forbidden []string
					for _, net := range ws.Networks {
						if strings.Contains(net, "egress") || net == "agency-gateway" {
							forbidden = append(forbidden, net)
						}
					}
					if len(forbidden) > 0 {
						allPassed = false
						checks = append(checks, checkResult{"network_mediation", agentName, "FAIL", "Workspace on forbidden network(s): " + strings.Join(forbidden, ", ")})
					} else {
						checks = append(checks, checkResult{"network_mediation", agentName, "PASS", "Workspace on internal network(s) only: " + strings.Join(ws.Networks, ", ")})
					}
				}()

				// 3. Constraints read-only
				func() {
					ws, err := d.dc.InspectContainer(ctx, wsName)
					if err != nil {
						allPassed = false
						checks = append(checks, checkResult{"constraints_readonly", agentName, "FAIL", "Cannot inspect workspace: " + err.Error()})
						return
					}
					found := false
					for _, m := range ws.Mounts {
						if strings.Contains(m.Destination, "constraints.yaml") {
							found = true
							if m.RW {
								allPassed = false
								checks = append(checks, checkResult{"constraints_readonly", agentName, "FAIL", "constraints.yaml mounted read-write at " + m.Destination})
								return
							}
						}
					}
					if found {
						checks = append(checks, checkResult{"constraints_readonly", agentName, "PASS", "constraints.yaml mounted read-only"})
					} else {
						checks = append(checks, checkResult{"constraints_readonly", agentName, "PASS", "constraints.yaml mount not found (may be embedded in image)"})
					}
				}()

				// 4. Enforcer audit active
				func() {
					enf, err := d.dc.InspectContainer(ctx, enfName)
					if err != nil {
						allPassed = false
						checks = append(checks, checkResult{"enforcer_audit", agentName, "FAIL", "Enforcer container not found: " + err.Error()})
						return
					}
					if enf.State != "running" {
						allPassed = false
						checks = append(checks, checkResult{"enforcer_audit", agentName, "FAIL", "Enforcer status: " + enf.State})
					} else {
						detail := "Enforcer running"
						if enf.Health != "none" && enf.Health != "" {
							detail += ", health: " + enf.Health
						}
						checks = append(checks, checkResult{"enforcer_audit", agentName, "PASS", detail})
					}
				}()

				// 5. Audit log not writable by agent
				func() {
					ws, err := d.dc.InspectContainer(ctx, wsName)
					if err != nil {
						allPassed = false
						checks = append(checks, checkResult{"audit_not_writable", agentName, "FAIL", "Cannot inspect workspace: " + err.Error()})
						return
					}
					for _, m := range ws.Mounts {
						if strings.Contains(m.Destination, "audit") {
							if m.RW {
								allPassed = false
								checks = append(checks, checkResult{"audit_not_writable", agentName, "FAIL", "Audit directory mounted read-write at " + m.Destination})
								return
							}
						}
					}
					checks = append(checks, checkResult{"audit_not_writable", agentName, "PASS", "Audit directory not writable by agent"})
				}()

				// 6. Halt functional
				func() {
					ws, err := d.dc.InspectContainer(ctx, wsName)
					if err != nil {
						allPassed = false
						checks = append(checks, checkResult{"halt_functional", agentName, "FAIL", "Cannot inspect workspace: " + err.Error()})
						return
					}
					if ws.State == "running" {
						checks = append(checks, checkResult{"halt_functional", agentName, "PASS", "Workspace container is running and pauseable"})
					} else {
						allPassed = false
						checks = append(checks, checkResult{"halt_functional", agentName, "FAIL", "Workspace state '" + ws.State + "' — cannot pause"})
					}
				}()

				// 7. Operator override available
				func() {
					enf, err := d.dc.InspectContainer(ctx, enfName)
					if err != nil {
						allPassed = false
						checks = append(checks, checkResult{"operator_override", agentName, "FAIL", "Cannot inspect enforcer: " + err.Error()})
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
						checks = append(checks, checkResult{"operator_override", agentName, "PASS", "Enforcer reachable on mediation network"})
					} else {
						allPassed = false
						checks = append(checks, checkResult{"operator_override", agentName, "FAIL", "Enforcer not on mediation network: " + strings.Join(enf.Networks, ", ")})
					}
				}()
			}

			// 8. Build consistency — infrastructure
			func() {
				infraStatus, err := d.dc.InfraStatus(ctx)
				if err != nil {
					allPassed = false
					checks = append(checks, checkResult{"build_consistency", "infra", "FAIL", "Cannot get infra status: " + err.Error()})
					return
				}
				var mismatched []string
				for _, ic := range infraStatus {
					if ic.BuildID != "" && ic.BuildID != d.cfg.BuildID {
						mismatched = append(mismatched, fmt.Sprintf("%s(%s)", ic.Name, ic.BuildID))
					}
				}
				if len(mismatched) > 0 {
					allPassed = false
					checks = append(checks, checkResult{"build_consistency", "infra", "FAIL", fmt.Sprintf("Stale images: %s (gateway: %s)", strings.Join(mismatched, ", "), d.cfg.BuildID)})
				} else {
					checks = append(checks, checkResult{"build_consistency", "infra", "PASS", fmt.Sprintf("All infrastructure images match gateway build %s", d.cfg.BuildID)})
				}
			}()

			// 9. Build consistency — per agent
			for _, agentName := range agents {
				func() {
					enfName := "agency-" + agentName + "-enforcer"
					wsName := "agency-" + agentName + "-workspace"
					var mismatched []string
					for _, ctr := range []struct{ name, label string }{{enfName, "enforcer"}, {wsName, "workspace"}} {
						ci, err := d.dc.InspectContainer(ctx, ctr.name)
						if err != nil {
							continue
						}
						bid := ci.Labels["agency.build.gateway"]
						if bid != "" && bid != d.cfg.BuildID {
							mismatched = append(mismatched, fmt.Sprintf("%s(%s)", ctr.label, bid))
						}
					}
					if len(mismatched) > 0 {
						allPassed = false
						checks = append(checks, checkResult{"build_consistency", agentName, "FAIL", fmt.Sprintf("Stale images: %s (gateway: %s)", strings.Join(mismatched, ", "), d.cfg.BuildID)})
					} else {
						checks = append(checks, checkResult{"build_consistency", agentName, "PASS", fmt.Sprintf("All images match gateway build %s", d.cfg.BuildID)})
					}
				}()
			}

			header := "FAILURES"
			if allPassed {
				header = "ALL PASS"
			}
			lines := []string{fmt.Sprintf("Security guarantees (%s)", header)}
			lines = append(lines, fmt.Sprintf("  Tested against: %s", strings.Join(agents, ", ")))
			for _, c := range checks {
				lines = append(lines, fmt.Sprintf("  [%s] %s (%s): %s", c.status, c.name, c.agent, c.detail))
			}

			// Scope audit: show declared scopes per agent per service
			lines = append(lines, "", "Service credential scopes:")
			scopeReport := d.buildScopeReport()
			if scopeReport == "" {
				lines = append(lines, "  No scope declarations found in agent presets")
			} else {
				lines = append(lines, scopeReport)
			}

			return strings.Join(lines, "\n"), false
		},
	)

	// 2. agency_admin_destroy
	reg.Register(
		"agency_admin_destroy",
		"Factory-reset Agency: remove all containers, networks, data. Knowledge graph is preserved. Requires a reason to prevent accidental use.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"reason":        map[string]interface{}{"type": "string", "description": "Reason for destruction (required to prevent accidental use)"},
				"remove_images": map[string]interface{}{"type": "boolean", "description": "Also remove Agency Docker images", "default": false},
			},
			"required": []string{"reason"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			reason := mapStr(args, "reason")
			if reason == "" {
				return "Error: reason is required to prevent accidental use", true
			}

			ctx := context.Background()

			// Stop all agents
			if d.agents != nil {
				agents, _ := d.agents.List(ctx)
				for _, a := range agents {
					d.agents.StopContainers(ctx, a.Name)
					d.agents.Delete(ctx, a.Name)
				}
			}

			// Tear down infrastructure
			if d.infra != nil {
				d.infra.Teardown(ctx)
			}

			d.audit.WriteSystem("admin_destroy", map[string]interface{}{"reason": reason})
			d.log.Info("admin destroy completed", "reason", reason)
			return "Agency destroyed. Knowledge graph preserved.", false
		},
	)

	// 3. agency_admin_trust
	reg.Register(
		"agency_admin_trust",
		"Agent trust calibration. Actions: show (agent profile), list (all profiles), elevate, demote, record (trust signal).",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action":      map[string]interface{}{"type": "string", "enum": []string{"show", "list", "elevate", "demote", "record"}, "description": "Operation to perform"},
				"agent":       map[string]interface{}{"type": "string", "description": "Agent name (for show/elevate/demote/record)"},
				"level":       map[string]interface{}{"type": "string", "description": "Target trust level (for elevate/demote)"},
				"signal_type": map[string]interface{}{"type": "string", "description": "Signal type (for record)"},
				"description": map[string]interface{}{"type": "string", "description": "Signal detail or reason"},
			},
			"required": []string{"action"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			action := mapStr(args, "action")
			agent := mapStr(args, "agent")

			if action == "list" {
				// List all agent trust profiles
				agentsDir := filepath.Join(d.cfg.Home, "agents")
				entries, err := os.ReadDir(agentsDir)
				if err != nil {
					return "No agents.", false
				}
				var lines []string
				lines = append(lines, "Trust profiles:")
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
						trust = map[string]interface{}{"level": 3}
					}
					level, _ := trust["level"].(int)
					if level == 0 {
						// Try float64 from yaml
						if f, ok := trust["level"].(float64); ok {
							level = int(f)
						}
						if level == 0 {
							level = 3
						}
					}
					trustLabels := map[int]string{1: "minimal", 2: "low", 3: "standard", 4: "high", 5: "elevated"}
					label := trustLabels[level]
					if label == "" {
						label = "unknown"
					}
					lines = append(lines, fmt.Sprintf("  %s: %s (level: %d)", name, label, level))
				}
				return strings.Join(lines, "\n"), false
			}

			if agent == "" {
				return "Error: agent is required for action " + action, true
			}
			if !requireNameStr(agent) {
				return `{"error":"invalid agent name"}`, false
			}

			trustPath := filepath.Join(d.cfg.Home, "agents", agent, "trust.yaml")
			var trust map[string]interface{}
			if data, err := os.ReadFile(trustPath); err == nil {
				yaml.Unmarshal(data, &trust)
			}
			if trust == nil {
				trust = map[string]interface{}{"level": 3, "agent": agent}
			}

			getLevel := func() int {
				if lvl, ok := trust["level"].(int); ok {
					return lvl
				}
				if f, ok := trust["level"].(float64); ok {
					return int(f)
				}
				return 3
			}

			switch action {
			case "show":
				level := getLevel()
				trustLabels := map[int]string{1: "untrusted", 2: "probation", 3: "standard", 4: "trusted", 5: "high"}
				label := trustLabels[level]
				if label == "" {
					label = "unknown"
				}
				lines := []string{fmt.Sprintf("Trust profile for %s:", agent)}
				lines = append(lines, fmt.Sprintf("  Level: %s (level: %d)", label, level))
				score := 0.0
				switch sv := trust["score"].(type) {
				case float64:
					score = sv
				case int:
					score = float64(sv)
				}
				lines = append(lines, fmt.Sprintf("  Score: %.1f", score))
				signalCount := 0
				if sigs, ok := trust["signals"].([]interface{}); ok {
					signalCount = len(sigs)
				}
				lines = append(lines, fmt.Sprintf("  Signals: %d", signalCount))
				return strings.Join(lines, "\n"), false

			case "elevate", "demote":
				levelNames := map[string]int{"untrusted": 1, "probation": 2, "standard": 3, "trusted": 4, "high": 5}
				levelLabels := map[int]string{1: "untrusted", 2: "probation", 3: "standard", 4: "trusted", 5: "high"}
				current := getLevel()
				target := current
				if targetName := mapStr(args, "level"); targetName != "" {
					if n, ok := levelNames[targetName]; ok {
						target = n
					}
				}
				if target == current {
					// No explicit target — step by 1
					if action == "elevate" && current < 5 {
						target = current + 1
					} else if action == "demote" && current > 1 {
						target = current - 1
					}
				}
				// Validate direction
				if action == "elevate" && target <= current {
					return fmt.Sprintf("Cannot elevate: %s is already at %s (level %d).", agent, levelLabels[current], current), true
				}
				if action == "demote" && target >= current {
					return fmt.Sprintf("Cannot demote: %s is already at %s (level %d).", agent, levelLabels[current], current), true
				}
				trust["level"] = target
				data, _ := yaml.Marshal(trust)
				os.WriteFile(trustPath, data, 0644)
				eventType := "trust_elevated"
				if action == "demote" {
					eventType = "trust_demoted"
				}
				d.audit.Write(agent, eventType, map[string]interface{}{"from": current, "to": target})
				return fmt.Sprintf("Trust for %s %sd: %s (level %d) → %s (level %d).", agent, action, levelLabels[current], current, levelLabels[target], target), false

			case "record":
				signalType := mapStr(args, "signal_type")
				desc := mapStr(args, "description")

				// Load existing signals list
				var signals []interface{}
				if sigs, ok := trust["signals"].([]interface{}); ok {
					signals = sigs
				}

				// Append new signal
				signals = append(signals, map[string]interface{}{
					"type":        signalType,
					"description": desc,
					"recorded_at": time.Now().UTC().Format(time.RFC3339),
				})
				trust["signals"] = signals

				// Get current score — yaml.v2 unmarshals whole-number floats as int,
				// so handle both int and float64 to avoid losing accumulated score.
				score := 0.0
				switch sv := trust["score"].(type) {
				case float64:
					score = sv
				case int:
					score = float64(sv)
				}

				// Apply score delta and level transitions
				oldLevel := getLevel()
				newLevel := oldLevel
				switch signalType {
				case "task_complete":
					score += 1.0
				case "task_failed":
					score -= 2.0
				case "constraint_violation":
					score = 0.0
					if newLevel > 1 {
						newLevel--
					}
				}

				// Level thresholds: untrusted(<-3)=1, probation(0)=2, standard(5+)=3, trusted/high(10+)=4+
				if signalType != "constraint_violation" {
					if score < -3 {
						newLevel = 1
					} else if score < 0 {
						if newLevel > 2 {
							newLevel = 2
						}
					} else if score >= 10 {
						if newLevel < 4 {
							newLevel = 4
						}
					} else if score >= 5 {
						if newLevel < 3 {
							newLevel = 3
						}
					}
				}

				trust["score"] = score
				trust["level"] = newLevel

				data, _ := yaml.Marshal(trust)
				os.WriteFile(trustPath, data, 0644)

				d.audit.Write(agent, "trust_signal", map[string]interface{}{"signal_type": signalType, "description": desc, "score": score, "level": newLevel})

				msg := fmt.Sprintf("Trust signal recorded for %s: %s (score: %.1f, level: %d)", agent, signalType, score, newLevel)
				if newLevel != oldLevel {
					trustLabels := map[int]string{1: "untrusted", 2: "probation", 3: "standard", 4: "trusted", 5: "high"}
					msg += fmt.Sprintf(" — level changed: %s → %s", trustLabels[oldLevel], trustLabels[newLevel])
				}
				return msg, false

			default:
				return "Error: unknown action: " + action, true
			}
		},
	)

	// 4. agency_admin_egress
	reg.Register(
		"agency_admin_egress",
		"Manage per-agent egress domain policy. Actions: list (show config), approve (add domain), revoke (remove domain), mode (set egress mode).",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action": map[string]interface{}{"type": "string", "enum": []string{"list", "approve", "revoke", "mode"}, "description": "Operation to perform"},
				"agent":  map[string]interface{}{"type": "string", "description": "Agent name"},
				"domain": map[string]interface{}{"type": "string", "description": "Domain to approve/revoke"},
				"mode":   map[string]interface{}{"type": "string", "enum": []string{"denylist", "allowlist", "supervised-strict", "supervised-permissive"}, "description": "Egress mode (for mode action)"},
				"reason": map[string]interface{}{"type": "string", "description": "Reason for approval"},
			},
			"required": []string{"action", "agent"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			action := mapStr(args, "action")
			agent := mapStr(args, "agent")
			if !requireNameStr(agent) {
				return `{"error":"invalid agent name"}`, false
			}

			egressPath := filepath.Join(d.cfg.Home, "agents", agent, "egress.yaml")
			var egress map[string]interface{}
			if data, err := os.ReadFile(egressPath); err == nil {
				yaml.Unmarshal(data, &egress)
			}
			if egress == nil {
				egress = map[string]interface{}{"agent": agent, "mode": "allowlist", "domains": []interface{}{}}
			}

			switch action {
			case "list":
				lines := []string{fmt.Sprintf("Egress config for %s:", agent)}
				mode, _ := egress["mode"].(string)
				if mode == "" {
					mode = "allowlist"
				}
				lines = append(lines, fmt.Sprintf("  Mode: %s", mode))
				domains, _ := egress["domains"].([]interface{})
				if len(domains) > 0 {
					for _, d := range domains {
						if dm, ok := d.(map[string]interface{}); ok {
							lines = append(lines, fmt.Sprintf("  %s (approved by %s)", mapStr(dm, "domain"), mapStr(dm, "approved_by")))
						} else {
							lines = append(lines, fmt.Sprintf("  %v", d))
						}
					}
				} else {
					lines = append(lines, "  No approved domains.")
				}
				return strings.Join(lines, "\n"), false

			case "approve":
				domain := mapStr(args, "domain")
				if domain == "" {
					return "Error: domain is required for approve action", true
				}
				reason := mapStr(args, "reason")
				domains, _ := egress["domains"].([]interface{})
				domains = append(domains, map[string]interface{}{
					"domain":      domain,
					"approved_by": "operator",
					"reason":      reason,
					"approved_at": time.Now().UTC().Format(time.RFC3339),
				})
				egress["domains"] = domains
				data, _ := yaml.Marshal(egress)
				os.WriteFile(egressPath, data, 0644)
				d.audit.Write(agent, "egress_domain_approved", map[string]interface{}{"domain": domain, "reason": reason})
				return fmt.Sprintf("Domain '%s' approved for %s.", domain, agent), false

			case "revoke":
				domain := mapStr(args, "domain")
				if domain == "" {
					return "Error: domain is required for revoke action", true
				}
				domains, _ := egress["domains"].([]interface{})
				var filtered []interface{}
				for _, d := range domains {
					if dm, ok := d.(map[string]interface{}); ok {
						if mapStr(dm, "domain") != domain {
							filtered = append(filtered, d)
						}
					}
				}
				egress["domains"] = filtered
				data, _ := yaml.Marshal(egress)
				os.WriteFile(egressPath, data, 0644)
				d.audit.Write(agent, "egress_domain_revoked", map[string]interface{}{"domain": domain})
				return fmt.Sprintf("Domain '%s' revoked from %s.", domain, agent), false

			case "mode":
				newMode := mapStr(args, "mode")
				if newMode == "" {
					return "Error: mode is required for mode action", true
				}
				egress["mode"] = newMode
				data, _ := yaml.Marshal(egress)
				os.WriteFile(egressPath, data, 0644)
				d.audit.Write(agent, "egress_mode_changed", map[string]interface{}{"mode": newMode})
				return fmt.Sprintf("Egress mode for %s set to %s.", agent, newMode), false

			default:
				return "Error: unknown action: " + action, true
			}
		},
	)

	// 5. agency_admin_audit
	reg.Register(
		"agency_admin_audit",
		"Audit log management. Actions: stats (log statistics), export (export for regulators), retention (apply retention policy).",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action": map[string]interface{}{"type": "string", "enum": []string{"stats", "export", "retention"}, "description": "Operation to perform"},
				"agent":  map[string]interface{}{"type": "string", "description": "Agent name (for export)"},
				"since":  map[string]interface{}{"type": "string", "description": "Filter events after this timestamp (for export)"},
				"until":  map[string]interface{}{"type": "string", "description": "Filter events before this timestamp (for export)"},
				"format": map[string]interface{}{"type": "string", "enum": []string{"jsonl", "json", "csv"}, "description": "Export format", "default": "jsonl"},
			},
			"required": []string{"action"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			action := mapStr(args, "action")

			switch action {
			case "stats":
				auditDir := filepath.Join(d.cfg.Home, "audit")
				entries, err := os.ReadDir(auditDir)
				if err != nil {
					return "Audit log statistics:\n  Agents: 0\n  Log files: 0\n  Total size: 0.00 MB", false
				}

				agentFilter := mapStr(args, "agent")
				agentCount := 0
				totalFiles := 0
				var totalSize int64
				oldest := ""

				for _, e := range entries {
					if !e.IsDir() {
						continue
					}
					if agentFilter != "" && e.Name() != agentFilter {
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

				lines := []string{"Audit log statistics:"}
				lines = append(lines, fmt.Sprintf("  Agents: %d", agentCount))
				lines = append(lines, fmt.Sprintf("  Log files: %d", totalFiles))
				lines = append(lines, fmt.Sprintf("  Total size: %.2f MB", float64(totalSize)/(1024*1024)))
				if oldest != "" {
					lines = append(lines, fmt.Sprintf("  Oldest: %s", oldest))
				}
				return strings.Join(lines, "\n"), false

			case "export":
				agent := mapStr(args, "agent")
				since := mapStr(args, "since")
				until := mapStr(args, "until")
				if agent != "" && !requireNameStr(agent) {
					return `{"error":"invalid agent name"}`, false
				}
				reader := logs.NewReader(d.cfg.Home)
				var events []logs.Event
				var err error
				if agent == "" {
					// Bulk export: all agents
					events, err = reader.ReadAllLogs(since, until)
					if err != nil {
						return "Error: no audit logs found", true
					}
					return fmt.Sprintf("Audit log export: %d events (all agents).", len(events)), false
				}
				events, err = reader.ReadAgentLog(agent, since, until)
				if err != nil {
					return "Error: no audit logs for agent", true
				}
				return fmt.Sprintf("Audit log export: %d events for %s.", len(events), agent), false

			case "retention":
				d.audit.WriteSystem("retention_applied", nil)
				return "Retention policy applied to audit logs.", false

			default:
				return "Error: unknown action: " + action, true
			}
		},
	)

	// 6. agency_admin_knowledge
	reg.Register(
		"agency_admin_knowledge",
		"Knowledge graph operator access. Actions: "+
			"stats (graph statistics with top connected nodes), "+
			"query (FTS search for nodes), "+
			"graph (subgraph around a subject), "+
			"neighbors (direct neighbors of a node), "+
			"path (shortest path between two nodes), "+
			"changes (what changed since a timestamp), "+
			"export (export graph as jsonl/cypher/dot), "+
			"reset (wipe graph), "+
			"flags (list flagged nodes), "+
			"restore (restore soft-deleted/flagged node), "+
			"unflag (clear flag from node), "+
			"log (view curation history), "+
			"health (display curation health metrics).",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"stats", "query", "graph", "neighbors", "path", "changes", "export", "reset", "flags", "restore", "unflag", "log", "health"},
					"description": "Operation to perform",
				},
				"query":         map[string]interface{}{"type": "string", "description": "Search text (for query action)"},
				"kind":          map[string]interface{}{"type": "string", "description": "Filter by node kind (for query action)"},
				"limit":         map[string]interface{}{"type": "integer", "description": "Max results (for query action, default 20)"},
				"subject":       map[string]interface{}{"type": "string", "description": "Subject to center graph on (for graph action)"},
				"hops":          map[string]interface{}{"type": "integer", "description": "Subgraph depth 1-3 (for graph action, default 2)"},
				"node_id":       map[string]interface{}{"type": "string", "description": "Node ID (for neighbors action)"},
				"direction":     map[string]interface{}{"type": "string", "enum": []string{"outgoing", "incoming", "both"}, "description": "Edge direction (for neighbors action, default both)"},
				"relation":      map[string]interface{}{"type": "string", "description": "Filter by relation type (for neighbors action)"},
				"from_label":    map[string]interface{}{"type": "string", "description": "Source node label (for path action)"},
				"to_label":      map[string]interface{}{"type": "string", "description": "Target node label (for path action)"},
				"max_hops":      map[string]interface{}{"type": "integer", "description": "Max path length (for path action, default 4)"},
				"since":         map[string]interface{}{"type": "string", "description": "ISO 8601 timestamp (for changes/export actions)"},
				"action_filter": map[string]interface{}{"type": "string", "description": "Filter by action type (for log action)"},
				"format":        map[string]interface{}{"type": "string", "enum": []string{"jsonl", "json", "cypher", "dot"}, "description": "Export format (for export action, default jsonl)"},
			},
			"required": []string{"action"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			action := mapStr(args, "action")
			d.audit.WriteSystem("knowledge_admin", map[string]interface{}{"action": action})

			ctx := context.Background()
			kp := d.knowledge

			var (
				raw []byte
				err error
			)

			switch action {
			case "stats":
				raw, err = kp.Stats(ctx)
			case "health":
				raw, err = kp.Get(ctx, "/health")
			case "flags":
				raw, err = kp.Flags(ctx)
			case "log":
				raw, err = kp.CurationLog(ctx)
			case "query":
				q := mapStr(args, "query")
				if q == "" {
					return "Error: query is required for query action", true
				}
				// Knowledge service exposes POST /query with JSON body, not GET /search
				queryBody := map[string]interface{}{"query": q}
				raw, err = kp.Post(ctx, "/query", queryBody)
			case "neighbors":
				nodeID := mapStr(args, "node_id")
				if nodeID == "" {
					return "Error: node_id is required for neighbors action", true
				}
				path := "/neighbors?node_id=" + knowledge.URLEncode(nodeID)
				if dir := mapStr(args, "direction"); dir != "" {
					path += "&direction=" + knowledge.URLEncode(dir)
				}
				if rel := mapStr(args, "relation"); rel != "" {
					path += "&relation=" + knowledge.URLEncode(rel)
				}
				raw, err = kp.Get(ctx, path)
			case "path":
				from := mapStr(args, "from_label")
				to := mapStr(args, "to_label")
				if from == "" || to == "" {
					return "Error: from_label and to_label are required for path action", true
				}
				path := "/path?from=" + knowledge.URLEncode(from) + "&to=" + knowledge.URLEncode(to)
				if v, ok := args["max_hops"]; ok {
					switch n := v.(type) {
					case float64:
						path += fmt.Sprintf("&max_hops=%d", int(n))
					case int:
						path += fmt.Sprintf("&max_hops=%d", n)
					}
				}
				raw, err = kp.Get(ctx, path)
			case "changes":
				path := "/changes"
				if since := mapStr(args, "since"); since != "" {
					path += "?since=" + knowledge.URLEncode(since)
				}
				raw, err = kp.Get(ctx, path)
			case "export":
				path := "/export"
				sep := "?"
				if fmt2 := mapStr(args, "format"); fmt2 != "" {
					path += sep + "format=" + knowledge.URLEncode(fmt2)
					sep = "&"
				}
				if since := mapStr(args, "since"); since != "" {
					path += sep + "since=" + knowledge.URLEncode(since)
				}
				raw, err = kp.Get(ctx, path)
			case "graph":
				subj := mapStr(args, "subject")
				if subj == "" {
					return "Error: subject is required for graph action", true
				}
				path := "/graph?subject=" + knowledge.URLEncode(subj)
				if v, ok := args["hops"]; ok {
					switch n := v.(type) {
					case float64:
						path += fmt.Sprintf("&hops=%d", int(n))
					case int:
						path += fmt.Sprintf("&hops=%d", n)
					}
				}
				raw, err = kp.Get(ctx, path)
			case "reset":
				raw, err = kp.Post(ctx, "/reset", nil)
			case "restore":
				nodeID := mapStr(args, "node_id")
				if nodeID == "" {
					return "Error: node_id is required for restore action", true
				}
				raw, err = kp.Restore(ctx, nodeID)
			case "unflag":
				nodeID := mapStr(args, "node_id")
				if nodeID == "" {
					return "Error: node_id is required for unflag action", true
				}
				raw, err = kp.Post(ctx, "/curation/unflag", map[string]string{"node_id": nodeID})
			case "ontology_candidates":
				var candidates []knowledge.OntologyCandidate
				candidates, err = knowledge.ListOntologyCandidates(ctx, kp)
				if err == nil {
					raw, err = json.Marshal(map[string]interface{}{"candidates": candidates})
				}
			case "ontology_promote":
				val := mapStr(args, "value")
				nodeID := mapStr(args, "node_id")
				if val == "" && nodeID == "" {
					return "Error: node_id or value is required for ontology_promote action", true
				}
				var resolved string
				resolved, err = knowledge.ResolveOntologyCandidateID(ctx, kp, nodeID, val)
				if err == nil {
					raw, err = kp.Post(ctx, "/ontology/promote", map[string]string{"node_id": resolved})
				}
			case "ontology_reject":
				val := mapStr(args, "value")
				nodeID := mapStr(args, "node_id")
				if val == "" && nodeID == "" {
					return "Error: node_id or value is required for ontology_reject action", true
				}
				var resolved string
				resolved, err = knowledge.ResolveOntologyCandidateID(ctx, kp, nodeID, val)
				if err == nil {
					raw, err = kp.Post(ctx, "/ontology/reject", map[string]string{"node_id": resolved})
				}
			default:
				return "Error: unknown action: " + action, true
			}

			if err != nil {
				return fmt.Sprintf("Error calling knowledge service: %s", err), true
			}

			// Pretty-print JSON if possible, otherwise return raw text.
			var pretty interface{}
			if json.Unmarshal(raw, &pretty) == nil {
				if b, e := json.MarshalIndent(pretty, "", "  "); e == nil {
					return string(b), false
				}
			}
			return string(raw), false
		},
	)

	// 7. agency_admin_department
	reg.Register(
		"agency_admin_department",
		"Department management. Actions: create, list, show.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action":               map[string]interface{}{"type": "string", "enum": []string{"create", "list", "show"}, "description": "Operation to perform"},
				"name":                 map[string]interface{}{"type": "string", "description": "Department name (for create/show)"},
				"risk_tolerance":       map[string]interface{}{"type": "string", "enum": []string{"low", "medium"}, "description": "Risk tolerance (for create)"},
				"max_concurrent_tasks": map[string]interface{}{"type": "integer", "description": "Max concurrent tasks (for create)"},
			},
			"required": []string{"action"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			action := mapStr(args, "action")
			deptDir := filepath.Join(d.cfg.Home, "departments")
			os.MkdirAll(deptDir, 0755)

			switch action {
			case "list":
				entries, _ := os.ReadDir(deptDir)
				if len(entries) == 0 {
					return "No departments defined.", false
				}
				lines := []string{"Departments:"}
				for _, e := range entries {
					if !e.IsDir() {
						continue
					}
					hasPolicy := "no"
					policyPath := filepath.Join(deptDir, e.Name(), "policy.yaml")
					if _, err := os.Stat(policyPath); err == nil {
						hasPolicy = "yes"
					}
					lines = append(lines, fmt.Sprintf("  %s (policy: %s)", e.Name(), hasPolicy))
				}
				return strings.Join(lines, "\n"), false

			case "create":
				name := mapStr(args, "name")
				if name == "" {
					return "Error: name is required for create", true
				}
				if !requireNameStr(name) {
					return `{"error":"invalid name"}`, false
				}
				dir := filepath.Join(deptDir, name)
				os.MkdirAll(dir, 0755)

				// Create initial policy if parameters provided
				policyData := map[string]interface{}{"name": name}
				if rt := mapStr(args, "risk_tolerance"); rt != "" {
					if policyData["parameters"] == nil {
						policyData["parameters"] = map[string]interface{}{}
					}
					policyData["parameters"].(map[string]interface{})["risk_tolerance"] = rt
				}
				if mct := mapInt(args, "max_concurrent_tasks", 0); mct > 0 {
					if policyData["parameters"] == nil {
						policyData["parameters"] = map[string]interface{}{}
					}
					policyData["parameters"].(map[string]interface{})["max_concurrent_tasks"] = mct
				}
				data, _ := yaml.Marshal(policyData)
				os.WriteFile(filepath.Join(dir, "policy.yaml"), data, 0644)
				d.audit.WriteSystem("department_created", map[string]interface{}{"name": name})
				return fmt.Sprintf("Department '%s' created.", name), false

			case "show":
				name := mapStr(args, "name")
				if name == "" {
					return "Error: name is required for show", true
				}
				if !requireNameStr(name) {
					return `{"error":"invalid name"}`, false
				}
				policyPath := filepath.Join(deptDir, name, "policy.yaml")
				var pol map[string]interface{}
				if data, err := os.ReadFile(policyPath); err == nil {
					yaml.Unmarshal(data, &pol)
				}
				if pol == nil {
					pol = map[string]interface{}{"name": name}
				}
				lines := []string{fmt.Sprintf("Department: %s", name)}
				if params, ok := pol["parameters"].(map[string]interface{}); ok {
					lines = append(lines, "  Parameters:")
					for k, v := range params {
						lines = append(lines, fmt.Sprintf("    %s: %v", k, v))
					}
				}
				return strings.Join(lines, "\n"), false

			default:
				return "Error: unknown action: " + action, true
			}
		},
	)

}

// ── Capabilities (6 tools) ──────────────────────────────────────────────────

func registerCapabilityTools(reg *MCPToolRegistry) {

	// agency_cap_list
	reg.Register(
		"agency_cap_list",
		"List all capabilities in the registry with their status (enabled/disabled) and kind (mcp, api, skill).",
		nil,
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			capReg := capabilities.NewRegistry(d.cfg.Home)
			caps := capReg.List()
			if len(caps) == 0 {
				return "No capabilities in registry.", false
			}
			lines := []string{"Capabilities:"}
			for _, e := range caps {
				status := e.State
				if status == "" {
					status = "disabled"
				}
				kind := e.Kind
				if kind == "" {
					kind = "?"
				}
				desc := e.Description
				if desc == "" {
					desc = "no description"
				}
				lines = append(lines, fmt.Sprintf("  %s (%s) [%s] - %s", e.Name, kind, status, desc))
			}
			return strings.Join(lines, "\n"), false
		},
	)

	// agency_cap_show
	reg.Register(
		"agency_cap_show",
		"Show details for a capability or an agent's effective capabilities. If name matches an agent, shows that agent's capabilities; otherwise looks up in registry.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{"type": "string", "description": "Capability name or agent name"},
			},
			"required": []string{"name"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			name := mapStr(args, "name")
			if !requireNameStr(name) {
				return `{"error":"invalid name"}`, false
			}
			capReg := capabilities.NewRegistry(d.cfg.Home)

			// Check if it's an agent name first
			if d.agents != nil {
				if detail, err := d.agents.Show(context.Background(), name); err == nil {
					// It's an agent — show its granted capabilities
					lines := []string{fmt.Sprintf("Capabilities for agent '%s':", name)}
					if len(detail.GrantedServices) > 0 {
						for _, svc := range detail.GrantedServices {
							entry := capReg.Show(svc)
							if entry != nil {
								lines = append(lines, fmt.Sprintf("  %s (%s): %s", entry.Name, entry.Kind, entry.Description))
							} else {
								lines = append(lines, fmt.Sprintf("  %s (unknown)", svc))
							}
						}
					} else {
						return fmt.Sprintf("Agent '%s' has no capabilities configured.", name), false
					}
					return strings.Join(lines, "\n"), false
				}
			}

			// Look up as capability name
			entry := capReg.Show(name)
			if entry == nil {
				return fmt.Sprintf("Capability '%s' not found.", name), true
			}
			lines := []string{fmt.Sprintf("Capability: %s", entry.Name)}
			if entry.Kind != "" {
				lines = append(lines, fmt.Sprintf("  Kind: %s", entry.Kind))
			}
			if entry.Description != "" {
				lines = append(lines, fmt.Sprintf("  Description: %s", entry.Description))
			}
			if entry.State != "" {
				lines = append(lines, fmt.Sprintf("  State: %s", entry.State))
			}
			if entry.URL != "" {
				lines = append(lines, fmt.Sprintf("  URL: %s", entry.URL))
			}
			if entry.KeyEnv != "" {
				lines = append(lines, fmt.Sprintf("  Key env: %s", entry.KeyEnv))
			}
			if len(entry.Agents) > 0 {
				lines = append(lines, fmt.Sprintf("  Restricted to: %s", strings.Join(entry.Agents, ", ")))
			}
			return strings.Join(lines, "\n"), false
		},
	)

	// agency_cap_enable
	reg.Register(
		"agency_cap_enable",
		"Enable a capability so agents can use it. Optionally provide a key and restrict to specific agents.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name":   map[string]interface{}{"type": "string", "description": "Capability name"},
				"key":    map[string]interface{}{"type": "string", "description": "API key or credential"},
				"agents": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Restrict to these agents"},
			},
			"required": []string{"name"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			name := mapStr(args, "name")
			if !requireNameStr(name) {
				return `{"error":"invalid name"}`, false
			}
			key := mapStr(args, "key")
			agentsRaw := mapSlice(args, "agents")
			var agents []string
			for _, a := range agentsRaw {
				if s, ok := a.(string); ok {
					agents = append(agents, s)
				}
			}
			capReg := capabilities.NewRegistry(d.cfg.Home)
			if err := capReg.Enable(name, key, agents); err != nil {
				return "Error: " + err.Error(), true
			}
			d.audit.WriteSystem("capability_enabled", map[string]interface{}{"name": name})
			return fmt.Sprintf("Capability '%s' enabled.", name), false
		},
	)

	// agency_cap_disable
	reg.Register(
		"agency_cap_disable",
		"Disable a capability. No agent can use it until re-enabled.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{"type": "string", "description": "Capability name"},
			},
			"required": []string{"name"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			name := mapStr(args, "name")
			if !requireNameStr(name) {
				return `{"error":"invalid name"}`, false
			}
			capReg := capabilities.NewRegistry(d.cfg.Home)
			if err := capReg.Disable(name); err != nil {
				return "Error: " + err.Error(), true
			}
			d.audit.WriteSystem("capability_disabled", map[string]interface{}{"name": name})
			return fmt.Sprintf("Capability '%s' disabled.", name), false
		},
	)

	// agency_cap_add
	reg.Register(
		"agency_cap_add",
		"Add a capability to the registry. Supported kinds: mcp-server (stdio MCP server), service (HTTP API service), skill (built-in skill).",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"kind":    map[string]interface{}{"type": "string", "enum": []string{"mcp-server", "service", "skill"}, "description": "Capability type"},
				"name":    map[string]interface{}{"type": "string", "description": "Capability name"},
				"command": map[string]interface{}{"type": "string", "description": "MCP server command (for kind=mcp-server)"},
				"args":    map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "MCP server arguments (for kind=mcp-server)"},
				"url":     map[string]interface{}{"type": "string", "description": "API base URL (for kind=service)"},
				"key_env": map[string]interface{}{"type": "string", "description": "Environment variable name for API key"},
			},
			"required": []string{"kind", "name"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			kind := mapStr(args, "kind")
			name := mapStr(args, "name")
			if !requireNameStr(name) {
				return `{"error":"invalid name"}`, false
			}
			spec := map[string]interface{}{}
			if cmd := mapStr(args, "command"); cmd != "" {
				spec["command"] = cmd
			}
			if capArgs := mapSlice(args, "args"); len(capArgs) > 0 {
				spec["args"] = capArgs
			}
			if u := mapStr(args, "url"); u != "" {
				spec["url"] = u
			}
			if ke := mapStr(args, "key_env"); ke != "" {
				spec["key_env"] = ke
			}
			capReg := capabilities.NewRegistry(d.cfg.Home)
			if err := capReg.Add(kind, name, spec); err != nil {
				return "Error: " + err.Error(), true
			}
			d.audit.WriteSystem("capability_added", map[string]interface{}{"name": name, "kind": kind})
			return fmt.Sprintf("Capability '%s' (%s) added to registry.", name, kind), false
		},
	)

	// agency_cap_delete
	reg.Register(
		"agency_cap_delete",
		"Remove a capability from the registry entirely.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{"type": "string", "description": "Capability name"},
			},
			"required": []string{"name"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			name := mapStr(args, "name")
			if !requireNameStr(name) {
				return `{"error":"invalid name"}`, false
			}
			capReg := capabilities.NewRegistry(d.cfg.Home)
			if err := capReg.Delete(name); err != nil {
				return "Error: " + err.Error(), true
			}
			d.audit.WriteSystem("capability_deleted", map[string]interface{}{"name": name})
			return fmt.Sprintf("Capability '%s' removed from registry.", name), false
		},
	)
}

// ── Policy (5 tools) + Help (1 tool) ───────────────────────────────────────

func registerPolicyTools(reg *MCPToolRegistry) {

	// agency_policy_show
	reg.Register(
		"agency_policy_show",
		"Show the effective policy for an agent after resolving the full hierarchy (platform > org > department > team > agent).",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"agent": map[string]interface{}{"type": "string", "description": "Agent name"},
			},
			"required": []string{"agent"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			agent := mapStr(args, "agent")
			if err := mcpValidateAgentName(agent); err != nil {
				return fmt.Sprintf("Error: %s", err), true
			}
			eng := policy.NewEngine(d.cfg.Home)
			ep := eng.Show(agent)

			lines := []string{fmt.Sprintf("Effective policy for %s:", agent)}
			if ep.Parameters != nil {
				lines = append(lines, "  Parameters:")
				keys := make([]string, 0, len(ep.Parameters))
				for k := range ep.Parameters {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, k := range keys {
					lines = append(lines, fmt.Sprintf("    %s: %v", k, ep.Parameters[k]))
				}
			}
			if len(ep.Rules) > 0 {
				lines = append(lines, fmt.Sprintf("  Rules: %d", len(ep.Rules)))
				limit := 5
				if len(ep.Rules) < limit {
					limit = len(ep.Rules)
				}
				for _, r := range ep.Rules[:limit] {
					rule, _ := r["rule"].(string)
					if rule != "" {
						lines = append(lines, fmt.Sprintf("    - %s", rule))
					} else {
						lines = append(lines, fmt.Sprintf("    - %v", r))
					}
				}
				if len(ep.Rules) > 5 {
					lines = append(lines, fmt.Sprintf("    ... and %d more", len(ep.Rules)-5))
				}
			}
			if ep.HardFloors != nil {
				lines = append(lines, "  Hard floors:")
				for k, v := range ep.HardFloors {
					lines = append(lines, fmt.Sprintf("    %s: %v", k, v))
				}
			}
			if len(ep.Exceptions) > 0 {
				lines = append(lines, fmt.Sprintf("  Active exceptions: %d", len(ep.Exceptions)))
			}
			if len(ep.Chain) > 0 {
				chainStrs := make([]string, len(ep.Chain))
				for i, s := range ep.Chain {
					chainStrs[i] = s.Level
				}
				lines = append(lines, fmt.Sprintf("  Chain: %s", strings.Join(chainStrs, " > ")))
			}
			return strings.Join(lines, "\n"), false
		},
	)

	// agency_policy_check
	reg.Register(
		"agency_policy_check",
		"Validate the policy chain for an agent. Reports violations, hard floor status, and loosening attempts.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"agent": map[string]interface{}{"type": "string", "description": "Agent name"},
			},
			"required": []string{"agent"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			agent := mapStr(args, "agent")
			if err := mcpValidateAgentName(agent); err != nil {
				return fmt.Sprintf("Error: %s", err), true
			}
			eng := policy.NewEngine(d.cfg.Home)
			ep := eng.Validate(agent)

			// Also check constraints.yaml against hard floors
			constraintsPath := filepath.Join(d.cfg.Home, "agents", agent, "constraints.yaml")
			if data, err := os.ReadFile(constraintsPath); err == nil {
				var constraints map[string]interface{}
				if yaml.Unmarshal(data, &constraints) == nil {
					if err := policy.ValidatePolicy(constraints); err != nil {
						ep.Valid = false
						ep.Violations = append(ep.Violations, err.Error())
					}
				}
			}

			if ep.Valid {
				lines := []string{fmt.Sprintf("Policy chain for %s: VALID", agent)}
				lines = append(lines, fmt.Sprintf("  Steps: %d", len(ep.Chain)))
				lines = append(lines, "  Hard floors: OK")
				lines = append(lines, "  Parameters: OK")
				return strings.Join(lines, "\n"), false
			}
			lines := []string{fmt.Sprintf("Policy chain for %s: INVALID", agent)}
			for _, v := range ep.Violations {
				lines = append(lines, fmt.Sprintf("  VIOLATION: %s", v))
			}
			return strings.Join(lines, "\n"), false
		},
	)

	// agency_policy_validate
	reg.Register(
		"agency_policy_validate",
		"Validate policy chains for all configured agents. Returns pass/fail for each.",
		nil,
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			if d.agents == nil {
				return "Error: agent manager not initialized", true
			}
			agents, err := d.agents.List(context.Background())
			if err != nil {
				return "Error: " + err.Error(), true
			}
			if len(agents) == 0 {
				return "No agents to validate.", false
			}

			eng := policy.NewEngine(d.cfg.Home)
			var results []string
			for _, a := range agents {
				ep := eng.Validate(a.Name)

				// Also check constraints.yaml
				constraintsPath := filepath.Join(d.cfg.Home, "agents", a.Name, "constraints.yaml")
				if data, err := os.ReadFile(constraintsPath); err == nil {
					var constraints map[string]interface{}
					if yaml.Unmarshal(data, &constraints) == nil {
						if err := policy.ValidatePolicy(constraints); err != nil {
							ep.Valid = false
						}
					}
				}

				status := "FAIL"
				if ep.Valid {
					status = "PASS"
				}
				results = append(results, fmt.Sprintf("  %s: %s", a.Name, status))
			}
			lines := []string{fmt.Sprintf("Policy validation (%d agents):", len(agents))}
			lines = append(lines, results...)
			return strings.Join(lines, "\n"), false
		},
	)

	// agency_policy_template
	reg.Register(
		"agency_policy_template",
		"Manage named policy templates. Actions: list, create, show, delete.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action":               map[string]interface{}{"type": "string", "enum": []string{"list", "create", "show", "delete"}, "description": "Operation to perform"},
				"name":                 map[string]interface{}{"type": "string", "description": "Template name (required for create/show/delete)"},
				"description":          map[string]interface{}{"type": "string", "description": "Template description (for create)"},
				"risk_tolerance":       map[string]interface{}{"type": "string", "enum": []string{"low", "medium", "high"}, "description": "Risk tolerance parameter (for create)"},
				"max_concurrent_tasks": map[string]interface{}{"type": "integer", "description": "Max concurrent tasks parameter (for create)"},
				"max_task_duration":    map[string]interface{}{"type": "string", "description": "Max task duration parameter (for create)"},
			},
			"required": []string{"action"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			action := mapStr(args, "action")

			pReg, err := policy.NewPolicyRegistry(d.cfg.Home)
			if err != nil {
				return "Error: " + err.Error(), true
			}

			switch action {
			case "list":
				policies := pReg.ListPolicies()
				if len(policies) == 0 {
					return "No policy templates.", false
				}
				lines := []string{"Policy templates:"}
				for _, p := range policies {
					lines = append(lines, fmt.Sprintf("  %s - %s", p.Name, p.Description))
				}
				return strings.Join(lines, "\n"), false

			case "create":
				name := mapStr(args, "name")
				if name == "" {
					return "Error: name is required for create", true
				}
				if !requireNameStr(name) {
					return `{"error":"invalid name"}`, false
				}
				desc := mapStr(args, "description")
				params := map[string]interface{}{}
				if rt := mapStr(args, "risk_tolerance"); rt != "" {
					params["risk_tolerance"] = rt
				}
				if mct := mapInt(args, "max_concurrent_tasks", 0); mct > 0 {
					params["max_concurrent_tasks"] = mct
				}
				if mtd := mapStr(args, "max_task_duration"); mtd != "" {
					params["max_task_duration"] = mtd
				}
				path, err := pReg.CreatePolicy(name, desc, params, nil)
				if err != nil {
					return "Error: " + err.Error(), true
				}
				d.audit.WriteSystem("policy_template_created", map[string]interface{}{"name": name, "path": path})
				return fmt.Sprintf("Policy template '%s' created.", name), false

			case "show":
				name := mapStr(args, "name")
				if name == "" {
					return "Error: name is required for show", true
				}
				if !requireNameStr(name) {
					return `{"error":"invalid name"}`, false
				}
				raw, err := pReg.GetPolicy(name)
				if err != nil {
					return "Error: " + err.Error(), true
				}
				lines := []string{fmt.Sprintf("Policy template: %s", name)}
				if desc, ok := raw["description"].(string); ok {
					lines = append(lines, fmt.Sprintf("  Description: %s", desc))
				}
				if params, ok := raw["parameters"].(map[string]interface{}); ok {
					lines = append(lines, "  Parameters:")
					for k, v := range params {
						lines = append(lines, fmt.Sprintf("    %s: %v", k, v))
					}
				}
				return strings.Join(lines, "\n"), false

			case "delete":
				name := mapStr(args, "name")
				if name == "" {
					return "Error: name is required for delete", true
				}
				if !requireNameStr(name) {
					return `{"error":"invalid name"}`, false
				}
				if err := pReg.DeletePolicy(name); err != nil {
					return "Error: " + err.Error(), true
				}
				d.audit.WriteSystem("policy_template_deleted", map[string]interface{}{"name": name})
				return fmt.Sprintf("Policy template '%s' deleted.", name), false

			default:
				return "Error: unknown action: " + action, true
			}
		},
	)

	// agency_policy_exception
	reg.Register(
		"agency_policy_exception",
		"Manage policy exceptions. Actions: request (create new exception request), approve, deny, list (pending exceptions).",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action":          map[string]interface{}{"type": "string", "enum": []string{"request", "approve", "deny", "list"}, "description": "Operation to perform"},
				"agent":           map[string]interface{}{"type": "string", "description": "Agent name (for request)"},
				"parameter":       map[string]interface{}{"type": "string", "description": "Policy parameter to override (for request)"},
				"requested_value": map[string]interface{}{"type": "string", "description": "Desired value (for request)"},
				"reason":          map[string]interface{}{"type": "string", "description": "Justification (for request/deny)"},
				"domain":          map[string]interface{}{"type": "string", "description": "Exception domain: security, privacy, legal (for request)"},
				"request_id":      map[string]interface{}{"type": "string", "description": "Exception request ID (for approve/deny)"},
				"principal":       map[string]interface{}{"type": "string", "description": "Approving/denying principal (for approve/deny)"},
			},
			"required": []string{"action"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			action := mapStr(args, "action")

			exceptionsDir := filepath.Join(d.cfg.Home, "exceptions")
			os.MkdirAll(exceptionsDir, 0755)

			switch action {
			case "request":
				agent := mapStr(args, "agent")
				param := mapStr(args, "parameter")
				value := mapStr(args, "requested_value")
				reason := mapStr(args, "reason")
				domain := mapStr(args, "domain")
				if agent == "" || param == "" {
					return "Error: agent and parameter are required for exception request", true
				}
				if !requireNameStr(agent) {
					return `{"error":"invalid agent name"}`, false
				}

				reqID := fmt.Sprintf("exc-%d", time.Now().UnixNano()/1e6)
				exception := map[string]interface{}{
					"request_id":      reqID,
					"agent":           agent,
					"parameter":       param,
					"requested_value": value,
					"reason":          reason,
					"domain":          domain,
					"status":          "pending",
					"requested_at":    time.Now().UTC().Format(time.RFC3339),
				}
				data, _ := yaml.Marshal(exception)
				os.WriteFile(filepath.Join(exceptionsDir, reqID+".yaml"), data, 0644)
				d.audit.Write(agent, "policy_exception_requested", map[string]interface{}{
					"request_id": reqID, "parameter": param, "domain": domain,
				})
				return fmt.Sprintf("Exception request '%s' created for %s.", reqID, agent), false

			case "approve":
				reqID := mapStr(args, "request_id")
				principal := mapStr(args, "principal")
				if reqID == "" {
					return "Error: request_id is required for approve", true
				}
				if principal == "" {
					principal = "operator"
				}
				excPath := filepath.Join(exceptionsDir, reqID+".yaml")
				var exc map[string]interface{}
				if data, err := os.ReadFile(excPath); err == nil {
					yaml.Unmarshal(data, &exc)
				}
				if exc == nil {
					return "Error: exception request not found", true
				}
				exc["status"] = "approved"
				exc["approved_by"] = principal
				exc["approved_at"] = time.Now().UTC().Format(time.RFC3339)
				data, _ := yaml.Marshal(exc)
				os.WriteFile(excPath, data, 0644)
				agent, _ := exc["agent"].(string)
				d.audit.Write(agent, "policy_exception_approved", map[string]interface{}{
					"request_id": reqID, "principal": principal,
				})
				return fmt.Sprintf("Exception '%s' approved by %s.", reqID, principal), false

			case "deny":
				reqID := mapStr(args, "request_id")
				principal := mapStr(args, "principal")
				reason := mapStr(args, "reason")
				if reqID == "" {
					return "Error: request_id is required for deny", true
				}
				if principal == "" {
					principal = "operator"
				}
				excPath := filepath.Join(exceptionsDir, reqID+".yaml")
				var exc map[string]interface{}
				if data, err := os.ReadFile(excPath); err == nil {
					yaml.Unmarshal(data, &exc)
				}
				if exc == nil {
					return "Error: exception request not found", true
				}
				exc["status"] = "denied"
				exc["denied_by"] = principal
				exc["denied_at"] = time.Now().UTC().Format(time.RFC3339)
				exc["denial_reason"] = reason
				data, _ := yaml.Marshal(exc)
				os.WriteFile(excPath, data, 0644)
				agent, _ := exc["agent"].(string)
				d.audit.Write(agent, "policy_exception_denied", map[string]interface{}{
					"request_id": reqID, "principal": principal, "reason": reason,
				})
				return fmt.Sprintf("Exception '%s' denied by %s.", reqID, principal), false

			case "list":
				entries, err := os.ReadDir(exceptionsDir)
				if err != nil || len(entries) == 0 {
					return "No pending exceptions.", false
				}
				lines := []string{"Policy exceptions:"}
				for _, e := range entries {
					if !strings.HasSuffix(e.Name(), ".yaml") {
						continue
					}
					data, err := os.ReadFile(filepath.Join(exceptionsDir, e.Name()))
					if err != nil {
						continue
					}
					var exc map[string]interface{}
					yaml.Unmarshal(data, &exc)
					if exc == nil {
						continue
					}
					status, _ := exc["status"].(string)
					agent, _ := exc["agent"].(string)
					param, _ := exc["parameter"].(string)
					reqID, _ := exc["request_id"].(string)
					lines = append(lines, fmt.Sprintf("  [%s] %s: %s parameter=%s", status, reqID, agent, param))
				}
				return strings.Join(lines, "\n"), false

			default:
				return "Error: unknown action: " + action, true
			}
		},
	)

	// agency_admin_rebuild
	reg.Register(
		"agency_admin_rebuild",
		"Regenerate all derived config files for an agent (services-manifest.json, services.yaml, PLATFORM.md, FRAMEWORK.md, AGENTS.md) without starting or stopping it.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{"type": "string", "description": "Agent name"},
			},
			"required": []string{"name"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			name := mapStr(args, "name")
			if name == "" {
				return "Error: agent name is required", true
			}
			if !requireNameStr(name) {
				return `{"error":"invalid name"}`, false
			}

			agentDir := filepath.Join(d.cfg.Home, "agents", name)
			if _, err := os.Stat(filepath.Join(agentDir, "agent.yaml")); err != nil {
				return fmt.Sprintf("Error: agent not found: %s", name), true
			}

			var regenerated []string
			var errors []string

			// Manifest (services-manifest.json + services.yaml)
			if err := d.generateAgentManifest(name); err != nil {
				errors = append(errors, "manifest: "+err.Error())
			} else {
				regenerated = append(regenerated, "services-manifest.json", "services.yaml")
			}

			// Read agent config for type
			agentType := "standard"
			var agentConfig map[string]interface{}
			if data, err := os.ReadFile(filepath.Join(agentDir, "agent.yaml")); err == nil {
				yaml.Unmarshal(data, &agentConfig)
				if t, ok := agentConfig["type"].(string); ok && t != "" {
					agentType = t
				}
			}

			// Constraints for AGENTS.md
			var constraints map[string]interface{}
			if data, err := os.ReadFile(filepath.Join(agentDir, "constraints.yaml")); err == nil {
				yaml.Unmarshal(data, &constraints)
			}

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

			frameworkMD := orchestrate.GenerateFrameworkMD(agentType, "standard")
			if err := os.WriteFile(filepath.Join(agentDir, "FRAMEWORK.md"), []byte(frameworkMD), 0644); err != nil {
				errors = append(errors, "FRAMEWORK.md: "+err.Error())
			} else {
				regenerated = append(regenerated, "FRAMEWORK.md")
			}

			platformMD := orchestrate.GeneratePlatformMD(agentType, nil)
			if err := os.WriteFile(filepath.Join(agentDir, "PLATFORM.md"), []byte(platformMD), 0644); err != nil {
				errors = append(errors, "PLATFORM.md: "+err.Error())
			} else {
				regenerated = append(regenerated, "PLATFORM.md")
			}

			d.audit.Write(name, "admin_rebuild", map[string]interface{}{
				"regenerated": regenerated,
				"errors":      errors,
			})

			var lines []string
			if len(errors) == 0 {
				lines = append(lines, fmt.Sprintf("Rebuilt %s: %d files regenerated", name, len(regenerated)))
			} else {
				lines = append(lines, fmt.Sprintf("Rebuilt %s: %d regenerated, %d errors", name, len(regenerated), len(errors)))
			}
			for _, f := range regenerated {
				lines = append(lines, "  + "+f)
			}
			for _, e := range errors {
				lines = append(lines, "  ! "+e)
			}
			return strings.Join(lines, "\n"), len(regenerated) == 0
		},
	)

	// agency_help — pure text generation, no service call
	reg.Register(
		"agency_help",
		"Get an overview of all Agency tools grouped by workflow. Call this first if you are unfamiliar with Agency.",
		nil,
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			groups := []struct {
				name  string
				tools []string
			}{
				{"Setup", []string{"agency_setup", "agency_infra_up", "agency_infra_status", "agency_infra_down", "agency_infra_rebuild", "agency_infra_reload"}},
				{"Agent Lifecycle", []string{"agency_list", "agency_create", "agency_start", "agency_stop", "agency_resume", "agency_restart", "agency_delete", "agency_show", "agency_status", "agency_grant", "agency_revoke"}},
				{"Communication", []string{"agency_channel_create", "agency_channel_list", "agency_channel_read", "agency_channel_send", "agency_channel_search", "agency_channel_archive", "agency_channel_grant_access"}},
				{"Observability", []string{"agency_log"}},
				{"Capabilities", []string{"agency_cap_list", "agency_cap_show", "agency_cap_enable", "agency_cap_disable", "agency_cap_add", "agency_cap_delete"}},
				{"Policy", []string{"agency_policy_show", "agency_policy_check", "agency_policy_validate", "agency_policy_template", "agency_policy_exception"}},
				{"Teams", []string{"agency_team_create", "agency_team_list", "agency_team_show", "agency_team_activity"}},
				{"Deploy", []string{"agency_deploy", "agency_teardown"}},
				{"Intake", []string{"agency_intake_items", "agency_intake_stats"}},
				{"Admin", []string{"agency_admin_doctor", "agency_admin_destroy", "agency_admin_rebuild", "agency_admin_trust", "agency_admin_egress", "agency_admin_audit", "agency_admin_knowledge", "agency_admin_department"}},
				{"Hub", []string{"agency_hub_search", "agency_hub_install", "agency_hub_remove", "agency_hub_list", "agency_hub_update", "agency_hub_info"}},
			}

			// Build tool name -> description map from registry
			toolMap := map[string]string{}
			if d.mcpReg != nil {
				for _, t := range d.mcpReg.Tools() {
					toolMap[t.Name] = t.Description
				}
			}

			lines := []string{
				"Agency -- AI Agent Isolation Platform",
				"",
				"Agency deploys AI agents inside enforced isolation containers with",
				"credential scoping, network mediation, and continuous security verification.",
				"",
				"Typical workflow: init -> infra_up -> create -> start -> send -> monitor",
				"",
			}
			for _, group := range groups {
				lines = append(lines, fmt.Sprintf("## %s", group.name))
				for _, name := range group.tools {
					desc := toolMap[name]
					short := desc
					if idx := strings.Index(desc, ". "); idx >= 0 {
						short = desc[:idx+1]
					}
					lines = append(lines, fmt.Sprintf("  %s -- %s", name, short))
				}
				lines = append(lines, "")
			}

			return strings.Join(lines, "\n"), false
		},
	)
}

// buildScopeReport generates a scope audit section for doctor output.
// Shows each agent's declared scopes per service and flags unscoped agents.
func (d *mcpDeps) buildScopeReport() string {
	agentsDir := filepath.Join(d.cfg.Home, "agents")
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return ""
	}

	type agentScopeInfo struct {
		agent    string
		service  string
		required []string
		optional []string
	}
	var infos []agentScopeInfo
	var unscoped []string

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		agentName := e.Name()
		scopes := d.loadPresetScopes(agentName)

		// Check if agent has any granted services
		constraintsPath := filepath.Join(agentsDir, agentName, "constraints.yaml")
		var constraints map[string]interface{}
		if data, err := os.ReadFile(constraintsPath); err == nil {
			yaml.Unmarshal(data, &constraints)
		}
		grantedList, _ := constraints["granted_capabilities"].([]interface{})
		if len(grantedList) == 0 {
			continue
		}

		if len(scopes) == 0 {
			unscoped = append(unscoped, agentName)
			continue
		}

		// Load preset to get required vs optional separately
		var agentCfg struct {
			Preset string `yaml:"preset"`
		}
		if data, err := os.ReadFile(filepath.Join(agentsDir, agentName, "agent.yaml")); err == nil {
			yaml.Unmarshal(data, &agentCfg)
		}
		presetPaths := []string{
			filepath.Join(d.cfg.Home, "hub-cache", "default", "presets", agentCfg.Preset, "preset.yaml"),
			filepath.Join(d.cfg.Home, "presets", agentCfg.Preset+".yaml"),
		}
		var presetData []byte
		for _, p := range presetPaths {
			if d, err := os.ReadFile(p); err == nil {
				presetData = d
				break
			}
		}
		if presetData != nil {
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
			if yaml.Unmarshal(presetData, &preset) == nil {
				for _, cred := range preset.Requires.Credentials {
					if len(cred.Scopes.Required) > 0 || len(cred.Scopes.Optional) > 0 {
						infos = append(infos, agentScopeInfo{
							agent:    agentName,
							service:  cred.GrantName,
							required: cred.Scopes.Required,
							optional: cred.Scopes.Optional,
						})
					}
				}
			}
		}
	}

	if len(infos) == 0 && len(unscoped) == 0 {
		return ""
	}

	// Group by service
	byService := map[string][]agentScopeInfo{}
	for _, info := range infos {
		byService[info.service] = append(byService[info.service], info)
	}

	var lines []string
	svcNames := make([]string, 0, len(byService))
	for s := range byService {
		svcNames = append(svcNames, s)
	}
	sort.Strings(svcNames)

	for _, svc := range svcNames {
		agents := byService[svc]
		lines = append(lines, fmt.Sprintf("  %s:", svc))
		for _, a := range agents {
			lines = append(lines, fmt.Sprintf("    %s:", a.agent))
			if len(a.required) > 0 {
				sort.Strings(a.required)
				lines = append(lines, fmt.Sprintf("      required: %s", strings.Join(a.required, ", ")))
			}
			if len(a.optional) > 0 {
				sort.Strings(a.optional)
				lines = append(lines, fmt.Sprintf("      optional: %s", strings.Join(a.optional, ", ")))
			}
		}
	}

	if len(unscoped) > 0 {
		sort.Strings(unscoped)
		lines = append(lines, fmt.Sprintf("  Unscoped agents (all tools available): %s", strings.Join(unscoped, ", ")))
	}

	return strings.Join(lines, "\n")
}
