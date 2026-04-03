package orchestrate

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

	"github.com/geoffbelknap/agency/internal/models"
)

// MissionManager handles CRUD and lifecycle operations on missions.
// Missions are stored as YAML files in ~/.agency/missions/{name}.yaml.
// Version history is stored as JSONL in ~/.agency/missions/.history/{id}.jsonl.
type MissionManager struct {
	Home string
}

// NewMissionManager creates a new MissionManager rooted at home.
func NewMissionManager(home string) *MissionManager {
	return &MissionManager{Home: home}
}

func (mm *MissionManager) missionsDir() string {
	return filepath.Join(mm.Home, "missions")
}

func (mm *MissionManager) historyDir() string {
	return filepath.Join(mm.Home, "missions", ".history")
}

func (mm *MissionManager) missionPath(name string) string {
	return filepath.Join(mm.missionsDir(), name+".yaml")
}

func (mm *MissionManager) historyPath(id string) string {
	return filepath.Join(mm.historyDir(), id+".jsonl")
}

func (mm *MissionManager) agentMissionPath(agentName string) string {
	return filepath.Join(mm.Home, "agents", agentName, "mission.yaml")
}

// Create validates and persists a new mission.
// If ID is empty a UUID is generated. Version is set to 1 and status to unassigned.
func (mm *MissionManager) Create(m *models.Mission) error {
	if m.ID == "" {
		m.ID = uuid.New().String()
	}
	m.Version = 1
	m.Status = "unassigned"

	if err := m.Validate(); err != nil {
		return fmt.Errorf("invalid mission: %w", err)
	}

	if err := os.MkdirAll(mm.missionsDir(), 0755); err != nil {
		return fmt.Errorf("create missions dir: %w", err)
	}
	if err := os.MkdirAll(mm.historyDir(), 0755); err != nil {
		return fmt.Errorf("create history dir: %w", err)
	}

	path := mm.missionPath(m.Name)
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("mission %q already exists", m.Name)
	}

	if err := writeMissionYAML(path, m); err != nil {
		return fmt.Errorf("write mission: %w", err)
	}

	if err := mm.appendHistory(m); err != nil {
		return fmt.Errorf("write history: %w", err)
	}

	return nil
}

// Get reads and returns the named mission.
func (mm *MissionManager) Get(name string) (*models.Mission, error) {
	path := mm.missionPath(name)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("mission %q not found", name)
		}
		return nil, fmt.Errorf("read mission %q: %w", name, err)
	}

	var m models.Mission
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse mission %q: %w", name, err)
	}
	if m.Status == "" {
		m.Status = "unassigned"
	}
	return &m, nil
}

// List returns all missions in the missions directory.
func (mm *MissionManager) List() ([]*models.Mission, error) {
	entries, err := os.ReadDir(mm.missionsDir())
	if err != nil {
		if os.IsNotExist(err) {
			return []*models.Mission{}, nil
		}
		return nil, fmt.Errorf("list missions: %w", err)
	}

	var missions []*models.Mission
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		name := e.Name()[:len(e.Name())-5] // strip .yaml
		m, err := mm.Get(name)
		if err != nil {
			continue
		}
		missions = append(missions, m)
	}
	return missions, nil
}

// Update merges new content into an existing mission while preserving runtime
// state (status, assignment, ID, version). Content fields (instructions,
// triggers, budget, etc.) are replaced from the updated mission. The previous
// version is written to history before the file is updated.
func (mm *MissionManager) Update(name string, updated *models.Mission) error {
	existing, err := mm.Get(name)
	if err != nil {
		return err
	}

	// Preserve runtime state from existing mission
	updated.ID = existing.ID
	updated.Name = existing.Name
	updated.Version = existing.Version + 1
	updated.Status = existing.Status
	updated.AssignedTo = existing.AssignedTo
	updated.AssignedType = existing.AssignedType

	if err := updated.Validate(); err != nil {
		return fmt.Errorf("invalid mission update: %w", err)
	}

	// Append the existing version to history before overwriting.
	if err := mm.appendHistory(existing); err != nil {
		return fmt.Errorf("write history: %w", err)
	}

	if err := writeMissionYAML(mm.missionPath(name), updated); err != nil {
		return fmt.Errorf("write mission: %w", err)
	}

	// If the mission is assigned, update the agent's copy too
	if existing.AssignedTo != "" {
		if err := mm.writeAgentCopy(existing.AssignedTo, updated); err != nil {
			return fmt.Errorf("update agent copy: %w", err)
		}
	}

	return nil
}

