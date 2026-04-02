package orchestrate

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/geoffbelknap/agency/internal/hub"
)

// HubHealthResponse is the health report for any hub component.
type HubHealthResponse struct {
	Name    string        `json:"name"`
	Kind    string        `json:"kind"`
	Status  string        `json:"status"` // "healthy", "degraded", "unhealthy", "unknown"
	Checks  []HealthCheck `json:"checks"`
	Summary string        `json:"summary"`
}

// HubHealthChecker evaluates health for any installed hub component.
type HubHealthChecker struct {
	Home string
}

// Check evaluates health for a named component.
func (hc *HubHealthChecker) Check(inst *hub.Instance) HubHealthResponse {
	resp := HubHealthResponse{
		Name: inst.Name,
		Kind: inst.Kind,
	}

	// State check — presets, packs, and missions don't need activation
	needsActive := inst.Kind == "connector" || inst.Kind == "service"
	if inst.State == "active" || !needsActive {
		resp.Checks = append(resp.Checks, HealthCheck{
			Name: "state", Status: "pass", Detail: inst.State,
		})
	} else {
		resp.Checks = append(resp.Checks, HealthCheck{
			Name: "state", Status: "warn",
			Detail: fmt.Sprintf("state is %s (not active)", inst.State),
			Fix:    fmt.Sprintf("agency hub activate %s", inst.Name),
		})
	}

	switch inst.Kind {
	case "connector":
		resp.Checks = append(resp.Checks, hc.checkConnector(inst)...)
	case "service":
		resp.Checks = append(resp.Checks, hc.checkService(inst)...)
	case "preset":
		resp.Checks = append(resp.Checks, hc.checkPreset(inst)...)
	case "pack":
		resp.Checks = append(resp.Checks, hc.checkPack(inst)...)
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

	if hasFail {
		resp.Status = "unhealthy"
		resp.Summary = strings.Join(failures, ". ")
	} else if hasWarn {
		resp.Status = "degraded"
		resp.Summary = "Working with warnings"
	} else if len(resp.Checks) > 0 {
		resp.Status = "healthy"
		resp.Summary = "All checks passing"
	} else {
		resp.Status = "unknown"
	}

	return resp
}

// CheckAll evaluates health for all installed components.
func (hc *HubHealthChecker) CheckAll(registry *hub.Registry) []HubHealthResponse {
	instances := registry.List("")
	var results []HubHealthResponse
	for _, inst := range instances {
		results = append(results, hc.Check(&inst))
	}
	return results
}

func (hc *HubHealthChecker) checkConnector(inst *hub.Instance) []HealthCheck {
	var checks []HealthCheck

	// Check resolved YAML exists
	connectorYAML := filepath.Join(hc.Home, "connectors", inst.Name+".yaml")
	if _, err := os.Stat(connectorYAML); os.IsNotExist(err) {
		checks = append(checks, HealthCheck{
			Name: "connector_deployed", Status: "fail",
			Detail: "resolved YAML not published to connectors dir",
			Fix:    "agency hub activate " + inst.Name + " && agency infra rebuild intake",
		})
	} else {
		checks = append(checks, HealthCheck{
			Name: "connector_deployed", Status: "pass",
			Detail: "resolved YAML published for intake",
		})
	}

	// Check credentials
	checks = append(checks, hc.checkCredentials(inst.Name)...)

	// Check live poll health via intake service
	checks = append(checks, hc.checkPollHealth(inst.Name)...)

	return checks
}

func (hc *HubHealthChecker) checkPollHealth(connectorName string) []HealthCheck {
	resp, err := http.Get("http://127.0.0.1:8205/poll-health")
	if err != nil {
		return nil // intake not reachable, skip
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)

	var result struct {
		Connectors map[string]struct {
			ConsecutiveFailures int    `json:"consecutive_failures"`
			Status              string `json:"status"`
		} `json:"connectors"`
	}
	if json.Unmarshal(data, &result) != nil {
		return nil
	}

	conn, ok := result.Connectors[connectorName]
	if !ok {
		return nil // not a poll connector or not tracked
	}

	if conn.ConsecutiveFailures > 0 {
		return []HealthCheck{{
			Name:   "poll_health",
			Status: "fail",
			Detail: fmt.Sprintf("%d consecutive poll failures", conn.ConsecutiveFailures),
			Fix:    "Check credentials and egress proxy — docker logs agency-infra-intake",
		}}
	}
	return []HealthCheck{{
		Name: "poll_health", Status: "pass", Detail: "polling successfully",
	}}
}

func (hc *HubHealthChecker) checkService(inst *hub.Instance) []HealthCheck {
	var checks []HealthCheck

	// Check service YAML exists in registry
	svcPath := filepath.Join(hc.Home, "registry", "services", inst.Name+".yaml")
	if _, err := os.Stat(svcPath); os.IsNotExist(err) {
		checks = append(checks, HealthCheck{
			Name: "service_synced", Status: "fail",
			Detail: "service definition not synced to registry",
			Fix:    "agency hub upgrade",
		})
	} else {
		checks = append(checks, HealthCheck{
			Name: "service_synced", Status: "pass",
			Detail: "service definition in registry",
		})
	}

	// Check if service key exists
	keysPath := filepath.Join(hc.Home, "infrastructure", ".service-keys.env")
	keysData, _ := os.ReadFile(keysPath)
	if strings.Contains(string(keysData), inst.Name+"=") {
		checks = append(checks, HealthCheck{
			Name: "service_key", Status: "pass",
			Detail: "service key configured",
		})
	} else {
		checks = append(checks, HealthCheck{
			Name: "service_key", Status: "warn",
			Detail: "service key not found (may not be required)",
		})
	}

	return checks
}

func (hc *HubHealthChecker) checkPreset(inst *hub.Instance) []HealthCheck {
	// Presets are static config — if installed, they're healthy
	return []HealthCheck{{
		Name: "preset_installed", Status: "pass",
		Detail: "preset available",
	}}
}

func (hc *HubHealthChecker) checkPack(inst *hub.Instance) []HealthCheck {
	// Packs are deployment bundles — check if team exists
	return []HealthCheck{{
		Name: "pack_installed", Status: "pass",
		Detail: "pack available for deployment",
	}}
}

func (hc *HubHealthChecker) checkCredentials(connectorName string) []HealthCheck {
	var checks []HealthCheck

	connectorYAML := filepath.Join(hc.Home, "connectors", connectorName+".yaml")
	data, err := os.ReadFile(connectorYAML)
	if err != nil {
		return checks
	}

	keysPath := filepath.Join(hc.Home, "infrastructure", ".service-keys.env")
	keysData, _ := os.ReadFile(keysPath)
	keys := string(keysData)

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

		if strings.Contains(keys, grantName+"=") {
			// Check non-empty
			for _, kline := range strings.Split(keys, "\n") {
				if strings.HasPrefix(kline, grantName+"=") {
					val := strings.TrimPrefix(kline, grantName+"=")
					if strings.TrimSpace(val) == "" {
						checks = append(checks, HealthCheck{
							Name: "credential", Status: "fail",
							Detail: fmt.Sprintf("%s: empty", grantName),
						})
					} else {
						checks = append(checks, HealthCheck{
							Name: "credential", Status: "pass",
							Detail: fmt.Sprintf("%s: configured", grantName),
						})
					}
					break
				}
			}
		} else {
			checks = append(checks, HealthCheck{
				Name: "credential", Status: "fail",
				Detail: fmt.Sprintf("%s: not found", grantName),
				Fix:    fmt.Sprintf("Add %s to ~/.agency/infrastructure/.service-keys.env", grantName),
			})
		}
	}

	return checks
}
