package orchestrate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/geoffbelknap/agency/internal/credstore"
	"github.com/geoffbelknap/agency/internal/models"
)

// HealthCheck is a single check result.
type HealthCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"` // "pass", "warn", "fail"
	Detail string `json:"detail"`
	Fix    string `json:"fix,omitempty"`
}

// MissionHealthResponse is the full health report for a mission.
type MissionHealthResponse struct {
	Mission string        `json:"mission"`
	Status  string        `json:"status"` // "healthy", "degraded", "unhealthy", "unknown"
	Checks  []HealthCheck `json:"checks"`
	Summary string        `json:"summary"`
}

// MissionHealthChecker evaluates mission health from existing data sources.
type MissionHealthChecker struct {
	Home      string
	CredStore *credstore.Store
}

// CheckHealth evaluates all health checks for a mission.
func (hc *MissionHealthChecker) CheckHealth(m *models.Mission) MissionHealthResponse {
	resp := MissionHealthResponse{
		Mission: m.Name,
		Status:  "unknown",
	}

	if m.Status != "active" {
		resp.Checks = append(resp.Checks, HealthCheck{
			Name: "mission_active", Status: "fail",
			Detail: fmt.Sprintf("Mission status is %q (not active)", m.Status),
		})
	} else {
		resp.Checks = append(resp.Checks, HealthCheck{
			Name: "mission_active", Status: "pass", Detail: "active",
		})
	}

	// Check agent running state
	if m.AssignedTo != "" {
		targets, err := missionHealthTargets(hc.Home, m)
		if err != nil {
			resp.Checks = append(resp.Checks, HealthCheck{
				Name:   "agent_running",
				Status: "fail",
				Detail: fmt.Sprintf("could not resolve mission targets: %v", err),
				Fix:    "check the team configuration",
			})
		} else if len(targets) == 0 {
			resp.Checks = append(resp.Checks, HealthCheck{
				Name:   "agent_running",
				Status: "fail",
				Detail: "Mission has no resolved execution targets",
				Fix:    fmt.Sprintf("agency mission assign %s <agent>", m.Name),
			})
		} else {
			for _, target := range targets {
				resp.Checks = append(resp.Checks, hc.checkAgentRunning(target))
			}
		}
	} else {
		resp.Checks = append(resp.Checks, HealthCheck{
			Name: "agent_assigned", Status: "fail",
			Detail: "Mission not assigned to any agent",
			Fix:    fmt.Sprintf("agency mission assign %s <agent>", m.Name),
		})
	}

	// Check trigger sources
	for _, trigger := range m.Triggers {
		switch trigger.Source {
		case "connector":
			resp.Checks = append(resp.Checks, hc.checkConnectorHealth(trigger.Connector)...)
		case "schedule":
			resp.Checks = append(resp.Checks, hc.checkScheduleHealth(trigger))
		}
	}

	// Compute overall status
	hasFail := false
	hasWarn := false
	var failures []string
	for _, c := range resp.Checks {
		switch c.Status {
		case "fail":
			hasFail = true
			failures = append(failures, c.Detail)
		case "warn":
			hasWarn = true
		}
	}

	if len(resp.Checks) == 0 {
		resp.Status = "unknown"
		resp.Summary = "No health checks applicable"
	} else if hasFail {
		resp.Status = "unhealthy"
		resp.Summary = strings.Join(failures, ". ")
	} else if hasWarn {
		resp.Status = "degraded"
		resp.Summary = "Working with warnings"
	} else {
		resp.Status = "healthy"
		resp.Summary = "All checks passing"
	}

	return resp
}

func (hc *MissionHealthChecker) checkAgentRunning(agentName string) HealthCheck {
	agentDir := filepath.Join(hc.Home, "agents", agentName)
	if _, err := os.Stat(agentDir); os.IsNotExist(err) {
		return HealthCheck{
			Name: "agent_running", Status: "fail",
			Detail: fmt.Sprintf("%s: agent not found", agentName),
			Fix:    fmt.Sprintf("agency start %s", agentName),
		}
	}
	return HealthCheck{
		Name: "agent_running", Status: "pass",
		Detail: fmt.Sprintf("%s: deployed", agentName),
	}
}

