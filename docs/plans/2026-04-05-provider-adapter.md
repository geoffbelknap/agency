# Provider Adapter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add model capability declarations, capability-aware tier routing with automatic fallback, Gemini as a first-class provider, and `agency provider add` for local model discovery.

**Architecture:** `Capabilities []string` added to `ModelConfig` in routing.go and `Model` in enforcer config.go. Enforcer detects required capabilities from request body, checks tier caps, and falls back to the nearest capable tier or rejects 422. Tier capability manifest served to body via `/config/tiers.json`. New `agency provider add` CLI command discovers models from OpenAI-compatible endpoints and writes to routing.local.yaml. Gemini provider YAML added to agency-hub.

**Tech Stack:** Go (routing models, enforcer, CLI), YAML (hub provider definitions)

**Spec:** `docs/specs/provider-adapter.md`

---

### Task 1: Add Capabilities to ModelConfig (routing.go)

**Files:**
- Modify: `internal/models/routing.go:31-37`
- Test: `internal/models/routing_test.go`

- [ ] **Step 1: Write tests for HasCapability**

Add to `internal/models/routing_test.go`:

```go
func TestModelConfig_HasCapability(t *testing.T) {
	m := ModelConfig{
		Provider:      "anthropic",
		ProviderModel: "claude-sonnet-4-20250514",
		Capabilities:  []string{"tools", "vision", "streaming"},
	}
	if !m.HasCapability("tools") {
		t.Error("expected HasCapability(tools) = true")
	}
	if !m.HasCapability("vision") {
		t.Error("expected HasCapability(vision) = true")
	}
	if m.HasCapability("thinking") {
		t.Error("expected HasCapability(thinking) = false")
	}
}

func TestModelConfig_HasCapabilityEmpty(t *testing.T) {
	m := ModelConfig{Provider: "ollama", ProviderModel: "llama3.1:8b"}
	if m.HasCapability("tools") {
		t.Error("expected HasCapability(tools) = false for empty capabilities")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/models/ -run TestModelConfig_HasCapability -v
```

Expected: Compilation error — `Capabilities` field and `HasCapability` method don't exist.

- [ ] **Step 3: Add Capabilities field and HasCapability method**

In `internal/models/routing.go`, change `ModelConfig` (lines 31-37):

```go
// ModelConfig describes a specific LLM model and its cost information.
type ModelConfig struct {
	Provider          string   `yaml:"provider" validate:"required"`
	ProviderModel     string   `yaml:"provider_model" validate:"required"`
	Capabilities      []string `yaml:"capabilities" validate:"required"`
	CostPerMTokIn     float64  `yaml:"cost_per_mtok_in" validate:"gte=0" default:"0"`
	CostPerMTokOut    float64  `yaml:"cost_per_mtok_out" validate:"gte=0" default:"0"`
	CostPerMTokCached float64  `yaml:"cost_per_mtok_cached" validate:"gte=0" default:"0"`
}

// HasCapability returns true if the model declares the given capability.
func (m ModelConfig) HasCapability(cap string) bool {
	for _, c := range m.Capabilities {
		if c == cap {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/models/ -run TestModelConfig_HasCapability -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/models/routing.go internal/models/routing_test.go
git commit -m "feat(routing): add Capabilities field and HasCapability to ModelConfig"
```

---

### Task 2: Add TierCapabilities to RoutingConfig

**Files:**
- Modify: `internal/models/routing.go`
- Test: `internal/models/routing_test.go`

- [ ] **Step 1: Write tests for TierCapabilities**

Add to `internal/models/routing_test.go`:

