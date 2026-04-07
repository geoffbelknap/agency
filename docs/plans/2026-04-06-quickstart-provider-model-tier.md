# Quickstart Provider Model Tier — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix agent model resolution so agents use the correct model from their installed provider's tier config instead of hardcoding `claude-sonnet`.

**Architecture:** Three fixes in two files: (A) copy `model_tier` from preset in agent creation, (B) rewrite `resolveModelTier()` to use the typed `models.RoutingConfig` struct and `ResolveTier()`, (C) replace hardcoded `claude-sonnet` default with tier-based resolution that fails closed.

**Tech Stack:** Go, gopkg.in/yaml.v3, internal/models (RoutingConfig, ResolveTier)

**Spec:** `docs/specs/quickstart-provider-model-tier.md`

---

## File Map

| File | Action | Responsibility |
|------|--------|----------------|
| `internal/orchestrate/agent.go` | Modify | Copy model_tier from preset to agent.yaml |
| `internal/orchestrate/start.go` | Modify | Rewrite resolveModelTier(), fix default fallback |
| `internal/orchestrate/agent_test.go` | Modify | Test model_tier copy in agent creation |
| `internal/orchestrate/start_test.go` | Create | Test resolveModelTier() and default fallback |

---

### Task 1: Copy model_tier from preset — test + implement

**Files:**
- Modify: `internal/orchestrate/agent.go:183-191`
- Modify: `internal/orchestrate/agent_test.go`

- [ ] **Step 1: Write the failing test**

Add to `agent_test.go`:

```go
func TestCreateAgentCopiesModelTier(t *testing.T) {
	home := t.TempDir()

	// Create a preset with model_tier
	presetDir := filepath.Join(home, "hub-cache", "default", "presets", "test-preset")
	os.MkdirAll(presetDir, 0755)
	presetYAML := []byte(`
name: test-preset
type: standard
model_tier: frontier
expertise:
  - security
responsiveness: immediate
identity:
  role: assistant
  purpose: Test agent
`)
	os.WriteFile(filepath.Join(presetDir, "preset.yaml"), presetYAML, 0644)

	am := &AgentManager{Home: home, Log: newTestLogger()}
	err := am.Create(nil, "test-agent", "test-preset")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Read back agent.yaml
	agentData, err := os.ReadFile(filepath.Join(home, "agents", "test-agent", "agent.yaml"))
	if err != nil {
		t.Fatalf("read agent.yaml: %v", err)
	}
	var agentCfg map[string]interface{}
	if err := yaml.Unmarshal(agentData, &agentCfg); err != nil {
		t.Fatalf("parse agent.yaml: %v", err)
	}

	modelTier, ok := agentCfg["model_tier"].(string)
	if !ok || modelTier != "frontier" {
		t.Fatalf("expected model_tier=frontier in agent.yaml, got %v", agentCfg["model_tier"])
	}

	// Also verify expertise was copied (existing behavior)
	if _, ok := agentCfg["expertise"]; !ok {
		t.Fatal("expertise not copied from preset")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency && go test ./internal/orchestrate/ -run "TestCreateAgentCopiesModelTier" -v`

Expected: FAIL — `model_tier` not found in agent.yaml.

- [ ] **Step 3: Add model_tier copy to agent creation**

In `internal/orchestrate/agent.go`, after the responsiveness copy block (around line 190), add:

```go
	// Copy model_tier from preset
	if mt, ok := presetData["model_tier"]; ok {
		agentYAML["model_tier"] = mt
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency && go test ./internal/orchestrate/ -run "TestCreateAgentCopiesModelTier" -v`

Expected: PASS

- [ ] **Step 5: Run full orchestrate test suite**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency && go test ./internal/orchestrate/ -v`

Expected: All tests pass.

- [ ] **Step 6: Commit**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency
git add internal/orchestrate/agent.go internal/orchestrate/agent_test.go
git commit -m "fix: copy model_tier from preset to agent.yaml on creation

Presets declare model_tier (frontier, standard, etc.) but agent
creation was not copying it to agent.yaml, causing tier resolution
to be skipped at startup."
```

---

### Task 2: Rewrite resolveModelTier — test + implement

**Files:**
- Modify: `internal/orchestrate/start.go:529-584`
- Create: `internal/orchestrate/start_test.go`

- [ ] **Step 1: Write tests for resolveModelTier**

Create `internal/orchestrate/start_test.go`:

```go
package orchestrate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveModelTier_ResolvesFromRouting(t *testing.T) {
	home := t.TempDir()
	infraDir := filepath.Join(home, "infrastructure")
	os.MkdirAll(infraDir, 0755)

	// routing.yaml with Gemini in standard tier
	routingYAML := []byte(`
version: "0.1"
providers:
  google:
    api_base: https://generativelanguage.googleapis.com/v1beta/openai
    auth_env: GOOGLE_API_KEY