// Delete removes the named mission. Only unassigned missions can be deleted.
func (mm *MissionManager) Delete(name string) error {
	m, err := mm.Get(name)
	if err != nil {
		return err
	}
	if m.Status == "active" {
		return fmt.Errorf("mission %q cannot be deleted while active — pause or complete it first", name)
	}
	return os.Remove(mm.missionPath(name))
}

// PreFlightResult holds the outcome of a pre-flight check.
type PreFlightResult struct {
	OK       bool     `json:"ok"`
	Failures []string `json:"failures,omitempty"`
}

// PreFlight validates that the target exists, required capabilities are available,
// and the agent does not already have an active mission.
func (mm *MissionManager) PreFlight(mission *models.Mission, target, targetType string) *PreFlightResult {
	result := &PreFlightResult{OK: true}

	// Check if agent/team exists.
	if targetType == "agent" {
		agentDir := filepath.Join(mm.Home, "agents", target)
		if _, err := os.Stat(filepath.Join(agentDir, "agent.yaml")); err != nil {
			result.OK = false
			result.Failures = append(result.Failures, fmt.Sprintf("agent %q not found", target))
		}
	} else if targetType == "team" {
		teamDir := filepath.Join(mm.Home, "teams", target)
		if _, err := os.Stat(filepath.Join(teamDir, "team.yaml")); err != nil {
			result.OK = false
			result.Failures = append(result.Failures, fmt.Sprintf("team %q not found", target))
		}
	}

	// Check required capabilities.
	if mission.Requires != nil {
		for _, cap := range mission.Requires.Capabilities {
			capsPath := filepath.Join(mm.Home, "capabilities.yaml")
			if data, err := os.ReadFile(capsPath); err == nil {
				var capsFile models.CapabilitiesFile
				yaml.Unmarshal(data, &capsFile) //nolint:errcheck
				cfg, exists := capsFile.Capabilities[cap]
				// An empty State means the default "available" per the model.
				// Reject if the capability is absent or explicitly not "available".
				if !exists || (cfg.State != "" && cfg.State != "available") {
					result.OK = false
					result.Failures = append(result.Failures, fmt.Sprintf("required capability %q is not available", cap))
				}
			}
		}
	}

	// Check if agent already has an active mission.
	if existing := mm.GetActiveForAgent(target); existing != nil {
		result.OK = false
		result.Failures = append(result.Failures, fmt.Sprintf("agent %q already has active mission %q", target, existing.Name))
	}

	return result
}

// CheckRequirements checks whether the granted capabilities satisfy the mission's requirements.
// Returns the list of missing capabilities, or nil if all are satisfied.
func (mm *MissionManager) CheckRequirements(name string, grantedCaps []string) ([]string, error) {
	mission, err := mm.Get(name)
	if err != nil || mission.Requires == nil {
		return nil, err
	}
	granted := make(map[string]bool, len(grantedCaps))
	for _, c := range grantedCaps {
		granted[c] = true
	}
	var missing []string
	for _, req := range mission.Requires.Capabilities {
		if !granted[req] {
			missing = append(missing, req)
		}
	}
	return missing, nil
}

// Assign assigns a mission to an agent or team. The mission must be unassigned.
// A copy of the mission YAML is written to the agent's directory.
func (mm *MissionManager) Assign(name, target, targetType string) error {
	m, err := mm.Get(name)
	if err != nil {
		return err
	}
	if m.Status != "unassigned" {
		return fmt.Errorf("mission %q cannot be assigned: status is %q (must be unassigned)", name, m.Status)
	}

	// Pre-flight checks.
	pf := mm.PreFlight(m, target, targetType)
	if !pf.OK {
		return fmt.Errorf("pre-flight failed: %s", strings.Join(pf.Failures, "; "))
	}

	m.AssignedTo = target
	m.AssignedType = targetType
	m.Status = "active"

	if err := writeMissionYAML(mm.missionPath(name), m); err != nil {
		return fmt.Errorf("write mission: %w", err)
	}

	if err := mm.writeAgentCopy(target, m); err != nil {
		return fmt.Errorf("write agent copy: %w", err)
	}
	return nil
}

