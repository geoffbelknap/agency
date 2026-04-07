# Setup Wizard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a conversational onboarding wizard for agency-web that guides users through hub sync, LLM provider configuration, agent creation, capability enablement, and a first chat — with all data sourced from the hub.

**Architecture:** Three workstreams with sequential dependencies: hub content (provider/setup/preset components) → backend (new kinds, install logic, API endpoints, tier strategy) → frontend (wizard UI with six steps). Each step validates before allowing progression.

**Tech Stack:** Go (backend), React 19 + Tailwind v4 + shadcn/ui (frontend), YAML (hub components)

**Spec:** `docs/specs/setup-wizard.md`

---

## Workstream 1: Hub Content (agency-hub)

### Task 1: Add provider components to agency-hub

**Files:**
- Create: `providers/anthropic/provider.yaml`
- Create: `providers/openai/provider.yaml`
- Create: `providers/ollama/provider.yaml`
- Create: `providers/openai-compatible/provider.yaml`

- [ ] **Step 1: Create providers directory**

```bash
mkdir -p /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-hub/providers/{anthropic,openai,ollama,openai-compatible}
```

- [ ] **Step 2: Create Anthropic provider**

Write `providers/anthropic/provider.yaml`:
```yaml
name: anthropic
display_name: Anthropic
description: Claude models — Opus, Sonnet, Haiku
category: cloud
credential:
  name: anthropic-api-key
  label: API Key
  env_var: ANTHROPIC_API_KEY
  api_key_url: https://console.anthropic.com/settings/keys
routing:
  api_base: https://api.anthropic.com/v1
  auth_header: x-api-key
  auth_prefix: ""
  models:
    claude-opus:
      provider_model: claude-opus-4-20250514
      cost_per_mtok_in: 5.0
      cost_per_mtok_out: 25.0
      cost_per_mtok_cached: 0.50
    claude-sonnet:
      provider_model: claude-sonnet-4-20250514
      cost_per_mtok_in: 3.0
      cost_per_mtok_out: 15.0
      cost_per_mtok_cached: 0.30
    claude-haiku:
      provider_model: claude-haiku-4-5-20251001
      cost_per_mtok_in: 1.0
      cost_per_mtok_out: 5.0
      cost_per_mtok_cached: 0.10
  tiers:
    frontier: claude-opus
    standard: claude-sonnet
    fast: claude-haiku
    mini: null
    nano: null
    batch: null
```

- [ ] **Step 3: Create OpenAI provider**

Write `providers/openai/provider.yaml`:
```yaml
name: openai
display_name: OpenAI
description: GPT models — GPT-4.1, GPT-4o, GPT-4.1-mini, GPT-4.1-nano
category: cloud
credential:
  name: openai-api-key
  label: API Key
  env_var: OPENAI_API_KEY
  api_key_url: https://platform.openai.com/api-keys
routing:
  api_base: https://api.openai.com/v1
  auth_header: Authorization
  auth_prefix: "Bearer "
  models:
    gpt-4.1:
      provider_model: gpt-4.1
      cost_per_mtok_in: 2.0
      cost_per_mtok_out: 8.0
      cost_per_mtok_cached: 0.50
    gpt-4o:
      provider_model: gpt-4o
      cost_per_mtok_in: 2.50
      cost_per_mtok_out: 10.0
      cost_per_mtok_cached: 1.25
    gpt-4.1-mini:
      provider_model: gpt-4.1-mini
      cost_per_mtok_in: 0.40
      cost_per_mtok_out: 1.60
      cost_per_mtok_cached: 0.10
    gpt-4.1-nano:
      provider_model: gpt-4.1-nano
      cost_per_mtok_in: 0.10
      cost_per_mtok_out: 0.40
      cost_per_mtok_cached: 0.025
  tiers:
    frontier: gpt-4.1
    standard: gpt-4o
    fast: gpt-4.1-mini
    mini: gpt-4.1-mini
    nano: gpt-4.1-nano
    batch: null
```

- [ ] **Step 4: Create Ollama provider**

Write `providers/ollama/provider.yaml`:
```yaml
name: ollama
display_name: Ollama
description: Run open models locally — Llama, Mistral, Gemma, Phi
category: local
credential: null
routing:
  api_base: http://localhost:11434/v1
  api_base_configurable: true
  auth_header: null
  auth_prefix: ""
  models: {}
  tiers: {}
```

- [ ] **Step 5: Create OpenAI-Compatible provider**

Write `providers/openai-compatible/provider.yaml`:
```yaml
name: openai-compatible
display_name: OpenAI-Compatible
description: LiteLLM, OpenRouter, Azure Foundry, AWS Bedrock, and other OpenAI-compatible APIs
category: compatible
credential:
  name: custom-llm-api-key
  label: API Key
  env_var: CUSTOM_LLM_API_KEY
routing:
  api_base_configurable: true
  auth_header: Authorization
  auth_prefix: "Bearer "
  models: {}
  tiers: {}
```

- [ ] **Step 6: Commit**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-hub
git add providers/
git commit -m "feat: add provider hub components (anthropic, openai, ollama, openai-compatible)"
```

---

### Task 2: Add setup wizard config and platform-expert preset

**Files:**
- Create: `setup/default-wizard/setup.yaml`
- Create: `presets/platform-expert/preset.yaml`

- [ ] **Step 1: Create setup directory**

```bash
mkdir -p /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-hub/setup/default-wizard
```

- [ ] **Step 2: Create wizard config**

Write `setup/default-wizard/setup.yaml`:
```yaml
name: default-wizard
kind: setup

capability_tiers:
  minimal:
    display_name: Minimal
    description: LLM access only. No optional tools or services.
    capabilities: []
  standard:
    display_name: Standard
    description: Web search, web fetch, and commonly useful tools.
    capabilities:
      - brave-search
      - web-fetch
```

- [ ] **Step 3: Create platform-expert preset**

```bash
mkdir -p /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-hub/presets/platform-expert
```

Write `presets/platform-expert/preset.yaml`:
```yaml
name: platform-expert
type: specialist
model_tier: standard
description: Agency platform expert — answers questions about capabilities, commands, architecture, and workflows

capabilities:
  - file_read
  - file_write
  - shell_exec
  - web_search

identity:
  purpose: "Help operators understand and use the Agency platform"
  body: |
    You are an Agency platform expert. You have deep knowledge of how Agency
    works — its architecture, commands, capabilities, connectors, agents,
    presets, missions, the hub, and common workflows.

    You help operators learn the platform, troubleshoot issues, and discover
    features. You answer questions clearly and suggest next steps.

    You have full use of your workspace — you can read, write, and execute
    within it. You do not have platform admin tools (trust elevation,
    credential management, infrastructure control).

    ## What you know about
    - Agent lifecycle (create, start, stop, presets, trust levels)
    - Capabilities (what they are, how to enable/disable, credential requirements)
    - Connectors (what they do, how to set up, credential flow)
    - The Hub (search, install, update, upgrade)
    - Knowledge graph (ontology, nodes, relationships)
    - Missions (assignment, objectives, success criteria)
    - Teams and departments
    - Policy framework and trust model
    - LLM routing (providers, tiers, strategies)
    - Common troubleshooting steps

    ## Response style
    - Be concise and practical
    - Suggest specific commands when helpful
    - Link to relevant concepts when explaining
  hard_limits:
    - rule: "never modify platform configuration directly"
      reason: "platform expert is advisory only"
    - rule: "never expose credentials, tokens, or secrets in any output"
      reason: "credential exposure is a critical security risk"
  escalation:
    always_escalate:
      - "requests to change platform configuration"
      - "security concerns or incidents"
    flag_before_proceeding:
      - "questions about production deployments"
```

- [ ] **Step 4: Commit**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-hub
git add setup/ presets/platform-expert/
git commit -m "feat: add setup wizard config and platform-expert preset"
```

---

## Workstream 2: Backend (agency)

### Task 3: Add provider and setup to KnownKinds, update discover()

**Files:**
- Modify: `internal/hub/hub.go`
- Modify: `internal/hub/hub_test.go`

- [ ] **Step 1: Write test for provider kind discovery**

Add to `internal/hub/hub_test.go`:
```go
func TestDiscoverProviderKind(t *testing.T) {
	home := t.TempDir()
	mgr := &Manager{Home: home}

	// Set up a fake hub source and cache
	cacheDir := filepath.Join(home, "hub-cache", "default", "providers", "test-provider")
	os.MkdirAll(cacheDir, 0755)
	os.WriteFile(filepath.Join(cacheDir, "provider.yaml"), []byte(`
provider: test-provider
display_name: Test Provider
description: A test provider
category: cloud
`), 0644)

	// Write config so discover finds the source
	cfgDir := filepath.Join(home, "hub")
	os.MkdirAll(cfgDir, 0755)
	os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(`
hub:
  sources:
    - name: default
      url: https://example.com/hub.git
`), 0644)

	results := mgr.discover()
	found := false
	for _, c := range results {
		if c.Name == "test-provider" && c.Kind == "provider" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected to discover provider 'test-provider', got %v", results)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency && go test ./internal/hub/ -run TestDiscoverProviderKind -v`
Expected: FAIL — "provider" not in KnownKinds

- [ ] **Step 3: Add provider and setup to KnownKinds**

In `internal/hub/hub.go`, change line 47:
```go
var KnownKinds = []string{"pack", "preset", "connector", "service", "mission", "skill", "workspace", "policy", "ontology", "provider", "setup"}
```

- [ ] **Step 4: Update discover() to handle provider key**

In `internal/hub/hub.go`, in the `discover()` function, update the name extraction block (around line 973):
```go
				name, _ := doc["name"].(string)
				if name == "" {
					name, _ = doc["service"].(string)
				}
				if name == "" {
					name, _ = doc["provider"].(string)
				}
				if name == "" {
					return nil
				}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency && go test ./internal/hub/ -run TestDiscoverProviderKind -v`
Expected: PASS

- [ ] **Step 6: Build to verify**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency && go build ./...`
Expected: compiles clean

- [ ] **Step 7: Commit**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency
git add internal/hub/hub.go internal/hub/hub_test.go
git commit -m "feat: add provider and setup to hub KnownKinds"
```

---

### Task 4: Add batch tier and tier_strategy to routing config

**Files:**
- Modify: `internal/models/routing.go`
- Modify: `internal/models/routing_test.go`

- [ ] **Step 1: Write tests for batch tier and tier strategies**