models:
  gemini-flash:
    provider: google
    provider_model: gemini-2.0-flash
    cost_per_mtok_in: 0.075
    cost_per_mtok_out: 0.3
tiers:
  standard:
    - model: gemini-flash
      preference: 0
  frontier:
    - model: gemini-flash
      preference: 0
`)
	os.WriteFile(filepath.Join(infraDir, "routing.yaml"), routingYAML, 0644)

	// Set the env var so credential check passes
	t.Setenv("GOOGLE_API_KEY", "test-key")

	ss := &StartSequence{Home: home}
	resolved := ss.resolveModelTier("standard")
	if resolved != "gemini-flash" {
		t.Fatalf("expected gemini-flash, got %q", resolved)
	}

	resolved = ss.resolveModelTier("frontier")
	if resolved != "gemini-flash" {
		t.Fatalf("expected gemini-flash for frontier, got %q", resolved)
	}
}

func TestResolveModelTier_SkipsProviderWithoutCredentials(t *testing.T) {
	home := t.TempDir()
	infraDir := filepath.Join(home, "infrastructure")
	os.MkdirAll(infraDir, 0755)

	routingYAML := []byte(`
version: "0.1"
providers:
  anthropic:
    api_base: https://api.anthropic.com/v1
    auth_env: ANTHROPIC_API_KEY
models:
  claude-sonnet:
    provider: anthropic
    provider_model: claude-sonnet-4-20250514
tiers:
  standard:
    - model: claude-sonnet
      preference: 0
`)
	os.WriteFile(filepath.Join(infraDir, "routing.yaml"), routingYAML, 0644)

	// Do NOT set ANTHROPIC_API_KEY — credential check should fail
	t.Setenv("ANTHROPIC_API_KEY", "")

	ss := &StartSequence{Home: home}
	resolved := ss.resolveModelTier("standard")
	if resolved != "" {
		t.Fatalf("expected empty string (no credentials), got %q", resolved)
	}
}

func TestResolveModelTier_EmptyTier(t *testing.T) {
	home := t.TempDir()
	infraDir := filepath.Join(home, "infrastructure")
	os.MkdirAll(infraDir, 0755)

	routingYAML := []byte(`
version: "0.1"
providers:
  anthropic:
    api_base: https://api.anthropic.com/v1
    auth_env: ANTHROPIC_API_KEY
models:
  claude-sonnet:
    provider: anthropic
    provider_model: claude-sonnet-4-20250514
tiers:
  standard:
    - model: claude-sonnet
      preference: 0
`)
	os.WriteFile(filepath.Join(infraDir, "routing.yaml"), routingYAML, 0644)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	ss := &StartSequence{Home: home}
	// "mini" tier has no entries
	resolved := ss.resolveModelTier("mini")
	if resolved != "" {
		t.Fatalf("expected empty string for empty tier, got %q", resolved)
	}
}

func TestResolveModelTier_NoRoutingFile(t *testing.T) {
	home := t.TempDir()
	ss := &StartSequence{Home: home}
	resolved := ss.resolveModelTier("standard")
	if resolved != "" {
		t.Fatalf("expected empty string with no routing file, got %q", resolved)
	}
}

func TestResolveModelTier_MultiProviderPicksCredentialed(t *testing.T) {
	home := t.TempDir()
	infraDir := filepath.Join(home, "infrastructure")
	os.MkdirAll(infraDir, 0755)

	// Two providers in standard tier, anthropic preferred but no creds, google has creds
	routingYAML := []byte(`
version: "0.1"
providers:
  anthropic:
    api_base: https://api.anthropic.com/v1
    auth_env: ANTHROPIC_API_KEY
  google:
    api_base: https://generativelanguage.googleapis.com/v1beta/openai
    auth_env: GOOGLE_API_KEY
models:
  claude-sonnet:
    provider: anthropic
    provider_model: claude-sonnet-4-20250514
  gemini-flash:
    provider: google
    provider_model: gemini-2.0-flash
tiers:
  standard:
    - model: claude-sonnet
      preference: 0
    - model: gemini-flash
      preference: 1
`)
	os.WriteFile(filepath.Join(infraDir, "routing.yaml"), routingYAML, 0644)

	// Only Google has credentials
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "test-key")

	ss := &StartSequence{Home: home}
	resolved := ss.resolveModelTier("standard")
	if resolved != "gemini-flash" {
		t.Fatalf("expected gemini-flash (only credentialed provider), got %q", resolved)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency && go test ./internal/orchestrate/ -run "TestResolveModelTier" -v`

Expected: Most tests fail — the current `resolveModelTier` treats providers as an array, so it never matches anything. `TestResolveModelTier_NoRoutingFile` and `TestResolveModelTier_EmptyTier` may pass vacuously (both return "").