// AssignToTeam assigns a mission to a team. If the team has a coordinator,
// the mission YAML is written only to the coordinator's agent dir.
// If no coordinator, it is written to all agent members' dirs.
// ASK tenet 11: coordinator must hold all required capabilities.
func (mm *MissionManager) AssignToTeam(name, teamName string, teamCfg *models.TeamConfig) error {
	m, err := mm.Get(name)
	if err != nil {
		return err
	}
	if m.Status != "unassigned" {
		return fmt.Errorf("mission %q cannot be assigned: status is %q (must be unassigned)", name, m.Status)
	}

	// ASK tenet 11: coordinator must hold all required capabilities (delegation
	// cannot exceed delegator scope).
	if teamCfg.Coordinator != "" && m.Requires != nil {
		coordCaps, _ := mm.getAgentCapabilities(teamCfg.Coordinator)
		for _, reqCap := range m.Requires.Capabilities {
			if !containsStr(coordCaps, reqCap) {
				return fmt.Errorf("coordinator %s missing required capability: %s (ASK tenet 11)", teamCfg.Coordinator, reqCap)
			}
		}
	}

	m.AssignedTo = teamName
	m.AssignedType = "team"
	m.Status = "active"

	if err := writeMissionYAML(mm.missionPath(name), m); err != nil {
		return fmt.Errorf("write mission: %w", err)
	}

	// Copy mission file to appropriate agents.
	if teamCfg.Coordinator != "" {
		if err := mm.writeAgentCopy(teamCfg.Coordinator, m); err != nil {
			return fmt.Errorf("write coordinator copy: %w", err)
		}
	} else {
		for _, member := range teamCfg.Members {
			if member.Type == "agent" {
				if err := mm.writeAgentCopy(member.Name, m); err != nil {
					return fmt.Errorf("write member copy for %s: %w", member.Name, err)
				}
			}
		}
	}
	return nil
}

// LoadTeamConfig reads team.yaml from ~/.agency/teams/{teamName}/team.yaml.
func (mm *MissionManager) LoadTeamConfig(teamName string) (*models.TeamConfig, error) {
	teamPath := filepath.Join(mm.Home, "teams", teamName, "team.yaml")
	data, err := os.ReadFile(teamPath)
	if err != nil {
		return nil, fmt.Errorf("read team config %q: %w", teamName, err)
	}
	var tc models.TeamConfig
	if err := yaml.Unmarshal(data, &tc); err != nil {
		return nil, fmt.Errorf("parse team config %q: %w", teamName, err)
	}
	return &tc, nil
}

// getAgentCapabilities reads granted capabilities for an agent.
func (mm *MissionManager) getAgentCapabilities(agentName string) ([]string, error) {
	capsPath := filepath.Join(mm.Home, "agents", agentName, "capabilities.yaml")
	data, err := os.ReadFile(capsPath)
	if err != nil {
		// Try platform-level capabilities file as fallback.
		capsPath = filepath.Join(mm.Home, "capabilities.yaml")
		data, err = os.ReadFile(capsPath)
		if err != nil {
			return nil, nil
		}
	}
	var capsFile models.CapabilitiesFile
	if err := yaml.Unmarshal(data, &capsFile); err != nil {
		return nil, err
	}
	var caps []string
	for name, cfg := range capsFile.Capabilities {
		if cfg.State == "" || cfg.State == "available" {
			caps = append(caps, name)
		}
	}
	return caps, nil
}

// RemoveMissionFromAgent removes the mission.yaml from the agent's directory.
func (mm *MissionManager) RemoveMissionFromAgent(agentName string) {
	agentCopy := mm.agentMissionPath(agentName)
	if _, err := os.Stat(agentCopy); err == nil {
		os.Remove(agentCopy)
	}
}

// AssignCoverageAgent writes the mission YAML to the coverage agent's directory
// for coordinator failover (ASK tenet 14).
func (mm *MissionManager) AssignCoverageAgent(m *models.Mission, coverageAgent string) error {
	return mm.writeAgentCopy(coverageAgent, m)
}

