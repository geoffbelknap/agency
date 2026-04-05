# Hybrid Model Routing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The enforcer reroutes LLM calls to cheaper tiers based on configurable rules (cost source, message patterns, step index) — transparently to the agent.

**Architecture:** Routing rules are evaluated in the enforcer's `ServeHTTP` before model resolution. Rules match on request metadata (X-Agency-Cost-Source header, message content, step index) and specify a target tier. The enforcer's RoutingConfig gains tiers and a rules evaluator. Rerouting is logged in audit entries for economics visibility.

**Tech Stack:** Go (enforcer)

**Spec:** `docs/specs/hybrid-model-routing.md`

---

### Task 1: Add Tiers to enforcer RoutingConfig

**Files:**
- Modify: `images/enforcer/config.go`
- Test: `images/enforcer/config_test.go`

- [ ] **Step 1: Write tests**

Add to `images/enforcer/config_test.go`:

```go
func TestRoutingConfig_ResolveTier(t *testing.T) {
	rc := &RoutingConfig{
		Providers: map[string]Provider{
			"anthropic": {APIBase: "https://api.anthropic.com/v1"},
			"ollama":    {APIBase: "http://localhost:11434/v1"},
		},
		Models: map[string]Model{
			"claude-sonnet": {Provider: "anthropic", ProviderModel: "claude-sonnet-4"},
			"llama-8b":      {Provider: "ollama", ProviderModel: "llama3.1:8b"},
		},
		Tiers: TierConfig{
			Standard: []TierEntry{{Model: "claude-sonnet", Preference: 0}},
			Mini:     []TierEntry{{Model: "llama-8b", Preference: 0}},
		},
	}

	model, ok := rc.ResolveTier("standard")
	if !ok || model != "claude-sonnet" {
		t.Errorf("expected claude-sonnet, got %s (ok=%v)", model, ok)
	}

	model, ok = rc.ResolveTier("mini")
	if !ok || model != "llama-8b" {
		t.Errorf("expected llama-8b, got %s (ok=%v)", model, ok)
	}

	_, ok = rc.ResolveTier("frontier")
	if ok {
		t.Error("expected false for empty tier")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd images/enforcer && go test -run TestRoutingConfig_ResolveTier -v
```

- [ ] **Step 3: Add tier types and ResolveTier to enforcer config**

Add to `images/enforcer/config.go`:

```go
// TierEntry is a model reference within a tier, with an ordering preference.
type TierEntry struct {
	Model      string `yaml:"model"`
	Preference int    `yaml:"preference"`
}

// TierConfig holds the ranked model lists for each routing tier.
type TierConfig struct {
	Frontier []TierEntry `yaml:"frontier"`
	Standard []TierEntry `yaml:"standard"`
	Fast     []TierEntry `yaml:"fast"`
	Mini     []TierEntry `yaml:"mini"`
	Nano     []TierEntry `yaml:"nano"`
	Batch    []TierEntry `yaml:"batch"`
}
```

Add `Tiers` to `RoutingConfig`:

```go
type RoutingConfig struct {
	Version      string              `yaml:"version"`
	Providers    map[string]Provider `yaml:"providers"`
	Models       map[string]Model    `yaml:"models"`
	Tiers        TierConfig          `yaml:"tiers"`
	RoutingRules []RoutingRule       `yaml:"routing_rules"`
	Settings     Settings            `yaml:"settings"`
}
```

Add `ResolveTier`:

```go
// ResolveTier returns the best model alias for a tier (lowest preference value).
// Returns ("", false) if the tier is empty or unknown.
func (rc *RoutingConfig) ResolveTier(tier string) (string, bool) {
	var entries []TierEntry
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
		return "", false
	}
	if len(entries) == 0 {
		return "", false
	}
	best := entries[0]
	for _, e := range entries[1:] {
		if e.Preference < best.Preference {
			best = e
		}
	}
	if _, ok := rc.Models[best.Model]; !ok {
		return "", false
	}
	return best.Model, true
}
```

- [ ] **Step 4: Run tests**

```bash
cd images/enforcer && go test -run TestRoutingConfig_ResolveTier -v
```

- [ ] **Step 5: Commit**

```bash
git add images/enforcer/config.go images/enforcer/config_test.go
git commit -m "feat(enforcer): add TierConfig and ResolveTier to RoutingConfig"
```

---

### Task 2: Add routing rules parser and evaluator

