package hub

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

const testProviderYAML = `
name: test-provider
version: "0.1"
routing:
  api_base: https://api.test-provider.com/v1
  auth_header: x-api-key
  auth_env: TEST_PROVIDER_KEY
  models:
    test-model-fast:
      context_window: 8192
      input_cost_per_1m: 0.25
      output_cost_per_1m: 1.25
    test-model-pro:
      context_window: 200000
      input_cost_per_1m: 3.00
      output_cost_per_1m: 15.00
  tiers:
    standard: test-model-fast
    premium: test-model-pro
`

const testProvider2YAML = `
name: other-provider
version: "0.1"
routing:
  api_base: https://api.other.com/v1
  auth_env: OTHER_KEY
  models:
    other-model:
      context_window: 4096
  tiers:
    standard: other-model
`

func TestMergeProviderRouting(t *testing.T) {
	home := t.TempDir()

	if err := MergeProviderRouting(home, "test-provider", []byte(testProviderYAML)); err != nil {
		t.Fatalf("MergeProviderRouting: %v", err)
	}

	routingPath := filepath.Join(home, "infrastructure", "routing.yaml")
	data, err := os.ReadFile(routingPath)
	if err != nil {
		t.Fatalf("read routing.yaml: %v", err)
	}

	var cfg map[string]interface{}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse routing.yaml: %v", err)
	}

	// Check provider entry
	providers, ok := cfg["providers"].(map[string]interface{})
	if !ok {
		t.Fatal("expected providers map in routing.yaml")
	}
	prov, ok := providers["test-provider"].(map[string]interface{})
	if !ok {
		t.Fatal("expected test-provider in providers")
	}
	if prov["api_base"] != "https://api.test-provider.com/v1" {
		t.Errorf("expected api_base, got: %v", prov["api_base"])
	}
	if prov["auth_env"] != "TEST_PROVIDER_KEY" {
		t.Errorf("expected auth_env, got: %v", prov["auth_env"])
	}

	// Check models
	models, ok := cfg["models"].(map[string]interface{})
	if !ok {
		t.Fatal("expected models map in routing.yaml")
	}
	fastModel, ok := models["test-model-fast"].(map[string]interface{})
	if !ok {
		t.Fatal("expected test-model-fast in models")
	}
	if fastModel["provider"] != "test-provider" {
		t.Errorf("expected provider=test-provider on model, got: %v", fastModel["provider"])
	}
	if _, ok := models["test-model-pro"]; !ok {
		t.Error("expected test-model-pro in models")
	}

	// Check tier entries
	tiers, ok := cfg["tiers"].(map[string]interface{})
	if !ok {
		t.Fatal("expected tiers map in routing.yaml")
	}
	standardEntries, ok := tiers["standard"].([]interface{})
	if !ok || len(standardEntries) == 0 {
		t.Fatal("expected tier entries for standard")
	}
	firstEntry, ok := standardEntries[0].(map[string]interface{})
	if !ok {
		t.Fatal("expected tier entry to be a map")
	}
	if firstEntry["model"] != "test-model-fast" {
		t.Errorf("expected model=test-model-fast in standard tier, got: %v", firstEntry["model"])
	}

	premiumEntries, ok := tiers["premium"].([]interface{})
	if !ok || len(premiumEntries) == 0 {
		t.Fatal("expected tier entries for premium")
	}
}

func TestRemoveProviderRouting(t *testing.T) {
	home := t.TempDir()

	// Install two providers
	if err := MergeProviderRouting(home, "test-provider", []byte(testProviderYAML)); err != nil {
		t.Fatalf("MergeProviderRouting test-provider: %v", err)
	}
	if err := MergeProviderRouting(home, "other-provider", []byte(testProvider2YAML)); err != nil {
		t.Fatalf("MergeProviderRouting other-provider: %v", err)
	}

	// Remove the first provider
	if err := RemoveProviderRouting(home, "test-provider"); err != nil {
		t.Fatalf("RemoveProviderRouting: %v", err)
	}

	routingPath := filepath.Join(home, "infrastructure", "routing.yaml")
	data, err := os.ReadFile(routingPath)
	if err != nil {
		t.Fatalf("read routing.yaml: %v", err)
	}

	var cfg map[string]interface{}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse routing.yaml: %v", err)
	}

	// test-provider should be gone
	providers := cfg["providers"].(map[string]interface{})
	if _, exists := providers["test-provider"]; exists {
		t.Error("test-provider should have been removed from providers")
	}
	// other-provider should remain
	if _, exists := providers["other-provider"]; !exists {
		t.Error("other-provider should still be in providers")
	}

	// test-provider models should be gone
	models := cfg["models"].(map[string]interface{})
	if _, exists := models["test-model-fast"]; exists {
		t.Error("test-model-fast should have been removed")
	}
	if _, exists := models["test-model-pro"]; exists {
		t.Error("test-model-pro should have been removed")
	}
	// other-provider models should remain
	if _, exists := models["other-model"]; !exists {
		t.Error("other-model should still be in models")
	}

	// Tier entries for removed models should be gone; other-model entry should remain
	tiers := cfg["tiers"].(map[string]interface{})
	standardEntries, ok := tiers["standard"].([]interface{})
	if !ok {
		t.Fatal("expected tiers.standard to be a list")
	}
	for _, e := range standardEntries {
		entry, ok := e.(map[string]interface{})
		if !ok {
			continue
		}
		if entry["model"] == "test-model-fast" {
			t.Error("test-model-fast tier entry should have been removed")
		}
	}
	// other-model tier entry should be present
	found := false
	for _, e := range standardEntries {
		entry, ok := e.(map[string]interface{})
		if !ok {
			continue
		}
		if entry["model"] == "other-model" {
			found = true
		}
	}
	if !found {
		t.Error("other-model tier entry should still be present in standard tier")
	}
}

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
      provider_tool_capabilities: [provider-web-search]
      provider_tool_pricing:
        provider-web-search:
          unit: search
          usd_per_unit: 0.01
          source: test
          confidence: exact
      cost_per_mtok_in: 1.0
      cost_per_mtok_out: 2.0
  tiers:
    standard: test-model