```go
func TestRoutingConfig_TierCapabilities(t *testing.T) {
	rc := RoutingConfig{
		Models: map[string]ModelConfig{
			"claude-sonnet": {Provider: "anthropic", ProviderModel: "claude-sonnet-4", Capabilities: []string{"tools", "vision", "streaming"}},
			"gemini-flash":  {Provider: "gemini", ProviderModel: "gemini-2.5-flash", Capabilities: []string{"tools", "vision", "streaming"}},
		},
		Tiers: TierConfig{
			Standard: []TierEntry{
				{Model: "claude-sonnet", Preference: 0},
				{Model: "gemini-flash", Preference: 1},
			},
		},
	}

	caps := rc.TierCapabilities("standard")
	// Intersection of two models that both have all three caps
	expected := map[string]bool{"tools": true, "vision": true, "streaming": true}
	for _, c := range caps {
		if !expected[c] {
			t.Errorf("unexpected capability: %s", c)
		}
		delete(expected, c)
	}
	if len(expected) > 0 {
		t.Errorf("missing capabilities: %v", expected)
	}
}

func TestRoutingConfig_TierCapabilitiesIntersection(t *testing.T) {
	rc := RoutingConfig{
		Models: map[string]ModelConfig{
			"claude-sonnet": {Provider: "anthropic", ProviderModel: "claude-sonnet-4", Capabilities: []string{"tools", "vision", "streaming"}},
			"llama-8b":      {Provider: "ollama", ProviderModel: "llama3.1:8b", Capabilities: []string{"streaming"}},
		},
		Tiers: TierConfig{
			Mini: []TierEntry{
				{Model: "claude-sonnet", Preference: 1},
				{Model: "llama-8b", Preference: 0},
			},
		},
	}

	caps := rc.TierCapabilities("mini")
	// Intersection: only "streaming" is shared
	if len(caps) != 1 || caps[0] != "streaming" {
		t.Errorf("expected [streaming], got %v", caps)
	}
}

func TestRoutingConfig_TierCapabilitiesEmpty(t *testing.T) {
	rc := RoutingConfig{}
	caps := rc.TierCapabilities("frontier")
	if len(caps) != 0 {
		t.Errorf("expected empty caps for empty tier, got %v", caps)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/models/ -run TestRoutingConfig_TierCapabilities -v
```

Expected: Compilation error — `TierCapabilities` doesn't exist.

- [ ] **Step 3: Implement TierCapabilities**

Add to `internal/models/routing.go`:

```go
// TierCapabilities returns the intersection of capabilities across all models
// in the given tier. A capability is included only if every model in the tier
// declares it. Returns nil for empty or unknown tiers.
func (r *RoutingConfig) TierCapabilities(tier string) []string {
	var entries []TierEntry
	switch tier {
	case "frontier":
		entries = r.Tiers.Frontier
	case "standard":
		entries = r.Tiers.Standard
	case "fast":
		entries = r.Tiers.Fast
	case "mini":
		entries = r.Tiers.Mini
	case "nano":
		entries = r.Tiers.Nano
	case "batch":
		entries = r.Tiers.Batch
	default:
		return nil
	}

	if len(entries) == 0 {
		return nil
	}

	// Start with the first model's capabilities, intersect with the rest
	first := r.Models[entries[0].Model]
	caps := make(map[string]bool)
	for _, c := range first.Capabilities {
		caps[c] = true
	}

	for _, entry := range entries[1:] {
		mc, ok := r.Models[entry.Model]
		if !ok {
			continue
		}
		has := make(map[string]bool)
		for _, c := range mc.Capabilities {
			has[c] = true
		}
		for c := range caps {
			if !has[c] {
				delete(caps, c)
			}
		}
	}

	result := make([]string, 0, len(caps))
	for c := range caps {
		result = append(result, c)
	}
	return result
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/models/ -run TestRoutingConfig_TierCapabilities -v
```

Expected: PASS.

- [ ] **Step 5: Run all routing tests**

```bash
go test ./internal/models/ -v
```

Expected: All pass. Existing tests don't use `Capabilities` — they should still compile since the field has `validate:"required"` but existing test configs don't set it. If any existing tests fail because of the `required` tag, remove the `validate:"required"` tag (validation is separate from deserialization).

- [ ] **Step 6: Commit**

```bash
git add internal/models/routing.go internal/models/routing_test.go
git commit -m "feat(routing): add TierCapabilities intersection method"
```

---

### Task 3: Add ResolveTierWithCapabilities

**Files:**
- Modify: `internal/models/routing.go`
- Test: `internal/models/routing_test.go`

- [ ] **Step 1: Write test**

Add to `internal/models/routing_test.go`:

```go
func TestResolveTierWithCapabilities(t *testing.T) {
	rc := RoutingConfig{
		Providers: map[string]ProviderConfig{
			"anthropic": {APIBase: "https://api.anthropic.com/v1"},
			"ollama":    {APIBase: "http://localhost:11434/v1"},
		},
		Models: map[string]ModelConfig{
			"claude-sonnet": {Provider: "anthropic", ProviderModel: "claude-sonnet-4", Capabilities: []string{"tools", "vision", "streaming"}},
			"llama-8b":      {Provider: "ollama", ProviderModel: "llama3.1:8b", Capabilities: []string{"streaming"}},
		},
		Tiers: TierConfig{
			Standard: []TierEntry{{Model: "claude-sonnet", Preference: 0}},
			Mini:     []TierEntry{{Model: "llama-8b", Preference: 0}},
		},
		Settings: RoutingSettings{TierStrategy: "best_effort"},
	}

	// Mini tier with tools requirement should fall back to standard
	pc, mc, fallbackTier := rc.ResolveTierWithCapabilities("mini", []string{"tools"}, nil)
	if pc == nil || mc == nil {
		t.Fatal("expected resolution, got nil")
	}
	if mc.ProviderModel != "claude-sonnet-4" {
		t.Errorf("expected claude-sonnet-4, got %s", mc.ProviderModel)
	}
	if fallbackTier != "standard" {
		t.Errorf("expected fallback to standard, got %s", fallbackTier)
	}

	// Mini tier with streaming only should resolve to mini
	pc, mc, fallbackTier = rc.ResolveTierWithCapabilities("mini", []string{"streaming"}, nil)
	if mc.ProviderModel != "llama3.1:8b" {
		t.Errorf("expected llama3.1:8b, got %s", mc.ProviderModel)
	}
	if fallbackTier != "mini" {
		t.Errorf("expected mini (no fallback), got %s", fallbackTier)
	}
}

func TestResolveTierWithCapabilitiesNoMatch(t *testing.T) {
	rc := RoutingConfig{
		Providers: map[string]ProviderConfig{
			"ollama": {APIBase: "http://localhost:11434/v1"},
		},
		Models: map[string]ModelConfig{
			"llama-8b": {Provider: "ollama", ProviderModel: "llama3.1:8b", Capabilities: []string{"streaming"}},
		},
		Tiers: TierConfig{
			Mini: []TierEntry{{Model: "llama-8b", Preference: 0}},
		},
		Settings: RoutingSettings{TierStrategy: "best_effort"},
	}

	// Vision required but no tier supports it
	pc, mc, _ := rc.ResolveTierWithCapabilities("mini", []string{"vision"}, nil)
	if pc != nil || mc != nil {
		t.Error("expected nil for unsatisfiable capability requirement")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/models/ -run TestResolveTierWithCapabilities -v
```

Expected: Compilation error.

- [ ] **Step 3: Implement ResolveTierWithCapabilities**

Add to `internal/models/routing.go`:

```go
// ResolveTierWithCapabilities resolves a tier that supports all required
// capabilities. If the requested tier doesn't support them, walks the tier
// hierarchy (best_effort strategy) to find the nearest tier that does.
// Returns the provider, model, and the tier that was actually used.
// Returns (nil, nil, "") if no tier satisfies the requirements.
func (r *RoutingConfig) ResolveTierWithCapabilities(tier string, required []string, extraEnv map[string]string) (*ProviderConfig, *ModelConfig, string) {
	if len(required) == 0 {
		pc, mc := r.ResolveTierWithStrategy(tier, extraEnv)
		return pc, mc, tier
	}

	// Check if requested tier satisfies capabilities
	if r.tierSatisfies(tier, required) {
		pc, mc := r.ResolveTier(tier, extraEnv)
		if pc != nil && mc != nil {
			return pc, mc, tier
		}
	}

	// Walk tier hierarchy for nearest capable tier
	pos := -1
	for i, t := range tierOrder {
		if t == tier {
			pos = i
			break
		}
	}
	if pos < 0 {
		return nil, nil, ""
	}

	for delta := 1; delta < len(tierOrder); delta++ {
		// Check lower tiers first (cheaper), then higher
		for _, d := range []int{delta, -delta} {
			idx := pos + d
			if idx < 0 || idx >= len(tierOrder) {
				continue
			}
			candidate := tierOrder[idx]
			if r.tierSatisfies(candidate, required) {
				pc, mc := r.ResolveTier(candidate, extraEnv)
				if pc != nil && mc != nil {
					return pc, mc, candidate
				}
			}
		}
	}

	return nil, nil, ""
}

// tierSatisfies returns true if every required capability is in the tier's
// capability intersection.
func (r *RoutingConfig) tierSatisfies(tier string, required []string) bool {
	caps := r.TierCapabilities(tier)
	capSet := make(map[string]bool, len(caps))
	for _, c := range caps {
		capSet[c] = true
	}
	for _, req := range required {
		if !capSet[req] {
			return false
		}
	}
	return true
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/models/ -run TestResolveTierWithCapabilities -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/models/routing.go internal/models/routing_test.go
git commit -m "feat(routing): add capability-aware tier resolution with fallback"
```

---

### Task 4: Add Capabilities to enforcer config and detect required caps

**Files:**
- Modify: `images/enforcer/config.go:27-33`
- Create: `images/enforcer/capabilities.go`
- Test: `images/enforcer/capabilities_test.go`

- [ ] **Step 1: Write tests for capability detection**

