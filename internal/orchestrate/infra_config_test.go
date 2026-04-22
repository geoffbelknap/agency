package orchestrate

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestRepairDefaultRoutingCapabilitiesHydratesExistingConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "routing.yaml")
	if err := os.WriteFile(path, []byte(`version: "0.1"
providers:
  anthropic:
    api_base: https://api.anthropic.com/v1
models:
  claude-sonnet:
    provider: anthropic
    provider_model: claude-sonnet-4-20250514
  custom-model:
    provider: anthropic
    provider_model: custom
`), 0644); err != nil {
		t.Fatalf("write routing.yaml: %v", err)
	}

	if err := repairDefaultRoutingCapabilities(path); err != nil {
		t.Fatalf("repairDefaultRoutingCapabilities: %v", err)
	}

	var cfg map[string]interface{}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read repaired routing.yaml: %v", err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse repaired routing.yaml: %v", err)
	}
	models := cfg["models"].(map[string]interface{})
	sonnet := models["claude-sonnet"].(map[string]interface{})
	if !containsYAMLString(sonnet["capabilities"], "tools") {
		t.Fatalf("claude-sonnet capabilities not hydrated: %#v", sonnet["capabilities"])
	}
	if !containsYAMLString(sonnet["provider_tool_capabilities"], "provider-web-search") {
		t.Fatalf("claude-sonnet provider tool capabilities not hydrated: %#v", sonnet["provider_tool_capabilities"])
	}
	custom := models["custom-model"].(map[string]interface{})
	if _, ok := custom["provider_tool_capabilities"]; ok {
		t.Fatalf("custom model should not be modified: %#v", custom)
	}
}

func containsYAMLString(raw interface{}, want string) bool {
	items, ok := raw.([]interface{})
	if !ok {
		return false
	}
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
