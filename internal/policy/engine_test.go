package policy

import (
	"path/filepath"
	"runtime"
	"testing"

	agencysecurity "github.com/geoffbelknap/agency/internal/security"
)

// testdataDir returns the absolute path to the testdata/chains directory.
func testdataDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// engine_test.go lives in internal/policy/; testdata is colocated
	return filepath.Join(filepath.Dir(file), "testdata", "chains")
}

func TestEngine_Compute_SimpleRestriction(t *testing.T) {
	home := filepath.Join(testdataDir(t), "simple_two_level")
	e := NewEngine(home)

	ep := e.Compute("test")

	if !ep.Valid {
		t.Fatalf("expected valid policy, got violations: %v", ep.Violations)
	}

	// Org set risk_tolerance=low; agent didn't change it, so it should remain low.
	if ep.Parameters["risk_tolerance"] != "low" {
		t.Errorf("risk_tolerance: want low, got %v", ep.Parameters["risk_tolerance"])
	}

	// Agent set max_concurrent_tasks=3 (more restrictive than platform default 5).
	maxTasks := ep.Parameters["max_concurrent_tasks"]
	// YAML numbers decode as int; accept both int and float64.
	var got int
	switch v := maxTasks.(type) {
	case int:
		got = v
	case float64:
		got = int(v)
	default:
		t.Fatalf("max_concurrent_tasks unexpected type %T: %v", maxTasks, maxTasks)
	}
	if got != 3 {
		t.Errorf("max_concurrent_tasks: want 3, got %d", got)
	}

	// Chain should include platform, org, department(missing), team(missing), agent.
	levelStatuses := map[string]agencysecurity.PolicyStepStatus{}
	for _, step := range ep.Chain {
		levelStatuses[step.Level] = step.Status
	}
	if levelStatuses["platform"] != "ok" {
		t.Errorf("platform step: want ok, got %s", levelStatuses["platform"])
	}
	if levelStatuses["org"] != "ok" {
		t.Errorf("org step: want ok, got %s", levelStatuses["org"])
	}
	if levelStatuses["department"] != "missing" {
		t.Errorf("department step: want missing, got %s", levelStatuses["department"])
	}
	if levelStatuses["team"] != "missing" {
		t.Errorf("team step: want missing, got %s", levelStatuses["team"])
	}
	if levelStatuses["agent"] != "ok" {
		t.Errorf("agent step: want ok, got %s", levelStatuses["agent"])
	}
}

func TestEngine_Compute_LooseningViolation(t *testing.T) {
	home := filepath.Join(testdataDir(t), "loosening_violation")
	e := NewEngine(home)

	ep := e.Compute("test")

	if ep.Valid {
		t.Fatal("expected invalid policy (loosening violation), but got valid")
	}

	// There must be at least one violation mentioning "loosened" or "risk_tolerance".
	found := false
	for _, v := range ep.Violations {
		if containsAny(v, "loosened", "risk_tolerance") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a loosening violation for risk_tolerance, got: %v", ep.Violations)
	}

	// Chain should contain a violation step at the agent level.
	foundViolationStep := false
	for _, step := range ep.Chain {
		if step.Level == "agent" && step.Status == "violation" {
			foundViolationStep = true
			break
		}
	}
	if !foundViolationStep {
		t.Errorf("expected chain to contain a violation step at agent level, chain: %+v", ep.Chain)
	}
}

func TestEngine_Compute_HardFloorViolation(t *testing.T) {
	home := filepath.Join(testdataDir(t), "hard_floor_violation")
	e := NewEngine(home)

	ep := e.Compute("test")

	if ep.Valid {
		t.Fatal("expected invalid policy (hard floor violation), but got valid")
	}

	// There must be at least one violation mentioning "hard floor" and "logging".
	found := false
	for _, v := range ep.Violations {
		if containsAny(v, "hard floor", "logging") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a hard floor violation for 'logging', got: %v", ep.Violations)
	}

	// The chain step for "agent" must be a violation.
	foundViolationStep := false
	for _, step := range ep.Chain {
		if step.Level == "agent" && step.Status == "violation" {
			foundViolationStep = true
			break
		}
	}
	if !foundViolationStep {
		t.Errorf("expected violation step at agent level, chain: %+v", ep.Chain)
	}
}

