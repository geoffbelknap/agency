package hostadapter

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
)

// DiagnosticCheck holds one backend diagnostic result and optional remediation guidance.
type DiagnosticCheck struct {
	Name    string `json:"name"`
	Agent   string `json:"agent,omitempty"`
	Scope   string `json:"scope,omitempty"`
	Backend string `json:"backend,omitempty"`
	Status  string `json:"status"`
	Detail  string `json:"detail,omitempty"`
	Fix     string `json:"fix,omitempty"`
}

func diagnosticPass(name, agent, detail string) DiagnosticCheck {
	return DiagnosticCheck{Name: name, Agent: agent, Scope: "runtime", Status: "pass", Detail: detail}
}

func diagnosticFail(name, agent, detail string) DiagnosticCheck {
	return DiagnosticCheck{Name: name, Agent: agent, Scope: "runtime", Status: "fail", Detail: detail}
}

// RuntimeDiagnostics verifies container-backed runtime security guarantees for
// each running agent. These checks are adapter-owned because their evidence
// comes from backend-specific container inspection.
func (a *ContainerAdapter) RuntimeDiagnostics(ctx context.Context, runningAgents []string) []DiagnosticCheck {
	var results []DiagnosticCheck
	if err := a.requireContainerBackend(); err != nil {
		return []DiagnosticCheck{diagnosticFail("runtime_backend", "", err.Error())}
	}
	for _, agentName := range runningAgents {
		wsName := "agency-" + agentName + "-workspace"
		enfName := "agency-" + agentName + "-enforcer"
		wsInspect, wsInspectErr := a.dc.InspectContainer(ctx, wsName)
		enfInspect, enfInspectErr := a.dc.InspectContainer(ctx, enfName)

		func() {
			ws, err := wsInspect, wsInspectErr
			if err != nil {
				results = append(results, diagnosticFail("credentials_isolated", agentName, "Cannot inspect workspace: "+err.Error()))
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
				results = append(results, diagnosticFail("credentials_isolated", agentName, "LLM credentials visible in workspace env: "+strings.Join(leaked, ", ")))
				return
			}
			results = append(results, diagnosticPass("credentials_isolated", agentName, "No LLM API keys in workspace environment"))
		}()

		func() {
			ws, err := wsInspect, wsInspectErr
			if err != nil {
				results = append(results, diagnosticFail("network_mediation", agentName, "Cannot inspect workspace: "+err.Error()))
				return
			}
			var forbidden []string
			for _, net := range ws.Networks {
				if strings.Contains(net, "egress") || strings.HasPrefix(net, "agency-gateway") {
					forbidden = append(forbidden, net)
				}
			}
			if len(forbidden) > 0 {
				results = append(results, diagnosticFail("network_mediation", agentName, "Workspace on forbidden network(s): "+strings.Join(forbidden, ", ")))
				return
			}
			results = append(results, diagnosticPass("network_mediation", agentName, "Workspace on internal network(s) only: "+strings.Join(ws.Networks, ", ")))
		}()

		func() {
			ws, err := wsInspect, wsInspectErr
			if err != nil {
				results = append(results, diagnosticFail("constraints_readonly", agentName, "Cannot inspect workspace: "+err.Error()))
				return
			}
			found := false
			for _, m := range ws.Mounts {
				if strings.Contains(m.Destination, "constraints.yaml") {
					found = true
					if m.RW {
						results = append(results, diagnosticFail("constraints_readonly", agentName, "constraints.yaml mounted read-write at "+m.Destination))
						return
					}
				}
			}
			if found {
				results = append(results, diagnosticPass("constraints_readonly", agentName, "constraints.yaml mounted read-only"))
				return
			}
			results = append(results, diagnosticPass("constraints_readonly", agentName, "constraints.yaml mount not found (may be embedded in image)"))
		}()

		func() {
			enf, err := enfInspect, enfInspectErr
			if err != nil {
				results = append(results, diagnosticFail("enforcer_audit", agentName, "Enforcer container not found: "+err.Error()))
				return
			}
			if enf.State != "running" {
				results = append(results, diagnosticFail("enforcer_audit", agentName, "Enforcer status: "+enf.State))
				return
			}
			detail := "Enforcer running"
			if enf.Health != "none" && enf.Health != "" {
				detail += ", health: " + enf.Health
			}
			results = append(results, diagnosticPass("enforcer_audit", agentName, detail))
		}()

		func() {
			ws, err := wsInspect, wsInspectErr
			if err != nil {
				results = append(results, diagnosticFail("audit_not_writable", agentName, "Cannot inspect workspace: "+err.Error()))
				return
			}
			for _, m := range ws.Mounts {
				if strings.Contains(m.Destination, "audit") && m.RW {
					results = append(results, diagnosticFail("audit_not_writable", agentName, "Audit directory mounted read-write at "+m.Destination))
					return
				}
			}
			results = append(results, diagnosticPass("audit_not_writable", agentName, "Audit directory not writable by agent"))
		}()

		func() {
			ws, err := wsInspect, wsInspectErr
			if err != nil {
				results = append(results, diagnosticFail("halt_functional", agentName, "Cannot inspect workspace: "+err.Error()))
				return
			}
			if ws.State == "running" {
				results = append(results, diagnosticPass("halt_functional", agentName, "Workspace container is running and pauseable"))
				return
			}
			results = append(results, diagnosticFail("halt_functional", agentName, "Workspace state '"+ws.State+"' - cannot pause"))
		}()

		func() {
			enf, err := enfInspect, enfInspectErr
			if err != nil {
				results = append(results, diagnosticFail("operator_override", agentName, "Cannot inspect enforcer: "+err.Error()))
				return
			}
			if !runtimehost.EnforcerHasOperatorOverridePath(enf.Networks) {
				results = append(results, diagnosticFail("operator_override", agentName, "Enforcer missing gateway mediation network: "+strings.Join(enf.Networks, ", ")))
				return
			}
			if unexpected := runtimehost.EnforcerUnexpectedExternalNetworks(enf.Networks); len(unexpected) > 0 {
				results = append(results, diagnosticFail("operator_override", agentName, "Enforcer attached to external network(s): "+strings.Join(unexpected, ", ")))
				return
			}
			results = append(results, diagnosticPass("operator_override", agentName, "Enforcer reachable on gateway mediation network"))
		}()
	}
	return results
}