Add to `internal/models/routing_test.go`:
```go
func TestResolveTierBatch(t *testing.T) {
	cfg := RoutingConfig{
		Providers: map[string]ProviderConfig{
			"test": {APIBase: "https://api.test.com"},
		},
		Models: map[string]ModelConfig{
			"test-batch": {Provider: "test", ProviderModel: "test-batch-v1"},
		},
		Tiers: TierConfig{
			Batch: []TierEntry{{Model: "test-batch", Preference: 0}},
		},
	}
	pc, mc := cfg.ResolveTier("batch", nil)
	if pc == nil || mc == nil {
		t.Fatal("expected to resolve batch tier")
	}
	if mc.ProviderModel != "test-batch-v1" {
		t.Errorf("expected provider_model 'test-batch-v1', got %q", mc.ProviderModel)
	}
}

func TestTierStrategyValidation(t *testing.T) {
	tests := []struct {
		strategy string
		wantErr  bool
	}{
		{"strict", false},
		{"best_effort", false},
		{"catch_all", false},
		{"", false},           // defaults to best_effort
		{"invalid", true},
	}
	for _, tt := range tests {
		cfg := RoutingConfig{
			Settings: RoutingSettings{
				DefaultTier:  "standard",
				TierStrategy: tt.strategy,
			},
		}
		err := cfg.Validate()
		if (err != nil) != tt.wantErr {
			t.Errorf("strategy=%q: got err=%v, wantErr=%v", tt.strategy, err, tt.wantErr)
		}
	}
}

func TestResolveTierBestEffortFallback(t *testing.T) {
	cfg := RoutingConfig{
		Providers: map[string]ProviderConfig{
			"test": {APIBase: "https://api.test.com"},
		},
		Models: map[string]ModelConfig{
			"test-fast": {Provider: "test", ProviderModel: "fast-v1"},
		},
		Tiers: TierConfig{
			Fast: []TierEntry{{Model: "test-fast", Preference: 0}},
		},
		Settings: RoutingSettings{
			TierStrategy: "best_effort",
			DefaultTier:  "standard",
		},
	}
	// nano has no entries, should fall back to fast
	pc, mc := cfg.ResolveTierWithStrategy("nano", nil)
	if pc == nil || mc == nil {
		t.Fatal("best_effort should fall back to nearest tier")
	}
	if mc.ProviderModel != "fast-v1" {
		t.Errorf("expected fallback to fast-v1, got %q", mc.ProviderModel)
	}
}

func TestResolveTierStrictNoFallback(t *testing.T) {
	cfg := RoutingConfig{
		Providers: map[string]ProviderConfig{
			"test": {APIBase: "https://api.test.com"},
		},
		Models: map[string]ModelConfig{
			"test-fast": {Provider: "test", ProviderModel: "fast-v1"},
		},
		Tiers: TierConfig{
			Fast: []TierEntry{{Model: "test-fast", Preference: 0}},
		},
		Settings: RoutingSettings{
			TierStrategy: "strict",
			DefaultTier:  "standard",
		},
	}
	// nano has no entries, strict should return nil
	pc, mc := cfg.ResolveTierWithStrategy("nano", nil)
	if pc != nil || mc != nil {
		t.Fatal("strict should not fall back")
	}
}

func TestResolveTierCatchAll(t *testing.T) {
	cfg := RoutingConfig{
		Providers: map[string]ProviderConfig{
			"test": {APIBase: "https://api.test.com"},
		},
		Models: map[string]ModelConfig{
			"test-standard": {Provider: "test", ProviderModel: "std-v1"},
		},
		Tiers: TierConfig{
			Standard: []TierEntry{{Model: "test-standard", Preference: 0}},
		},
		Settings: RoutingSettings{
			TierStrategy: "catch_all",
			DefaultTier:  "standard",
		},
	}
	// nano has no entries, catch_all should return any model
	pc, mc := cfg.ResolveTierWithStrategy("nano", nil)
	if pc == nil || mc == nil {
		t.Fatal("catch_all should return any available model")
	}
	if mc.ProviderModel != "std-v1" {
		t.Errorf("expected catch_all to return std-v1, got %q", mc.ProviderModel)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency && go test ./internal/models/ -run "TestResolveTierBatch|TestTierStrategy|TestResolveTierBestEffort|TestResolveTierStrict|TestResolveTierCatchAll" -v`
Expected: FAIL — Batch field, TierStrategy field, and ResolveTierWithStrategy not defined

- [ ] **Step 3: Add batch tier to TierConfig**

In `internal/models/routing.go`, update `VALID_TIERS`:
```go
var VALID_TIERS = []string{"frontier", "standard", "fast", "mini", "nano", "batch"}
```

Add `Batch` field to `TierConfig`:
```go
type TierConfig struct {
	Frontier []TierEntry `yaml:"frontier"`
	Standard []TierEntry `yaml:"standard"`
	Fast     []TierEntry `yaml:"fast"`
	Mini     []TierEntry `yaml:"mini"`
	Nano     []TierEntry `yaml:"nano"`
	Batch    []TierEntry `yaml:"batch"`
}
```

Add `"batch"` case to `ResolveTier`:
```go
	case "batch":
		entries = r.Tiers.Batch
```

- [ ] **Step 4: Add TierStrategy to RoutingSettings**

In `internal/models/routing.go`, update `RoutingSettings`:
```go
type RoutingSettings struct {
	XPIAScan       bool   `yaml:"xpia_scan" default:"true"`
	DefaultTimeout int    `yaml:"default_timeout" default:"300"`
	DefaultTier    string `yaml:"default_tier" default:"standard"`
	TierStrategy   string `yaml:"tier_strategy" default:"best_effort"`
}
```

Update `RoutingSettings.Validate()`:
```go
func (s *RoutingSettings) Validate() error {
	validTier := false
	for _, t := range VALID_TIERS {
		if s.DefaultTier == t {
			validTier = true
			break
		}
	}
	if !validTier {
		return fmt.Errorf(
			"default_tier must be one of %v, got %q",
			VALID_TIERS, s.DefaultTier,
		)
	}
	if s.DefaultTimeout < 1 || s.DefaultTimeout > 3600 {
		return fmt.Errorf("default_timeout must be between 1 and 3600, got %d", s.DefaultTimeout)
	}
	validStrategies := []string{"strict", "best_effort", "catch_all"}
	validStrategy := false
	for _, st := range validStrategies {
		if s.TierStrategy == st {
			validStrategy = true
			break
		}
	}
	if !validStrategy {
		return fmt.Errorf("tier_strategy must be one of %v, got %q", validStrategies, s.TierStrategy)
	}
	return nil
}
```

Update `RoutingConfig.Validate()` to default `TierStrategy`:
```go
	if r.Settings.TierStrategy == "" {
		r.Settings.TierStrategy = "best_effort"
	}
```

- [ ] **Step 5: Implement ResolveTierWithStrategy**

Add to `internal/models/routing.go`:
```go
// tierOrder defines the hierarchy from most capable to least.
var tierOrder = []string{"frontier", "standard", "fast", "mini", "nano", "batch"}

// ResolveTierWithStrategy resolves a tier using the configured tier_strategy.
// - strict: return nil if the requested tier has no entries
// - best_effort: walk to nearest available tier (down first, then up)
// - catch_all: return any available model regardless of tier
func (r *RoutingConfig) ResolveTierWithStrategy(tier string, extraEnv map[string]string) (*ProviderConfig, *ModelConfig) {
	strategy := r.Settings.TierStrategy
	if strategy == "" {
		strategy = "best_effort"
	}

	// Try the requested tier first
	pc, mc := r.ResolveTier(tier, extraEnv)
	if pc != nil && mc != nil {
		return pc, mc
	}

	switch strategy {
	case "strict":
		return nil, nil

	case "best_effort":
		// Find position of requested tier in order
		pos := -1
		for i, t := range tierOrder {
			if t == tier {
				pos = i
				break
			}
		}
		if pos < 0 {
			return nil, nil
		}
		// Walk down (lighter tiers), then up (heavier tiers)
		for delta := 1; delta < len(tierOrder); delta++ {
			// Try lighter
			if pos+delta < len(tierOrder) {
				if pc, mc := r.ResolveTier(tierOrder[pos+delta], extraEnv); pc != nil && mc != nil {
					return pc, mc
				}
			}
			// Try heavier
			if pos-delta >= 0 {
				if pc, mc := r.ResolveTier(tierOrder[pos-delta], extraEnv); pc != nil && mc != nil {
					return pc, mc
				}
			}
		}
		return nil, nil

	case "catch_all":
		// Try every tier until one works
		for _, t := range tierOrder {
			if pc, mc := r.ResolveTier(t, extraEnv); pc != nil && mc != nil {
				return pc, mc
			}
		}
		return nil, nil

	default:
		return nil, nil
	}
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency && go test ./internal/models/ -run "TestResolveTierBatch|TestTierStrategy|TestResolveTierBestEffort|TestResolveTierStrict|TestResolveTierCatchAll" -v`
Expected: all PASS

- [ ] **Step 7: Run full model tests**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency && go test ./internal/models/ -v`
Expected: all PASS

- [ ] **Step 8: Commit**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency
git add internal/models/routing.go internal/models/routing_test.go
git commit -m "feat: add batch tier and tier_strategy (strict/best_effort/catch_all)"
```

---

### Task 5: Provider install merges routing config

**Files:**
- Modify: `internal/hub/hub.go`
- Create: `internal/hub/provider.go`
- Create: `internal/hub/provider_test.go`

- [ ] **Step 1: Write test for provider routing merge**

Create `internal/hub/provider_test.go`:
```go
package hub

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestMergeProviderRouting(t *testing.T) {
	home := t.TempDir()
	infraDir := filepath.Join(home, "infrastructure")
	os.MkdirAll(infraDir, 0755)

	// Start with empty routing config
	initialRouting := map[string]interface{}{
		"version":   "0.1",
		"providers": map[string]interface{}{},
		"models":    map[string]interface{}{},
		"tiers":     map[string]interface{}{},
		"settings":  map[string]interface{}{"default_tier": "standard", "tier_strategy": "best_effort"},
	}
	data, _ := yaml.Marshal(initialRouting)
	os.WriteFile(filepath.Join(infraDir, "routing.yaml"), data, 0644)

	// Provider YAML with routing block
	providerYAML := []byte(`
provider: test-provider
display_name: Test Provider
category: cloud
routing:
  api_base: https://api.test.com/v1
  auth_header: Authorization
  auth_prefix: "Bearer "
  models:
    test-standard:
      provider_model: test-std-v1
      cost_per_mtok_in: 3.0
      cost_per_mtok_out: 15.0
  tiers:
    standard: test-standard
    fast: null
