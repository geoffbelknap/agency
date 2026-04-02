package orchestrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/log"
	"gopkg.in/yaml.v3"
)

func newTestLogger() *log.Logger {
	return log.New(os.Stderr)
}

func TestTaskIsComplete_MatchingTaskID(t *testing.T) {
	dir := t.TempDir()
	sigFile := filepath.Join(dir, "agent-signals.jsonl")

	sig := map[string]interface{}{
		"signal_type": "task_complete",
		"timestamp":   "2026-03-17T15:00:00Z",
		"data":        map[string]interface{}{"task_id": "task-123"},
	}
	line, _ := json.Marshal(sig)
	os.WriteFile(sigFile, line, 0644)

	if !taskIsComplete(sigFile, "task-123") {
		t.Error("expected taskIsComplete=true for matching task_id")
	}
}

func TestTaskIsComplete_DifferentTaskID(t *testing.T) {
	dir := t.TempDir()
	sigFile := filepath.Join(dir, "agent-signals.jsonl")

	sig := map[string]interface{}{
		"signal_type": "task_complete",
		"timestamp":   "2026-03-17T15:00:00Z",
		"data":        map[string]interface{}{"task_id": "task-123"},
	}
	line, _ := json.Marshal(sig)
	os.WriteFile(sigFile, line, 0644)

	if taskIsComplete(sigFile, "task-999") {
		t.Error("expected taskIsComplete=false for non-matching task_id")
	}
}

func TestTaskIsComplete_NoSignalsFile(t *testing.T) {
	if taskIsComplete("/nonexistent/path/signals.jsonl", "task-123") {
		t.Error("expected taskIsComplete=false when signals file doesn't exist")
	}
}

func TestTaskIsComplete_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	sigFile := filepath.Join(dir, "agent-signals.jsonl")
	os.WriteFile(sigFile, []byte(""), 0644)

	if taskIsComplete(sigFile, "task-123") {
		t.Error("expected taskIsComplete=false when signals file is empty")
	}
}

func TestTaskIsComplete_EmptyTaskID(t *testing.T) {
	dir := t.TempDir()
	sigFile := filepath.Join(dir, "agent-signals.jsonl")

	sig := map[string]interface{}{
		"signal_type": "task_complete",
		"timestamp":   "2026-03-17T15:00:00Z",
		"data":        map[string]interface{}{"task_id": "task-123"},
	}
	line, _ := json.Marshal(sig)
	os.WriteFile(sigFile, line, 0644)

	if !taskIsComplete(sigFile, "") {
		t.Error("expected taskIsComplete=true when taskID is empty (any completion counts)")
	}
}

func TestArchiveAuditLogs_MovesDirectoryToArchive(t *testing.T) {
	dir := t.TempDir()
	auditAgentDir := filepath.Join(dir, "audit", "test-agent")
	os.MkdirAll(auditAgentDir, 0700)
	os.WriteFile(filepath.Join(auditAgentDir, "2026-03-21.jsonl"), []byte(`{"event":"test"}`), 0644)

	am := &AgentManager{Home: dir}
	am.archiveAuditLogs("test-agent")

	// Original audit dir should be gone
	if _, err := os.Stat(auditAgentDir); !os.IsNotExist(err) {
		t.Error("expected original audit dir to be removed after archiving")
	}

	// Archived dir should exist under .archived/
	archivedBase := filepath.Join(dir, "audit", ".archived")
	entries, err := os.ReadDir(archivedBase)
	if err != nil {
		t.Fatalf("expected .archived dir to exist: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 archived entry, got %d", len(entries))
	}
	archivedName := entries[0].Name()
	if len(archivedName) < len("test-agent-") {
		t.Errorf("archived dir name %q doesn't look right", archivedName)
	}

	// Log file should be present inside the archive
	logFile := filepath.Join(archivedBase, archivedName, "2026-03-21.jsonl")
	if _, err := os.Stat(logFile); err != nil {
		t.Errorf("expected log file to exist in archive: %v", err)
	}
}

