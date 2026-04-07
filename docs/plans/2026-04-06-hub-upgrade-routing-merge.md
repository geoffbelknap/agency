# Hub Upgrade Routing Merge — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix `syncRouting()` so that `hub upgrade` preserves non-default providers installed via `hub install`, instead of overwriting routing.yaml.

**Architecture:** Extract the pure merge logic from `MergeProviderRouting` into an unexported `mergeProviderInto` helper. Rewrite `syncRouting` to: load hub base, merge non-default installed providers on top, validate the merged output, then write. TDD — test first, then implement.

**Tech Stack:** Go, gopkg.in/yaml.v3

**Spec:** `docs/specs/hub-upgrade-routing-merge.md`

---

## File Map

| File | Action | Responsibility |
|------|--------|----------------|
| `internal/hub/provider.go` | Modify | Extract `mergeProviderInto` from `MergeProviderRouting` |
| `internal/hub/provider_test.go` | Create | Unit test for `mergeProviderInto` |
| `internal/hub/hub.go` | Modify | Rewrite `syncRouting()` to use merge strategy |
| `internal/hub/hub_test.go` | Modify | Integration test: upgrade preserves installed providers |

---

### Task 1: Extract `mergeProviderInto` helper — test

**Files:**
- Create: `internal/hub/provider_test.go`

- [ ] **Step 1: Write the failing test for `mergeProviderInto`**

```go
package hub

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestMergeProviderInto(t *testing.T) {
	// Start with a base config that has one existing provider
	cfg := map[string]interface{}{
		"version": "0.1",
		"providers": map[string]interface{}{
			"anthropic": map[string]interface{}{
				"api_base": "https://api.anthropic.com/v1",
				"auth_env": "ANTHROPIC_API_KEY",
			},
		},
		"models": map[string]interface{}{
			"claude-sonnet": map[string]interface{}{
				"provider":       "anthropic",
				"provider_model": "claude-sonnet-4-20250514",
			},
		},
		"tiers": map[string]interface{}{
			"standard": []interface{}{
				map[string]interface{}{"model": "claude-sonnet", "preference": 0},
			},
		},
	}

	// Provider YAML with routing block (same format as hub install provider data)
	providerData := []byte(`
name: together-ai
routing:
  api_base: "https://api.together.xyz/v1"
  auth_env: OPENAI_API_KEY
  auth_header: Authorization
  auth_prefix: "Bearer "
  models:
    together-llama:
      provider_model: meta-llama/Llama-3-70b-chat-hf
      cost_per_mtok_in: 0.9
      cost_per_mtok_out: 0.9
  tiers:
    standard: together-llama
`)

	if err := mergeProviderInto(cfg, "together-ai", providerData); err != nil {
		t.Fatalf("mergeProviderInto failed: %v", err)
	}

	// Existing provider untouched
	providers := cfg["providers"].(map[string]interface{})
	if _, ok := providers["anthropic"]; !ok {
		t.Fatal("existing provider 'anthropic' was removed")
	}

	// New provider added
	tai, ok := providers["together-ai"].(map[string]interface{})
	if !ok {
		t.Fatal("together-ai provider not found after merge")
	}
	if tai["api_base"] != "https://api.together.xyz/v1" {
		t.Fatalf("unexpected api_base: %v", tai["api_base"])
	}

	// New model added with provider stamp
	models := cfg["models"].(map[string]interface{})
	tlm, ok := models["together-llama"].(map[string]interface{})
	if !ok {
		t.Fatal("together-llama model not found after merge")
	}
	if tlm["provider"] != "together-ai" {
		t.Fatalf("model provider not stamped: %v", tlm["provider"])
	}

	// Existing model untouched
	if _, ok := models["claude-sonnet"]; !ok {
		t.Fatal("existing model 'claude-sonnet' was removed")
	}

	// Tier entry appended (not replaced)
	tiers := cfg["tiers"].(map[string]interface{})
	standard, ok := tiers["standard"].([]interface{})
	if !ok {
		t.Fatalf("standard tier not a slice: %T", tiers["standard"])
	}
	if len(standard) != 2 {
		t.Fatalf("expected 2 tier entries, got %d", len(standard))
	}
}

func TestMergeProviderIntoNoRoutingBlock(t *testing.T) {
	cfg := map[string]interface{}{
		"version":   "0.1",
		"providers": map[string]interface{}{},
		"models":    map[string]interface{}{},
		"tiers":     map[string]interface{}{},
	}

	// Provider YAML with no routing block — should be a no-op
	providerData := []byte(`