// BackendDiagnostics performs backend-specific safety checks needed for routine
// Doctor runs. Cleanup and observability hygiene such as dangling resources,
// orphan scans, log sizes, and log rotation are intentionally kept out of this
// path because they are not required to prove the active runtime boundary.
//
// Checks performed:
//  1. PID limits        — workspace containers must have PidsLimit > 0
//  2. Network isolation — agent networks must have Internal: true
func (a *ContainerAdapter) BackendDiagnostics(ctx context.Context, runningAgents []string) []DiagnosticCheck {
	var results []DiagnosticCheck
	backend := runtimehost.NormalizeContainerBackend(a.backend)
	if backend == "" {
		backend = runtimehost.BackendDocker
	}
	if err := a.requireContainerBackend(); err != nil {
		return []DiagnosticCheck{{
			Name:    "connectivity",
			Scope:   "backend",
			Backend: backend,
			Status:  "fail",
			Detail:  err.Error(),
		}}
	}

	pass := func(name, detail string) DiagnosticCheck {
		return DiagnosticCheck{Name: name, Scope: "backend", Backend: backend, Status: "pass", Detail: detail}
	}
	fail := func(name, detail string) DiagnosticCheck {
		return DiagnosticCheck{Name: name, Scope: "backend", Backend: backend, Status: "fail", Detail: detail}
	}

	// ── 1. PID limits ─────────────────────────────────────────────────────────
	func() {
		var violations []string
		for _, agentName := range runningAgents {
			wsName := "agency-" + agentName + "-workspace"
			info, err := a.dc.ContainerInspectRaw(ctx, wsName)
			if err != nil {
				violations = append(violations, agentName+"(inspect error: "+err.Error()+")")
				continue
			}
			if !containerHasPIDLimit(info, backend) {
				violations = append(violations, agentName)
			}
		}
		if len(violations) > 0 {
			results = append(results, fail("pid_limits",
				fmt.Sprintf("Workspace container(s) missing PID limit: %s", strings.Join(violations, ", "))))
		} else if len(runningAgents) == 0 {
			results = append(results, pass("pid_limits", "No running agents to check"))
		} else {
			results = append(results, pass("pid_limits",
				"All workspace containers have PID limits set"))
		}
	}()

	// ── 2. Network isolation ──────────────────────────────────────────────────
	func() {
		var violations []string
		for _, agentName := range runningAgents {
			// Agent networks follow the pattern agency-{name}-* excluding mediation and egress
			nets, err := a.dc.ListNetworksByLabel(ctx, "agency.agent="+agentName)
			if err != nil {
				violations = append(violations, agentName+"(list error: "+err.Error()+")")
				continue
			}
			for _, n := range nets {
				// Skip mediation and egress networks — these are intentionally not internal
				if strings.Contains(n.Name, "mediation") || strings.Contains(n.Name, "egress") {
					continue
				}
				if !n.Internal {
					violations = append(violations, n.Name)
				}
			}
		}
		if len(violations) > 0 {
			results = append(results, fail("network_isolation",
				fmt.Sprintf("Agent network(s) not set to Internal=true: %s", strings.Join(violations, ", "))))
		} else {
			results = append(results, pass("network_isolation",
				"All agent networks are internally isolated"))
		}
	}()

	return results
}

func containerHasPIDLimit(info *container.InspectResponse, backend string) bool {
	if info == nil {
		return false
	}
	if info.HostConfig != nil && info.HostConfig.PidsLimit != nil && *info.HostConfig.PidsLimit > 0 {
		return true
	}
	if runtimehost.NormalizeContainerBackend(backend) != runtimehost.BackendContainerd || info.Config == nil {
		return false
	}
	value := strings.TrimSpace(info.Config.Labels["agency.policy.pids_limit"])
	limit, err := strconv.ParseInt(value, 10, 64)
	return err == nil && limit > 0
}