**Files:**
- Create: `images/enforcer/routing_rules.go`
- Create: `images/enforcer/routing_rules_test.go`

- [ ] **Step 1: Write tests**

Create `images/enforcer/routing_rules_test.go`:

```go
package main

import "testing"

func TestEvaluateRules_CostSource(t *testing.T) {
	rules := []RoutingRule{
		{Match: RuleMatch{CostSource: "reflection"}, Tier: "fast"},
		{Match: RuleMatch{CostSource: "memory_capture"}, Tier: "mini"},
	}

	ctx := RuleContext{CostSource: "reflection"}
	tier, matched := evaluateRules(rules, ctx)
	if !matched || tier != "fast" {
		t.Errorf("expected fast, got %s (matched=%v)", tier, matched)
	}

	ctx = RuleContext{CostSource: "agent_task"}
	_, matched = evaluateRules(rules, ctx)
	if matched {
		t.Error("expected no match for agent_task")
	}
}

func TestEvaluateRules_StepIndex(t *testing.T) {
	rules := []RoutingRule{
		{Match: RuleMatch{StepIndexGT: intPtr(10)}, Tier: "fast"},
	}

	ctx := RuleContext{StepIndex: 15}
	tier, matched := evaluateRules(rules, ctx)
	if !matched || tier != "fast" {
		t.Errorf("expected fast, got %s", tier)
	}

	ctx = RuleContext{StepIndex: 5}
	_, matched = evaluateRules(rules, ctx)
	if matched {
		t.Error("expected no match for step 5")
	}
}

func TestEvaluateRules_ToolOutputTokens(t *testing.T) {
	rules := []RoutingRule{
		{Match: RuleMatch{LastMessageRole: "tool", ToolOutputTokensGT: intPtr(5000)}, Tier: "fast"},
	}

	ctx := RuleContext{LastMessageRole: "tool", ToolOutputTokensEst: 8000}
	tier, matched := evaluateRules(rules, ctx)
	if !matched || tier != "fast" {
		t.Errorf("expected fast, got %s", tier)
	}
}

func TestEvaluateRules_FirstMatchWins(t *testing.T) {
	rules := []RoutingRule{
		{Match: RuleMatch{CostSource: "reflection"}, Tier: "mini"},
		{Match: RuleMatch{CostSource: "reflection"}, Tier: "nano"},
	}

	ctx := RuleContext{CostSource: "reflection"}
	tier, _ := evaluateRules(rules, ctx)
	if tier != "mini" {
		t.Errorf("expected mini (first match), got %s", tier)
	}
}

func TestEvaluateRules_ModelTierHint(t *testing.T) {
	rules := []RoutingRule{
		{Match: RuleMatch{ModelTierHint: "fast"}, Tier: "fast"},
	}

	ctx := RuleContext{ModelTierHint: "fast"}
	tier, matched := evaluateRules(rules, ctx)
	if !matched || tier != "fast" {
		t.Errorf("expected fast, got %s", tier)
	}
}

func intPtr(i int) *int { return &i }
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd images/enforcer && go test -run TestEvaluateRules -v
```

- [ ] **Step 3: Implement routing rules**

Create `images/enforcer/routing_rules.go`:

```go
package main

// RoutingRule defines a condition → tier reroute.
type RoutingRule struct {
	Match  RuleMatch `yaml:"match"`
	Tier   string    `yaml:"tier"`
	Reason string    `yaml:"reason,omitempty"`
}

// RuleMatch defines the conditions for a routing rule to fire.
// All non-zero fields must match (AND logic).
type RuleMatch struct {
	CostSource         string `yaml:"cost_source,omitempty"`
	LastMessageRole    string `yaml:"last_message_role,omitempty"`
	ToolOutputTokensGT *int   `yaml:"tool_output_tokens_gt,omitempty"`
	HasToolCalls       *bool  `yaml:"has_tool_calls,omitempty"`
	StepIndexGT        *int   `yaml:"step_index_gt,omitempty"`
	ModelTierHint      string `yaml:"model_override,omitempty"`
}

// RuleContext holds the request-derived values to match against rules.
type RuleContext struct {
	CostSource          string
	LastMessageRole     string
	ToolOutputTokensEst int
	HasToolCalls        bool
	StepIndex           int
	ModelTierHint       string
}

// evaluateRules returns the target tier from the first matching rule.
// Returns ("", false) if no rule matches.
func evaluateRules(rules []RoutingRule, ctx RuleContext) (string, bool) {
	for _, rule := range rules {
		if matchesRule(rule.Match, ctx) {
			return rule.Tier, true
		}
	}
	return "", false
}

// matchesRule returns true if all non-zero fields in the match spec
// are satisfied by the context. Empty/nil fields are ignored (wildcard).
func matchesRule(m RuleMatch, ctx RuleContext) bool {
	if m.CostSource != "" && m.CostSource != ctx.CostSource {
		return false
	}
	if m.LastMessageRole != "" && m.LastMessageRole != ctx.LastMessageRole {
		return false
	}
	if m.ToolOutputTokensGT != nil && ctx.ToolOutputTokensEst <= *m.ToolOutputTokensGT {
		return false
	}
	if m.HasToolCalls != nil && *m.HasToolCalls != ctx.HasToolCalls {
		return false
	}
	if m.StepIndexGT != nil && ctx.StepIndex <= *m.StepIndexGT {
		return false
	}
	if m.ModelTierHint != "" && m.ModelTierHint != ctx.ModelTierHint {
		return false
	}
	return true
}

// buildRuleContext extracts rule context from a parsed request.
func buildRuleContext(reqBody map[string]interface{}, costSource string, stepIndex int, modelTierHint string) RuleContext {
	ctx := RuleContext{
		CostSource:    costSource,
		StepIndex:     stepIndex,
		ModelTierHint: modelTierHint,
	}

	// Check for tool definitions
	if tools, ok := reqBody["tools"].([]interface{}); ok && len(tools) > 0 {
		ctx.HasToolCalls = true
	}

	// Get last message role and estimate tool output tokens
	if messages, ok := reqBody["messages"].([]interface{}); ok && len(messages) > 0 {
		last, ok := messages[len(messages)-1].(map[string]interface{})
		if ok {
			ctx.LastMessageRole, _ = last["role"].(string)
			if ctx.LastMessageRole == "tool" {
				content, _ := last["content"].(string)
				ctx.ToolOutputTokensEst = len(content) / 4 // rough token estimate
			}
		}
	}

	return ctx
}
```

- [ ] **Step 4: Run tests**

```bash
cd images/enforcer && go test -run TestEvaluateRules -v
```

- [ ] **Step 5: Run all enforcer tests**

```bash
cd images/enforcer && go test ./... -v 2>&1 | tail -10
```

- [ ] **Step 6: Commit**

```bash
git add images/enforcer/routing_rules.go images/enforcer/routing_rules_test.go
git commit -m "feat(enforcer): add routing rules parser and evaluator"
```

---

### Task 3: Wire reroute logic into LLM handler

**Files:**
- Modify: `images/enforcer/llm.go` (ServeHTTP, around line 208-213)
- Modify: `images/enforcer/audit.go` (add new audit fields)

- [ ] **Step 1: Add reroute audit fields**

In `images/enforcer/audit.go`, add to `AuditEntry` (before `Extra`):

```go
	ReroutedFrom  string `json:"rerouted_from,omitempty"`
	RerouteRule   string `json:"reroute_rule,omitempty"`
	TargetTier    string `json:"target_tier,omitempty"`
	ModelTierHint string `json:"model_tier_hint,omitempty"`
```

- [ ] **Step 2: Add reroute logic in ServeHTTP**

In `llm.go`, after extracting `modelAlias` (line 209) and before `ResolveModel` call, insert:

```go
	// Read routing headers
	costSource := r.Header.Get("X-Agency-Cost-Source")
	modelTierHint := r.Header.Get("X-Agency-Model-Tier")

	// Evaluate routing rules for potential reroute
	var reroutedFrom, rerouteRule, targetTier string
	if len(lh.routing.RoutingRules) > 0 {
		ctx := buildRuleContext(reqBody, costSource, stepIndex, modelTierHint)
		if tier, matched := evaluateRules(lh.routing.RoutingRules, ctx); matched {
			if newModel, ok := lh.routing.ResolveTier(tier); ok {
				reroutedFrom = modelAlias
				rerouteRule = tier
				targetTier = tier
				modelAlias = newModel
			}
		}
	}
```

Then pass the reroute fields to all four relay functions' audit entries:

```go
	ReroutedFrom:  reroutedFrom,
	RerouteRule:   rerouteRule,
	TargetTier:    targetTier,
	ModelTierHint: modelTierHint,
```