name: bare-provider
version: "1.0"
`)

	if err := mergeProviderInto(cfg, "bare-provider", providerData); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	providers := cfg["providers"].(map[string]interface{})
	if len(providers) != 0 {
		t.Fatalf("expected no providers after no-op merge, got %d", len(providers))
	}
}

// Verify MergeProviderRouting still works identically after refactor
func TestMergeProviderRoutingUnchangedBehavior(t *testing.T) {
	home := t.TempDir()

	providerData := []byte(`
name: test-provider
routing:
  api_base: "https://api.test.com/v1"
  auth_env: OPENAI_API_KEY
  auth_header: Authorization
  auth_prefix: "Bearer "
  models:
    test-model:
      provider_model: test-v1
      cost_per_mtok_in: 1.0
      cost_per_mtok_out: 2.0
  tiers:
    standard: test-model
credential:
  env_var: OPENAI_API_KEY
`)

	if err := MergeProviderRouting(home, "test-provider", providerData); err != nil {
		t.Fatalf("MergeProviderRouting failed: %v", err)
	}

	// Read the written routing.yaml and verify
	data, err := os.ReadFile(filepath.Join(home, "infrastructure", "routing.yaml"))
	if err != nil {
		t.Fatalf("failed to read routing.yaml: %v", err)
	}

	var cfg map[string]interface{}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("failed to parse routing.yaml: %v", err)
	}

	providers := cfg["providers"].(map[string]interface{})
	tp, ok := providers["test-provider"].(map[string]interface{})
	if !ok {
		t.Fatal("test-provider not found in routing.yaml")
	}
	if tp["api_base"] != "https://api.test.com/v1" {
		t.Fatalf("unexpected api_base: %v", tp["api_base"])
	}

	models := cfg["models"].(map[string]interface{})
	tm, ok := models["test-model"].(map[string]interface{})
	if !ok {
		t.Fatal("test-model not found in routing.yaml")
	}
	if tm["provider"] != "test-provider" {
		t.Fatalf("model provider not stamped: %v", tm["provider"])
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency && go test ./internal/hub/ -run "TestMergeProviderInto" -v`

Expected: Compilation error — `mergeProviderInto` is undefined.