Create `images/enforcer/capabilities_test.go`:

```go
package main

import (
	"testing"
)

func TestDetectRequiredCaps_Tools(t *testing.T) {
	body := map[string]interface{}{
		"model": "claude-sonnet",
		"tools": []interface{}{
			map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": "foo"}},
		},
		"messages": []interface{}{},
	}
	caps := detectRequiredCaps(body)
	if !containsCap(caps, "tools") {
		t.Error("expected 'tools' capability for request with tools")
	}
}

func TestDetectRequiredCaps_Vision(t *testing.T) {
	body := map[string]interface{}{
		"model": "claude-sonnet",
		"messages": []interface{}{
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": "https://example.com/img.png"}},
				},
			},
		},
	}
	caps := detectRequiredCaps(body)
	if !containsCap(caps, "vision") {
		t.Error("expected 'vision' capability for request with image content")
	}
}

func TestDetectRequiredCaps_Streaming(t *testing.T) {
	body := map[string]interface{}{
		"model":  "claude-sonnet",
		"stream": true,
	}
	caps := detectRequiredCaps(body)
	if !containsCap(caps, "streaming") {
		t.Error("expected 'streaming' capability")
	}
}

func TestDetectRequiredCaps_PlainText(t *testing.T) {
	body := map[string]interface{}{
		"model": "claude-sonnet",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "hello"},
		},
	}
	caps := detectRequiredCaps(body)
	if len(caps) != 0 {
		t.Errorf("expected no capabilities for plain text request, got %v", caps)
	}
}

func containsCap(caps []string, target string) bool {
	for _, c := range caps {
		if c == target {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd images/enforcer && go test -run TestDetectRequired -v
```

Expected: Compilation error — `detectRequiredCaps` doesn't exist.

- [ ] **Step 3: Implement capabilities.go**

Create `images/enforcer/capabilities.go`:

```go
package main

// detectRequiredCaps inspects an OpenAI-format request body and returns
// the capabilities the request requires from the target model.
func detectRequiredCaps(body map[string]interface{}) []string {
	var caps []string

	// Tools present and non-empty → requires "tools"
	if tools, ok := body["tools"].([]interface{}); ok && len(tools) > 0 {
		caps = append(caps, "tools")
	}

	// Stream requested → requires "streaming"
	if stream, ok := body["stream"].(bool); ok && stream {
		caps = append(caps, "streaming")
	}

	// Any message with image content → requires "vision"
	if messages, ok := body["messages"].([]interface{}); ok {
		for _, msg := range messages {
			m, ok := msg.(map[string]interface{})
			if !ok {
				continue
			}
			content, ok := m["content"].([]interface{})
			if !ok {
				continue
			}
			for _, block := range content {
				b, ok := block.(map[string]interface{})
				if !ok {
					continue
				}
				if b["type"] == "image_url" || b["type"] == "image" {
					caps = append(caps, "vision")
					return caps // found vision, no need to keep scanning
				}
			}
		}
	}

	return caps
}
```

- [ ] **Step 4: Add Capabilities to enforcer Model struct**

In `images/enforcer/config.go`, change the `Model` struct (lines 27-33):

```go
// Model represents an LLM model alias configuration.
type Model struct {
	Provider      string   `yaml:"provider"`
	ProviderModel string   `yaml:"provider_model"`
	Capabilities  []string `yaml:"capabilities"`
	CostIn        float64  `yaml:"cost_per_mtok_in"`
	CostOut       float64  `yaml:"cost_per_mtok_out"`
	CostCached    float64  `yaml:"cost_per_mtok_cached"`
}

// HasCapability returns true if the model declares the given capability.
func (m Model) HasCapability(cap string) bool {
	for _, c := range m.Capabilities {
		if c == cap {
			return true
		}
	}
	return false
}
```

- [ ] **Step 5: Run tests**

```bash
cd images/enforcer && go test -run TestDetectRequired -v
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add images/enforcer/capabilities.go images/enforcer/capabilities_test.go images/enforcer/config.go
git commit -m "feat(enforcer): add capability detection and Model.Capabilities field"
```

---

### Task 5: Generate and serve /config/tiers.json

The gateway has the full `RoutingConfig` with tiers (in `internal/models/routing.go`). The enforcer only sees providers, models, and settings — no tiers. So the **gateway** generates `tiers.json` into the agent's config directory (alongside PLATFORM.md, mission.yaml), and the enforcer serves it via the existing config endpoint.

**Files:**
- Modify: `images/enforcer/config_serve.go:19-29` (add to whitelist)
- Modify: `internal/orchestrate/enforcer.go` or `internal/orchestrate/start.go` (generate tiers.json)