- [ ] **Step 3: Rewrite resolveModelTier**

Replace the function in `start.go:529-584` with:

```go
func (ss *StartSequence) resolveModelTier(tier string) string {
	routingPath := filepath.Join(ss.Home, "infrastructure", "routing.yaml")
	data, err := os.ReadFile(routingPath)
	if err != nil {
		return ""
	}

	var rc models.RoutingConfig
	if err := yaml.Unmarshal(data, &rc); err != nil {
		return ""
	}

	// Credential check helper
	hasCredential := func(authEnv string) bool {
		if authEnv == "" {
			return true // no credential needed (e.g., local Ollama)
		}
		if os.Getenv(authEnv) != "" {
			return true
		}
		if ss.CredStore != nil {
			if entry, err := ss.CredStore.Get(authEnv); err == nil && entry.Value != "" {
				return true
			}
		}
		return false
	}

	// Walk tier entries in preference order, return first with valid credentials
	var entries []models.TierEntry
	switch tier {
	case "frontier":
		entries = rc.Tiers.Frontier
	case "standard":
		entries = rc.Tiers.Standard
	case "fast":
		entries = rc.Tiers.Fast
	case "mini":
		entries = rc.Tiers.Mini
	case "nano":
		entries = rc.Tiers.Nano
	case "batch":
		entries = rc.Tiers.Batch
	default:
		return ""
	}

	// Sort by preference (lowest wins)
	for _, e := range sortedByPreference(entries) {
		pc, _ := rc.ResolveModel(e.Model)
		if pc == nil {
			continue
		}
		if hasCredential(pc.AuthEnv) {
			return e.Model
		}
	}
	return ""
}

// sortedByPreference returns tier entries sorted by preference (lowest first).
// The input slice is not modified.
func sortedByPreference(entries []models.TierEntry) []models.TierEntry {
	sorted := make([]models.TierEntry, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Preference < sorted[j].Preference
	})
	return sorted
}
```

Add `"sort"` to imports and ensure `models` import exists:
```go
import "github.com/geoffbelknap/agency/internal/models"
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency && go test ./internal/orchestrate/ -run "TestResolveModelTier" -v`

Expected: All 5 tests pass.

- [ ] **Step 5: Commit**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency
git add internal/orchestrate/start.go internal/orchestrate/start_test.go
git commit -m "fix: rewrite resolveModelTier to use typed RoutingConfig

The old implementation walked providers as an array and looked for
tier fields inside model objects — but routing.yaml has providers
as a map and tiers as a separate top-level section. The function
never matched anything.

Now uses models.RoutingConfig struct and walks tier entries in
preference order, checking credential availability for each."
```

---

### Task 3: Fix default fallback — test + implement

**Files:**
- Modify: `internal/orchestrate/start.go:248-258`
- Modify: `internal/orchestrate/start_test.go`

- [ ] **Step 1: Write tests for default model resolution**

Add to `start_test.go`:

```go
func TestDefaultModelResolution_UsesTierWhenAvailable(t *testing.T) {
	home := t.TempDir()

	// Set up routing with Gemini in standard tier
	infraDir := filepath.Join(home, "infrastructure")
	os.MkdirAll(infraDir, 0755)
	routingYAML := []byte(`
version: "0.1"
providers:
  google:
    api_base: https://generativelanguage.googleapis.com/v1beta/openai
    auth_env: GOOGLE_API_KEY
models:
  gemini-flash:
    provider: google
    provider_model: gemini-2.0-flash
tiers:
  standard:
    - model: gemini-flash
      preference: 0
settings:
  default_tier: standard
`)
	os.WriteFile(filepath.Join(infraDir, "routing.yaml"), routingYAML, 0644)
	t.Setenv("GOOGLE_API_KEY", "test-key")

	// Agent config with NO model and NO model_tier
	agentConfig := map[string]interface{}{
		"name": "test-agent",
		"type": "standard",
	}

	ss := &StartSequence{Home: home, agentConfig: agentConfig}
	model := ss.resolveDefaultModel()
	if model != "gemini-flash" {
		t.Fatalf("expected gemini-flash from default tier, got %q", model)
	}
}

func TestDefaultModelResolution_ModelTierOverridesDefault(t *testing.T) {
	home := t.TempDir()

	infraDir := filepath.Join(home, "infrastructure")
	os.MkdirAll(infraDir, 0755)
	routingYAML := []byte(`
version: "0.1"
providers:
  google:
    api_base: https://generativelanguage.googleapis.com/v1beta/openai
    auth_env: GOOGLE_API_KEY
models:
  gemini-flash:
    provider: google
    provider_model: gemini-2.0-flash
  gemini-pro:
    provider: google
    provider_model: gemini-2.0-pro
tiers:
  standard:
    - model: gemini-flash
      preference: 0
  frontier:
    - model: gemini-pro
      preference: 0
settings:
  default_tier: standard
`)
	os.WriteFile(filepath.Join(infraDir, "routing.yaml"), routingYAML, 0644)
	t.Setenv("GOOGLE_API_KEY", "test-key")

	// Agent config with model_tier set
	agentConfig := map[string]interface{}{
		"name":       "test-agent",
		"model_tier": "frontier",
	}

	ss := &StartSequence{Home: home, agentConfig: agentConfig}
	model := ss.resolveDefaultModel()
	if model != "gemini-pro" {
		t.Fatalf("expected gemini-pro from model_tier=frontier, got %q", model)
	}
}