func TestArchiveAuditLogs_NoOpWhenAuditDirMissing(t *testing.T) {
	dir := t.TempDir()
	am := &AgentManager{Home: dir}
	// Should not panic or error when audit dir doesn't exist
	am.archiveAuditLogs("nonexistent-agent")
}

func TestStaleTaskClearedWhenTaskComplete(t *testing.T) {
	// Integration test: simulate a stale session-context with current_task
	// and a signals file with task_complete. Verify loadAgentDetail
	// returns nil CurrentTask and clears the context file.
	dir := t.TempDir()
	agentDir := filepath.Join(dir, "agents", "testbot")
	stateDir := filepath.Join(agentDir, "state")
	os.MkdirAll(stateDir, 0755)

	// Write agent.yaml
	os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte("type: standard\n"), 0644)

	// Write constraints.yaml
	os.WriteFile(filepath.Join(agentDir, "constraints.yaml"), []byte("identity:\n  role: assistant\n"), 0644)

	// Write stale session-context with current_task
	ctx := map[string]interface{}{
		"current_task": map[string]interface{}{
			"task_id":   "task-stale-001",
			"content":   "Old task that already completed",
			"timestamp": "2026-03-17T14:00:00Z",
		},
	}
	ctxJSON, _ := json.MarshalIndent(ctx, "", "  ")
	ctxFile := filepath.Join(stateDir, "session-context.json")
	os.WriteFile(ctxFile, ctxJSON, 0644)

	// Write task_complete signal for the stale task
	sig := map[string]interface{}{
		"signal_type": "task_complete",
		"timestamp":   "2026-03-17T15:00:00Z",
		"data":        map[string]interface{}{"task_id": "task-stale-001"},
	}
	sigLine, _ := json.Marshal(sig)
	os.WriteFile(filepath.Join(stateDir, "agent-signals.jsonl"), sigLine, 0644)

	// Create a minimal AgentManager and call loadAgentDetail
	am := &AgentManager{Home: dir}
	running := map[string]containerInfo{}
	detail := am.loadAgentDetail("testbot", filepath.Join(dir, "agents"), running, map[string]string{})

	// CurrentTask should be nil (task_complete signal found)
	if detail.CurrentTask != nil {
		t.Errorf("expected CurrentTask=nil, got %+v", detail.CurrentTask)
	}

	// session-context.json should have current_task removed
	updatedCtx, _ := os.ReadFile(ctxFile)
	var sc map[string]interface{}
	json.Unmarshal(updatedCtx, &sc)
	if _, exists := sc["current_task"]; exists {
		t.Error("expected current_task to be cleared from session-context.json")
	}
}

func TestCreate_GeneratesLifecycleID(t *testing.T) {
	dir := t.TempDir()
	am := &AgentManager{
		Home: dir,
		log:  newTestLogger(),
	}

	// Create will fail after writing agent.yaml (no Docker), so we ignore the error
	// and just verify that agent.yaml was written with a valid lifecycle_id.
	_ = am.Create(nil, "test-agent", "default") //nolint:staticcheck

	agentYAMLPath := filepath.Join(dir, "agents", "test-agent", "agent.yaml")
	data, err := os.ReadFile(agentYAMLPath)
	if err != nil {
		t.Fatalf("expected agent.yaml to be written: %v", err)
	}

	var ay map[string]interface{}
	if err := yaml.Unmarshal(data, &ay); err != nil {
		t.Fatalf("failed to parse agent.yaml: %v", err)
	}

	lid, ok := ay["lifecycle_id"].(string)
	if !ok || lid == "" {
		t.Fatalf("expected lifecycle_id in agent.yaml, got: %v", ay["lifecycle_id"])
	}
	// A UUID is 36 characters: 8-4-4-4-12 with hyphens
	if len(lid) != 36 {
		t.Errorf("expected lifecycle_id to be 36 chars (UUID), got %d chars: %q", len(lid), lid)
	}
}

