package main

import (
	"testing"
	"time"
)

func makeEntry(tool string, success bool, ts time.Time) ToolEntry {
	return ToolEntry{ToolName: tool, Success: success, Timestamp: ts}
}

// --- Window tests ---

func TestToolWindow_Append(t *testing.T) {
	cfg := DefaultTrajectoryConfig()
	tm := NewTrajectoryMonitor(cfg)

	now := time.Now()
	for i := 0; i < 60; i++ {
		tm.RecordToolCall(makeEntry("read_file", true, now.Add(time.Duration(i)*time.Second)))
	}

	tm.mu.Lock()
	n := len(tm.window)
	tm.mu.Unlock()

	if n != maxWindowSize {
		t.Fatalf("expected window size %d, got %d", maxWindowSize, n)
	}
}

func TestToolWindow_Empty(t *testing.T) {
	cfg := DefaultTrajectoryConfig()
	tm := NewTrajectoryMonitor(cfg)

	anomalies := tm.RunDetectors()
	if len(anomalies) != 0 {
		t.Fatalf("expected no anomalies on empty window, got %d", len(anomalies))
	}
}

// --- tool_repetition tests ---

func TestDetectToolRepetition_Fires(t *testing.T) {
	cfg := DetectorConfig{Threshold: 5, Severity: "warning"}
	now := time.Now()
	window := []ToolEntry{
		makeEntry("write_file", true, now),
		makeEntry("write_file", true, now.Add(time.Second)),
		makeEntry("write_file", true, now.Add(2*time.Second)),
		makeEntry("write_file", true, now.Add(3*time.Second)),
		makeEntry("write_file", true, now.Add(4*time.Second)),
		makeEntry("write_file", true, now.Add(5*time.Second)),
	}
	a := detectToolRepetition(window, cfg)
	if a == nil {
		t.Fatal("expected anomaly, got nil")
	}
}

func TestDetectToolRepetition_NoFire_Mixed(t *testing.T) {
	cfg := DetectorConfig{Threshold: 5, Severity: "warning"}
	now := time.Now()
	window := []ToolEntry{
		makeEntry("read_file", true, now),
		makeEntry("write_file", true, now.Add(time.Second)),
		makeEntry("read_file", true, now.Add(2*time.Second)),
		makeEntry("write_file", true, now.Add(3*time.Second)),
	}
	a := detectToolRepetition(window, cfg)
	if a != nil {
		t.Fatalf("expected no anomaly, got %+v", a)
	}
}

func TestDetectToolRepetition_NoFire_BelowThreshold(t *testing.T) {
	cfg := DetectorConfig{Threshold: 5, Severity: "warning"}
	now := time.Now()
	window := []ToolEntry{
		makeEntry("read_file", true, now),
		makeEntry("read_file", true, now.Add(time.Second)),
		makeEntry("read_file", true, now.Add(2*time.Second)),
	}
	a := detectToolRepetition(window, cfg)
	if a != nil {
		t.Fatalf("expected no anomaly, got %+v", a)
	}
}

func TestDetectToolRepetition_ExactThreshold(t *testing.T) {
	cfg := DetectorConfig{Threshold: 5, Severity: "warning"}
	now := time.Now()
	window := []ToolEntry{
		makeEntry("read_file", true, now),
		makeEntry("read_file", true, now.Add(time.Second)),
		makeEntry("read_file", true, now.Add(2*time.Second)),
		makeEntry("read_file", true, now.Add(3*time.Second)),
		makeEntry("read_file", true, now.Add(4*time.Second)),
	}
	a := detectToolRepetition(window, cfg)
	if a == nil {
		t.Fatal("expected anomaly at exact threshold, got nil")
	}
}

// --- tool_cycle tests ---

func TestDetectToolCycle_TwoToolPattern(t *testing.T) {
	cfg := DetectorConfig{Threshold: 4, Severity: "warning"}
	now := time.Now()
	// [read, write] repeated 4 times
	window := []ToolEntry{
		makeEntry("read", true, now),
		makeEntry("write", true, now.Add(1*time.Second)),
		makeEntry("read", true, now.Add(2*time.Second)),
		makeEntry("write", true, now.Add(3*time.Second)),
		makeEntry("read", true, now.Add(4*time.Second)),
		makeEntry("write", true, now.Add(5*time.Second)),
		makeEntry("read", true, now.Add(6*time.Second)),
		makeEntry("write", true, now.Add(7*time.Second)),
	}
	a := detectToolCycle(window, cfg)
	if a == nil {
		t.Fatal("expected cycle anomaly, got nil")
	}
}

