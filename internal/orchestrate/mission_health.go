package orchestrate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"log/slog"

	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
	"gopkg.in/yaml.v3"

	"github.com/geoffbelknap/agency/internal/models"
	runtimecontract "github.com/geoffbelknap/agency/internal/runtime/contract"
)

// MissionHealthAlertFunc is called when a health check detects a problem.
// Implementations emit platform events and send operator alerts.
type MissionHealthAlertFunc func(missionName, reason string)

type missionRuntimeStatus interface {
	Get(ctx context.Context, runtimeID string) (runtimecontract.RuntimeStatus, error)
	Validate(ctx context.Context, runtimeID string) error
}

// MissionHealthMonitor runs periodic health checks on active missions.
// It checks every 60 seconds whether:
//  1. The assigned agent runtime components are running.
//  2. All required capabilities are still granted.
//
// If either check fails, it alerts the operator and auto-pauses the mission.
type MissionHealthMonitor struct {
	mm      *MissionManager
	runtime missionRuntimeStatus
	backend *runtimehost.DockerHandle
	alert   MissionHealthAlertFunc
	pause   func(name, reason string) error
	logger  *slog.Logger
	cancel  context.CancelFunc
}

// NewMissionHealthMonitor creates a health monitor.
// pauseFn is called to pause a mission when a problem is detected.
// alertFn is called with (missionName, reason) for every alert.
func NewMissionHealthMonitor(
	mm *MissionManager,
	alertFn MissionHealthAlertFunc,
	pauseFn func(name, reason string) error,
	logger *slog.Logger,
) (*MissionHealthMonitor, error) {
	return NewMissionHealthMonitorWithRuntime(mm, nil, alertFn, pauseFn, logger, nil)
}

// NewMissionHealthMonitorWithClient creates a health monitor using the provided backend client.
func NewMissionHealthMonitorWithClient(
	mm *MissionManager,
	alertFn MissionHealthAlertFunc,
	pauseFn func(name, reason string) error,
	logger *slog.Logger,
	dc *runtimehost.DockerHandle,
) (*MissionHealthMonitor, error) {
	return NewMissionHealthMonitorWithRuntime(mm, nil, alertFn, pauseFn, logger, dc)
}

// NewMissionHealthMonitorWithRuntime creates a health monitor using runtime
// status as the primary execution-health contract. The backend client is a
// compatibility fallback while host event shims are still being introduced.
func NewMissionHealthMonitorWithRuntime(
	mm *MissionManager,
	runtime missionRuntimeStatus,
	alertFn MissionHealthAlertFunc,
	pauseFn func(name, reason string) error,
	logger *slog.Logger,
	dc *runtimehost.DockerHandle,
) (*MissionHealthMonitor, error) {
	if logger == nil {
		logger = slog.Default()
	}
	return &MissionHealthMonitor{
		mm:      mm,
		runtime: runtime,
		backend: dc,
		alert:   alertFn,
		pause:   pauseFn,
		logger:  logger,
	}, nil
}

// Start launches the background health-check goroutine.
// The monitor runs until the returned context is cancelled.
func (m *MissionHealthMonitor) Start(ctx context.Context) {
	if m.runtime == nil && m.backend == nil {
		m.logger.Info("mission health monitor disabled: runtime backend client unavailable")
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	m.cancel = cancel

	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.runChecks(ctx)
			}
		}
	}()
}

// Stop cancels the background goroutine.
func (m *MissionHealthMonitor) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
}

// runChecks iterates all active missions and checks health.
func (m *MissionHealthMonitor) runChecks(ctx context.Context) {
	missions, err := m.mm.List()
	if err != nil {
		m.logger.Warn("mission health: failed to list missions", "error", err)
		return
	}

	running := map[string]string{}
	if m.runtime == nil {
		running = m.runningContainers(ctx)
	}

	for _, mission := range missions {
		if mission.Status != "active" {
			continue
		}
		m.checkMission(ctx, mission, running)
	}
}

// checkMission checks a single active mission.
func (m *MissionHealthMonitor) checkMission(ctx context.Context, mission *models.Mission, running map[string]string) {
	targets, err := missionHealthTargets(m.mm.Home, mission)
	if err != nil {
		m.logger.Warn("mission health alert: failed to resolve targets",
			"mission", mission.Name,
			"assigned_to", mission.AssignedTo,
			"assigned_type", mission.AssignedType,
			"error", err,
		)
		m.triggerAlert(mission, "could not resolve mission health targets")
		return
	}
	if len(targets) == 0 {
		return
	}

	var issues []string
	healthyTarget := ""
	for _, agentName := range targets {
		healthy, issue := m.runtimeTargetHealthy(ctx, agentName, running)
		if healthy {
			healthyTarget = agentName
			break
		}
		issues = append(issues, fmt.Sprintf("%s(%s)", agentName, issue))
	}
	if healthyTarget == "" {
		reason := fmt.Sprintf("no healthy execution target running for mission %q: %s", mission.Name, strings.Join(issues, ", "))
		m.logger.Warn("mission health alert: execution targets unavailable",
			"mission", mission.Name,
			"targets", targets,
			"issues", strings.Join(issues, ", "),
		)
		m.triggerAlert(mission, reason)
		return
	}

	// Check 3: are all required capabilities still granted?
	if mission.Requires == nil || len(mission.Requires.Capabilities) == 0 {
		return
	}

	missingCaps := m.checkCapabilities(mission)
	if len(missingCaps) > 0 {
		reason := fmt.Sprintf("required capabilities no longer available: %v", missingCaps)
		m.logger.Warn("mission health alert: capability revoked",
			"mission", mission.Name,
			"agent", healthyTarget,
			"missing", missingCaps,
		)
		m.triggerAlert(mission, reason)
	}
}