`)

	err := MergeProviderRouting(home, "test-provider", providerYAML)
	if err != nil {
		t.Fatalf("MergeProviderRouting: %v", err)
	}

	// Read back and verify
	merged, _ := os.ReadFile(filepath.Join(infraDir, "routing.yaml"))
	var cfg map[string]interface{}
	yaml.Unmarshal(merged, &cfg)

	providers, _ := cfg["providers"].(map[string]interface{})
	if _, ok := providers["test-provider"]; !ok {
		t.Error("expected provider 'test-provider' in routing config")
	}

	models, _ := cfg["models"].(map[string]interface{})
	if _, ok := models["test-standard"]; !ok {
		t.Error("expected model 'test-standard' in routing config")
	}
}

func TestRemoveProviderRouting(t *testing.T) {
	home := t.TempDir()
	infraDir := filepath.Join(home, "infrastructure")
	os.MkdirAll(infraDir, 0755)

	// Start with a provider already in routing
	routing := map[string]interface{}{
		"version": "0.1",
		"providers": map[string]interface{}{
			"keep":   map[string]interface{}{"api_base": "https://keep.com"},
			"remove": map[string]interface{}{"api_base": "https://remove.com"},
		},
		"models": map[string]interface{}{
			"keep-model":   map[string]interface{}{"provider": "keep", "provider_model": "v1"},
			"remove-model": map[string]interface{}{"provider": "remove", "provider_model": "v1"},
		},
		"tiers":    map[string]interface{}{},
		"settings": map[string]interface{}{"default_tier": "standard"},
	}
	data, _ := yaml.Marshal(routing)
	os.WriteFile(filepath.Join(infraDir, "routing.yaml"), data, 0644)

	err := RemoveProviderRouting(home, "remove")
	if err != nil {
		t.Fatalf("RemoveProviderRouting: %v", err)
	}

	merged, _ := os.ReadFile(filepath.Join(infraDir, "routing.yaml"))
	var cfg map[string]interface{}
	yaml.Unmarshal(merged, &cfg)

	providers, _ := cfg["providers"].(map[string]interface{})
	if _, ok := providers["remove"]; ok {
		t.Error("provider 'remove' should have been deleted")
	}
	if _, ok := providers["keep"]; !ok {
		t.Error("provider 'keep' should still exist")
	}

	models, _ := cfg["models"].(map[string]interface{})
	if _, ok := models["remove-model"]; ok {
		t.Error("model 'remove-model' should have been deleted")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency && go test ./internal/hub/ -run "TestMergeProviderRouting|TestRemoveProviderRouting" -v`
Expected: FAIL — functions not defined

- [ ] **Step 3: Implement provider routing merge/remove**

Create `internal/hub/provider.go`:
```go
package hub

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// MergeProviderRouting reads a provider YAML, extracts its routing block,
// and merges it into ~/.agency/infrastructure/routing.yaml.
func MergeProviderRouting(home, providerName string, providerData []byte) error {
	var doc map[string]interface{}
	if err := yaml.Unmarshal(providerData, &doc); err != nil {
		return fmt.Errorf("parse provider YAML: %w", err)
	}

	routing, ok := doc["routing"].(map[string]interface{})
	if !ok {
		return nil // no routing block — nothing to merge
	}

	routingPath := filepath.Join(home, "infrastructure", "routing.yaml")
	cfg, err := loadRoutingYAML(routingPath)
	if err != nil {
		return err
	}

	// Merge provider config
	providers := ensureMap(cfg, "providers")
	providerCfg := map[string]interface{}{}
	if v, ok := routing["api_base"]; ok {
		providerCfg["api_base"] = v
	}
	if v, ok := routing["auth_header"]; ok {
		providerCfg["auth_header"] = v
	}
	if v, ok := routing["auth_prefix"]; ok {
		providerCfg["auth_prefix"] = v
	}
	if v, ok := routing["auth_env"]; ok {
		providerCfg["auth_env"] = v
	}
	providers[providerName] = providerCfg

	// Merge models
	models := ensureMap(cfg, "models")
	if routingModels, ok := routing["models"].(map[string]interface{}); ok {
		for modelName, modelCfg := range routingModels {
			mc, ok := modelCfg.(map[string]interface{})
			if !ok {
				continue
			}
			mc["provider"] = providerName
			models[modelName] = mc
		}
	}

	// Merge tier entries
	tiers := ensureMap(cfg, "tiers")
	if routingTiers, ok := routing["tiers"].(map[string]interface{}); ok {
		for tierName, modelRef := range routingTiers {
			if modelRef == nil {
				continue // null means provider doesn't cover this tier
			}
			modelName, ok := modelRef.(string)
			if !ok {
				continue
			}
			existing, _ := tiers[tierName].([]interface{})
			entry := map[string]interface{}{
				"model":      modelName,
				"preference": len(existing), // append at end
			}
			tiers[tierName] = append(existing, entry)
		}
	}

	return writeRoutingYAML(routingPath, cfg)
}

// RemoveProviderRouting removes a provider and its models from routing.yaml.
func RemoveProviderRouting(home, providerName string) error {
	routingPath := filepath.Join(home, "infrastructure", "routing.yaml")
	cfg, err := loadRoutingYAML(routingPath)
	if err != nil {
		return err
	}

	// Remove provider
	if providers, ok := cfg["providers"].(map[string]interface{}); ok {
		delete(providers, providerName)
	}

	// Remove models belonging to this provider
	if models, ok := cfg["models"].(map[string]interface{}); ok {
		for name, mc := range models {
			if m, ok := mc.(map[string]interface{}); ok {
				if m["provider"] == providerName {
					delete(models, name)
				}
			}
		}
	}

	// Remove tier entries referencing deleted models
	if tiers, ok := cfg["tiers"].(map[string]interface{}); ok {
		models, _ := cfg["models"].(map[string]interface{})
		for tierName, entries := range tiers {
			tierEntries, ok := entries.([]interface{})
			if !ok {
				continue
			}
			var kept []interface{}
			for _, e := range tierEntries {
				entry, ok := e.(map[string]interface{})
				if !ok {
					continue
				}
				modelName, _ := entry["model"].(string)
				if _, exists := models[modelName]; exists {
					kept = append(kept, entry)
				}
			}
			tiers[tierName] = kept
		}
	}

	return writeRoutingYAML(routingPath, cfg)
}

func loadRoutingYAML(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]interface{}{
				"version":   "0.1",
				"providers": map[string]interface{}{},
				"models":    map[string]interface{}{},
				"tiers":     map[string]interface{}{},
				"settings":  map[string]interface{}{"default_tier": "standard", "tier_strategy": "best_effort"},
			}, nil
		}
		return nil, fmt.Errorf("read routing.yaml: %w", err)
	}
	var cfg map[string]interface{}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse routing.yaml: %w", err)
	}
	return cfg, nil
}

func writeRoutingYAML(path string, cfg map[string]interface{}) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal routing.yaml: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

func ensureMap(parent map[string]interface{}, key string) map[string]interface{} {
	if m, ok := parent[key].(map[string]interface{}); ok {
		return m
	}
	m := map[string]interface{}{}
	parent[key] = m
	return m
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency && go test ./internal/hub/ -run "TestMergeProviderRouting|TestRemoveProviderRouting" -v`
Expected: all PASS

- [ ] **Step 5: Wire merge/remove into Install and Remove**

In `internal/hub/hub.go`, in the `Install` method, after the YAML file is written to the instance directory (after the `os.WriteFile` call), add provider-specific handling:

```go
	// Provider-specific: merge routing config
	if kind == "provider" {
		if err := MergeProviderRouting(m.Home, componentName, data); err != nil {
			m.log.Warn("failed to merge provider routing", "err", err)
		}
	}
```

In the `Remove` method, before the registry removal, add:

```go
	// Provider-specific: remove routing entries
	if inst.Kind == "provider" {
		if err := RemoveProviderRouting(m.Home, inst.Name); err != nil {
			m.log.Warn("failed to remove provider routing", "err", err)
		}
	}
```

- [ ] **Step 6: Build and run all hub tests**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency && go build ./... && go test ./internal/hub/ -v`
Expected: compiles clean, all tests pass

- [ ] **Step 7: Commit**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency
git add internal/hub/provider.go internal/hub/provider_test.go internal/hub/hub.go
git commit -m "feat: provider install merges routing config, remove cleans it up"
```

---

### Task 6: Add providers and setup/config API endpoints

**Files:**
- Modify: `internal/api/routes.go` (add route registrations)
- Modify: `internal/api/handlers_routing.go` (add handler methods)

- [ ] **Step 1: Add route registrations**

In `internal/api/routes.go`, in the `RegisterRoutesWithOptions` function, in the route registration section, add:

```go
	r.Get("/api/v1/infra/providers", h.listProviders)
	r.Get("/api/v1/infra/setup/config", h.setupConfig)
```

- [ ] **Step 2: Implement listProviders handler**

Add to `internal/api/handlers_routing.go`:

```go
// listProviders returns installed + available providers with credential status.
func (h *handler) listProviders(w http.ResponseWriter, r *http.Request) {
	hubMgr := hub.NewManager(h.cfg.Home, h.log)

	// Discover available providers from hub cache
	available := hubMgr.SearchByKind("provider")

	// Get installed providers
	installed := hubMgr.ListByKind("provider")
	installedNames := make(map[string]bool)
	for _, inst := range installed {
		installedNames[inst.Name] = true
	}

	type providerResponse struct {
		Name              string      `json:"name"`
		DisplayName       string      `json:"display_name"`
		Description       string      `json:"description"`
		Category          string      `json:"category"`
		Installed         bool        `json:"installed"`
		CredentialName    string      `json:"credential_name,omitempty"`
		CredentialLabel   string      `json:"credential_label,omitempty"`
		APIKeyURL         string      `json:"api_key_url,omitempty"`
		APIBaseConfigurable bool      `json:"api_base_configurable,omitempty"`
		CredentialConfigured bool     `json:"credential_configured"`
	}

	var results []providerResponse
	for _, comp := range available {
		// Read the full provider YAML for metadata
		data, err := os.ReadFile(comp.Path)
		if err != nil {
			continue
		}
		var doc map[string]interface{}
		if yaml.Unmarshal(data, &doc) != nil {
			continue
		}

		pr := providerResponse{
			Name:        comp.Name,
			DisplayName: stringField(doc, "display_name"),
			Description: comp.Description,
			Category:    stringField(doc, "category"),
			Installed:   installedNames[comp.Name],
		}

		// Extract credential info
		if cred, ok := doc["credential"].(map[string]interface{}); ok {
			pr.CredentialName = stringField(cred, "name")
			pr.CredentialLabel = stringField(cred, "label")
			pr.APIKeyURL = stringField(cred, "api_key_url")

			// Check if credential is configured
			if pr.CredentialName != "" && h.credStore != nil {
				if _, err := h.credStore.Get(pr.CredentialName); err == nil {
					pr.CredentialConfigured = true
				}
			}
		}

		// Check api_base_configurable
		if routing, ok := doc["routing"].(map[string]interface{}); ok {
			if abc, ok := routing["api_base_configurable"].(bool); ok {
				pr.APIBaseConfigurable = abc
			}
		}

		results = append(results, pr)
	}

	writeJSON(w, 200, results)
}

func stringField(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}
```

- [ ] **Step 3: Implement setupConfig handler**

Add to `internal/api/handlers_routing.go`:

```go
// setupConfig returns the hub-sourced wizard configuration.
func (h *handler) setupConfig(w http.ResponseWriter, r *http.Request) {
	hubMgr := hub.NewManager(h.cfg.Home, h.log)

	// Find setup components in hub cache
	setupComps := hubMgr.SearchByKind("setup")

	if len(setupComps) == 0 {
		writeJSON(w, 200, map[string]interface{}{
			"capability_tiers": map[string]interface{}{},
		})
		return
	}

	// Use the first setup component (typically "default-wizard")
	data, err := os.ReadFile(setupComps[0].Path)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to read setup config"})
		return
	}

	var doc map[string]interface{}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to parse setup config"})
		return
	}

	writeJSON(w, 200, doc)
}
```