- [ ] **Step 3: Commit test file**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency
git add internal/hub/provider_test.go
git commit -m "test: add failing tests for mergeProviderInto helper"
```

---

### Task 2: Extract `mergeProviderInto` helper — implement

**Files:**
- Modify: `internal/hub/provider.go:14-85` (refactor `MergeProviderRouting`)

- [ ] **Step 1: Extract the merge logic into `mergeProviderInto`**

Add this new function to `provider.go` (before `MergeProviderRouting`):

```go
// mergeProviderInto merges a single provider's routing block into cfg.
// cfg is modified in place. If providerData has no routing block, this is a no-op.
func mergeProviderInto(cfg map[string]interface{}, providerName string, providerData []byte) error {
	var doc map[string]interface{}
	if err := yaml.Unmarshal(providerData, &doc); err != nil {
		return fmt.Errorf("parse provider YAML: %w", err)
	}

	routing, ok := doc["routing"].(map[string]interface{})
	if !ok {
		return nil // no routing block — nothing to merge
	}

	// Merge provider config (api_base, auth fields)
	providers := ensureMap(cfg, "providers")
	providerCfg := map[string]interface{}{}
	for _, key := range []string{"api_base", "auth_header", "auth_prefix", "auth_env"} {
		if v, ok := routing[key]; ok {
			providerCfg[key] = v
		}
	}
	// Fall back to credential.env_var if auth_env isn't in the routing block.
	if _, hasAuthEnv := providerCfg["auth_env"]; !hasAuthEnv {
		if cred, ok := doc["credential"].(map[string]interface{}); ok {
			if envVar, ok := cred["env_var"].(string); ok && envVar != "" {
				providerCfg["auth_env"] = envVar
			}
		}
	}
	providers[providerName] = providerCfg

	// Merge models, stamping each with the provider name
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
				continue
			}
			modelName, ok := modelRef.(string)
			if !ok {
				continue
			}
			existing, _ := tiers[tierName].([]interface{})
			entry := map[string]interface{}{
				"model":      modelName,
				"preference": len(existing),
			}
			tiers[tierName] = append(existing, entry)
		}
	}

	return nil
}
```

- [ ] **Step 2: Rewrite `MergeProviderRouting` to use the helper**

Replace the body of `MergeProviderRouting` (lines 14-85) with:

```go
func MergeProviderRouting(home, providerName string, providerData []byte) error {
	routingPath := filepath.Join(home, "infrastructure", "routing.yaml")
	cfg, err := loadRoutingYAML(routingPath)
	if err != nil {
		return err
	}

	if err := mergeProviderInto(cfg, providerName, providerData); err != nil {
		return err
	}

	return writeRoutingYAML(routingPath, cfg)
}
```

- [ ] **Step 3: Run the tests**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency && go test ./internal/hub/ -run "TestMergeProviderInto" -v`

Expected: All three tests pass — `TestMergeProviderInto`, `TestMergeProviderIntoNoRoutingBlock`, `TestMergeProviderRoutingUnchangedBehavior`.

- [ ] **Step 4: Run the full hub test suite to check for regressions**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency && go test ./internal/hub/ -v`

Expected: All existing tests pass.

- [ ] **Step 5: Commit**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency
git add internal/hub/provider.go
git commit -m "refactor: extract mergeProviderInto from MergeProviderRouting

Pure merge logic extracted into an unexported helper so syncRouting
can reuse it. MergeProviderRouting behavior is unchanged — it still
loads, merges, and writes."
```

---

### Task 3: Rewrite `syncRouting` — test

**Files:**
- Modify: `internal/hub/hub_test.go`

- [ ] **Step 1: Write the failing integration test**

Add to `hub_test.go` after the existing `TestUpgradeAllSyncsManagedFiles`:

```go
func TestUpgradePreservesInstalledProviders(t *testing.T) {
	home := t.TempDir()
	mgr := NewManager(home)

	// --- Set up hub cache with default routing (anthropic + openai) ---
	cacheDir := filepath.Join(home, "hub-cache", "default")
	os.MkdirAll(filepath.Join(cacheDir, "pricing"), 0755)
	os.WriteFile(filepath.Join(cacheDir, "pricing", "routing.yaml"), []byte(`
version: "0.1"
providers:
  anthropic:
    api_base: https://api.anthropic.com/v1
    auth_env: ANTHROPIC_API_KEY
    auth_header: x-api-key
    auth_prefix: ""
  openai:
    api_base: https://api.openai.com/v1
    auth_env: OPENAI_API_KEY
    auth_header: Authorization
    auth_prefix: "Bearer "
models:
  claude-sonnet:
    provider: anthropic
    provider_model: claude-sonnet-4-20250514
    cost_per_mtok_in: 3.0
    cost_per_mtok_out: 15.0
  gpt-4o:
    provider: openai
    provider_model: gpt-4o
    cost_per_mtok_in: 2.5
    cost_per_mtok_out: 10.0
tiers:
  standard:
    - model: claude-sonnet
      preference: 0