func TestLoadAgentDetail_GeneratesLifecycleID_WhenMissing(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, "agents", "old-agent")
	os.MkdirAll(agentDir, 0755)

	// Write agent.yaml WITHOUT lifecycle_id (simulating an older agent)
	os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte("type: standard\npreset: default\n"), 0644)

	am := &AgentManager{Home: dir}
	running := map[string]containerInfo{}
	detail := am.loadAgentDetail("old-agent", filepath.Join(dir, "agents"), running, map[string]string{})

	// LifecycleID should be populated in the returned detail
	if detail.LifecycleID == "" {
		t.Error("expected LifecycleID to be backfilled in AgentDetail")
	}
	if len(detail.LifecycleID) != 36 {
		t.Errorf("expected LifecycleID to be 36 chars (UUID), got %d chars: %q", len(detail.LifecycleID), detail.LifecycleID)
	}

	// Re-read agent.yaml and verify lifecycle_id was written back
	data, err := os.ReadFile(filepath.Join(agentDir, "agent.yaml"))
	if err != nil {
		t.Fatalf("failed to read agent.yaml after backfill: %v", err)
	}
	var ay map[string]interface{}
	if err := yaml.Unmarshal(data, &ay); err != nil {
		t.Fatalf("failed to parse agent.yaml: %v", err)
	}
	lid, ok := ay["lifecycle_id"].(string)
	if !ok || lid == "" {
		t.Fatalf("expected lifecycle_id to be written to agent.yaml, got: %v", ay["lifecycle_id"])
	}
	if lid != detail.LifecycleID {
		t.Errorf("lifecycle_id in file %q doesn't match returned LifecycleID %q", lid, detail.LifecycleID)
	}
}

func TestLoadAgentDetail_UnhealthyWhenEnforcerDown(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, "agents", "testbot")
	os.MkdirAll(agentDir, 0755)
	os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte("type: standard\n"), 0644)
	os.WriteFile(filepath.Join(agentDir, "constraints.yaml"), []byte("identity:\n  role: assistant\n"), 0644)

	am := &AgentManager{Home: dir}

	// Workspace running, enforcer exited → unhealthy
	running := map[string]containerInfo{
		"agency-testbot-workspace": {State: "running"},
		"agency-testbot-enforcer":  {State: "exited"},
	}
	detail := am.loadAgentDetail("testbot", filepath.Join(dir, "agents"), running, map[string]string{})
	if detail.Status != "unhealthy" {
		t.Errorf("expected status=unhealthy when enforcer exited, got %q", detail.Status)
	}
	if detail.Workspace != "running" {
		t.Errorf("expected workspace=running, got %q", detail.Workspace)
	}
	if detail.Enforcer != "exited" {
		t.Errorf("expected enforcer=exited, got %q", detail.Enforcer)
	}

	// Both running → running (healthy)
	running["agency-testbot-enforcer"] = containerInfo{State: "running"}
	detail = am.loadAgentDetail("testbot", filepath.Join(dir, "agents"), running, map[string]string{})
	if detail.Status != "running" {
		t.Errorf("expected status=running when both containers running, got %q", detail.Status)
	}

	// Workspace running, enforcer missing → unhealthy
	delete(running, "agency-testbot-enforcer")
	detail = am.loadAgentDetail("testbot", filepath.Join(dir, "agents"), running, map[string]string{})
	if detail.Status != "unhealthy" {
		t.Errorf("expected status=unhealthy when enforcer missing, got %q", detail.Status)
	}
}

func TestLoadAgentDetail_PreservesExistingLifecycleID(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, "agents", "existing-agent")
	os.MkdirAll(agentDir, 0755)

	existingID := "550e8400-e29b-41d4-a716-446655440000"
	yamlContent := "type: standard\npreset: default\nlifecycle_id: " + existingID + "\n"
	os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(yamlContent), 0644)

	am := &AgentManager{Home: dir}
	running := map[string]containerInfo{}
	detail := am.loadAgentDetail("existing-agent", filepath.Join(dir, "agents"), running, map[string]string{})

	if detail.LifecycleID != existingID {
		t.Errorf("expected existing lifecycle_id %q to be preserved, got %q", existingID, detail.LifecycleID)
	}
}