func TestEngine_ValidateExceptions_Valid(t *testing.T) {
	home := filepath.Join(testdataDir(t), "valid_exception")
	e := NewEngine(home)

	ep := e.Compute("test")

	if !ep.Valid {
		t.Fatalf("expected valid policy, got violations: %v", ep.Violations)
	}

	if len(ep.Exceptions) == 0 {
		t.Fatal("expected at least one exception, got none")
	}

	exc := ep.Exceptions[0]
	if exc.ExceptionID != "EXC-001" {
		t.Errorf("exception_id: want EXC-001, got %s", exc.ExceptionID)
	}
	if exc.Status != "active" {
		t.Errorf("exception status: want active, got %s (detail: %s)", exc.Status, exc.Detail)
	}
	if exc.GrantRef != "GRANT-001" {
		t.Errorf("grant_ref: want GRANT-001, got %s", exc.GrantRef)
	}
}

func TestEngine_ValidateExceptions_Expired(t *testing.T) {
	home := filepath.Join(testdataDir(t), "expired_exception")
	e := NewEngine(home)

	ep := e.Compute("test")

	// An expired exception makes the policy itself still valid (it's not a hard floor
	// violation), but the exception entry should be marked expired.
	if len(ep.Exceptions) == 0 {
		t.Fatal("expected at least one exception, got none")
	}

	exc := ep.Exceptions[0]
	if exc.ExceptionID != "EXC-001" {
		t.Errorf("exception_id: want EXC-001, got %s", exc.ExceptionID)
	}
	if exc.Status != "expired" {
		t.Errorf("exception status: want expired, got %s (detail: %s)", exc.Status, exc.Detail)
	}
}

func TestEngine_Compute_FiveLevelChain(t *testing.T) {
	home := filepath.Join(testdataDir(t), "full_five_level")
	e := NewEngine(home)

	ep := e.Compute("test")

	if !ep.Valid {
		t.Fatalf("expected valid five-level policy, got violations: %v", ep.Violations)
	}

	// Chain must visit all five levels in order.
	wantOrder := []string{"platform", "org", "department", "team", "agent"}
	if len(ep.Chain) < len(wantOrder) {
		t.Fatalf("chain too short: want at least %d steps, got %d: %+v", len(wantOrder), len(ep.Chain), ep.Chain)
	}
	for i, want := range wantOrder {
		if ep.Chain[i].Level != want {
			t.Errorf("chain[%d]: want level %q, got %q", i, want, ep.Chain[i].Level)
		}
	}

	// Department and team must be resolved (status=ok, not missing).
	levelStatuses := map[string]agencysecurity.PolicyStepStatus{}
	for _, step := range ep.Chain {
		levelStatuses[step.Level] = step.Status
	}
	if levelStatuses["department"] != "ok" {
		t.Errorf("department step: want ok, got %s", levelStatuses["department"])
	}
	if levelStatuses["team"] != "ok" {
		t.Errorf("team step: want ok, got %s", levelStatuses["team"])
	}

	// Resolution order: org=10, dept=5, team=3, agent=2.  Final value must be 2.
	maxTasks := ep.Parameters["max_concurrent_tasks"]
	var got int
	switch v := maxTasks.(type) {
	case int:
		got = v
	case float64:
		got = int(v)
	default:
		t.Fatalf("max_concurrent_tasks unexpected type %T: %v", maxTasks, maxTasks)
	}
	if got != 2 {
		t.Errorf("max_concurrent_tasks: want 2 (agent level), got %d", got)
	}

	// risk_tolerance: org set medium, no level changed it.
	if ep.Parameters["risk_tolerance"] != "medium" {
		t.Errorf("risk_tolerance: want medium, got %v", ep.Parameters["risk_tolerance"])
	}
}

// containsAny returns true if s contains any of the provided substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(sub) > 0 {
			if len(s) >= len(sub) {
				for i := 0; i <= len(s)-len(sub); i++ {
					if s[i:i+len(sub)] == sub {
						return true
					}
				}
			}
		}
	}
	return false
}