settings:
  default_tier: standard
  tier_strategy: best_effort
`), 0644)

	// Also need ontology for upgrade to not error
	os.MkdirAll(filepath.Join(cacheDir, "ontology"), 0755)
	os.WriteFile(filepath.Join(cacheDir, "ontology", "base-ontology.yaml"),
		[]byte("version: v1\nentity_types:\n  Host: {}\n"), 0644)

	// Config
	os.WriteFile(filepath.Join(home, "config.yaml"),
		[]byte("hub:\n  sources:\n    - name: default\n      url: https://example.com\n"), 0644)

	// --- Install a non-default provider via MergeProviderRouting ---
	// First write the hub base routing so MergeProviderRouting has something to merge into
	infraDir := filepath.Join(home, "infrastructure")
	os.MkdirAll(infraDir, 0755)
	hubRouting, _ := os.ReadFile(filepath.Join(cacheDir, "pricing", "routing.yaml"))
	os.WriteFile(filepath.Join(infraDir, "routing.yaml"), hubRouting, 0644)

	// Create the provider in the registry
	inst, err := mgr.Registry.Create("together-ai", "provider", "default/together-ai")
	if err != nil {
		t.Fatal(err)
	}

	// Write provider YAML to instance dir (same as hub install does)
	providerYAML := []byte(`
name: together-ai
version: "1.0"
routing:
  api_base: "https://api.together.xyz/v1"
  auth_env: OPENAI_API_KEY
  auth_header: Authorization
  auth_prefix: "Bearer "
  models:
    together-llama:
      provider_model: meta-llama/Llama-3-70b-chat-hf
      cost_per_mtok_in: 0.9
      cost_per_mtok_out: 0.9
  tiers:
    standard: together-llama
`)
	instDir := mgr.Registry.InstanceDir(inst.Name)
	os.MkdirAll(instDir, 0755)
	os.WriteFile(filepath.Join(instDir, "provider.yaml"), providerYAML, 0644)

	// Merge it into routing.yaml (simulates what hub install does)
	if err := MergeProviderRouting(home, "together-ai", providerYAML); err != nil {
		t.Fatalf("MergeProviderRouting: %v", err)
	}

	// Verify it's in routing.yaml before upgrade
	preUpgrade, _ := os.ReadFile(filepath.Join(infraDir, "routing.yaml"))
	var preCfg map[string]interface{}
	yaml.Unmarshal(preUpgrade, &preCfg)
	preProviders := preCfg["providers"].(map[string]interface{})
	if _, ok := preProviders["together-ai"]; !ok {
		t.Fatal("together-ai not in routing.yaml before upgrade")
	}

	// --- Run upgrade ---
	report, err := mgr.Upgrade(nil)
	if err != nil {
		t.Fatalf("Upgrade: %v", err)
	}

	// Verify routing was synced
	foundRouting := false
	for _, f := range report.Files {
		if f.Category == "routing" {
			foundRouting = true
			if f.Status == "error" {
				t.Fatalf("routing sync error: %s", f.Summary)
			}
		}
	}
	if !foundRouting {
		t.Fatal("no routing entry in upgrade report")
	}

	// --- Assert: together-ai survived the upgrade ---
	postUpgrade, err := os.ReadFile(filepath.Join(infraDir, "routing.yaml"))
	if err != nil {
		t.Fatalf("read routing.yaml after upgrade: %v", err)
	}

	var postCfg map[string]interface{}
	if err := yaml.Unmarshal(postUpgrade, &postCfg); err != nil {
		t.Fatalf("parse routing.yaml after upgrade: %v", err)
	}

	postProviders := postCfg["providers"].(map[string]interface{})

	// Non-default provider preserved
	tai, ok := postProviders["together-ai"].(map[string]interface{})
	if !ok {
		t.Fatal("together-ai provider LOST after upgrade — this is the bug")
	}
	if tai["api_base"] != "https://api.together.xyz/v1" {
		t.Fatalf("together-ai api_base wrong: %v", tai["api_base"])
	}

	// Default providers still present and up to date from hub cache
	if _, ok := postProviders["anthropic"]; !ok {
		t.Fatal("default provider 'anthropic' missing after upgrade")
	}
	if _, ok := postProviders["openai"]; !ok {
		t.Fatal("default provider 'openai' missing after upgrade")
	}

	// Non-default model preserved
	postModels := postCfg["models"].(map[string]interface{})
	if _, ok := postModels["together-llama"]; !ok {
		t.Fatal("together-llama model LOST after upgrade")
	}

	// Default models still present
	if _, ok := postModels["claude-sonnet"]; !ok {
		t.Fatal("default model 'claude-sonnet' missing after upgrade")
	}
}

func TestUpgradeDoesNotPreserveRemovedProviders(t *testing.T) {
	home := t.TempDir()
	mgr := NewManager(home)

	// Hub cache with defaults
	cacheDir := filepath.Join(home, "hub-cache", "default")
	os.MkdirAll(filepath.Join(cacheDir, "pricing"), 0755)
	os.WriteFile(filepath.Join(cacheDir, "pricing", "routing.yaml"), []byte(`
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
`), 0644)

	os.MkdirAll(filepath.Join(cacheDir, "ontology"), 0755)
	os.WriteFile(filepath.Join(cacheDir, "ontology", "base-ontology.yaml"),
		[]byte("version: v1\nentity_types:\n  Host: {}\n"), 0644)

	os.WriteFile(filepath.Join(home, "config.yaml"),
		[]byte("hub:\n  sources:\n    - name: default\n      url: https://example.com\n"), 0644)

	// Write initial routing with a stale provider that is NOT in the registry
	infraDir := filepath.Join(home, "infrastructure")
	os.MkdirAll(infraDir, 0755)
	os.WriteFile(filepath.Join(infraDir, "routing.yaml"), []byte(`