// containsStr checks if a slice contains a string.
func containsStr(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

// Pause sets a mission's status to paused. The mission must be active.
func (mm *MissionManager) Pause(name, reason string) error {
	m, err := mm.Get(name)
	if err != nil {
		return err
	}
	if m.Status != "active" {
		return fmt.Errorf("mission %q cannot be paused: status is %q (must be active)", name, m.Status)
	}

	_ = reason // stored in history via the YAML, not as a separate field currently
	m.Status = "paused"

	if err := writeMissionYAML(mm.missionPath(name), m); err != nil {
		return fmt.Errorf("write mission: %w", err)
	}

	if m.AssignedTo != "" {
		if err := mm.writeAgentCopy(m.AssignedTo, m); err != nil {
			return fmt.Errorf("update agent copy: %w", err)
		}
	}
	return nil
}

// Resume sets a mission's status back to active. The mission must be paused.
func (mm *MissionManager) Resume(name string) error {
	m, err := mm.Get(name)
	if err != nil {
		return err
	}
	if m.Status != "paused" && m.Status != "completed" {
		return fmt.Errorf("mission %q cannot be resumed: status is %q (must be paused or completed)", name, m.Status)
	}

	m.Status = "active"

	if err := writeMissionYAML(mm.missionPath(name), m); err != nil {
		return fmt.Errorf("write mission: %w", err)
	}

	if m.AssignedTo != "" {
		if err := mm.writeAgentCopy(m.AssignedTo, m); err != nil {
			return fmt.Errorf("update agent copy: %w", err)
		}
	}
	return nil
}

// Complete marks a mission as completed. The mission must be active or paused.
// The agent copy is removed on completion.
func (mm *MissionManager) Complete(name string) error {
	m, err := mm.Get(name)
	if err != nil {
		return err
	}
	if m.Status != "active" && m.Status != "paused" {
		return fmt.Errorf("mission %q cannot be completed: status is %q (must be active or paused)", name, m.Status)
	}

	agentName := m.AssignedTo
	m.Status = "completed"

	if err := writeMissionYAML(mm.missionPath(name), m); err != nil {
		return fmt.Errorf("write mission: %w", err)
	}

	if agentName != "" {
		agentCopy := mm.agentMissionPath(agentName)
		if _, err := os.Stat(agentCopy); err == nil {
			os.Remove(agentCopy)
		}
	}
	return nil
}

// GetActiveForAgent returns the first active mission assigned to the named agent,
// or nil if no such mission exists.
func (mm *MissionManager) GetActiveForAgent(agentName string) *models.Mission {
	missions, err := mm.List()
	if err != nil {
		return nil
	}
	for _, m := range missions {
		if m.AssignedTo == agentName && m.Status == "active" {
			return m
		}
	}
	return nil
}

// History returns all recorded history entries for the mission identified by id.
// Each entry is the parsed JSON object from a line in the JSONL file.
func (mm *MissionManager) History(name string) ([]map[string]interface{}, error) {
	// We need the mission ID to locate the history file.
	m, err := mm.Get(name)
	if err != nil {
		return nil, err
	}

	path := mm.historyPath(m.ID)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []map[string]interface{}{}, nil
		}
		return nil, fmt.Errorf("open history for %q: %w", name, err)
	}
	defer f.Close()

	var entries []map[string]interface{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var entry map[string]interface{}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read history for %q: %w", name, err)
	}
	return entries, nil
}

// -- internal helpers --

// appendHistory writes a JSONL entry for the current mission state.
func (mm *MissionManager) appendHistory(m *models.Mission) (err error) {
	if err := os.MkdirAll(mm.historyDir(), 0755); err != nil {
		return err
	}

	yamlBytes, err := yaml.Marshal(m)
	if err != nil {
		return err
	}

	entry := map[string]interface{}{
		"version":   m.Version,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"content":   string(yamlBytes),
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(mm.historyPath(m.ID), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	_, err = fmt.Fprintf(f, "%s\n", line)
	return err
}

// writeAgentCopy writes the mission YAML to the agent's directory.
func (mm *MissionManager) writeAgentCopy(agentName string, m *models.Mission) error {
	agentDir := filepath.Join(mm.Home, "agents", agentName)
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		return err
	}
	return writeMissionYAML(mm.agentMissionPath(agentName), m)
}

// writeMissionYAML marshals a Mission to YAML and writes it to path.
func writeMissionYAML(path string, m *models.Mission) error {
	data, err := yaml.Marshal(m)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