func (m *MissionHealthMonitor) runtimeTargetHealthy(ctx context.Context, agentName string, running map[string]string) (bool, string) {
	if m.runtime != nil {
		status, err := m.runtime.Get(ctx, agentName)
		if err != nil {
			return false, "runtime_status_error=" + err.Error()
		}
		if err := m.runtime.Validate(ctx, agentName); err != nil {
			return false, "runtime_validate_error=" + err.Error()
		}
		if status.Phase != runtimecontract.RuntimePhaseRunning {
			return false, "phase=" + status.Phase
		}
		if !status.Healthy {
			return false, "healthy=false"
		}
		if !status.Transport.EnforcerConnected {
			return false, "enforcer_connected=false"
		}
		return true, "runtime=running"
	}

	wsContainer := fmt.Sprintf("%s-%s-workspace", prefix, agentName)
	enfContainer := fmt.Sprintf("%s-%s-enforcer", prefix, agentName)
	wsState := running[wsContainer]
	enfState := running[enfContainer]
	if wsState == "running" && enfState == "running" {
		return true, "components=running"
	}
	return false, fmt.Sprintf("workspace=%q,enforcer=%q", wsState, enfState)
}

// triggerAlert fires the alert callback and auto-pauses the mission.
func (m *MissionHealthMonitor) triggerAlert(mission *models.Mission, reason string) {
	if m.alert != nil {
		m.alert(mission.Name, reason)
	}

	// Auto-pause the mission.
	if err := m.pause(mission.Name, reason); err != nil {
		m.logger.Error("mission health: failed to pause mission",
			"mission", mission.Name,
			"error", err,
		)
	}
}

// checkCapabilities reads the platform capabilities file and returns the list
// of mission-required capabilities that are no longer available.
func (m *MissionHealthMonitor) checkCapabilities(mission *models.Mission) []string {
	capsPath := filepath.Join(m.mm.Home, "capabilities.yaml")
	data, err := os.ReadFile(capsPath)
	if err != nil {
		// Can't read caps file — assume available to avoid false-positive alerts.
		return nil
	}

	var capsFile models.CapabilitiesFile
	if err := yaml.Unmarshal(data, &capsFile); err != nil {
		return nil
	}

	var missing []string
	for _, cap := range mission.Requires.Capabilities {
		cfg, exists := capsFile.Capabilities[cap]
		if !exists || (cfg.State != "" && cfg.State != "available") {
			missing = append(missing, cap)
		}
	}
	return missing
}

// runningContainers returns a map of runtime component name → state for all
// Agency runtime components that are currently listed by the host backend.
func (m *MissionHealthMonitor) runningContainers(ctx context.Context) map[string]string {
	result, err := runtimehost.ListAgencyContainerStates(ctx, m.backend)
	if err != nil {
		m.logger.Warn("mission health: runtime component list failed", "error", err)
		return map[string]string{}
	}
	return result
}

// HealthStatus describes the health of a single active mission.
type HealthStatus struct {
	MissionName  string   `json:"mission_name"`
	AgentName    string   `json:"agent_name"`
	AgentRunning bool     `json:"agent_running"`
	MissingCaps  []string `json:"missing_capabilities,omitempty"`
	Healthy      bool     `json:"healthy"`
}

// CheckHealth performs an on-demand health check for a named mission.
// Returns nil if the mission is not found or not active.
func (m *MissionHealthMonitor) CheckHealth(ctx context.Context, missionName string) (*HealthStatus, error) {
	mission, err := m.mm.Get(missionName)
	if err != nil {
		return nil, err
	}
	if mission.Status != "active" {
		return &HealthStatus{
			MissionName:  mission.Name,
			AgentName:    mission.AssignedTo,
			AgentRunning: false,
			Healthy:      false,
		}, nil
	}

	agentName := mission.AssignedTo
	running := map[string]string{}
	if m.runtime == nil {
		running = m.runningContainers(ctx)
	}
	agentRunning, _ := m.runtimeTargetHealthy(ctx, agentName, running)

	var missingCaps []string
	if mission.Requires != nil && len(mission.Requires.Capabilities) > 0 {
		missingCaps = m.checkCapabilities(mission)
	}

	return &HealthStatus{
		MissionName:  mission.Name,
		AgentName:    agentName,
		AgentRunning: agentRunning,
		MissingCaps:  missingCaps,
		Healthy:      agentRunning && len(missingCaps) == 0,
	}, nil
}