version: "0.1"
providers:
  anthropic:
    api_base: https://api.anthropic.com/v1
    auth_env: ANTHROPIC_API_KEY
  stale-provider:
    api_base: https://stale.example.com/v1
    auth_env: OPENAI_API_KEY
models:
  claude-sonnet:
    provider: anthropic
  stale-model:
    provider: stale-provider
tiers:
  standard:
    - model: claude-sonnet
      preference: 0
`), 0644)

	// Run upgrade — stale-provider is NOT in the registry
	_, err := mgr.Upgrade(nil)
	if err != nil {
		t.Fatalf("Upgrade: %v", err)
	}

	postData, _ := os.ReadFile(filepath.Join(infraDir, "routing.yaml"))
	var postCfg map[string]interface{}
	yaml.Unmarshal(postData, &postCfg)

	postProviders := postCfg["providers"].(map[string]interface{})
	if _, ok := postProviders["stale-provider"]; ok {
		t.Fatal("stale-provider should have been cleaned up by upgrade")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency && go test ./internal/hub/ -run "TestUpgradePreservesInstalledProviders" -v`

Expected: FAIL — "together-ai provider LOST after upgrade — this is the bug"

- [ ] **Step 3: Commit failing test**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency
git add internal/hub/hub_test.go
git commit -m "test: add failing test for hub upgrade clobbering installed providers