func (hc *MissionHealthChecker) checkConnectorHealth(connectorName string) []HealthCheck {
	var checks []HealthCheck
	if connectorName == "" {
		return checks
	}

	// Check connector state in hub registry
	state := hc.findConnectorState(connectorName)
	if state == "" {
		checks = append(checks, HealthCheck{
			Name: "connector_installed", Status: "fail",
			Detail: fmt.Sprintf("%s: not installed", connectorName),
			Fix:    fmt.Sprintf("agency hub install %s --kind connector && agency hub activate %s", connectorName, connectorName),
		})
		return checks
	}
	if state != "active" {
		checks = append(checks, HealthCheck{
			Name: "connector_active", Status: "fail",
			Detail: fmt.Sprintf("%s: state is %s (not active)", connectorName, state),
			Fix:    fmt.Sprintf("agency hub activate %s", connectorName),
		})
	} else {
		checks = append(checks, HealthCheck{
			Name: "connector_active", Status: "pass",
			Detail: fmt.Sprintf("%s: active", connectorName),
		})
	}

	// Check connector YAML exists for intake
	connectorYAML := filepath.Join(hc.Home, "connectors", connectorName+".yaml")
	if _, err := os.Stat(connectorYAML); os.IsNotExist(err) {
		checks = append(checks, HealthCheck{
			Name: "connector_deployed", Status: "fail",
			Detail: fmt.Sprintf("%s: resolved YAML not published to connectors dir", connectorName),
			Fix:    "agency hub activate " + connectorName + " && agency infra rebuild intake",
		})
	}

	// Check service key for credentials referenced in the connector
	checks = append(checks, hc.checkConnectorCredentials(connectorName)...)

	return checks
}

func (hc *MissionHealthChecker) checkConnectorCredentials(connectorName string) []HealthCheck {
	var checks []HealthCheck

	connectorYAML := filepath.Join(hc.Home, "connectors", connectorName+".yaml")
	data, err := os.ReadFile(connectorYAML)
	if err != nil {
		return checks
	}

	credentialExists := func(name string) (exists bool, nonEmpty bool) {
		if hc.CredStore == nil {
			return false, false
		}
		entry, err := hc.CredStore.Get(name)
		if err != nil {
			return false, false
		}
		return true, entry.Value != ""
	}

	// Find grant_name references in the connector YAML
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "grant_name:") {
			continue
		}
		grantName := strings.TrimSpace(strings.TrimPrefix(trimmed, "grant_name:"))
		grantName = strings.Trim(grantName, "\"'")
		if grantName == "" {
			continue
		}

		exists, nonEmpty := credentialExists(grantName)
		if !exists {
			checks = append(checks, HealthCheck{
				Name: "credential_health", Status: "fail",
				Detail: fmt.Sprintf("%s: service key not found in credential store", grantName),
				Fix:    fmt.Sprintf("agency creds set %s <YOUR_KEY> --kind service --scope platform --protocol api-key", grantName),
			})
		} else if !nonEmpty {
			checks = append(checks, HealthCheck{
				Name: "credential_health", Status: "fail",
				Detail: fmt.Sprintf("%s: credential value is empty", grantName),
				Fix:    fmt.Sprintf("agency creds set %s <YOUR_KEY> --kind service --scope platform --protocol api-key", grantName),
			})
		} else {
			checks = append(checks, HealthCheck{
				Name: "credential_health", Status: "pass",
				Detail: fmt.Sprintf("%s: configured", grantName),
			})
		}
	}

	return checks
}

func (hc *MissionHealthChecker) checkScheduleHealth(trigger models.MissionTrigger) HealthCheck {
	if trigger.Cron == "" {
		return HealthCheck{
			Name: "schedule_configured", Status: "warn",
			Detail: "Schedule trigger has no cron expression",
		}
	}
	return HealthCheck{
		Name: "schedule_configured", Status: "pass",
		Detail: fmt.Sprintf("Cron: %s", trigger.Cron),
	}
}

func (hc *MissionHealthChecker) findConnectorState(name string) string {
	registryPath := filepath.Join(hc.Home, "hub-registry", "registry.yaml")
	data, err := os.ReadFile(registryPath)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	var currentName, currentState, currentKind string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "name:") {
			currentName = strings.TrimSpace(strings.TrimPrefix(trimmed, "name:"))
		} else if strings.HasPrefix(trimmed, "state:") {
			currentState = strings.TrimSpace(strings.TrimPrefix(trimmed, "state:"))
		} else if strings.HasPrefix(trimmed, "kind:") {
			currentKind = strings.TrimSpace(strings.TrimPrefix(trimmed, "kind:"))
		}
		if currentName == name && currentKind == "connector" && currentState != "" {
			return currentState
		}
	}
	return ""
}
