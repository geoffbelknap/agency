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