Reproduces P0 bug: syncRouting overwrites routing.yaml, dropping
non-default providers added via hub install."
```

---

### Task 4: Rewrite `syncRouting` — implement

**Files:**
- Modify: `internal/hub/hub.go:174-195`

- [ ] **Step 1: Rewrite `syncRouting`**

Replace the current `syncRouting` function (hub.go:174-195) with:

```go
func (m *Manager) syncRouting(cacheDir string) error {
	// Step 1-2: Read and validate hub cache base
	var hubBase []byte
	entries, _ := os.ReadDir(cacheDir)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(cacheDir, e.Name(), "pricing/routing.yaml"))
		if err != nil {
			continue
		}
		validated, err := validateRoutingConfig(data)
		if err != nil {
			return fmt.Errorf("routing validation: %w", err)
		}
		hubBase = validated
		break
	}
	if hubBase == nil {
		return nil // no routing.yaml in hub cache
	}

	// Step 3: Unmarshal hub base
	var cfg map[string]interface{}
	if err := yaml.Unmarshal(hubBase, &cfg); err != nil {
		return fmt.Errorf("parse hub routing: %w", err)
	}

	// Step 4: Identify default provider names from hub base
	defaultProviders := map[string]bool{}
	if providers, ok := cfg["providers"].(map[string]interface{}); ok {
		for name := range providers {
			defaultProviders[name] = true
		}
	}

	// Steps 5-7: Query installed providers, filter to non-defaults, merge
	for _, inst := range m.Registry.List("provider") {
		if defaultProviders[inst.Name] {
			continue // hub base is authoritative for defaults
		}
		instDir := m.Registry.InstanceDir(inst.Name)
		if instDir == "" {
			continue
		}
		providerData, err := os.ReadFile(filepath.Join(instDir, "provider.yaml"))
		if err != nil {
			log.Printf("[hub] WARNING: cannot read provider %q for routing merge: %v", inst.Name, err)
			continue
		}
		if err := mergeProviderInto(cfg, inst.Name, providerData); err != nil {
			log.Printf("[hub] WARNING: failed to merge provider %q routing: %v", inst.Name, err)
		}
	}

	// Step 8: Validate merged output (catches auth_env allowlist changes since install)
	merged, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal merged routing: %w", err)
	}
	validated, err := validateRoutingConfig(merged)
	if err != nil {
		return fmt.Errorf("merged routing validation: %w", err)
	}

	// Step 9: Write
	destPath := filepath.Join(m.Home, "infrastructure", "routing.yaml")
	os.MkdirAll(filepath.Dir(destPath), 0755)
	return os.WriteFile(destPath, validated, 0644)
}
```

- [ ] **Step 2: Run the new tests**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency && go test ./internal/hub/ -run "TestUpgrade(PreservesInstalled|DoesNotPreserveRemoved)" -v`

Expected: Both pass.

- [ ] **Step 3: Run the full hub test suite**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency && go test ./internal/hub/ -v`

Expected: All tests pass, including the existing `TestUpgradeAllSyncsManagedFiles`.

- [ ] **Step 4: Run the full project test suite**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency && go test ./... 2>&1 | tail -30`

Expected: No regressions. If unrelated tests fail, note them but don't block.

- [ ] **Step 5: Commit**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency
git add internal/hub/hub.go
git commit -m "fix: hub upgrade preserves non-default installed providers

syncRouting now merges installed providers on top of the hub base
instead of overwriting routing.yaml. Default providers (shipped in
the hub cache) are authoritative. Non-default providers (added via
hub install) are re-merged from their instance dirs.

Merged output is re-validated to catch auth_env allowlist changes.

Fixes P0: hub upgrade clobbers installed providers."
```

---

### Task 5: Verify end-to-end (manual smoke test)

This task is not automated — it validates the fix against the real hub flow.

- [ ] **Step 1: Build**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency && go build ./cmd/gateway/`

Expected: Clean build, no errors.

- [ ] **Step 2: Verify the fix manually**

```bash
# Check current routing.yaml
cat ~/.agency/infrastructure/routing.yaml

# Install a test provider (if one is available) or inspect routing.yaml
# after running: agency hub update && agency hub upgrade
# Verify non-default providers survive the upgrade cycle
```

- [ ] **Step 3: Verify WriteSwapConfig ordering**

After upgrade, check that `~/.agency/infrastructure/credential-swaps.yaml` includes entries for both default and non-default providers. This confirms `WriteSwapConfig` reads the merged routing.yaml.