- [ ] **Step 1: Add tiers.json to enforcer config whitelist**

In `images/enforcer/config_serve.go`, add `"tiers.json"` to `configWhitelist`:

```go
var configWhitelist = map[string]bool{
	"PLATFORM.md":            true,
	"mission.yaml":           true,
	"services-manifest.json": true,
	"FRAMEWORK.md":           true,
	"AGENTS.md":              true,
	"identity.md":            true,
	"constraints.yaml":       true,
	"skills-manifest.json":   true,
	"session-context.json":   true,
	"tiers.json":             true,
}
```

- [ ] **Step 2: Add tiers.json generation to the gateway**

Find where the gateway writes agent config files (PLATFORM.md, services-manifest.json, etc.) in the orchestrate package. Add a function that generates tiers.json from the gateway's `RoutingConfig`:

```go
// generateTiersJSON creates a tiers.json manifest from the routing config
// and writes it to the agent's config directory.
func generateTiersJSON(rc *models.RoutingConfig, agentDir string) error {
	tiersMap := map[string]interface{}{}

	for _, tierName := range models.VALID_TIERS {
		caps := rc.TierCapabilities(tierName)
		if caps == nil {
			continue
		}
		sort.Strings(caps)
		tiersMap[tierName] = map[string]interface{}{"capabilities": caps}
	}

	manifest := map[string]interface{}{
		"tiers":        tiersMap,
		"default_tier": rc.Settings.DefaultTier,
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(agentDir, "tiers.json"), data, 0644)
}
```

Call this alongside other config file generation during agent start and on config reload (SIGHUP).

- [ ] **Step 3: Run gateway tests**

```bash
go test ./internal/orchestrate/ -v 2>&1 | tail -10
```

Expected: All pass.

- [ ] **Step 4: Commit**

```bash
git add images/enforcer/config_serve.go internal/orchestrate/
git commit -m "feat: generate tiers.json from gateway, serve from enforcer config endpoint"
```

---

### Task 6: Capability-aware routing in enforcer LLM handler

**Files:**
- Modify: `images/enforcer/llm.go:186-204`

- [ ] **Step 1: Add capability check after model resolution**

In `images/enforcer/llm.go`, after the model resolution block (line 193-204) and before the XPIA scanning (line 206), add capability checking. The enforcer currently resolves by model alias. When the body starts requesting tiers (E2), this will use `X-Agency-Tier` header. For now, check that the resolved model supports what the request needs:

```go
	// Detect required capabilities from request body
	requiredCaps := detectRequiredCaps(reqBody)

	// Check if resolved model supports required capabilities
	if len(requiredCaps) > 0 {
		model := lh.routing.Models[modelAlias]
		for _, req := range requiredCaps {
			if !model.HasCapability(req) {
				lh.audit.Log(AuditEntry{
					Type:          "LLM_CAPABILITY_MISMATCH",
					Model:         modelAlias,
					CorrelationID: correlationID,
					Error:         fmt.Sprintf("model %s does not support %s", modelAlias, req),
				})
				http.Error(w, fmt.Sprintf(`{"error":"model %s does not support capability: %s"}`, modelAlias, req), 422)
				return
			}
		}
	}
```

This is the per-model check. Tier-level fallback routing (using `ResolveTierWithCapabilities`) will be wired in as part of E2 when the body starts sending `X-Agency-Tier` headers. For now, the enforcer validates that the specific model the body requested can handle the request.

- [ ] **Step 2: Run enforcer tests**

```bash
cd images/enforcer && go test ./... -v 2>&1 | tail -20
```

Expected: All pass.

- [ ] **Step 3: Commit**

```bash
git add images/enforcer/llm.go
git commit -m "feat(enforcer): reject requests when model lacks required capabilities"
```

---

### Task 7: Update hub provider YAMLs with capabilities

**Files:**
- Modify: `agency-hub/providers/anthropic/provider.yaml`
- Modify: `agency-hub/providers/openai/provider.yaml`
- Modify: `agency-hub/providers/ollama/provider.yaml`
- Modify: `agency-hub/providers/openai-compatible/provider.yaml`
- Create: `agency-hub/providers/gemini/provider.yaml`

- [ ] **Step 1: Update Anthropic provider**