func TestDetectToolCycle_ThreeToolPattern(t *testing.T) {
	cfg := DetectorConfig{Threshold: 4, Severity: "warning"}
	now := time.Now()
	// [a, b, c] repeated 4 times
	window := []ToolEntry{
		makeEntry("a", true, now),
		makeEntry("b", true, now.Add(1*time.Second)),
		makeEntry("c", true, now.Add(2*time.Second)),
		makeEntry("a", true, now.Add(3*time.Second)),
		makeEntry("b", true, now.Add(4*time.Second)),
		makeEntry("c", true, now.Add(5*time.Second)),
		makeEntry("a", true, now.Add(6*time.Second)),
		makeEntry("b", true, now.Add(7*time.Second)),
		makeEntry("c", true, now.Add(8*time.Second)),
		makeEntry("a", true, now.Add(9*time.Second)),
		makeEntry("b", true, now.Add(10*time.Second)),
		makeEntry("c", true, now.Add(11*time.Second)),
	}
	a := detectToolCycle(window, cfg)
	if a == nil {
		t.Fatal("expected cycle anomaly, got nil")
	}
}

func TestDetectToolCycle_NoFire_NotEnoughCycles(t *testing.T) {
	cfg := DetectorConfig{Threshold: 4, Severity: "warning"}
	now := time.Now()
	// [read, write] repeated only 2 times
	window := []ToolEntry{
		makeEntry("read", true, now),
		makeEntry("write", true, now.Add(1*time.Second)),
		makeEntry("read", true, now.Add(2*time.Second)),
		makeEntry("write", true, now.Add(3*time.Second)),
	}
	a := detectToolCycle(window, cfg)
	if a != nil {
		t.Fatalf("expected no anomaly, got %+v", a)
	}
}

func TestDetectToolCycle_PrefersShortPattern(t *testing.T) {
	cfg := DetectorConfig{Threshold: 4, Severity: "warning"}
	now := time.Now()
	// [a, b] repeated 4 times — length-2 should be detected, not length-3
	window := []ToolEntry{
		makeEntry("a", true, now),
		makeEntry("b", true, now.Add(1*time.Second)),
		makeEntry("a", true, now.Add(2*time.Second)),
		makeEntry("b", true, now.Add(3*time.Second)),
		makeEntry("a", true, now.Add(4*time.Second)),
		makeEntry("b", true, now.Add(5*time.Second)),
		makeEntry("a", true, now.Add(6*time.Second)),
		makeEntry("b", true, now.Add(7*time.Second)),
	}
	a := detectToolCycle(window, cfg)
	if a == nil {
		t.Fatal("expected cycle anomaly, got nil")
	}
	// Verify length-2 pattern was detected (not length-3)
	expected := "tool cycle of length 2"
	if len(a.Detail) < len(expected) || a.Detail[:len(expected)] != expected {
		t.Fatalf("expected detail to start with %q, got %q", expected, a.Detail)
	}
}

// --- error_cascade tests ---

func TestDetectErrorCascade_Fires(t *testing.T) {
	cfg := DetectorConfig{Threshold: 5, WindowMinutes: 2, Severity: "warning"}
	now := time.Now()
	window := []ToolEntry{
		makeEntry("tool", false, now.Add(-50*time.Second)),
		makeEntry("tool", false, now.Add(-40*time.Second)),
		makeEntry("tool", false, now.Add(-30*time.Second)),
		makeEntry("tool", false, now.Add(-20*time.Second)),
		makeEntry("tool", false, now.Add(-10*time.Second)),
		makeEntry("tool", false, now),
	}
	a := detectErrorCascade(window, cfg)
	if a == nil {
		t.Fatal("expected error cascade anomaly, got nil")
	}
}

