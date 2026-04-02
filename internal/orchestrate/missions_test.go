package orchestrate

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/geoffbelknap/agency/internal/models"
)

// newTestMission returns a valid Mission for use in tests.
func newTestMission(name string) *models.Mission {
	return &models.Mission{
		Name:         name,
		Description:  "Test mission for " + name,
		Instructions: "Do the thing for " + name,
	}
}

// makeTestAgent creates an agent directory with a stub agent.yaml so PreFlight checks pass.
func makeTestAgent(t *testing.T, home, agentName string) {
	t.Helper()
	dir := filepath.Join(home, "agents", agentName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("makeTestAgent mkdir %s: %v", agentName, err)
	}
	stub := []byte("name: " + agentName + "\n")
	if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), stub, 0644); err != nil {
		t.Fatalf("makeTestAgent write agent.yaml %s: %v", agentName, err)
	}
}

func TestMissionCreate(t *testing.T) {
	mm := NewMissionManager(t.TempDir())

	m := newTestMission("test-mission")
	if err := mm.Create(m); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// ID, version and status should be set
	if m.ID == "" {
		t.Error("expected ID to be set")
	}
	if m.Version != 1 {
		t.Errorf("expected version 1, got %d", m.Version)
	}
	if m.Status != "unassigned" {
		t.Errorf("expected status unassigned, got %q", m.Status)
	}

	// File must exist
	path := mm.missionPath("test-mission")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("mission file not found: %v", err)
	}

	// History file must exist with one entry
	entries, err := mm.History("test-mission")
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 history entry, got %d", len(entries))
	}
}

func TestMissionCreateDuplicate(t *testing.T) {
	mm := NewMissionManager(t.TempDir())

	m := newTestMission("dupe")
	if err := mm.Create(m); err != nil {
		t.Fatalf("first Create: %v", err)
	}

	m2 := newTestMission("dupe")
	if err := mm.Create(m2); err == nil {
		t.Error("expected error on duplicate create")
	}
}

func TestMissionCreateInvalid(t *testing.T) {
	mm := NewMissionManager(t.TempDir())

	// Missing name
	m := &models.Mission{Description: "desc", Instructions: "do it"}
	if err := mm.Create(m); err == nil {
		t.Error("expected error for missing name")
	}

	// Missing instructions
	m2 := &models.Mission{Name: "ok-name", Description: "desc"}
	if err := mm.Create(m2); err == nil {
		t.Error("expected error for missing instructions")
	}
}

func TestMissionGet(t *testing.T) {
	mm := NewMissionManager(t.TempDir())

	m := newTestMission("get-me")
	if err := mm.Create(m); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := mm.Get("get-me")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "get-me" {
		t.Errorf("expected name get-me, got %q", got.Name)
	}
	if got.ID != m.ID {
		t.Errorf("ID mismatch: %q vs %q", got.ID, m.ID)
	}
}

func TestMissionGetNotFound(t *testing.T) {
	mm := NewMissionManager(t.TempDir())
	if _, err := mm.Get("no-such-mission"); err == nil {
		t.Error("expected error for missing mission")
	}
}

func TestMissionList(t *testing.T) {
	mm := NewMissionManager(t.TempDir())

	// Empty list
	missions, err := mm.List()
	if err != nil {
		t.Fatalf("List on empty dir: %v", err)
	}
	if len(missions) != 0 {
		t.Errorf("expected 0, got %d", len(missions))
	}

	// Create three
	for _, name := range []string{"alpha", "beta", "gamma"} {
		if err := mm.Create(newTestMission(name)); err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
	}

	missions, err = mm.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(missions) != 3 {
		t.Errorf("expected 3, got %d", len(missions))
	}
}

func TestMissionUpdate(t *testing.T) {
	mm := NewMissionManager(t.TempDir())

	m := newTestMission("updatable")
	if err := mm.Create(m); err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated := &models.Mission{
		Name:         "updatable",
		Description:  "Updated description",
		Instructions: "Updated instructions",
	}
	if err := mm.Update("updatable", updated); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := mm.Get("updatable")
	if err != nil {
		t.Fatalf("Get after Update: %v", err)
	}
	if got.Version != 2 {
		t.Errorf("expected version 2, got %d", got.Version)
	}
	if got.Description != "Updated description" {
		t.Errorf("description not updated: %q", got.Description)
	}
	// ID must be preserved
	if got.ID != m.ID {
		t.Errorf("ID changed after update: %q vs %q", got.ID, m.ID)
	}

	// History should have two entries: v1 from Create and v1 again from Update (old version)
	entries, err := mm.History("updatable")
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 history entries (create + update), got %d", len(entries))
	}
}