- [ ] **Step 4: Add SearchByKind and ListByKind helpers to hub manager**

Check if these already exist. If not, add to `internal/hub/hub.go`:

```go
// SearchByKind returns all cached components of the given kind.
func (m *Manager) SearchByKind(kind string) []Component {
	all := m.discover()
	var results []Component
	for _, c := range all {
		if c.Kind == kind {
			results = append(results, c)
		}
	}
	return results
}

// ListByKind returns all installed instances of the given kind.
func (m *Manager) ListByKind(kind string) []Instance {
	all := m.List()
	var results []Instance
	for _, inst := range all {
		if inst.Kind == kind {
			results = append(results, inst)
		}
	}
	return results
}
```

- [ ] **Step 5: Build to verify**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency && go build ./...`
Expected: compiles clean

- [ ] **Step 6: Commit**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency
git add internal/api/routes.go internal/api/handlers_routing.go internal/hub/hub.go
git commit -m "feat: add GET /providers and GET /setup/config API endpoints"
```

---

## Workstream 3: Frontend (agency-web)

### Task 7: Add API client methods and types

**Files:**
- Modify: `src/app/lib/api.ts`
- Modify: `src/app/types.ts`

- [ ] **Step 1: Add types**

Add to `src/app/types.ts`:

```ts
export type ProviderCategory = 'cloud' | 'local' | 'compatible';
export type TierStrategy = 'strict' | 'best_effort' | 'catch_all';

export interface Provider {
  name: string;
  display_name: string;
  description: string;
  category: ProviderCategory;
  installed: boolean;
  credential_name?: string;
  credential_label?: string;
  api_key_url?: string;
  api_base_configurable?: boolean;
  credential_configured: boolean;
}

export interface CapabilityTier {
  display_name: string;
  description: string;
  capabilities: string[];
}

export interface SetupConfig {
  capability_tiers: Record<string, CapabilityTier>;
}
```

- [ ] **Step 2: Add ComponentKind variants**

In `src/app/types.ts`, update `ComponentKind`:
```ts
export type ComponentKind = 'pack' | 'preset' | 'connector' | 'skill' | 'policy' | 'workspace' | 'provider' | 'setup';
```

- [ ] **Step 3: Add API client methods**

Add to `src/app/lib/api.ts`, in the `api` object:

```ts
  credentials: {
    list: (filters?: Record<string, string>) => {
      const params = filters ? '?' + new URLSearchParams(filters).toString() : '';
      return req<{ name: string; value: string; metadata?: Record<string, unknown> }[]>(`/credentials${params}`);
    },
    store: (name: string, value: string, opts?: { kind?: string; scope?: string; protocol?: string; service?: string }) =>
      req<OkResponse>('/credentials', { method: 'POST', body: JSON.stringify({ name, value, ...opts }) }),
    test: (name: string) =>
      req<{ ok: boolean; status?: number; message?: string; latency_ms?: number }>(`/credentials/${encodeURIComponent(name)}/test`, { method: 'POST', body: '{}' }),
    delete: (name: string) =>
      req<OkResponse>(`/credentials/${encodeURIComponent(name)}`, { method: 'DELETE' }),
  },

  providers: {
    list: () => req<Provider[]>('/providers'),
  },

  setup: {
    config: () => req<SetupConfig>('/setup/config'),
  },
```