func TestDetectErrorCascade_NoFire_SpreadOut(t *testing.T) {
	cfg := DetectorConfig{Threshold: 5, WindowMinutes: 2, Severity: "warning"}
	now := time.Now()
	// 6 errors but spread across 10 minutes — only 2 in the 2-minute window
	window := []ToolEntry{
		makeEntry("tool", false, now.Add(-9*time.Minute)),
		makeEntry("tool", false, now.Add(-8*time.Minute)),
		makeEntry("tool", false, now.Add(-7*time.Minute)),
		makeEntry("tool", false, now.Add(-6*time.Minute)),
		makeEntry("tool", false, now.Add(-90*time.Second)),
		makeEntry("tool", false, now),
	}
	a := detectErrorCascade(window, cfg)
	if a != nil {
		t.Fatalf("expected no anomaly, got %+v", a)
	}
}

func TestDetectErrorCascade_NoFire_MixedSuccessFailure(t *testing.T) {
	cfg := DetectorConfig{Threshold: 5, WindowMinutes: 2, Severity: "warning"}
	now := time.Now()
	window := []ToolEntry{
		makeEntry("tool", false, now.Add(-50*time.Second)),
		makeEntry("tool", true, now.Add(-40*time.Second)),
		makeEntry("tool", false, now.Add(-30*time.Second)),
		makeEntry("tool", true, now.Add(-20*time.Second)),
		makeEntry("tool", false, now.Add(-10*time.Second)),
	}
	a := detectErrorCascade(window, cfg)
	if a != nil {
		t.Fatalf("expected no anomaly with only 3 errors, got %+v", a)
	}
}

// --- Cooldown tests ---

func TestCooldown_SuppressesSameDetector(t *testing.T) {
	cfg := DefaultTrajectoryConfig()
	cfg.CooldownMinutes = 5
	tm := NewTrajectoryMonitor(cfg)

	now := time.Now()
	// Load 6 consecutive same-tool calls to trigger tool_repetition
	for i := 0; i < 6; i++ {
		tm.RecordToolCall(makeEntry("read_file", true, now.Add(time.Duration(i)*time.Second)))
	}

	first := tm.RunDetectors()
	var repFound bool
	for _, a := range first {
		if a.Detector == "tool_repetition" {
			repFound = true
		}
	}
	if !repFound {
		t.Fatal("expected tool_repetition anomaly on first run")
	}

	// Second run should be suppressed by cooldown
	second := tm.RunDetectors()
	for _, a := range second {
		if a.Detector == "tool_repetition" {
			t.Fatal("expected tool_repetition to be suppressed by cooldown")
		}
	}
}

func TestCooldown_DifferentDetectorsIndependent(t *testing.T) {
	cfg := DefaultTrajectoryConfig()
	cfg.CooldownMinutes = 5
	tm := NewTrajectoryMonitor(cfg)

	now := time.Now()
	// Trigger tool_repetition: 6 consecutive calls
	for i := 0; i < 6; i++ {
		tm.RecordToolCall(makeEntry("read_file", true, now.Add(time.Duration(i)*time.Second)))
	}

	// First run fires tool_repetition
	tm.RunDetectors()

	// Now add entries that will trigger error_cascade (6 errors in 2 min window)
	base := now.Add(10 * time.Second)
	for i := 0; i < 6; i++ {
		tm.RecordToolCall(makeEntry("other_tool", false, base.Add(time.Duration(i)*10*time.Second)))
	}

	second := tm.RunDetectors()
	var cascadeFound bool
	for _, a := range second {
		if a.Detector == "error_cascade" {
			cascadeFound = true
		}
	}
	if !cascadeFound {
		t.Fatal("expected error_cascade to fire independently of tool_repetition cooldown")
	}
}

// --- Integration test ---

func TestRunDetectors_MultipleAnomalies(t *testing.T) {
	cfg := DefaultTrajectoryConfig()
	tm := NewTrajectoryMonitor(cfg)

	now := time.Now()
	// 6 consecutive same-tool + all failures = repetition AND error cascade
	for i := 0; i < 6; i++ {
		tm.RecordToolCall(makeEntry("write_file", false, now.Add(time.Duration(i)*10*time.Second)))
	}

	anomalies := tm.RunDetectors()
	detectorSet := make(map[string]bool)
	for _, a := range anomalies {
		detectorSet[a.Detector] = true
	}

	if !detectorSet["tool_repetition"] {
		t.Error("expected tool_repetition anomaly")
	}
	if !detectorSet["error_cascade"] {
		t.Error("expected error_cascade anomaly")
	}
}
