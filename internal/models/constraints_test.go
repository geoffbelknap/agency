// agency-gateway/internal/models/constraints_test.go
package models

import (
	"os"
	"path/filepath"
	"testing"
)

// --- MCPPolicy.IsServerAllowed ---

func TestMCPPolicy_IsServerAllowed_Denylist(t *testing.T) {
	p := MCPPolicy{
		Mode:          "denylist",
		DeniedServers: []string{"bad-server"},
	}

	if !p.IsServerAllowed("good-server") {
		t.Error("expected good-server to be allowed in denylist mode")
	}
	if p.IsServerAllowed("bad-server") {
		t.Error("expected bad-server to be denied in denylist mode")
	}
}

func TestMCPPolicy_IsServerAllowed_Allowlist(t *testing.T) {
	p := MCPPolicy{
		Mode:           "allowlist",
		AllowedServers: []string{"brave-search"},
	}

	if !p.IsServerAllowed("brave-search") {
		t.Error("expected brave-search to be allowed in allowlist mode")
	}
	if p.IsServerAllowed("other-server") {
		t.Error("expected other-server to be denied in allowlist mode")
	}
}

func TestMCPPolicy_IsServerAllowed_EmptyDenylist(t *testing.T) {
	p := MCPPolicy{Mode: "denylist"}
	if !p.IsServerAllowed("any-server") {
		t.Error("expected any-server to be allowed with empty denylist")
	}
}

func TestMCPPolicy_IsServerAllowed_EmptyAllowlist(t *testing.T) {
	p := MCPPolicy{Mode: "allowlist"}
	if p.IsServerAllowed("any-server") {
		t.Error("expected any-server to be denied with empty allowlist")
	}
}

// --- MCPPolicy.IsToolAllowed ---

func TestMCPPolicy_IsToolAllowed_NoRestrictions(t *testing.T) {
	p := MCPPolicy{}
	if !p.IsToolAllowed("any-tool") {
		t.Error("expected any-tool to be allowed with no restrictions")
	}
}

func TestMCPPolicy_IsToolAllowed_DeniedTools(t *testing.T) {
	p := MCPPolicy{DeniedTools: []string{"dangerous-tool"}}
	if p.IsToolAllowed("dangerous-tool") {
		t.Error("expected dangerous-tool to be denied")
	}
	if !p.IsToolAllowed("safe-tool") {
		t.Error("expected safe-tool to be allowed when not in denied list")
	}
}

func TestMCPPolicy_IsToolAllowed_AllowedTools(t *testing.T) {
	p := MCPPolicy{AllowedTools: []string{"search", "fetch"}}
	if !p.IsToolAllowed("search") {
		t.Error("expected search to be allowed")
	}
	if !p.IsToolAllowed("fetch") {
		t.Error("expected fetch to be allowed")
	}
	if p.IsToolAllowed("other-tool") {
		t.Error("expected other-tool to be denied when allowlist is set")
	}
}

func TestMCPPolicy_IsToolAllowed_DeniedOverridesAllowed(t *testing.T) {
	// denied_tools check runs first — if a tool is denied, it stays denied
	// even if allowed_tools is also set
	p := MCPPolicy{
		AllowedTools: []string{"search", "fetch"},
		DeniedTools:  []string{"fetch"},
	}
	if p.IsToolAllowed("fetch") {
		t.Error("expected fetch to be denied even though it is in allowed_tools")
	}
	if !p.IsToolAllowed("search") {
		t.Error("expected search to be allowed")
	}
}

// --- BudgetConfig validation ---

func TestBudgetConfig_NegativeSoftLimit(t *testing.T) {
	b := BudgetConfig{
		Mode:                "notify",
		SoftLimit:           -1.0,
		WarningThresholdPct: 80,
	}
	err := validate.Struct(b)
	if err == nil {
		t.Error("expected validation error for negative soft_limit")
	}
}

func TestBudgetConfig_NegativeHardLimit(t *testing.T) {
	b := BudgetConfig{
		Mode:                "notify",
		HardLimit:           -5.0,
		WarningThresholdPct: 80,
	}
	err := validate.Struct(b)
	if err == nil {
		t.Error("expected validation error for negative hard_limit")
	}
}

func TestBudgetConfig_ThresholdTooLow(t *testing.T) {
	b := BudgetConfig{
		Mode:                "notify",
		WarningThresholdPct: 0,
	}
	err := validate.Struct(b)
	if err == nil {
		t.Error("expected validation error for warning_threshold_pct=0")
	}
}

func TestBudgetConfig_ThresholdTooHigh(t *testing.T) {
	b := BudgetConfig{
		Mode:                "notify",
		WarningThresholdPct: 101,
	}
	err := validate.Struct(b)
	if err == nil {
		t.Error("expected validation error for warning_threshold_pct=101")
	}
}

func TestBudgetConfig_ValidThresholdRange(t *testing.T) {
	for _, pct := range []int{1, 50, 80, 100} {
		b := BudgetConfig{
			Mode:                "notify",
			WarningThresholdPct: pct,
		}
		err := validate.Struct(b)
		if err != nil {
			t.Errorf("expected valid for warning_threshold_pct=%d, got: %v", pct, err)
		}
	}
}

// --- ConstraintsConfig fixture loading ---

func TestConstraintsConfig_ValidMinimal(t *testing.T) {
	path := filepath.Join("testdata", "models", "constraints", "valid_minimal.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("fixture not found: %s", path)
	}
	dir := t.TempDir()
	f := filepath.Join(dir, "constraints.yaml")
	os.WriteFile(f, data, 0644)

	err = LoadAndValidate(f)
	if err != nil {
		t.Errorf("expected valid, got error: %v", err)
	}
}

func TestConstraintsConfig_ValidFull(t *testing.T) {
	path := filepath.Join("testdata", "models", "constraints", "valid_full.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("fixture not found: %s", path)
	}
	dir := t.TempDir()
	f := filepath.Join(dir, "constraints.yaml")
	os.WriteFile(f, data, 0644)

	err = LoadAndValidate(f)
	if err != nil {
		t.Errorf("expected valid, got error: %v", err)
	}
}

// --- Defaults ---

func TestConstraintsConfig_Defaults(t *testing.T) {
	var cfg ConstraintsConfig
	data := []byte("agent: test-agent\nidentity:\n  role: researcher\n  purpose: research tasks\n")
	dir := t.TempDir()
	f := filepath.Join(dir, "constraints.yaml")
	os.WriteFile(f, data, 0644)

	if err := Load(f, &cfg); err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	if cfg.Version != "0.1" {
		t.Errorf("expected default version '0.1', got '%s'", cfg.Version)
	}
	if cfg.Budget.Mode != "notify" {
		t.Errorf("expected budget mode 'notify', got '%s'", cfg.Budget.Mode)
	}
	if cfg.Budget.WarningThresholdPct != 80 {
		t.Errorf("expected budget warning_threshold_pct 80, got %d", cfg.Budget.WarningThresholdPct)
	}
	if cfg.MCP.Mode != "denylist" {
		t.Errorf("expected mcp mode 'denylist', got '%s'", cfg.MCP.Mode)
	}
	if cfg.Network.EgressMode != "denylist" {
		t.Errorf("expected network egress_mode 'denylist', got '%s'", cfg.Network.EgressMode)
	}
}

func TestMCPPolicy_Defaults(t *testing.T) {
	p := MCPPolicy{}
	applyDefaults(&p)

	if p.Mode != "denylist" {
		t.Errorf("expected mode 'denylist', got '%s'", p.Mode)
	}
}