Add the `Provider` and `SetupConfig` imports at the top of api.ts (or inline them if the file doesn't use type imports from types.ts — check the existing pattern).

- [ ] **Step 4: Commit**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web
git add src/app/lib/api.ts src/app/types.ts
git commit -m "feat: add credentials, providers, and setup API client methods"
```

---

### Task 8: Create Setup wizard shell and routing

**Files:**
- Create: `src/app/screens/Setup.tsx`
- Modify: `src/app/routes.tsx`
- Modify: `src/app/components/Layout.tsx`

- [ ] **Step 1: Read Layout.tsx to understand the redirect pattern**

Read `src/app/components/Layout.tsx` to understand how to add the first-launch redirect.

- [ ] **Step 2: Create Setup.tsx wizard shell**

Create `src/app/screens/Setup.tsx`:
```tsx
import { useState, useReducer, useCallback } from 'react';
import { useNavigate } from 'react-router';

type WizardStep = 'hub-sync' | 'welcome' | 'providers' | 'agent' | 'capabilities' | 'chat';

const STEPS: WizardStep[] = ['hub-sync', 'welcome', 'providers', 'agent', 'capabilities', 'chat'];

interface WizardState {
  step: WizardStep;
  operatorName: string;
  providers: Record<string, { configured: boolean; validated: boolean }>;
  tierStrategy: 'strict' | 'best_effort' | 'catch_all';
  agentName: string;
  agentPreset: string;
  platformExpert: boolean;
  capabilities: string[];
  hubSynced: boolean;
}

type WizardAction =
  | { type: 'SET_STEP'; step: WizardStep }
  | { type: 'SET_OPERATOR'; name: string }
  | { type: 'SET_PROVIDER'; name: string; configured: boolean; validated: boolean }
  | { type: 'SET_TIER_STRATEGY'; strategy: WizardState['tierStrategy'] }
  | { type: 'SET_AGENT'; name: string; preset: string }
  | { type: 'SET_PLATFORM_EXPERT'; enabled: boolean }
  | { type: 'SET_CAPABILITIES'; capabilities: string[] }
  | { type: 'HUB_SYNCED' };

function wizardReducer(state: WizardState, action: WizardAction): WizardState {
  switch (action.type) {
    case 'SET_STEP': return { ...state, step: action.step };
    case 'SET_OPERATOR': return { ...state, operatorName: action.name };
    case 'SET_PROVIDER':
      return { ...state, providers: { ...state.providers, [action.name]: { configured: action.configured, validated: action.validated } } };
    case 'SET_TIER_STRATEGY': return { ...state, tierStrategy: action.strategy };
    case 'SET_AGENT': return { ...state, agentName: action.name, agentPreset: action.preset };
    case 'SET_PLATFORM_EXPERT': return { ...state, platformExpert: action.enabled };
    case 'SET_CAPABILITIES': return { ...state, capabilities: action.capabilities };
    case 'HUB_SYNCED': return { ...state, hubSynced: true };
    default: return state;
  }
}

const initialState: WizardState = {
  step: 'hub-sync',
  operatorName: '',
  providers: {},
  tierStrategy: 'best_effort',
  agentName: 'henry',
  agentPreset: 'platform-expert',
  platformExpert: true,
  capabilities: [],
  hubSynced: false,
};

export function Setup() {
  const navigate = useNavigate();
  const [state, dispatch] = useReducer(wizardReducer, initialState);

  const currentIdx = STEPS.indexOf(state.step);
  const totalSteps = STEPS.length;

  const goNext = useCallback(() => {
    const nextIdx = currentIdx + 1;
    if (nextIdx < STEPS.length) {
      dispatch({ type: 'SET_STEP', step: STEPS[nextIdx] });
    }
  }, [currentIdx]);

  const goBack = useCallback(() => {
    const prevIdx = currentIdx - 1;
    if (prevIdx >= 0) {
      dispatch({ type: 'SET_STEP', step: STEPS[prevIdx] });
    }
  }, [currentIdx]);

  const finish = useCallback(() => {
    navigate('/channels', { replace: true });
  }, [navigate]);

  return (
    <div className="min-h-screen bg-background flex flex-col items-center justify-center px-4">
      {/* Progress dots */}
      <div className="flex gap-2 mb-12">
        {STEPS.map((s, i) => (
          <div
            key={s}
            className={`w-2 h-2 rounded-full transition-colors ${
              i < currentIdx ? 'bg-emerald-500' :
              i === currentIdx ? 'bg-foreground' :
              'bg-muted-foreground/30'
            }`}
          />
        ))}
      </div>

      {/* Step content */}
      <div className="w-full max-w-lg">
        {state.step === 'hub-sync' && (
          <HubSyncPlaceholder onComplete={() => { dispatch({ type: 'HUB_SYNCED' }); goNext(); }} />
        )}
        {state.step === 'welcome' && (
          <WelcomePlaceholder state={state} dispatch={dispatch} onNext={goNext} onSkip={finish} />
        )}
        {state.step === 'providers' && (
          <ProvidersPlaceholder state={state} dispatch={dispatch} onNext={goNext} onBack={goBack} />
        )}
        {state.step === 'agent' && (
          <AgentPlaceholder state={state} dispatch={dispatch} onNext={goNext} onBack={goBack} />
        )}
        {state.step === 'capabilities' && (
          <CapabilitiesPlaceholder state={state} dispatch={dispatch} onNext={goNext} onBack={goBack} />
        )}
        {state.step === 'chat' && (
          <ChatPlaceholder state={state} onFinish={finish} onBack={goBack} />
        )}
      </div>
    </div>
  );
}

// Temporary placeholders — replaced in subsequent tasks
function HubSyncPlaceholder({ onComplete }: { onComplete: () => void }) {
  return (
    <div className="text-center space-y-4">
      <h2 className="text-2xl font-semibold text-foreground">Preparing your platform...</h2>
      <p className="text-muted-foreground">Syncing hub components</p>
      <button onClick={onComplete} className="text-sm text-muted-foreground underline">Skip (dev)</button>
    </div>
  );
}

function WelcomePlaceholder({ onNext, onSkip }: { state: WizardState; dispatch: React.Dispatch<WizardAction>; onNext: () => void; onSkip: () => void }) {
  return (
    <div className="text-center space-y-4">
      <h2 className="text-2xl font-semibold text-foreground">Welcome</h2>
      <p className="text-muted-foreground">Step placeholder</p>
      <div className="flex gap-4 justify-center">
        <button onClick={onNext} className="text-sm text-foreground underline">Next</button>
        <button onClick={onSkip} className="text-sm text-muted-foreground underline">Skip Setup</button>
      </div>
    </div>
  );
}

function ProvidersPlaceholder({ onNext, onBack }: { state: WizardState; dispatch: React.Dispatch<WizardAction>; onNext: () => void; onBack: () => void }) {
  return (
    <div className="text-center space-y-4">
      <h2 className="text-2xl font-semibold text-foreground">LLM Providers</h2>
      <p className="text-muted-foreground">Step placeholder</p>
      <div className="flex gap-4 justify-center">
        <button onClick={onBack} className="text-sm text-muted-foreground underline">Back</button>
        <button onClick={onNext} className="text-sm text-foreground underline">Next</button>
      </div>
    </div>
  );
}

function AgentPlaceholder({ onNext, onBack }: { state: WizardState; dispatch: React.Dispatch<WizardAction>; onNext: () => void; onBack: () => void }) {
  return (
    <div className="text-center space-y-4">
      <h2 className="text-2xl font-semibold text-foreground">Your First Agent</h2>
      <p className="text-muted-foreground">Step placeholder</p>
      <div className="flex gap-4 justify-center">
        <button onClick={onBack} className="text-sm text-muted-foreground underline">Back</button>
        <button onClick={onNext} className="text-sm text-foreground underline">Next</button>
      </div>
    </div>
  );
}

function CapabilitiesPlaceholder({ onNext, onBack }: { state: WizardState; dispatch: React.Dispatch<WizardAction>; onNext: () => void; onBack: () => void }) {
  return (
    <div className="text-center space-y-4">
      <h2 className="text-2xl font-semibold text-foreground">Capabilities</h2>
      <p className="text-muted-foreground">Step placeholder</p>
      <div className="flex gap-4 justify-center">
        <button onClick={onBack} className="text-sm text-muted-foreground underline">Back</button>
        <button onClick={onNext} className="text-sm text-foreground underline">Next</button>
      </div>
    </div>
  );
}

function ChatPlaceholder({ onFinish, onBack }: { state: WizardState; onFinish: () => void; onBack: () => void }) {
  return (
    <div className="text-center space-y-4">
      <h2 className="text-2xl font-semibold text-foreground">Chat with your agent</h2>
      <p className="text-muted-foreground">Step placeholder</p>
      <div className="flex gap-4 justify-center">
        <button onClick={onBack} className="text-sm text-muted-foreground underline">Back</button>
        <button onClick={onFinish} className="text-sm text-foreground underline">Finish Setup</button>
      </div>
    </div>
  );
}
```

- [ ] **Step 3: Add route**

In `src/app/routes.tsx`, add the import and route:

```tsx
import { Setup } from './screens/Setup';
```

Add before the Layout route (setup has its own layout, no sidebar):
```tsx
  { path: '/setup', Component: Setup, ErrorBoundary: RouteErrorBoundary },
```

- [ ] **Step 4: Add first-launch redirect**

In `src/app/components/Layout.tsx`, add a check on mount. Read the file first to find the right insertion point, then add:

```tsx
// In the Layout component, add state and effect:
const [setupRequired, setSetupRequired] = useState<boolean | null>(null);
const navigate = useNavigate();

useEffect(() => {
  api.routing.config().then((cfg: any) => {
    if (cfg.configured === false) {
      navigate('/setup', { replace: true });
    } else {
      setSetupRequired(false);
    }
  }).catch(() => {
    setSetupRequired(false); // can't check — proceed normally
  });
}, []);

// Early return while checking
if (setupRequired === null) {
  return <div className="min-h-screen bg-background" />;
}
```

Note: Check if `api.routing.config()` exists. If not, use `api.admin.doctor()` or add it. The existing `routingConfig` handler returns a `configured` field based on whether providers exist.

- [ ] **Step 5: Verify dev server runs**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web && npx agency-web dev`
Navigate to `https://localhost:8280/setup` — should see the placeholder wizard.
Expected: wizard shell renders with progress dots and placeholder content

- [ ] **Step 6: Commit**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web
git add src/app/screens/Setup.tsx src/app/routes.tsx src/app/components/Layout.tsx
git commit -m "feat: setup wizard shell with routing and first-launch redirect"
```

---

### Task 9: Implement HubSyncStep

**Files:**
- Create: `src/app/screens/setup/HubSyncStep.tsx`
- Modify: `src/app/screens/Setup.tsx`

- [ ] **Step 1: Create HubSyncStep component**

Create `src/app/screens/setup/HubSyncStep.tsx`:
```tsx
import { useState, useEffect } from 'react';
import { RefreshCw } from 'lucide-react';
import { api } from '../../lib/api';
import { Button } from '../../components/ui/button';

interface HubSyncStepProps {
  onComplete: () => void;
}

export function HubSyncStep({ onComplete }: HubSyncStepProps) {
  const [status, setStatus] = useState<'syncing' | 'error' | 'done'>('syncing');
  const [error, setError] = useState('');
  const [phase, setPhase] = useState<'update' | 'upgrade'>('update');

  const runSync = async () => {
    setStatus('syncing');
    setError('');
    try {
      setPhase('update');
      await api.hub.update();
      setPhase('upgrade');
      await api.hub.upgrade();
      setStatus('done');
      // Brief pause so user sees success before transition
      setTimeout(onComplete, 600);
    } catch (e: any) {
      setStatus('error');
      setError(e.message || 'Failed to sync hub');
    }
  };

  useEffect(() => {
    runSync();
  }, []);

  return (
    <div className="text-center space-y-6">
      <div className="space-y-2">
        <h2 className="text-2xl font-semibold text-foreground">
          {status === 'error' ? 'Hub sync failed' : 'Preparing your platform...'}
        </h2>
        <p className="text-muted-foreground text-sm">
          {status === 'syncing' && phase === 'update' && 'Updating hub sources...'}
          {status === 'syncing' && phase === 'upgrade' && 'Installing components...'}
          {status === 'done' && 'Ready to go'}
          {status === 'error' && 'Could not reach the hub registry'}
        </p>
      </div>

      {status === 'syncing' && (
        <RefreshCw className="w-6 h-6 text-muted-foreground animate-spin mx-auto" />
      )}

      {status === 'error' && (
        <div className="space-y-4">
          <p className="text-sm text-red-400 bg-red-950/30 border border-red-900/50 rounded px-4 py-2">
            {error}
          </p>
          <div className="flex gap-3 justify-center">
            <Button variant="outline" size="sm" onClick={runSync}>
              <RefreshCw className="w-3 h-3 mr-1.5" />
              Retry
            </Button>
            <Button variant="ghost" size="sm" onClick={onComplete} className="text-muted-foreground">
              Continue anyway
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}
```

- [ ] **Step 2: Replace placeholder in Setup.tsx**

In `src/app/screens/Setup.tsx`, replace the `HubSyncPlaceholder` usage:

Add import:
```tsx
import { HubSyncStep } from './setup/HubSyncStep';
```

Replace the hub-sync step rendering:
```tsx
{state.step === 'hub-sync' && (
  <HubSyncStep onComplete={() => { dispatch({ type: 'HUB_SYNCED' }); goNext(); }} />
)}
```

Remove the `HubSyncPlaceholder` function.

- [ ] **Step 3: Commit**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web
git add src/app/screens/setup/HubSyncStep.tsx src/app/screens/Setup.tsx
git commit -m "feat: implement HubSyncStep — hub update and upgrade on first launch"
```

---

### Task 10: Implement WelcomeStep

**Files:**
- Create: `src/app/screens/setup/WelcomeStep.tsx`
- Modify: `src/app/screens/Setup.tsx`

- [ ] **Step 1: Create WelcomeStep component**

Create `src/app/screens/setup/WelcomeStep.tsx`:
```tsx
import { useState } from 'react';
import { api } from '../../lib/api';
import { Input } from '../../components/ui/input';
import { Button } from '../../components/ui/button';

interface WelcomeStepProps {
  operatorName: string;
  onUpdate: (name: string) => void;
  onNext: () => void;
  onSkip: () => void;
  isReSetup: boolean;
}

const NAME_PATTERN = /^[a-zA-Z0-9][a-zA-Z0-9-]*$/;

export function WelcomeStep({ operatorName, onUpdate, onNext, onSkip, isReSetup }: WelcomeStepProps) {
  const [name, setName] = useState(operatorName);
  const [error, setError] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const isValid = name.length >= 1 && name.length <= 64 && NAME_PATTERN.test(name);

  const handleSubmit = async () => {
    if (!isValid) return;
    setSubmitting(true);
    setError('');
    try {
      await api.init({ operator: name, force: isReSetup });
      onUpdate(name);
      onNext();
    } catch (e: any) {
      setError(e.message || 'Initialization failed');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="text-center space-y-8">
      <div className="space-y-3">
        <h2 className="text-2xl font-semibold text-foreground">
          {isReSetup ? 'Re-configure Agency' : 'Welcome to Agency'}
        </h2>
        <p className="text-muted-foreground text-sm max-w-sm mx-auto">
          {isReSetup
            ? "Let's walk through your configuration. You can update anything or skip ahead."
            : "Let's get your platform set up. This will take a few minutes."}
        </p>
      </div>

      <div className="space-y-3 max-w-xs mx-auto">
        <label className="text-sm text-muted-foreground text-left block">Your name</label>
        <Input
          value={name}
          onChange={(e) => setName(e.target.value.replace(/[^a-zA-Z0-9-]/g, ''))}
          placeholder="operator"
          maxLength={64}
          className="text-center bg-card border-border"
          onKeyDown={(e) => e.key === 'Enter' && isValid && handleSubmit()}
        />
        {error && <p className="text-xs text-red-400">{error}</p>}
      </div>

      <div className="space-y-3">
        <Button
          onClick={handleSubmit}
          disabled={!isValid || submitting}
          className="w-48"
        >
          {submitting ? 'Initializing...' : 'Continue'}
        </Button>

        {isReSetup && (
          <div>
            <button onClick={onSkip} className="text-xs text-muted-foreground hover:text-foreground transition-colors">
              Skip setup
            </button>
          </div>
        )}
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Add init method to api.ts if missing**

Check if `api.init` exists. If not, add to `src/app/lib/api.ts`:

```ts
  init: (opts: { operator: string; force?: boolean; anthropic_api_key?: string; openai_api_key?: string }) =>
    req<{ status: string; home: string }>('/init', { method: 'POST', body: JSON.stringify(opts) }),
```

- [ ] **Step 3: Replace placeholder in Setup.tsx**

Add import:
```tsx
import { WelcomeStep } from './setup/WelcomeStep';
```

Replace the welcome step rendering. The `isReSetup` prop is true when providers are already configured (detect via initial routing config check).

Remove the `WelcomePlaceholder` function.

- [ ] **Step 4: Commit**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web
git add src/app/screens/setup/WelcomeStep.tsx src/app/screens/Setup.tsx src/app/lib/api.ts
git commit -m "feat: implement WelcomeStep — operator name and platform init"
```

---

### Task 11: Implement ProvidersStep

**Files:**
- Create: `src/app/screens/setup/ProvidersStep.tsx`
- Modify: `src/app/screens/Setup.tsx`

- [ ] **Step 1: Create ProvidersStep component**

Create `src/app/screens/setup/ProvidersStep.tsx`:
```tsx
import { useState, useEffect } from 'react';
import { Check, ExternalLink, Loader2, X, ChevronDown } from 'lucide-react';
import { api } from '../../lib/api';
import { Provider, TierStrategy } from '../../types';
import { Input } from '../../components/ui/input';
import { Button } from '../../components/ui/button';

interface ProvidersStepProps {
  providers: Record<string, { configured: boolean; validated: boolean }>;
  tierStrategy: TierStrategy;
  onProviderUpdate: (name: string, configured: boolean, validated: boolean) => void;
  onTierStrategyUpdate: (strategy: TierStrategy) => void;
  onNext: () => void;
  onBack: () => void;
}

export function ProvidersStep({
  providers: configuredProviders,
  tierStrategy,
  onProviderUpdate,
  onTierStrategyUpdate,
  onNext,
  onBack,
}: ProvidersStepProps) {
  const [available, setAvailable] = useState<Provider[]>([]);
  const [loading, setLoading] = useState(true);
  const [expanded, setExpanded] = useState<string | null>(null);
  const [keyInputs, setKeyInputs] = useState<Record<string, string>>({});
  const [baseInputs, setBaseInputs] = useState<Record<string, string>>({});
  const [testing, setTesting] = useState<string | null>(null);
  const [testError, setTestError] = useState<Record<string, string>>({});

  const hasValidProvider = Object.values(configuredProviders).some((p) => p.validated);

  useEffect(() => {
    api.providers.list().then((data) => {
      setAvailable(data || []);
      setLoading(false);
    }).catch(() => setLoading(false));
  }, []);

  const handleVerify = async (provider: Provider) => {
    const credName = provider.credential_name;
    const key = keyInputs[provider.name] || '';

    if (credName && !key && !provider.credential_configured) return;

    setTesting(provider.name);
    setTestError((prev) => ({ ...prev, [provider.name]: '' }));

    try {
      // Store credential if provided
      if (credName && key) {
        await api.credentials.store(credName, key, { kind: 'provider', protocol: 'api-key' });
      }

      // Test credential
      if (credName) {
        const result = await api.credentials.test(credName);
        if (!result.ok) {
          setTestError((prev) => ({ ...prev, [provider.name]: result.message || 'Verification failed' }));
          onProviderUpdate(provider.name, true, false);
          return;
        }
      }

      // Install provider if not already
      if (!provider.installed) {
        await api.hub.install(provider.name, 'provider');
      }

      onProviderUpdate(provider.name, true, true);
      setExpanded(null);
    } catch (e: any) {
      setTestError((prev) => ({ ...prev, [provider.name]: e.message || 'Failed' }));
      onProviderUpdate(provider.name, true, false);
    } finally {
      setTesting(null);
    }
  };

  const grouped = {
    cloud: available.filter((p) => p.category === 'cloud'),
    local: available.filter((p) => p.category === 'local'),
    compatible: available.filter((p) => p.category === 'compatible'),
  };

  const categoryLabels: Record<string, string> = {
    cloud: 'Cloud Providers',
    local: 'Local',
    compatible: 'OpenAI-Compatible',
  };

  if (loading) {
    return (
      <div className="text-center space-y-4">
        <h2 className="text-2xl font-semibold text-foreground">LLM Providers</h2>
        <Loader2 className="w-5 h-5 animate-spin mx-auto text-muted-foreground" />
      </div>
    );
  }

  return (
    <div className="space-y-8">
      <div className="text-center space-y-2">
        <h2 className="text-2xl font-semibold text-foreground">LLM Providers</h2>
        <p className="text-muted-foreground text-sm">
          Connect at least one provider to power your agents.
        </p>
      </div>

      <div className="space-y-6">
        {(['cloud', 'local', 'compatible'] as const).map((cat) => {
          const items = grouped[cat];
          if (items.length === 0) return null;
          return (
            <div key={cat} className="space-y-2">
              <h3 className="text-xs font-medium text-muted-foreground uppercase tracking-wide">
                {categoryLabels[cat]}
              </h3>
              <div className="space-y-2">
                {items.map((provider) => {
                  const status = configuredProviders[provider.name];
                  const isExpanded = expanded === provider.name;
                  const isValidated = status?.validated || provider.credential_configured;

                  return (
                    <div key={provider.name} className="border border-border rounded-lg bg-card overflow-hidden">
                      <button
                        className="w-full flex items-center justify-between px-4 py-3 text-left hover:bg-secondary/30 transition-colors"
                        onClick={() => setExpanded(isExpanded ? null : provider.name)}
                      >
                        <div className="flex items-center gap-3">
                          <span className="text-sm font-medium text-foreground">{provider.display_name}</span>
                          {isValidated && <Check className="w-4 h-4 text-emerald-500" />}
                        </div>
                        <ChevronDown className={`w-4 h-4 text-muted-foreground transition-transform ${isExpanded ? 'rotate-180' : ''}`} />
                      </button>

                      {isExpanded && (
                        <div className="px-4 pb-4 space-y-3 border-t border-border pt-3">
                          <p className="text-xs text-muted-foreground">{provider.description}</p>

                          {provider.credential_name && (
                            <div className="space-y-1.5">
                              <label className="text-xs text-muted-foreground">
                                {provider.credential_label || 'API Key'}
                              </label>
                              <Input
                                type="password"
                                value={keyInputs[provider.name] || ''}
                                onChange={(e) => setKeyInputs((prev) => ({ ...prev, [provider.name]: e.target.value }))}
                                placeholder={isValidated ? '••••••••' : 'Enter your API key'}
                                className="text-sm bg-background"
                              />
                              {provider.api_key_url && (
                                <a
                                  href={provider.api_key_url}
                                  target="_blank"
                                  rel="noopener noreferrer"
                                  className="text-xs text-blue-400 hover:text-blue-300 inline-flex items-center gap-1"
                                >
                                  Get an API key <ExternalLink className="w-3 h-3" />
                                </a>
                              )}
                            </div>
                          )}

                          {provider.api_base_configurable && (
                            <div className="space-y-1.5">
                              <label className="text-xs text-muted-foreground">API Base URL</label>
                              <Input
                                value={baseInputs[provider.name] || ''}
                                onChange={(e) => setBaseInputs((prev) => ({ ...prev, [provider.name]: e.target.value }))}
                                placeholder="http://localhost:11434/v1"
                                className="text-sm bg-background"
                              />
                            </div>
                          )}

                          {testError[provider.name] && (
                            <p className="text-xs text-red-400 flex items-center gap-1">
                              <X className="w-3 h-3" /> {testError[provider.name]}
                            </p>
                          )}

                          <Button
                            size="sm"
                            onClick={() => handleVerify(provider)}
                            disabled={testing === provider.name}
                            className="w-full"
                          >
                            {testing === provider.name ? (
                              <><Loader2 className="w-3 h-3 mr-1.5 animate-spin" /> Verifying...</>
                            ) : isValidated ? 'Reconfigure' : 'Verify & Save'}
                          </Button>
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
            </div>
          );
        })}
      </div>

      {/* Tier strategy — shown after at least one provider is configured */}
      {hasValidProvider && (
        <div className="space-y-3 pt-4 border-t border-border">
          <h3 className="text-sm font-medium text-foreground">Model Routing Strategy</h3>
          <p className="text-xs text-muted-foreground">How should the platform handle model tier requests?</p>
          <div className="space-y-2">
            {([
              { value: 'best_effort' as const, label: 'Best Effort', desc: 'Use the nearest available model when the requested tier is unmapped.' },
              { value: 'strict' as const, label: 'Strict', desc: 'Only use exact tier matches. Fail if no model is mapped to the requested tier.' },
              { value: 'catch_all' as const, label: 'Catch-all', desc: 'Route all tiers to whatever model is available. Best for single-model setups.' },
            ]).map((opt) => (
              <button
                key={opt.value}
                className={`w-full text-left px-3 py-2.5 rounded border transition-colors ${
                  tierStrategy === opt.value
                    ? 'border-foreground/30 bg-secondary/50'
                    : 'border-border hover:border-border/80'
                }`}
                onClick={() => onTierStrategyUpdate(opt.value)}
              >
                <div className="text-sm font-medium text-foreground">{opt.label}</div>
                <div className="text-xs text-muted-foreground">{opt.desc}</div>
              </button>
            ))}
          </div>
        </div>
      )}

      {/* Navigation */}
      <div className="flex items-center justify-between pt-4">
        <button onClick={onBack} className="text-sm text-muted-foreground hover:text-foreground transition-colors">
          Back
        </button>
        <Button onClick={onNext} disabled={!hasValidProvider}>
          Continue
        </Button>
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Replace placeholder in Setup.tsx**

Add import and replace providers step rendering. Wire up the dispatch actions:
```tsx
import { ProvidersStep } from './setup/ProvidersStep';
```

```tsx
{state.step === 'providers' && (
  <ProvidersStep
    providers={state.providers}
    tierStrategy={state.tierStrategy}
    onProviderUpdate={(name, configured, validated) =>
      dispatch({ type: 'SET_PROVIDER', name, configured, validated })
    }
    onTierStrategyUpdate={(strategy) =>
      dispatch({ type: 'SET_TIER_STRATEGY', strategy })
    }
    onNext={goNext}
    onBack={goBack}
  />
)}
```

Remove the `ProvidersPlaceholder` function.

- [ ] **Step 3: Commit**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web
git add src/app/screens/setup/ProvidersStep.tsx src/app/screens/Setup.tsx
git commit -m "feat: implement ProvidersStep — provider catalog, credential validation, tier strategy"
```

---

### Task 12: Implement AgentStep

**Files:**
- Create: `src/app/screens/setup/AgentStep.tsx`
- Create: `src/app/data/agent-names.ts`
- Modify: `src/app/screens/Setup.tsx`

- [ ] **Step 1: Create name generator data**

Create `src/app/data/agent-names.ts`:
```ts
export const AGENT_NAMES = [
  'ada', 'archie', 'atlas', 'bard', 'beacon', 'bolt', 'cipher', 'coral',
  'dash', 'echo', 'ember', 'felix', 'flint', 'forge', 'ghost', 'halo',
  'haven', 'hex', 'iris', 'juno', 'kite', 'lark', 'luna', 'mako',
  'maven', 'neo', 'nexus', 'nyx', 'onyx', 'orbit', 'pace', 'pax',
  'pixel', 'prism', 'quest', 'radar', 'raven', 'reef', 'sage', 'scout',
  'sigma', 'spark', 'storm', 'terra', 'trace', 'vale', 'vex', 'wren',
  'zara', 'zen',
];

export function randomAgentName(): string {
  return AGENT_NAMES[Math.floor(Math.random() * AGENT_NAMES.length)];
}
```

- [ ] **Step 2: Create AgentStep component**

Create `src/app/screens/setup/AgentStep.tsx`:
```tsx
import { useState, useEffect } from 'react';
import { Shuffle, Loader2 } from 'lucide-react';
import { api } from '../../lib/api';
import { Input } from '../../components/ui/input';
import { Button } from '../../components/ui/button';
import { randomAgentName } from '../../data/agent-names';

interface AgentStepProps {
  agentName: string;
  agentPreset: string;
  platformExpert: boolean;
  onUpdate: (name: string, preset: string) => void;
  onPlatformExpertToggle: (enabled: boolean) => void;
  onNext: () => void;
  onBack: () => void;
}

const NAME_PATTERN = /^[a-z0-9][a-z0-9-]*[a-z0-9]$/;
const RESERVED = new Set(['infra-egress', 'agency', 'enforcer', 'gateway', 'workspace']);

interface Preset {
  name: string;
  description?: string;
  type?: string;
  source?: string;
}

export function AgentStep({
  agentName,
  agentPreset,
  platformExpert,
  onUpdate,
  onPlatformExpertToggle,
  onNext,
  onBack,
}: AgentStepProps) {
  const [name, setName] = useState(agentName);
  const [preset, setPreset] = useState(agentPreset);
  const [expert, setExpert] = useState(platformExpert);
  const [presets, setPresets] = useState<Preset[]>([]);
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    api.presets.list().then((data: Preset[]) => {
      setPresets(data || []);
    }).catch(() => {});
  }, []);

  const sanitize = (input: string) => {
    return input.toLowerCase().replace(/[^a-z0-9-]/g, '').replace(/--+/g, '-');
  };

  const isValid = name.length >= 2 && name.length <= 64 && NAME_PATTERN.test(name) && !RESERVED.has(name);

  const handleCreate = async () => {
    if (!isValid) return;
    setCreating(true);
    setError('');
    try {
      const selectedPreset = expert ? 'platform-expert' : preset;
      await api.agents.create(name, selectedPreset, 'assisted');
      await api.agents.start(name);
      onUpdate(name, selectedPreset);
      onNext();
    } catch (e: any) {
      if (e.message?.includes('Docker') || e.message?.includes('docker')) {
        setError('Docker is required to run agents. Please start Docker and try again.');
      } else {
        setError(e.message || 'Failed to create agent');
      }
    } finally {
      setCreating(false);
    }
  };

  const handleShuffle = () => {
    let newName = randomAgentName();
    while (newName === name) {
      newName = randomAgentName();
    }
    setName(newName);
  };

  return (
    <div className="space-y-8">
      <div className="text-center space-y-2">
        <h2 className="text-2xl font-semibold text-foreground">Your First Agent</h2>
        <p className="text-muted-foreground text-sm">
          Give your agent a name and it'll be ready to chat.
        </p>
      </div>

      <div className="space-y-5 max-w-sm mx-auto">
        {/* Agent name */}
        <div className="space-y-1.5">
          <label className="text-xs text-muted-foreground">Agent name</label>
          <div className="flex gap-2">
            <Input
              value={name}
              onChange={(e) => setName(sanitize(e.target.value))}
              placeholder="henry"
              className="flex-1 bg-card border-border"
              maxLength={64}
            />
            <Button variant="outline" size="icon" onClick={handleShuffle} title="Random name">
              <Shuffle className="w-4 h-4" />
            </Button>
          </div>
        </div>

        {/* Platform expert toggle */}
        <div
          className={`flex items-start gap-3 px-3 py-3 rounded border cursor-pointer transition-colors ${
            expert ? 'border-foreground/30 bg-secondary/50' : 'border-border hover:border-border/80'
          }`}
          onClick={() => { setExpert(!expert); onPlatformExpertToggle(!expert); }}
        >
          <div className={`mt-0.5 w-4 h-4 rounded border flex items-center justify-center flex-shrink-0 ${
            expert ? 'bg-foreground border-foreground' : 'border-muted-foreground/50'
          }`}>
            {expert && <span className="text-background text-xs">✓</span>}
          </div>
          <div>
            <div className="text-sm font-medium text-foreground">Platform Expert</div>
            <div className="text-xs text-muted-foreground">
              Your agent will know how Agency works and can help you learn the platform. Recommended for first-time setup.
            </div>
          </div>
        </div>

        {/* Preset selector (shown when expert is off) */}
        {!expert && presets.length > 0 && (
          <div className="space-y-1.5">
            <label className="text-xs text-muted-foreground">Preset</label>
            <select
              value={preset}
              onChange={(e) => setPreset(e.target.value)}
              className="w-full h-9 rounded border border-border bg-card px-3 text-sm text-foreground"
            >
              {presets.map((p) => (
                <option key={p.name} value={p.name}>{p.name}{p.description ? ` — ${p.description}` : ''}</option>
              ))}
            </select>
          </div>
        )}

        {error && (
          <p className="text-xs text-red-400 bg-red-950/30 border border-red-900/50 rounded px-3 py-2">
            {error}
          </p>
        )}
      </div>

      {/* Navigation */}
      <div className="flex items-center justify-between pt-4">
        <button onClick={onBack} className="text-sm text-muted-foreground hover:text-foreground transition-colors">
          Back
        </button>
        <div className="flex items-center gap-3">
          <button
            onClick={() => { onUpdate(name, preset); onNext(); }}
            className="text-xs text-muted-foreground hover:text-foreground transition-colors"
          >
            Skip
          </button>
          <Button onClick={handleCreate} disabled={!isValid || creating}>
            {creating ? <><Loader2 className="w-3 h-3 mr-1.5 animate-spin" /> Creating...</> : 'Create & Start'}
          </Button>
        </div>
      </div>
    </div>
  );
}
```

- [ ] **Step 3: Replace placeholder in Setup.tsx**

Add imports, wire up the agent step, remove `AgentPlaceholder`.

- [ ] **Step 4: Commit**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web
git add src/app/screens/setup/AgentStep.tsx src/app/data/agent-names.ts src/app/screens/Setup.tsx
git commit -m "feat: implement AgentStep — name generator, preset picker, platform expert toggle"
```

---

### Task 13: Implement CapabilitiesStep

**Files:**
- Create: `src/app/screens/setup/CapabilitiesStep.tsx`
- Modify: `src/app/screens/Setup.tsx`

- [ ] **Step 1: Create CapabilitiesStep component**

Create `src/app/screens/setup/CapabilitiesStep.tsx`:
```tsx
import { useState, useEffect } from 'react';
import { Check, Loader2 } from 'lucide-react';
import { api } from '../../lib/api';
import { Capability, SetupConfig } from '../../types';
import { Input } from '../../components/ui/input';
import { Button } from '../../components/ui/button';

interface CapabilitiesStepProps {
  capabilities: string[];
  onUpdate: (capabilities: string[]) => void;
  onNext: () => void;
  onBack: () => void;
}

type TierChoice = 'minimal' | 'standard' | 'custom';

export function CapabilitiesStep({ capabilities, onUpdate, onNext, onBack }: CapabilitiesStepProps) {
  const [available, setAvailable] = useState<Capability[]>([]);
  const [setupConfig, setSetupConfig] = useState<SetupConfig | null>(null);
  const [tier, setTier] = useState<TierChoice>('standard');
  const [selected, setSelected] = useState<Set<string>>(new Set(capabilities));
  const [loading, setLoading] = useState(true);
  const [applying, setApplying] = useState(false);
  const [error, setError] = useState('');
  const [credPrompt, setCredPrompt] = useState<{ name: string; capName: string } | null>(null);
  const [credValue, setCredValue] = useState('');

  useEffect(() => {
    Promise.all([
      api.capabilities.list(),
      api.setup.config(),
    ]).then(([caps, config]) => {
      const mapped: Capability[] = (caps || []).map((c: any) => ({
        id: c.name,
        name: c.name,
        kind: c.kind || 'service',
        state: c.state || 'disabled',
        description: c.description || '',
      }));
      setAvailable(mapped);
      setSetupConfig(config);

      // Pre-select standard tier capabilities
      const standardCaps = config?.capability_tiers?.standard?.capabilities || [];
      setSelected(new Set(standardCaps));
      setLoading(false);
    }).catch(() => setLoading(false));
  }, []);

  const handleTierChange = (newTier: TierChoice) => {
    setTier(newTier);
    if (newTier === 'minimal') {
      setSelected(new Set());
    } else if (newTier === 'standard') {
      const caps = setupConfig?.capability_tiers?.standard?.capabilities || [];
      setSelected(new Set(caps));
    }
    // 'custom' keeps current selection
  };

  const toggleCap = (name: string) => {
    setTier('custom');
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(name)) {
        next.delete(name);
      } else {
        next.add(name);
      }
      return next;
    });
  };

  const handleApply = async () => {
    setApplying(true);
    setError('');
    try {
      // Enable selected, disable unselected
      for (const cap of available) {
        const shouldEnable = selected.has(cap.name);
        const isEnabled = cap.state === 'enabled';
        if (shouldEnable && !isEnabled) {
          await api.capabilities.enable(cap.name);
        } else if (!shouldEnable && isEnabled) {
          await api.capabilities.disable(cap.name);
        }
      }
      onUpdate(Array.from(selected));
      onNext();
    } catch (e: any) {
      setError(e.message || 'Failed to apply capabilities');
    } finally {
      setApplying(false);
    }
  };

  const tierCards = [
    { key: 'minimal' as const, label: setupConfig?.capability_tiers?.minimal?.display_name || 'Minimal', desc: setupConfig?.capability_tiers?.minimal?.description || 'LLM access only.' },
    { key: 'standard' as const, label: setupConfig?.capability_tiers?.standard?.display_name || 'Standard', desc: setupConfig?.capability_tiers?.standard?.description || 'Recommended capabilities.' },
    { key: 'custom' as const, label: 'Custom', desc: 'Start from Standard and pick what you want.' },
  ];

  if (loading) {
    return (
      <div className="text-center space-y-4">
        <h2 className="text-2xl font-semibold text-foreground">Capabilities</h2>
        <Loader2 className="w-5 h-5 animate-spin mx-auto text-muted-foreground" />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div className="text-center space-y-2">
        <h2 className="text-2xl font-semibold text-foreground">What should your agents be able to do?</h2>
        <p className="text-muted-foreground text-sm">
          You can always change these later in Admin.
        </p>
      </div>

      {/* Tier cards */}
      <div className="grid grid-cols-3 gap-2">
        {tierCards.map((tc) => (
          <button
            key={tc.key}
            className={`text-left px-3 py-3 rounded border transition-colors ${
              tier === tc.key ? 'border-foreground/30 bg-secondary/50' : 'border-border hover:border-border/80'
            }`}
            onClick={() => handleTierChange(tc.key)}
          >
            <div className="text-sm font-medium text-foreground">{tc.label}</div>
            <div className="text-xs text-muted-foreground mt-0.5">{tc.desc}</div>
          </button>
        ))}
      </div>

      {/* Capability list (shown for standard and custom) */}
      {tier !== 'minimal' && available.length > 0 && (
        <div className="space-y-1 max-h-64 overflow-y-auto">
          {available.map((cap) => (
            <button
              key={cap.name}
              className={`w-full flex items-center gap-3 px-3 py-2 rounded text-left transition-colors ${
                selected.has(cap.name) ? 'bg-secondary/50' : 'hover:bg-secondary/20'
              }`}
              onClick={() => toggleCap(cap.name)}
            >
              <div className={`w-4 h-4 rounded border flex items-center justify-center flex-shrink-0 ${
                selected.has(cap.name) ? 'bg-emerald-600 border-emerald-600' : 'border-muted-foreground/40'
              }`}>
                {selected.has(cap.name) && <Check className="w-3 h-3 text-white" />}
              </div>
              <div className="flex-1 min-w-0">
                <div className="text-sm text-foreground">{cap.name}</div>
                {cap.description && <div className="text-xs text-muted-foreground truncate">{cap.description}</div>}
              </div>
              <span className="text-xs text-muted-foreground/60">{cap.kind}</span>
            </button>
          ))}
        </div>
      )}

      {error && <p className="text-xs text-red-400">{error}</p>}

      {/* Credential prompt dialog */}
      {credPrompt && (
        <div className="bg-card border border-border rounded p-3 space-y-2">
          <p className="text-xs text-muted-foreground">{credPrompt.capName} requires an API key</p>
          <Input
            type="password"
            value={credValue}
            onChange={(e) => setCredValue(e.target.value)}
            placeholder="API key"
            className="text-sm"
          />
          <div className="flex gap-2">
            <Button size="sm" onClick={async () => {
              await api.credentials.store(credPrompt.name, credValue, { kind: 'service' });
              await api.capabilities.enable(credPrompt.capName, credValue);
              setCredPrompt(null);
              setCredValue('');
            }}>Save</Button>
            <Button size="sm" variant="ghost" onClick={() => { setCredPrompt(null); toggleCap(credPrompt.capName); }}>Cancel</Button>
          </div>
        </div>
      )}

      {/* Navigation */}
      <div className="flex items-center justify-between pt-4">
        <button onClick={onBack} className="text-sm text-muted-foreground hover:text-foreground transition-colors">
          Back
        </button>
        <Button onClick={handleApply} disabled={applying}>
          {applying ? <><Loader2 className="w-3 h-3 mr-1.5 animate-spin" /> Applying...</> : 'Continue'}
        </Button>
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Replace placeholder in Setup.tsx**

Add import, wire up, remove `CapabilitiesPlaceholder`.

- [ ] **Step 3: Commit**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web
git add src/app/screens/setup/CapabilitiesStep.tsx src/app/screens/Setup.tsx
git commit -m "feat: implement CapabilitiesStep — tier presets and toggleable capability list"
```

---

### Task 14: Implement ChatStep

**Files:**
- Create: `src/app/screens/setup/ChatStep.tsx`
- Modify: `src/app/screens/Setup.tsx`

- [ ] **Step 1: Read existing chat components to understand reuse**

Read `src/app/components/chat/MessageArea.tsx`, `src/app/components/chat/ComposeBar.tsx`, and `src/app/hooks/useChannelMessages.ts` to understand how to embed the chat.

- [ ] **Step 2: Create ChatStep component**

Create `src/app/screens/setup/ChatStep.tsx`:

This step needs to:
1. Find or create a DM channel with the agent created in Step 3
2. Render the existing `MessageArea` + `ComposeBar` (or a simplified version)
3. Pre-fill a suggested first message
4. Show "Finish Setup" and "Skip" buttons

The exact implementation depends on how channels/DMs are created in the existing codebase. Read the channel creation flow first, then implement. The key principle: reuse existing chat components, don't build new ones.

```tsx
import { useState, useEffect } from 'react';
import { Loader2 } from 'lucide-react';
import { api } from '../../lib/api';
import { Button } from '../../components/ui/button';
import { MessageArea } from '../../components/chat/MessageArea';
import { ComposeBar } from '../../components/chat/ComposeBar';
import { useChannelMessages } from '../../hooks/useChannelMessages';
import { useChannelSocket } from '../../hooks/useChannelSocket';

interface ChatStepProps {
  agentName: string;
  onFinish: () => void;
  onBack: () => void;
}

export function ChatStep({ agentName, onFinish, onBack }: ChatStepProps) {
  const [channelName, setChannelName] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  // Find or create DM channel with the agent
  useEffect(() => {
    const setup = async () => {
      try {
        // Check for existing DM channel
        const channels = await api.channels.list();
        const dm = (channels || []).find((c: any) =>
          c.name === `dm-${agentName}` || c.name === agentName
        );
        if (dm) {
          setChannelName(dm.name);
        } else {
          // Create a DM channel
          await api.channels.create(`dm-${agentName}`, [agentName]);
          setChannelName(`dm-${agentName}`);
        }
      } catch (e: any) {
        setError(e.message || 'Could not open chat');
      } finally {
        setLoading(false);
      }
    };
    if (agentName) setup();
  }, [agentName]);

  if (loading) {
    return (
      <div className="text-center space-y-4">
        <h2 className="text-2xl font-semibold text-foreground">Opening chat...</h2>
        <Loader2 className="w-5 h-5 animate-spin mx-auto text-muted-foreground" />
      </div>
    );
  }

  if (error || !channelName) {
    return (
      <div className="text-center space-y-4">
        <h2 className="text-2xl font-semibold text-foreground">Chat</h2>
        <p className="text-sm text-red-400">{error || 'No channel available'}</p>
        <Button onClick={onFinish}>Finish Setup</Button>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <div className="text-center space-y-2">
        <h2 className="text-2xl font-semibold text-foreground">Talk to {agentName}</h2>
        <p className="text-muted-foreground text-sm">
          Try sending a message. Your agent is ready.
        </p>
      </div>

      {/* Embedded chat — reuses existing components */}
      <div className="border border-border rounded-lg bg-card overflow-hidden" style={{ height: '400px' }}>
        <EmbeddedChat channelName={channelName} />
      </div>

      {/* Navigation */}
      <div className="flex items-center justify-between pt-2">
        <button onClick={onBack} className="text-sm text-muted-foreground hover:text-foreground transition-colors">
          Back
        </button>
        <Button onClick={onFinish}>
          Finish Setup
        </Button>
      </div>
    </div>
  );
}

// Simplified embedded chat using existing hooks and components.
// This is a minimal wrapper — the actual implementation will depend on
// how MessageArea and ComposeBar accept their props. Read those files
// during implementation and adapt accordingly.
function EmbeddedChat({ channelName }: { channelName: string }) {
  // Use existing hooks for message fetching and WebSocket
  const { messages, loading, sendMessage } = useChannelMessages(channelName);
  useChannelSocket(channelName);

  return (
    <div className="flex flex-col h-full">
      <div className="flex-1 overflow-y-auto p-3">
        {loading ? (
          <div className="text-center text-muted-foreground text-sm py-8">Loading...</div>
        ) : messages.length === 0 ? (
          <div className="text-center text-muted-foreground text-sm py-8">
            Send a message to get started
          </div>
        ) : (
          <MessageArea messages={messages} channelName={channelName} />
        )}
      </div>
      <div className="border-t border-border p-2">
        <ComposeBar
          channelName={channelName}
          onSend={sendMessage}
          placeholder="What can you help me with?"
        />
      </div>
    </div>
  );
}
```

Note: The `EmbeddedChat` component above is a sketch. During implementation, read `MessageArea.tsx`, `ComposeBar.tsx`, `useChannelMessages.ts`, and `useChannelSocket.ts` to understand their actual prop interfaces and adapt. The key point: reuse, don't rebuild.

- [ ] **Step 3: Replace placeholder in Setup.tsx**

Add import, wire up, remove `ChatPlaceholder`.

- [ ] **Step 4: Commit**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web
git add src/app/screens/setup/ChatStep.tsx src/app/screens/Setup.tsx
git commit -m "feat: implement ChatStep — embedded chat with first agent"
```

---

### Task 15: Add re-setup access from Admin

**Files:**
- Modify: `src/app/screens/Admin.tsx`

- [ ] **Step 1: Add Setup Wizard tab to Admin**

In `src/app/screens/Admin.tsx`, add 'setup' to `VALID_TABS`:
```ts
const VALID_TABS = new Set([
  'infrastructure', 'hub', 'intake', 'knowledge', 'capabilities', 'presets',
  'trust', 'egress', 'policy', 'doctor', 'usage', 'events', 'webhooks',
  'notifications', 'audit', 'danger', 'setup',
]);
```

Add the tab trigger in the TabsList, before the danger zone separator:
```tsx
<TabsTrigger value="setup">Setup Wizard</TabsTrigger>
```

Add the tab content:
```tsx
<TabsContent value="setup">
  <div className="text-center py-12 space-y-4">
    <h3 className="text-lg font-medium text-foreground">Re-run Setup Wizard</h3>
    <p className="text-sm text-muted-foreground max-w-md mx-auto">
      Walk through platform configuration again — update providers, capabilities, and agent settings.
    </p>
    <Button variant="outline" onClick={() => navigate('/setup')}>
      Open Setup Wizard
    </Button>
  </div>
</TabsContent>
```

Add import for `Button` if not already imported.

- [ ] **Step 2: Commit**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web
git add src/app/screens/Admin.tsx
git commit -m "feat: add Setup Wizard tab to Admin for re-setup access"
```

---

### Task 16: Add routing config API method and wire first-launch detection

**Files:**
- Modify: `src/app/lib/api.ts`
- Modify: `src/app/components/Layout.tsx`

- [ ] **Step 1: Check if routing config API method exists**

Read `src/app/lib/api.ts` and search for `routing`. If `api.routing.config()` doesn't exist, add:

```ts
  routing: {
    config: () => req<{ configured: boolean; version: string; [key: string]: unknown }>('/routing/config'),
  },
```

- [ ] **Step 2: Wire first-launch detection in Layout.tsx**

Read `src/app/components/Layout.tsx`, find where the main content renders, and add the setup check from Task 8 Step 4. The exact implementation depends on the Layout component's current structure.

- [ ] **Step 3: Verify the full flow**

Start dev server, navigate to the app:
- If no providers configured → should redirect to `/setup`
- If providers configured → should render normally
- Navigate to `/admin/setup` → should show re-setup link
- Click through wizard steps → placeholders should work end to end

- [ ] **Step 4: Commit**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web
git add src/app/lib/api.ts src/app/components/Layout.tsx
git commit -m "feat: first-launch detection redirects to setup wizard"
```

---

### Task 17: Visual polish and final integration test

**Files:**
- All setup step components (review and polish)

- [ ] **Step 1: Review all step transitions**

Navigate through the wizard manually. Check:
- Progress dots update correctly
- Back/forward navigation works at every step
- Skip works from welcome step
- Finish navigates to `/channels`

- [ ] **Step 2: Test validation gates**

- Welcome: can't proceed without valid name
- Providers: can't proceed without at least one validated provider
- Agent: can't proceed until agent starts (or skip works)
- Capabilities: apply succeeds and proceeds

- [ ] **Step 3: Test re-setup flow**

Navigate to `/admin/setup`, click "Open Setup Wizard", verify:
- Existing providers show as configured
- Skip is visible
- Pre-filled values are correct

- [ ] **Step 4: Run build**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web && npm run build`
Expected: builds without errors

- [ ] **Step 5: Run tests**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web && npm test`
Expected: existing tests still pass

- [ ] **Step 6: Final commit**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web
git add -A
git commit -m "feat: setup wizard polish and integration"
```

---

## Push all repos

- [ ] **Push agency-hub**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-hub && git push origin main
```

- [ ] **Push agency**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency && git push origin main
```

- [ ] **Push agency-web**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web && git push origin main
```