Add `capabilities` to each model in `/Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-hub/providers/anthropic/provider.yaml`:

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
      capabilities: [tools, vision, streaming]
      cost_per_mtok_in: 5.0
      cost_per_mtok_out: 25.0
      cost_per_mtok_cached: 0.50
    claude-sonnet:
      provider_model: claude-sonnet-4-20250514
      capabilities: [tools, vision, streaming]
      cost_per_mtok_in: 3.0
      cost_per_mtok_out: 15.0
      cost_per_mtok_cached: 0.30
    claude-haiku:
      provider_model: claude-haiku-4-5-20251001
      capabilities: [tools, vision, streaming]
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

- [ ] **Step 2: Update OpenAI provider**

Add `capabilities: [tools, vision, streaming]` to all four models in the OpenAI provider YAML.

- [ ] **Step 3: Update Ollama and OpenAI-Compatible providers**

Both have empty models. Add a comment documenting the capabilities field:

```yaml
  models: {}  # Operator defines models; include capabilities: [tools, streaming] per model
```

- [ ] **Step 4: Create Gemini provider**

Create `/Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-hub/providers/gemini/provider.yaml`:

```yaml
name: gemini
display_name: Google Gemini
description: Gemini models — 2.5 Pro, 2.5 Flash
version: "0.1.0"
category: cloud
credential:
  name: gemini-api-key
  label: API Key
  env_var: GEMINI_API_KEY
  api_key_url: https://aistudio.google.com/apikey
routing:
  api_base: https://generativelanguage.googleapis.com/v1beta/openai
  auth_header: Authorization
  auth_prefix: "Bearer "
  models:
    gemini-2.5-pro:
      provider_model: gemini-2.5-pro
      capabilities: [tools, vision, streaming]
      cost_per_mtok_in: 1.25
      cost_per_mtok_out: 10.0
      cost_per_mtok_cached: 0.315
    gemini-2.5-flash:
      provider_model: gemini-2.5-flash
      capabilities: [tools, vision, streaming]
      cost_per_mtok_in: 0.15
      cost_per_mtok_out: 0.60
      cost_per_mtok_cached: 0.0375
  tiers:
    frontier: gemini-2.5-pro
    standard: gemini-2.5-pro
    fast: gemini-2.5-flash
    mini: gemini-2.5-flash
    nano: gemini-2.5-flash
    batch: null
```