`)

	err := MergeProviderRouting(home, "test-provider", providerYAML)
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(home, "infrastructure", "routing.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	var cfg map[string]interface{}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}

	models, ok := cfg["models"].(map[string]interface{})
	if !ok {
		t.Fatal("expected models map in routing.yaml")
	}
	model, ok := models["test-model"].(map[string]interface{})
	if !ok {
		t.Fatal("expected test-model entry in models")
	}
	caps, ok := model["capabilities"].([]interface{})
	if !ok {
		t.Fatal("expected capabilities field in merged model")
	}
	if len(caps) != 2 {
		t.Errorf("expected 2 capabilities, got %d", len(caps))
	}
	providerToolCaps, ok := model["provider_tool_capabilities"].([]interface{})
	if !ok {
		t.Fatal("expected provider_tool_capabilities field in merged model")
	}
	if len(providerToolCaps) != 1 || providerToolCaps[0] != "provider-web-search" {
		t.Errorf("unexpected provider tool capabilities: %#v", providerToolCaps)
	}
	providerToolPricing, ok := model["provider_tool_pricing"].(map[string]interface{})
	if !ok {
		t.Fatal("expected provider_tool_pricing field in merged model")
	}
	webSearchPrice, ok := providerToolPricing["provider-web-search"].(map[string]interface{})
	if !ok {
		t.Fatal("expected provider-web-search pricing")
	}
	if webSearchPrice["unit"] != "search" || webSearchPrice["confidence"] != "exact" {
		t.Errorf("unexpected provider tool pricing: %#v", webSearchPrice)
	}

	// Verify the actual capability values
	expected := map[string]bool{"tools": true, "streaming": true}
	for _, c := range caps {
		s, ok := c.(string)
		if !ok {
			t.Errorf("expected string capability, got %T", c)
			continue
		}
		if !expected[s] {
			t.Errorf("unexpected capability: %s", s)
		}
	}
}

func TestMergeProviderInto(t *testing.T) {
	// Base config: provider-a, standard model, standard tier with one entry.
	cfg := map[string]interface{}{
		"version": "0.1",
		"providers": map[string]interface{}{
			"provider-a": map[string]interface{}{
				"api_base": "https://provider-a.example.com",
				"auth_env": "PROVIDER_A_API_KEY",
			},
		},
		"models": map[string]interface{}{
			"standard": map[string]interface{}{
				"provider":     "provider-a",
				"capabilities": []interface{}{"tools", "vision"},
			},
		},
		"tiers": map[string]interface{}{
			"standard": []interface{}{
				map[string]interface{}{
					"model":      "standard",
					"preference": 0,
				},
			},
		},
	}

	providerYAML := `
routing:
  api_base: https://api.together.xyz/v1
  auth_env: TOGETHER_API_KEY
  models:
    together-llama:
      capabilities:
        - tools
  tiers:
    standard: together-llama