func TestDefaultModelResolution_ExplicitModelWins(t *testing.T) {
	home := t.TempDir()

	agentConfig := map[string]interface{}{
		"name":  "test-agent",
		"model": "custom-model-alias",
	}

	ss := &StartSequence{Home: home, agentConfig: agentConfig}
	model := ss.resolveDefaultModel()
	if model != "custom-model-alias" {
		t.Fatalf("expected explicit model, got %q", model)
	}
}

func TestDefaultModelResolution_FailsClosedWhenNothingResolves(t *testing.T) {
	home := t.TempDir()

	// No routing file, no explicit model
	agentConfig := map[string]interface{}{
		"name": "test-agent",
	}

	ss := &StartSequence{Home: home, agentConfig: agentConfig}
	model := ss.resolveDefaultModel()
	if model != "" {
		t.Fatalf("expected empty string (fail closed), got %q", model)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency && go test ./internal/orchestrate/ -run "TestDefaultModelResolution" -v`

Expected: Compilation error — `resolveDefaultModel` is undefined.

- [ ] **Step 3: Implement resolveDefaultModel and update phase3Constraints**

Add `resolveDefaultModel` to `start.go`:

```go
// resolveDefaultModel determines the model for this agent using the priority:
// 1. Explicit "model" field in agent.yaml
// 2. "model_tier" field in agent.yaml → tier resolution
// 3. Default tier from routing settings → tier resolution
// Returns "" if nothing resolves (caller should fail closed).
func (ss *StartSequence) resolveDefaultModel() string {
	// Priority 1: explicit model
	if m, ok := ss.agentConfig["model"].(string); ok && m != "" {
		return m
	}

	// Priority 2: model_tier from agent config (set by preset)
	if tier, ok := ss.agentConfig["model_tier"].(string); ok && tier != "" {
		if resolved := ss.resolveModelTier(tier); resolved != "" {
			return resolved
		}
	}

	// Priority 3: default tier from routing settings
	routingPath := filepath.Join(ss.Home, "infrastructure", "routing.yaml")
	data, err := os.ReadFile(routingPath)
	if err != nil {
		return ""
	}
	var rc models.RoutingConfig
	if err := yaml.Unmarshal(data, &rc); err != nil {
		return ""
	}
	defaultTier := rc.Settings.DefaultTier
	if defaultTier == "" {
		defaultTier = "standard"
	}
	return ss.resolveModelTier(defaultTier)
}
```

Then update `phase3Constraints` (around line 248-258). Replace:

```go
	// Resolve model
	ss.model = "claude-sonnet"
	if m, ok := ss.agentConfig["model"].(string); ok && m != "" {
		ss.model = m
	}
	if tier, ok := ss.agentConfig["model_tier"].(string); ok && tier != "" {
		resolved := ss.resolveModelTier(tier)
		if resolved != "" {
			ss.model = resolved
		}
	}
```

With:

```go
	// Resolve model — fail closed if nothing resolves
	ss.model = ss.resolveDefaultModel()
	if ss.model == "" {
		return fmt.Errorf("no model available: no explicit model, no model_tier resolved, and default tier has no credentialed providers — check routing.yaml and credentials")
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency && go test ./internal/orchestrate/ -run "TestDefaultModelResolution|TestResolveModelTier" -v`

Expected: All tests pass.

- [ ] **Step 5: Run full test suites**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency && go test ./internal/orchestrate/ -v && go test ./... 2>&1 | grep -E "^(ok|FAIL)"`

Expected: All pass, no regressions.

- [ ] **Step 6: Commit**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency
git add internal/orchestrate/start.go internal/orchestrate/start_test.go
git commit -m "fix: replace hardcoded claude-sonnet default with tier resolution

Model resolution now follows priority: explicit model > model_tier
from preset > default tier from routing settings. If nothing resolves,
agent startup fails with a clear error instead of silently using a
model that may not have credentials."
```

---

### Task 4: Build and verify

- [ ] **Step 1: Build**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency && go build ./cmd/gateway/`

Expected: Clean build.

- [ ] **Step 2: Full test suite**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency && go test ./... 2>&1 | grep -E "^(ok|FAIL)"`

Expected: All pass.