- [ ] **Step 5: Commit in agency-hub**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-hub
git add providers/
git commit -m "feat: add capabilities to all providers, add Gemini provider"
```

---

### Task 8: Pass capabilities through in MergeProviderRouting

**Files:**
- Modify: `internal/hub/provider.go:41-52`
- Test: `internal/hub/provider_test.go`

- [ ] **Step 1: Write test**

The existing `MergeProviderRouting` copies model config fields but doesn't explicitly handle `capabilities`. Since it copies the entire `mc` map, capabilities should flow through automatically. Write a test to verify:

Add to `internal/hub/provider_test.go` (create if it doesn't exist):

```go
package hub

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestMergeProviderRouting_PassesCapabilities(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, "infrastructure"), 0755)

	providerYAML := []byte(`name: test-provider
routing:
  api_base: https://api.example.com/v1
  auth_header: Authorization
  auth_prefix: "Bearer "
  models:
    test-model:
      provider_model: test-model-v1
      capabilities: [tools, streaming]
      cost_per_mtok_in: 1.0
      cost_per_mtok_out: 2.0
  tiers:
    standard: test-model
`)

	err := MergeProviderRouting(home, "test-provider", providerYAML)
	if err != nil {
		t.Fatal(err)
	}

	// Read back and verify capabilities are present
	data, err := os.ReadFile(filepath.Join(home, "infrastructure", "routing.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	var cfg map[string]interface{}
	yaml.Unmarshal(data, &cfg)

	models := cfg["models"].(map[string]interface{})
	model := models["test-model"].(map[string]interface{})
	caps, ok := model["capabilities"].([]interface{})
	if !ok {
		t.Fatal("expected capabilities field in merged model")
	}
	if len(caps) != 2 {
		t.Errorf("expected 2 capabilities, got %d", len(caps))
	}
}
```

- [ ] **Step 2: Run test**

```bash
go test ./internal/hub/ -run TestMergeProviderRouting_PassesCapabilities -v
```

Expected: PASS — the existing code copies the entire model map, so `capabilities` should flow through without changes. If it fails, the merge loop needs to explicitly include `capabilities`.

- [ ] **Step 3: Commit**

```bash
git add internal/hub/provider_test.go
git commit -m "test(hub): verify capabilities pass through in MergeProviderRouting"
```

---

### Task 9: `agency provider add` CLI command

**Files:**
- Modify: `internal/cli/commands.go`
- Create: `internal/cli/provider_discovery.go`

- [ ] **Step 1: Create provider_discovery.go**

Create `internal/cli/provider_discovery.go`:

```go
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// discoveredModel represents a model found via the /models endpoint.
type discoveredModel struct {
	ID           string
	Capabilities []string
}

// discoverModels calls the OpenAI-compatible /models endpoint and returns
// discovered models with probed capabilities.
func discoverModels(ctx context.Context, baseURL string, credential string) ([]discoveredModel, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	modelsURL := strings.TrimRight(baseURL, "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, "GET", modelsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if credential != "" {
		req.Header.Set("Authorization", "Bearer "+credential)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list models at %s: %w", modelsURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("list models: HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("parse model list: %w", err)
	}

	var models []discoveredModel
	for _, m := range result.Data {
		caps := probeCapabilities(ctx, client, baseURL, m.ID, credential)
		models = append(models, discoveredModel{ID: m.ID, Capabilities: caps})
	}
	return models, nil
}

// probeCapabilities sends test requests to determine model capabilities.
// Returns discovered capabilities. Best-effort: defaults to ["streaming"].
func probeCapabilities(ctx context.Context, client *http.Client, baseURL, modelID, credential string) []string {
	caps := []string{"streaming"} // assumed universal

	// Probe tool support with a minimal tool call request
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	toolProbe := map[string]interface{}{
		"model": modelID,
		"messages": []map[string]string{
			{"role": "user", "content": "test"},
		},
		"tools": []map[string]interface{}{
			{
				"type": "function",
				"function": map[string]interface{}{
					"name":        "test_probe",
					"description": "capability probe",
					"parameters":  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
				},
			},
		},
		"max_tokens": 1,
	}

	body, _ := json.Marshal(toolProbe)
	url := strings.TrimRight(baseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(probeCtx, "POST", url, strings.NewReader(string(body)))
	if err != nil {
		return caps
	}
	req.Header.Set("Content-Type", "application/json")
	if credential != "" {
		req.Header.Set("Authorization", "Bearer "+credential)
	}

	resp, err := client.Do(req)
	if err != nil {
		return caps
	}
	defer resp.Body.Close()
	io.ReadAll(io.LimitReader(resp.Body, 1024)) // drain

	if resp.StatusCode == 200 {
		caps = append(caps, "tools")
	}

	return caps
}
```

- [ ] **Step 2: Add the `provider add` command**

In `internal/cli/commands.go`, find the `hubCmd()` function and add a new `provider` subcommand group. Add after the existing source management commands (around line 1780):

```go
	// ── Provider management ──

	providerCmd := &cobra.Command{Use: "provider", Short: "Provider management"}

	providerAddCmd := &cobra.Command{
		Use:   "add <name> <base-url>",
		Short: "Discover and configure a local or custom LLM provider",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			baseURL := args[1]
			credential, _ := cmd.Flags().GetString("credential")
			noProbe, _ := cmd.Flags().GetBool("no-probe")

			var credValue string
			if credential != "" {
				fmt.Printf("Enter API key for %s: ", credential)
				fmt.Scanln(&credValue)
			}

			if noProbe {
				fmt.Println("Skipping discovery. Writing skeleton config...")
				return writeProviderSkeleton(name, baseURL, credential)
			}

			fmt.Printf("Discovering models at %s...\n", baseURL)
			models, err := discoverModels(context.Background(), baseURL, credValue)
			if err != nil {
				return fmt.Errorf("discovery failed: %w\n\nTry --no-probe to skip discovery and write a skeleton config", err)
			}

			if len(models) == 0 {
				return fmt.Errorf("no models found at %s", baseURL)
			}

			// Display results
			fmt.Printf("\nFound %d models:\n\n", len(models))
			for _, m := range models {
				fmt.Printf("  %-30s %s\n", m.ID, strings.Join(m.Capabilities, ", "))
			}

			fmt.Printf("\nWrite to routing.local.yaml? [Y/n] ")
			var confirm string
			fmt.Scanln(&confirm)
			if confirm != "" && strings.ToLower(confirm) != "y" {
				fmt.Println("Cancelled.")
				return nil
			}

			if err := writeProviderConfig(name, baseURL, credential, models); err != nil {
				return err
			}

			// Store credential if provided
			if credential != "" && credValue != "" {
				c, err := requireGateway()
				if err == nil {
					c.Post("/api/v1/credentials", map[string]interface{}{
						"name":  credential,
						"value": credValue,
					})
				}
			}

			fmt.Printf("%s Provider %s configured with %d models\n", green.Render("✓"), bold.Render(name), len(models))
			return nil
		},
	}
	providerAddCmd.Flags().String("credential", "", "Credential env var name (e.g., CUSTOM_LLM_API_KEY)")
	providerAddCmd.Flags().Bool("no-probe", false, "Skip model discovery, write skeleton config")
	providerCmd.AddCommand(providerAddCmd)

	cmd.AddCommand(providerCmd)
```

- [ ] **Step 3: Implement writeProviderConfig and writeProviderSkeleton**

Add to `internal/cli/provider_discovery.go`:

```go
import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// writeProviderConfig writes discovered models to routing.local.yaml.
func writeProviderConfig(name, baseURL, credential string, models []discoveredModel) error {
	home, _ := os.UserHomeDir()
	localPath := filepath.Join(home, ".agency", "infrastructure", "routing.local.yaml")

	cfg := loadOrCreateLocalRouting(localPath)

	// Add provider
	providers := ensureMapIn(cfg, "providers")
	providerCfg := map[string]interface{}{
		"api_base":    baseURL,
		"auth_header": "Authorization",
		"auth_prefix": "Bearer ",
	}
	if credential != "" {
		providerCfg["auth_env"] = credential
	}
	providers[name] = providerCfg

	// Add models
	modelsCfg := ensureMapIn(cfg, "models")
	for _, m := range models {
		modelsCfg[m.ID] = map[string]interface{}{
			"provider":       name,
			"provider_model": m.ID,
			"capabilities":   m.Capabilities,
			"cost_per_mtok_in":  0,
			"cost_per_mtok_out": 0,
		}
	}

	return writeLocalRouting(localPath, cfg)
}

// writeProviderSkeleton writes a minimal config template for manual editing.
func writeProviderSkeleton(name, baseURL, credential string) error {
	home, _ := os.UserHomeDir()
	localPath := filepath.Join(home, ".agency", "infrastructure", "routing.local.yaml")

	cfg := loadOrCreateLocalRouting(localPath)

	providers := ensureMapIn(cfg, "providers")
	providerCfg := map[string]interface{}{
		"api_base":    baseURL,
		"auth_header": "Authorization",
		"auth_prefix": "Bearer ",
	}
	if credential != "" {
		providerCfg["auth_env"] = credential
	}
	providers[name] = providerCfg

	// Add a placeholder model
	modelsCfg := ensureMapIn(cfg, "models")
	modelsCfg["REPLACE-ME"] = map[string]interface{}{
		"provider":          name,
		"provider_model":    "REPLACE-ME",
		"capabilities":      []string{"streaming"},
		"cost_per_mtok_in":  0,
		"cost_per_mtok_out": 0,
	}

	if err := writeLocalRouting(localPath, cfg); err != nil {
		return err
	}
	fmt.Printf("Skeleton written to %s — edit to add your models\n", localPath)
	return nil
}

func loadOrCreateLocalRouting(path string) map[string]interface{} {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]interface{}{}
	}
	var cfg map[string]interface{}
	if yaml.Unmarshal(data, &cfg) != nil {
		return map[string]interface{}{}
	}
	return cfg
}