`

	err := mergeProviderInto(cfg, "together-ai", []byte(providerYAML))
	if err != nil {
		t.Fatalf("mergeProviderInto returned error: %v", err)
	}

	// Existing provider untouched.
	providers := cfg["providers"].(map[string]interface{})
	providerA, ok := providers["provider-a"].(map[string]interface{})
	if !ok {
		t.Fatal("provider-a missing after merge")
	}
	if providerA["api_base"] != "https://provider-a.example.com" {
		t.Errorf("provider-a api_base changed: %v", providerA["api_base"])
	}

	// New provider added with correct api_base.
	together, ok := providers["together-ai"].(map[string]interface{})
	if !ok {
		t.Fatal("together-ai provider not added")
	}
	if together["api_base"] != "https://api.together.xyz/v1" {
		t.Errorf("together-ai api_base = %v, want https://api.together.xyz/v1", together["api_base"])
	}

	// New model stamped with provider name.
	models := cfg["models"].(map[string]interface{})
	llama, ok := models["together-llama"].(map[string]interface{})
	if !ok {
		t.Fatal("together-llama model not added")
	}
	if llama["provider"] != "together-ai" {
		t.Errorf("together-llama provider = %v, want together-ai", llama["provider"])
	}

	// Existing model untouched.
	standard, ok := models["standard"].(map[string]interface{})
	if !ok {
		t.Fatal("standard model missing after merge")
	}
	if standard["provider"] != "provider-a" {
		t.Errorf("standard provider changed: %v", standard["provider"])
	}

	// Tier entries appended (should have 2 entries in standard tier).
	tiers := cfg["tiers"].(map[string]interface{})
	standardTier, ok := tiers["standard"].([]interface{})
	if !ok {
		t.Fatal("standard tier missing or wrong type")
	}
	if len(standardTier) != 2 {
		t.Fatalf("standard tier has %d entries, want 2", len(standardTier))
	}

	// First entry should be the original.
	entry0 := standardTier[0].(map[string]interface{})
	if entry0["model"] != "standard" {
		t.Errorf("tier entry 0 model = %v, want standard", entry0["model"])
	}

	// Second entry should be the new one.
	entry1 := standardTier[1].(map[string]interface{})
	if entry1["model"] != "together-llama" {
		t.Errorf("tier entry 1 model = %v, want together-llama", entry1["model"])
	}
	if entry1["preference"] != 1 {
		t.Errorf("tier entry 1 preference = %v, want 1", entry1["preference"])
	}
}

func TestMergeProviderIntoNoRoutingBlock(t *testing.T) {
	cfg := map[string]interface{}{
		"version":   "0.1",
		"providers": map[string]interface{}{},
		"models":    map[string]interface{}{},
		"tiers":     map[string]interface{}{},
	}

	providerYAML := `
name: some-provider
credential:
  env_var: SOME_KEY
`

	err := mergeProviderInto(cfg, "some-provider", []byte(providerYAML))
	if err != nil {
		t.Fatalf("mergeProviderInto returned error: %v", err)
	}

	// Should be a no-op: no providers, models, or tiers added.
	providers := cfg["providers"].(map[string]interface{})
	if len(providers) != 0 {
		t.Errorf("providers should be empty, got %d entries", len(providers))
	}
	models := cfg["models"].(map[string]interface{})
	if len(models) != 0 {
		t.Errorf("models should be empty, got %d entries", len(models))
	}
	tiers := cfg["tiers"].(map[string]interface{})
	if len(tiers) != 0 {
		t.Errorf("tiers should be empty, got %d entries", len(tiers))
	}
}

func TestMergeProviderRoutingUnchangedBehavior(t *testing.T) {
	home := t.TempDir()

	providerYAML := `
routing:
  api_base: https://provider-a.example.com/v1
  auth_env: PROVIDER_A_API_KEY
  models:
    provider-a-standard:
      capabilities:
        - tools
        - vision
  tiers:
    standard: provider-a-standard
credential:
  env_var: PROVIDER_A_API_KEY
`

	err := MergeProviderRouting(home, "provider-a", []byte(providerYAML))
	if err != nil {
		t.Fatalf("MergeProviderRouting returned error: %v", err)
	}

	// Read back routing.yaml and verify.
	routingPath := filepath.Join(home, "infrastructure", "routing.yaml")
	data, err := os.ReadFile(routingPath)
	if err != nil {
		t.Fatalf("failed to read routing.yaml: %v", err)
	}

	var cfg map[string]interface{}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("failed to parse routing.yaml: %v", err)
	}

	// Provider present.
	providers, ok := cfg["providers"].(map[string]interface{})
	if !ok {
		t.Fatal("providers section missing")
	}
	providerA, ok := providers["provider-a"].(map[string]interface{})
	if !ok {
		t.Fatal("provider-a not found")
	}
	if providerA["api_base"] != "https://provider-a.example.com/v1" {
		t.Errorf("provider-a api_base = %v, want https://provider-a.example.com/v1", providerA["api_base"])
	}

	// Model present with provider stamp.
	models, ok := cfg["models"].(map[string]interface{})
	if !ok {
		t.Fatal("models section missing")
	}
	model, ok := models["provider-a-standard"].(map[string]interface{})
	if !ok {
		t.Fatal("provider-a-standard model not found")
	}
	if model["provider"] != "provider-a" {
		t.Errorf("provider-a-standard provider = %v, want provider-a", model["provider"])
	}

	// Tier entry present.
	tiers, ok := cfg["tiers"].(map[string]interface{})
	if !ok {
		t.Fatal("tiers section missing")
	}
	standardTier, ok := tiers["standard"].([]interface{})
	if !ok {
		t.Fatal("standard tier missing or wrong type")
	}
	if len(standardTier) != 1 {
		t.Fatalf("standard tier has %d entries, want 1", len(standardTier))
	}
	entry := standardTier[0].(map[string]interface{})
	if entry["model"] != "provider-a-standard" {
		t.Errorf("tier entry model = %v, want provider-a-standard", entry["model"])
	}
}