func TestMissionDelete(t *testing.T) {
	mm := NewMissionManager(t.TempDir())

	m := newTestMission("to-delete")
	if err := mm.Create(m); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := mm.Delete("to-delete"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := mm.Get("to-delete"); err == nil {
		t.Error("expected error after delete")
	}
}

func TestMissionDeleteActiveBlocked(t *testing.T) {
	mm := NewMissionManager(t.TempDir())

	m := newTestMission("active-nodelet")
	if err := mm.Create(m); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Make agent dir so Assign can write the copy
	makeTestAgent(t, mm.Home, "bot")
	if err := mm.Assign("active-nodelet", "bot", "agent"); err != nil {
		t.Fatalf("Assign: %v", err)
	}

	if err := mm.Delete("active-nodelet"); err == nil {
		t.Error("expected error when deleting active mission")
	}
}

func TestMissionAssign(t *testing.T) {
	mm := NewMissionManager(t.TempDir())

	m := newTestMission("assignable")
	if err := mm.Create(m); err != nil {
		t.Fatalf("Create: %v", err)
	}

	makeTestAgent(t, mm.Home, "worker-1")
	if err := mm.Assign("assignable", "worker-1", "agent"); err != nil {
		t.Fatalf("Assign: %v", err)
	}

	got, err := mm.Get("assignable")
	if err != nil {
		t.Fatalf("Get after Assign: %v", err)
	}
	if got.Status != "active" {
		t.Errorf("expected status active, got %q", got.Status)
	}
	if got.AssignedTo != "worker-1" {
		t.Errorf("expected AssignedTo worker-1, got %q", got.AssignedTo)
	}
	if got.AssignedType != "agent" {
		t.Errorf("expected AssignedType agent, got %q", got.AssignedType)
	}

	// Agent copy should exist
	agentCopy := mm.agentMissionPath("worker-1")
	if _, err := os.Stat(agentCopy); err != nil {
		t.Errorf("agent copy not found: %v", err)
	}
}

func TestMissionAssignAlreadyAssigned(t *testing.T) {
	mm := NewMissionManager(t.TempDir())

	m := newTestMission("double-assign")
	if err := mm.Create(m); err != nil {
		t.Fatalf("Create: %v", err)
	}

	makeTestAgent(t, mm.Home, "agent-a")
	if err := mm.Assign("double-assign", "agent-a", "agent"); err != nil {
		t.Fatalf("first Assign: %v", err)
	}

	makeTestAgent(t, mm.Home, "agent-b")
	if err := mm.Assign("double-assign", "agent-b", "agent"); err == nil {
		t.Error("expected error assigning already-assigned mission")
	}
}

func TestMissionPause(t *testing.T) {
	mm := NewMissionManager(t.TempDir())

	m := newTestMission("pauseable")
	if err := mm.Create(m); err != nil {
		t.Fatalf("Create: %v", err)
	}

	makeTestAgent(t, mm.Home, "pauser")
	if err := mm.Assign("pauseable", "pauser", "agent"); err != nil {
		t.Fatalf("Assign: %v", err)
	}
	if err := mm.Pause("pauseable", "maintenance"); err != nil {
		t.Fatalf("Pause: %v", err)
	}

	got, err := mm.Get("pauseable")
	if err != nil {
		t.Fatalf("Get after Pause: %v", err)
	}
	if got.Status != "paused" {
		t.Errorf("expected status paused, got %q", got.Status)
	}

	// Agent copy should reflect paused status
	data, err := os.ReadFile(mm.agentMissionPath("pauser"))
	if err != nil {
		t.Fatalf("read agent copy: %v", err)
	}
	var agentM models.Mission
	if err := yaml.Unmarshal(data, &agentM); err != nil {
		t.Fatalf("parse agent copy: %v", err)
	}
	if agentM.Status != "paused" {
		t.Errorf("agent copy status should be paused, got %q", agentM.Status)
	}
}

func TestMissionPauseNonActive(t *testing.T) {
	mm := NewMissionManager(t.TempDir())

	m := newTestMission("not-active")
	if err := mm.Create(m); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := mm.Pause("not-active", "nope"); err == nil {
		t.Error("expected error pausing unassigned mission")
	}
}

func TestMissionResume(t *testing.T) {
	mm := NewMissionManager(t.TempDir())

	m := newTestMission("resumeable")
	if err := mm.Create(m); err != nil {
		t.Fatalf("Create: %v", err)
	}

	makeTestAgent(t, mm.Home, "resumer")
	if err := mm.Assign("resumeable", "resumer", "agent"); err != nil {
		t.Fatalf("Assign: %v", err)
	}
	if err := mm.Pause("resumeable", "break"); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if err := mm.Resume("resumeable"); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	got, err := mm.Get("resumeable")
	if err != nil {
		t.Fatalf("Get after Resume: %v", err)
	}
	if got.Status != "active" {
		t.Errorf("expected status active, got %q", got.Status)
	}
}

func TestMissionResumeNonPaused(t *testing.T) {
	mm := NewMissionManager(t.TempDir())

	m := newTestMission("not-paused")
	if err := mm.Create(m); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := mm.Resume("not-paused"); err == nil {
		t.Error("expected error resuming unassigned mission")
	}
}

func TestMissionComplete(t *testing.T) {
	mm := NewMissionManager(t.TempDir())

	m := newTestMission("completeable")
	if err := mm.Create(m); err != nil {
		t.Fatalf("Create: %v", err)
	}

	makeTestAgent(t, mm.Home, "completer")
	if err := mm.Assign("completeable", "completer", "agent"); err != nil {
		t.Fatalf("Assign: %v", err)
	}
	if err := mm.Complete("completeable"); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	got, err := mm.Get("completeable")
	if err != nil {
		t.Fatalf("Get after Complete: %v", err)
	}
	if got.Status != "completed" {
		t.Errorf("expected status completed, got %q", got.Status)
	}

	// Agent copy should be removed
	agentCopy := mm.agentMissionPath("completer")
	if _, err := os.Stat(agentCopy); err == nil {
		t.Error("agent copy should have been removed on complete")
	}
}

func TestMissionCompletePaused(t *testing.T) {
	mm := NewMissionManager(t.TempDir())

	m := newTestMission("complete-from-paused")
	if err := mm.Create(m); err != nil {
		t.Fatalf("Create: %v", err)
	}

	makeTestAgent(t, mm.Home, "agent-c")
	if err := mm.Assign("complete-from-paused", "agent-c", "agent"); err != nil {
		t.Fatalf("Assign: %v", err)
	}
	if err := mm.Pause("complete-from-paused", "hold"); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if err := mm.Complete("complete-from-paused"); err != nil {
		t.Fatalf("Complete from paused: %v", err)
	}

	got, _ := mm.Get("complete-from-paused")
	if got.Status != "completed" {
		t.Errorf("expected completed, got %q", got.Status)
	}
}

func TestMissionCompleteUnassigned(t *testing.T) {
	mm := NewMissionManager(t.TempDir())

	m := newTestMission("cant-complete")
	if err := mm.Create(m); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := mm.Complete("cant-complete"); err == nil {
		t.Error("expected error completing unassigned mission")
	}
}

func TestMissionGetActiveForAgent(t *testing.T) {
	mm := NewMissionManager(t.TempDir())

	m := newTestMission("agent-active")
	if err := mm.Create(m); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Nothing active yet
	if got := mm.GetActiveForAgent("worker"); got != nil {
		t.Errorf("expected nil, got %v", got)
	}

	makeTestAgent(t, mm.Home, "worker")
	if err := mm.Assign("agent-active", "worker", "agent"); err != nil {
		t.Fatalf("Assign: %v", err)
	}

	got := mm.GetActiveForAgent("worker")
	if got == nil {
		t.Fatal("expected active mission, got nil")
	}
	if got.Name != "agent-active" {
		t.Errorf("expected agent-active, got %q", got.Name)
	}

	// Pause it — should no longer appear as active
	if err := mm.Pause("agent-active", "test"); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if got := mm.GetActiveForAgent("worker"); got != nil {
		t.Errorf("expected nil after pause, got %v", got)
	}
}

func TestMissionHistory(t *testing.T) {
	mm := NewMissionManager(t.TempDir())

	m := newTestMission("historical")
	if err := mm.Create(m); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Update twice to generate more history
	u1 := &models.Mission{Description: "rev2", Instructions: "instructions 2"}
	if err := mm.Update("historical", u1); err != nil {
		t.Fatalf("Update 1: %v", err)
	}
	u2 := &models.Mission{Description: "rev3", Instructions: "instructions 3"}
	if err := mm.Update("historical", u2); err != nil {
		t.Fatalf("Update 2: %v", err)
	}

	entries, err := mm.History("historical")
	if err != nil {
		t.Fatalf("History: %v", err)
	}

	// Create writes 1 entry; each Update appends the old version before overwriting, so 3 total.
	if len(entries) != 3 {
		t.Errorf("expected 3 history entries, got %d", len(entries))
	}

	// Each entry should have version, timestamp, and content fields
	for i, e := range entries {
		if _, ok := e["version"]; !ok {
			t.Errorf("entry %d missing version", i)
		}
		if _, ok := e["timestamp"]; !ok {
			t.Errorf("entry %d missing timestamp", i)
		}
		if _, ok := e["content"]; !ok {
			t.Errorf("entry %d missing content", i)
		}
	}
}

func TestMissionHistoryEmpty(t *testing.T) {
	mm := NewMissionManager(t.TempDir())

	m := newTestMission("no-history")
	if err := mm.Create(m); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Remove the history file to simulate the empty case
	os.Remove(mm.historyPath(m.ID))

	entries, err := mm.History("no-history")
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

// -- PreFlight tests --

func TestPreFlightAgentExists(t *testing.T) {
	mm := NewMissionManager(t.TempDir())

	m := newTestMission("pf-agent-exists")
	if err := mm.Create(m); err != nil {
		t.Fatalf("Create: %v", err)
	}

	makeTestAgent(t, mm.Home, "ready-agent")

	pf := mm.PreFlight(m, "ready-agent", "agent")
	if !pf.OK {
		t.Errorf("expected OK=true, got failures: %v", pf.Failures)
	}
	if len(pf.Failures) != 0 {
		t.Errorf("expected no failures, got %v", pf.Failures)
	}
}

func TestPreFlightAgentNotFound(t *testing.T) {
	mm := NewMissionManager(t.TempDir())

	m := newTestMission("pf-agent-missing")
	if err := mm.Create(m); err != nil {
		t.Fatalf("Create: %v", err)
	}

	pf := mm.PreFlight(m, "ghost-agent", "agent")
	if pf.OK {
		t.Error("expected OK=false when agent does not exist")
	}
	if len(pf.Failures) == 0 {
		t.Error("expected at least one failure message")
	}
}

func TestPreFlightTeamNotFound(t *testing.T) {
	mm := NewMissionManager(t.TempDir())

	m := newTestMission("pf-team-missing")
	if err := mm.Create(m); err != nil {
		t.Fatalf("Create: %v", err)
	}

	pf := mm.PreFlight(m, "ghost-team", "team")
	if pf.OK {
		t.Error("expected OK=false when team does not exist")
	}
}

func TestPreFlightTeamExists(t *testing.T) {
	mm := NewMissionManager(t.TempDir())

	m := newTestMission("pf-team-exists")
	if err := mm.Create(m); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Create team directory with team.yaml
	teamDir := filepath.Join(mm.Home, "teams", "alpha-team")
	os.MkdirAll(teamDir, 0755)
	os.WriteFile(filepath.Join(teamDir, "team.yaml"), []byte("name: alpha-team\n"), 0644)

	pf := mm.PreFlight(m, "alpha-team", "team")
	if !pf.OK {
		t.Errorf("expected OK=true for existing team, got failures: %v", pf.Failures)
	}
}

func TestPreFlightCapabilityAvailable(t *testing.T) {
	mm := NewMissionManager(t.TempDir())

	m := newTestMission("pf-cap-ok")
	m.Requires = &models.MissionRequires{
		Capabilities: []string{"brave-search"},
	}
	if err := mm.Create(m); err != nil {
		t.Fatalf("Create: %v", err)
	}

	makeTestAgent(t, mm.Home, "cap-agent")

	// Write capabilities.yaml with brave-search as available.
	capsYAML := `capabilities:
  brave-search:
    state: available
`
	os.WriteFile(filepath.Join(mm.Home, "capabilities.yaml"), []byte(capsYAML), 0644)

	pf := mm.PreFlight(m, "cap-agent", "agent")
	if !pf.OK {
		t.Errorf("expected OK=true when capability is available, got failures: %v", pf.Failures)
	}
}

func TestPreFlightCapabilityMissing(t *testing.T) {
	mm := NewMissionManager(t.TempDir())

	m := newTestMission("pf-cap-missing")
	m.Requires = &models.MissionRequires{
		Capabilities: []string{"slack"},
	}
	if err := mm.Create(m); err != nil {
		t.Fatalf("Create: %v", err)
	}

	makeTestAgent(t, mm.Home, "nocap-agent")

	// Write capabilities.yaml without slack.
	capsYAML := `capabilities:
  brave-search:
    state: available
`
	os.WriteFile(filepath.Join(mm.Home, "capabilities.yaml"), []byte(capsYAML), 0644)

	pf := mm.PreFlight(m, "nocap-agent", "agent")
	if pf.OK {
		t.Error("expected OK=false when required capability is absent")
	}
}

func TestPreFlightCapabilityDisabled(t *testing.T) {
	mm := NewMissionManager(t.TempDir())

	m := newTestMission("pf-cap-disabled")
	m.Requires = &models.MissionRequires{
		Capabilities: []string{"slack"},
	}
	if err := mm.Create(m); err != nil {
		t.Fatalf("Create: %v", err)
	}

	makeTestAgent(t, mm.Home, "disabled-cap-agent")

	// Write capabilities.yaml with slack disabled.
	capsYAML := `capabilities:
  slack:
    state: disabled
`
	os.WriteFile(filepath.Join(mm.Home, "capabilities.yaml"), []byte(capsYAML), 0644)

	pf := mm.PreFlight(m, "disabled-cap-agent", "agent")
	if pf.OK {
		t.Error("expected OK=false when required capability is disabled")
	}
}

func TestPreFlightAlreadyAssigned(t *testing.T) {
	mm := NewMissionManager(t.TempDir())

	// Create and assign first mission.
	m1 := newTestMission("pf-mission-1")
	if err := mm.Create(m1); err != nil {
		t.Fatalf("Create m1: %v", err)
	}
	makeTestAgent(t, mm.Home, "busy-agent")
	if err := mm.Assign("pf-mission-1", "busy-agent", "agent"); err != nil {
		t.Fatalf("Assign m1: %v", err)
	}

	// Create a second mission and try pre-flight against the same agent.
	m2 := newTestMission("pf-mission-2")
	if err := mm.Create(m2); err != nil {
		t.Fatalf("Create m2: %v", err)
	}

	pf := mm.PreFlight(m2, "busy-agent", "agent")
	if pf.OK {
		t.Error("expected OK=false when agent already has an active mission")
	}
}

// -- CheckRequirements tests --

func TestCheckRequirementsAllGranted(t *testing.T) {
	mm := NewMissionManager(t.TempDir())

	m := newTestMission("check-req-ok")
	m.Requires = &models.MissionRequires{
		Capabilities: []string{"slack", "brave-search"},
	}
	if err := mm.Create(m); err != nil {
		t.Fatalf("Create: %v", err)
	}

	missing, err := mm.CheckRequirements("check-req-ok", []string{"slack", "brave-search", "extra"})
	if err != nil {
		t.Fatalf("CheckRequirements: %v", err)
	}
	if len(missing) != 0 {
		t.Errorf("expected no missing capabilities, got %v", missing)
	}
}

func TestCheckRequirementsMissing(t *testing.T) {
	mm := NewMissionManager(t.TempDir())

	m := newTestMission("check-req-miss")
	m.Requires = &models.MissionRequires{
		Capabilities: []string{"slack", "brave-search"},
	}
	if err := mm.Create(m); err != nil {
		t.Fatalf("Create: %v", err)
	}

	missing, err := mm.CheckRequirements("check-req-miss", []string{"slack"})
	if err != nil {
		t.Fatalf("CheckRequirements: %v", err)
	}
	if len(missing) != 1 || missing[0] != "brave-search" {
		t.Errorf("expected [brave-search] missing, got %v", missing)
	}
}

func TestCheckRequirementsNoRequires(t *testing.T) {
	mm := NewMissionManager(t.TempDir())

	m := newTestMission("check-req-none")
	if err := mm.Create(m); err != nil {
		t.Fatalf("Create: %v", err)
	}

	missing, err := mm.CheckRequirements("check-req-none", []string{})
	if err != nil {
		t.Fatalf("CheckRequirements: %v", err)
	}
	if len(missing) != 0 {
		t.Errorf("expected no missing capabilities for mission with no requires, got %v", missing)
	}
}

func TestCheckRequirementsNotFound(t *testing.T) {
	mm := NewMissionManager(t.TempDir())

	_, err := mm.CheckRequirements("no-such-mission", []string{})
	if err == nil {
		t.Error("expected error for missing mission")
	}
}
