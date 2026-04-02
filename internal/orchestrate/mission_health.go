package orchestrate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/charmbracelet/log"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"gopkg.in/yaml.v3"

	"github.com/geoffbelknap/agency/internal/models"
)

// MissionHealthAlertFunc is called when a health check detects a problem.
// Implementations emit platform events and send operator alerts.
type MissionHealthAlertFunc func(missionName, reason string)

// MissionHealthMonitor runs periodic health checks on active missions.
// It checks every 60 seconds whether:
//  1. The assigned agent is running (container state == "running").
//  2. All required capabilities are still granted.
//
// If either check fails, it alerts the operator and auto-pauses the mission.
type MissionHealthMonitor struct {
	mm      *MissionManager
	cli     *client.Client
	alert   MissionHealthAlertFunc
	pause   func(name, reason string) error
	logger  *log.Logger
	cancel  context.CancelFunc
}

// NewMissionHealthMonitor creates a health monitor.
// pauseFn is called to pause a mission when a problem is detected.
// alertFn is called with (missionName, reason) for every alert.
func NewMissionHealthMonitor(
	mm *MissionManager,
	alertFn MissionHealthAlertFunc,
	pauseFn func(name, reason string) error,
	logger *log.Logger,
) (*MissionHealthMonitor, error) {
	return NewMissionHealthMonitorWithClient(mm, alertFn, pauseFn, logger, nil)
}

// NewMissionHealthMonitorWithClient creates a health monitor using the provided Docker
// client (or a new one if cli is nil). Prefer passing a shared client.
func NewMissionHealthMonitorWithClient(
	mm *MissionManager,
	alertFn MissionHealthAlertFunc,
	pauseFn func(name, reason string) error,
	logger *log.Logger,
	cli *client.Client,
) (*MissionHealthMonitor, error) {
	if cli == nil {
		var err error
		cli, err = client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
		if err != nil {
			return nil, fmt.Errorf("mission health monitor: docker client: %w", err)
		}
	}
	return &MissionHealthMonitor{
		mm:     mm,
		cli:    cli,
		alert:  alertFn,
		pause:  pauseFn,
		logger: logger,
	}, nil
}

// Start launches the background health-check goroutine.
// The monitor runs until the returned context is cancelled.
func (m *MissionHealthMonitor) Start(ctx context.Context) {
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

	running := m.runningContainers(ctx)

	for _, mission := range missions {
		if mission.Status != "active" {
			continue
		}
		m.checkMission(mission, running)
	}
}

// checkMission checks a single active mission.
func (m *MissionHealthMonitor) checkMission(mission *models.Mission, running map[string]string) {
	agentName := mission.AssignedTo
	if agentName == "" {
		return
	}

	// Check 1: is the assigned agent running?
	wsContainer := fmt.Sprintf("%s-%s-workspace", prefix, agentName)
	state, ok := running[wsContainer]
	if !ok || state != "running" {
		reason := fmt.Sprintf("agent %q workspace container is not running (state=%q)", agentName, state)
		m.logger.Warn("mission health alert: agent stopped",
			"mission", mission.Name,
			"agent", agentName,
			"container", wsContainer,
			"state", state,
		)
		m.triggerAlert(mission, reason)
		return
	}

	// Check 2: is the enforcer running? (ASK Tenet 3: mediation must be complete)
	enfContainer := fmt.Sprintf("%s-%s-enforcer", prefix, agentName)
	enfState := running[enfContainer]
	if enfState != "running" {
		reason := fmt.Sprintf("agent %q enforcer is not running (state=%q) — unmediated API access", agentName, enfState)
		m.logger.Warn("mission health alert: enforcer down",
			"mission", mission.Name,
			"agent", agentName,
			"container", enfContainer,
			"state", enfState,
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
			"agent", agentName,
			"missing", missingCaps,
		)
		m.triggerAlert(mission, reason)
	}
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

// runningContainers returns a map of container name → state for all agency
// containers that are currently listed by Docker.
func (m *MissionHealthMonitor) runningContainers(ctx context.Context) map[string]string {
	result := make(map[string]string)
	containers, err := m.cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("name", prefix+"-")),
	})
	if err != nil {
		m.logger.Warn("mission health: docker list failed", "error", err)
		return result
	}
	for _, c := range containers {
		for _, n := range c.Names {
			name := n
			if len(name) > 0 && name[0] == '/' {
				name = name[1:]
			}
			result[name] = c.State
		}
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
			MissionName: mission.Name,
			AgentName:   mission.AssignedTo,
			AgentRunning: false,
			Healthy:     false,
		}, nil
	}

	running := m.runningContainers(ctx)
	agentName := mission.AssignedTo
	wsContainer := fmt.Sprintf("%s-%s-workspace", prefix, agentName)
	state, ok := running[wsContainer]
	agentRunning := ok && state == "running"

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