- [ ] **Step 3: Run all enforcer tests**

```bash
cd images/enforcer && go test ./... -v 2>&1 | tail -10
```

- [ ] **Step 4: Commit**

```bash
git add images/enforcer/llm.go images/enforcer/audit.go
git commit -m "feat(enforcer): wire routing rules reroute into LLM handler"
```

---

### Task 4: Update gateway to include tiers and rules in enforcer config

**Files:**
- Modify: `internal/orchestrate/enforcer.go` (where routing.yaml is mounted)

- [ ] **Step 1: Verify routing.yaml already includes tiers**

The gateway generates `~/.agency/infrastructure/routing.yaml` from hub provider installations. Check if the `tiers` section is already included. Read `internal/hub/provider.go` — the `MergeProviderRouting` function already writes tiers.

If tiers are already in routing.yaml and the enforcer mounts it, the enforcer will parse them automatically after Task 1's changes. Verify:

```bash
grep -A 5 "tiers:" ~/.agency/infrastructure/routing.yaml 2>/dev/null || echo "no tiers section"
```

If tiers are present, no gateway changes needed — the enforcer's YAML parser will read them.

- [ ] **Step 2: Add default routing rules**

The routing rules need to be in routing.yaml (or a separate config file). The simplest approach: add a `routing_rules` section to the existing routing.yaml. Since this is hub-managed, add default rules to the hub's pricing/routing.yaml or let operators configure them in routing.local.yaml.

For the MVP, add a helper that writes default routing rules to routing.local.yaml if none exist:

In `internal/config/init.go`, in the `RunInit` function, after writing config.yaml, add a seed for routing rules:

```go
// Seed default routing rules if routing.local.yaml doesn't exist
localPath := filepath.Join(home, ".agency", "infrastructure", "routing.local.yaml")
if _, err := os.Stat(localPath); os.IsNotExist(err) {
    defaultRules := `# Routing rules — first match wins. Remove or edit to customize.
routing_rules:
  - match:
      cost_source: memory_capture
    tier: mini
    reason: "Memory capture is structured extraction"
  - match:
      cost_source: consolidation
    tier: fast
    reason: "Consolidation is summarization"
  - match:
      cost_source: context_summary
    tier: mini
    reason: "Context summarization is compression"
  - match:
      cost_source: evaluation
    tier: fast
    reason: "Success criteria evaluation is checklist comparison"
`
    os.WriteFile(localPath, []byte(defaultRules), 0644)
}
```

- [ ] **Step 3: Verify the enforcer reads routing.local.yaml overlay**

Check if the gateway merges routing.local.yaml with routing.yaml before mounting into the enforcer. If it does, routing rules from local.yaml will be available. If not, the enforcer needs to read both files.

Read `internal/orchestrate/enforcer.go` to see what files are mounted.

- [ ] **Step 4: Build and test**

```bash
go build ./...
cd images/enforcer && go test ./... -v 2>&1 | tail -10
```

- [ ] **Step 5: Commit**

```bash
git add internal/config/init.go internal/orchestrate/
git commit -m "feat: seed default routing rules in routing.local.yaml"
```

---

### Task 5: Documentation and push

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Add routing rules documentation**

Add to CLAUDE.md Key Rules:

```
- **Hybrid model routing**: The enforcer reroutes LLM calls to cheaper tiers based on `routing_rules` in routing config. Rules match on `X-Agency-Cost-Source` header (reflection→fast, memory_capture→mini, etc.), message patterns, and step index. First match wins. Default rules seeded in `routing.local.yaml`. Reroutes are transparent to the agent and logged in audit entries (`ReroutedFrom`, `TargetTier`). Body can hint via `X-Agency-Model-Tier` header (preference, not instruction — enforcer decides).
```

- [ ] **Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: document hybrid model routing rules"
```

---

### Task 6: Push and create PR

- [ ] **Step 1: Run all tests**

```bash
go test ./... 2>&1 | tail -10
cd images/enforcer && go test ./... 2>&1 | tail -10
```

- [ ] **Step 2: Create branch and push**

```bash
git checkout -b feature/hybrid-model-routing
git push -u origin feature/hybrid-model-routing
```

- [ ] **Step 3: Create PR**

```bash
gh pr create --title "feat: hybrid model routing — enforcer-side reroute rules" --body "..."
```