func writeLocalRouting(path string, cfg map[string]interface{}) error {
	os.MkdirAll(filepath.Dir(path), 0755)
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func ensureMapIn(parent map[string]interface{}, key string) map[string]interface{} {
	if m, ok := parent[key].(map[string]interface{}); ok {
		return m
	}
	m := map[string]interface{}{}
	parent[key] = m
	return m
}
```

- [ ] **Step 4: Build and verify**

```bash
go build ./cmd/gateway/
```

Expected: Clean build.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/provider_discovery.go internal/cli/commands.go
git commit -m "feat(cli): add 'agency provider add' with model discovery"
```

---

### Task 10: Update documentation

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Add provider adapter documentation to CLAUDE.md**

Add a bullet in the Key Rules section:

```
- **Model capabilities**: Every model in routing.yaml declares `capabilities: [tools, vision, streaming]`. The enforcer validates that the target model supports what the request needs (tools, vision, streaming). On mismatch, returns HTTP 422. Tier capabilities are the intersection of models in the tier — served to the body as `/config/tiers.json`. `agency provider add <name> <url>` discovers models from OpenAI-compatible endpoints.
```

- [ ] **Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: document model capabilities and provider add command"
```

---

### Task 11: Push and create PR

- [ ] **Step 1: Run all tests**

```bash
go test ./... 2>&1 | tail -10
cd images/enforcer && go test ./... 2>&1 | tail -10
```

Expected: All pass.

- [ ] **Step 2: Push**

```bash
git push origin feature/oci-hub-distribution
```

- [ ] **Step 3: Push agency-hub changes**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-hub
git push origin main
```
