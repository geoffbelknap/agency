package hub

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestSourceUpdateEmptyWhenNoChange(t *testing.T) {
	su := SourceUpdate{Name: "test", OldCommit: "abc1234", NewCommit: "abc1234"}
	if su.OldCommit != su.NewCommit {
		t.Fatal("expected same commit")
	}
	if su.CommitCount != 0 {
		t.Fatal("expected 0 commits")
	}
}

func TestOutdatedDetectsVersionDiff(t *testing.T) {
	home := t.TempDir()
	mgr := NewManager(home)

	// Create a fake installed instance with version 0.1.0
	inst, err := mgr.Registry.Create("test-connector", "connector", "default/test-connector")
	if err != nil {
		t.Fatal(err)
	}
	mgr.Registry.SetVersion(inst.Name, "0.1.0")

	// Write a fake component YAML in hub-cache with version 0.2.0
	cacheDir := filepath.Join(home, "hub-cache", "default", "connectors", "test-connector")
	os.MkdirAll(cacheDir, 0755)
	os.WriteFile(filepath.Join(cacheDir, "connector.yaml"), []byte("name: test-connector\nversion: \"0.2.0\"\n"), 0644)

	// Write hub config
	os.MkdirAll(home, 0755)
	os.WriteFile(filepath.Join(home, "config.yaml"), []byte("hub:\n  sources:\n    - name: default\n      url: https://example.com\n"), 0644)

	upgrades := mgr.Outdated()
	found := false
	for _, u := range upgrades {
		if u.Name == "test-connector" && u.InstalledVersion == "0.1.0" && u.AvailableVersion == "0.2.0" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected test-connector upgrade, got: %+v", upgrades)
	}
}

func TestUpdateReturnsReport(t *testing.T) {
	home := t.TempDir()
	mgr := NewManager(home)

	// No sources configured — should return empty report, no error
	os.MkdirAll(home, 0755)
	os.WriteFile(filepath.Join(home, "config.yaml"), []byte("hub:\n  sources: []\n"), 0644)

	report, err := mgr.Update()
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Sources) != 0 {
		t.Fatalf("expected 0 sources, got %d", len(report.Sources))
	}
}

func TestUpgradeAllSyncsManagedFiles(t *testing.T) {
	home := t.TempDir()
	mgr := NewManager(home)

	// Set up hub cache with an ontology file
	cacheDir := filepath.Join(home, "hub-cache", "default")
	os.MkdirAll(filepath.Join(cacheDir, "ontology"), 0755)
	os.WriteFile(filepath.Join(cacheDir, "ontology", "base-ontology.yaml"),
		[]byte("version: v2\nentity_types:\n  Host: {}\n  Software: {}\n"), 0644)

	// Set up local ontology (older version)
	os.MkdirAll(filepath.Join(home, "knowledge"), 0755)
	os.WriteFile(filepath.Join(home, "knowledge", "base-ontology.yaml"),
		[]byte("version: v1\nentity_types:\n  Host: {}\n"), 0644)

	// Config
	os.WriteFile(filepath.Join(home, "config.yaml"),
		[]byte("hub:\n  sources:\n    - name: default\n      url: https://example.com\n"), 0644)

	report, err := mgr.Upgrade(nil)
	if err != nil {
		t.Fatal(err)
	}

	foundOntology := false
	for _, f := range report.Files {
		if f.Category == "ontology" && f.Status == "upgraded" {
			foundOntology = true
		}
	}
	if !foundOntology {
		t.Fatalf("expected ontology upgrade, got: %+v", report.Files)
	}
}

func TestUpgradeSpecificComponent(t *testing.T) {
	home := t.TempDir()
	mgr := NewManager(home)

	// Create installed instance at version 0.1.0
	inst, _ := mgr.Registry.Create("test-conn", "connector", "default/test-conn")
	mgr.Registry.SetVersion(inst.Name, "0.1.0")

	// Write old YAML in instance dir
	instDir := mgr.Registry.InstanceDir(inst.Name)
	os.WriteFile(filepath.Join(instDir, "connector.yaml"),
		[]byte("name: test-conn\nversion: \"0.1.0\"\n"), 0644)

	// Write new version in hub cache
	cacheDir := filepath.Join(home, "hub-cache", "default", "connectors", "test-conn")
	os.MkdirAll(cacheDir, 0755)
	os.WriteFile(filepath.Join(cacheDir, "connector.yaml"),
		[]byte("name: test-conn\nversion: \"0.2.0\"\n"), 0644)

	// Config
	os.WriteFile(filepath.Join(home, "config.yaml"),
		[]byte("hub:\n  sources:\n    - name: default\n      url: https://example.com\n"), 0644)

	report, err := mgr.Upgrade([]string{"test-conn"})
	if err != nil {
		t.Fatal(err)
	}

	if len(report.Components) != 1 {
		t.Fatalf("expected 1 component, got %d", len(report.Components))
	}
	cu := report.Components[0]
	if cu.Status != "upgraded" || cu.OldVersion != "0.1.0" || cu.NewVersion != "0.2.0" {
		t.Fatalf("unexpected component upgrade: %+v", cu)
	}

	// Verify version was updated in registry
	updated := mgr.Registry.Resolve("test-conn")
	if updated.Version != "0.2.0" {
		t.Fatalf("expected version 0.2.0, got %s", updated.Version)
	}
}

func TestOutdatedNoChangeWhenVersionsMatch(t *testing.T) {
	home := t.TempDir()
	mgr := NewManager(home)

	inst, err := mgr.Registry.Create("test-connector", "connector", "default/test-connector")
	if err != nil {
		t.Fatal(err)
	}
	mgr.Registry.SetVersion(inst.Name, "0.1.0")

	cacheDir := filepath.Join(home, "hub-cache", "default", "connectors", "test-connector")
	os.MkdirAll(cacheDir, 0755)
	os.WriteFile(filepath.Join(cacheDir, "connector.yaml"), []byte("name: test-connector\nversion: \"0.1.0\"\n"), 0644)

	os.WriteFile(filepath.Join(home, "config.yaml"), []byte("hub:\n  sources:\n    - name: default\n      url: https://example.com\n"), 0644)

	upgrades := mgr.Outdated()
	if len(upgrades) != 0 {
		t.Fatalf("expected no upgrades, got: %+v", upgrades)
	}
}

func TestUpgradePreservesInstalledProviders(t *testing.T) {
	home := t.TempDir()
	mgr := NewManager(home)

	// Hub cache with Anthropic + OpenAI defaults
	hubRoutingYAML := `version: "0.1"
providers:
  anthropic:
    api_base: https://api.anthropic.com
    auth_env: ANTHROPIC_API_KEY
  openai:
    api_base: https://api.openai.com
    auth_env: OPENAI_API_KEY
models:
  claude-sonnet:
    provider: anthropic
    capabilities: [tools, vision, streaming]
  gpt-4o:
    provider: openai
    capabilities: [tools, vision, streaming]
tiers:
  standard:
    - model: claude-sonnet
      preference: 0
    - model: gpt-4o
      preference: 1
settings:
  default_tier: standard
  tier_strategy: best_effort
`
	cacheDir := filepath.Join(home, "hub-cache", "default")
	os.MkdirAll(filepath.Join(cacheDir, "pricing"), 0755)
	os.WriteFile(filepath.Join(cacheDir, "pricing", "routing.yaml"), []byte(hubRoutingYAML), 0644)

	// Ontology in cache (upgrade syncs ontology too)
	os.MkdirAll(filepath.Join(cacheDir, "ontology"), 0755)
	os.WriteFile(filepath.Join(cacheDir, "ontology", "base-ontology.yaml"),
		[]byte("version: v2\nentity_types:\n  Host: {}\n"), 0644)

	// Config with hub sources
	os.WriteFile(filepath.Join(home, "config.yaml"),
		[]byte("hub:\n  sources:\n    - name: default\n      url: https://example.com\n"), 0644)

	// Write the hub base routing.yaml to infrastructure/
	os.MkdirAll(filepath.Join(home, "infrastructure"), 0755)
	os.WriteFile(filepath.Join(home, "infrastructure", "routing.yaml"), []byte(hubRoutingYAML), 0644)

	// Register a non-default provider "together-ai" in the registry
	_, err := mgr.Registry.Create("together-ai", "provider", "default/together-ai")
	if err != nil {
		t.Fatal(err)
	}

	// Write provider YAML to the instance dir
	instDir := mgr.Registry.InstanceDir("together-ai")
	if instDir == "" {
		t.Fatal("expected instance dir for together-ai")
	}
	providerYAML := `provider: together-ai
version: "1.0.0"
routing:
  api_base: https://api.together.xyz
  auth_env: OPENAI_API_KEY
  models:
    together-llama:
      capabilities: [tools, streaming]
  tiers:
    standard: together-llama
`
	os.WriteFile(filepath.Join(instDir, "provider.yaml"), []byte(providerYAML), 0644)

	// Merge provider routing (simulates hub install)
	if err := MergeProviderRouting(home, "together-ai", []byte(providerYAML)); err != nil {
		t.Fatalf("MergeProviderRouting failed: %v", err)
	}

	// Verify together-ai is in routing.yaml before upgrade
	preData, _ := os.ReadFile(filepath.Join(home, "infrastructure", "routing.yaml"))
	if !strings.Contains(string(preData), "together-ai") {
		t.Fatal("expected together-ai in routing.yaml before upgrade")
	}

	// Run full upgrade
	_, err = mgr.Upgrade(nil)
	if err != nil {
		t.Fatalf("Upgrade failed: %v", err)
	}

	// Read the routing.yaml after upgrade
	postData, err := os.ReadFile(filepath.Join(home, "infrastructure", "routing.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	var cfg map[string]interface{}
	if err := yaml.Unmarshal(postData, &cfg); err != nil {
		t.Fatal(err)
	}

	providers, _ := cfg["providers"].(map[string]interface{})
	models, _ := cfg["models"].(map[string]interface{})

	// Assert: together-ai provider still present with correct api_base
	togetherProv, ok := providers["together-ai"].(map[string]interface{})
	if !ok {
		t.Fatalf("together-ai provider missing after upgrade; providers: %v", providers)
	}
	if togetherProv["api_base"] != "https://api.together.xyz" {
		t.Errorf("expected together-ai api_base https://api.together.xyz, got %v", togetherProv["api_base"])
	}

	// Assert: together-ai model still present
	if _, ok := models["together-llama"]; !ok {
		t.Errorf("together-llama model missing after upgrade; models: %v", models)
	}

	// Assert: default providers still present and match hub cache
	if _, ok := providers["anthropic"]; !ok {
		t.Errorf("anthropic provider missing after upgrade")
	}
	if _, ok := providers["openai"]; !ok {
		t.Errorf("openai provider missing after upgrade")
	}

	// Assert: default models still present
	if _, ok := models["claude-sonnet"]; !ok {
		t.Errorf("claude-sonnet model missing after upgrade")
	}
	if _, ok := models["gpt-4o"]; !ok {
		t.Errorf("gpt-4o model missing after upgrade")
	}
}

func TestUpgradeDoesNotPreserveRemovedProviders(t *testing.T) {
	home := t.TempDir()
	mgr := NewManager(home)

	// Hub cache with just anthropic
	hubRoutingYAML := `version: "0.1"
providers:
  anthropic:
    api_base: https://api.anthropic.com
    auth_env: ANTHROPIC_API_KEY
models:
  claude-sonnet:
    provider: anthropic
    capabilities: [tools, vision, streaming]
tiers:
  standard:
    - model: claude-sonnet
      preference: 0
settings:
  default_tier: standard
  tier_strategy: best_effort
`
	cacheDir := filepath.Join(home, "hub-cache", "default")
	os.MkdirAll(filepath.Join(cacheDir, "pricing"), 0755)
	os.WriteFile(filepath.Join(cacheDir, "pricing", "routing.yaml"), []byte(hubRoutingYAML), 0644)

	// Config
	os.WriteFile(filepath.Join(home, "config.yaml"),
		[]byte("hub:\n  sources:\n    - name: default\n      url: https://example.com\n"), 0644)

	// Write routing.yaml that has a stale provider "stale-provider" (NOT in registry)
	staleRoutingYAML := `version: "0.1"
providers:
  anthropic:
    api_base: https://api.anthropic.com
    auth_env: ANTHROPIC_API_KEY
  stale-provider:
    api_base: https://api.stale.com
    auth_env: OPENAI_API_KEY
models:
  claude-sonnet:
    provider: anthropic
    capabilities: [tools, vision, streaming]
  stale-model:
    provider: stale-provider
    capabilities: [tools]
tiers:
  standard:
    - model: claude-sonnet
      preference: 0
    - model: stale-model
      preference: 1
settings:
  default_tier: standard
  tier_strategy: best_effort
`
	os.MkdirAll(filepath.Join(home, "infrastructure"), 0755)
	os.WriteFile(filepath.Join(home, "infrastructure", "routing.yaml"), []byte(staleRoutingYAML), 0644)

	// Run full upgrade
	_, err := mgr.Upgrade(nil)
	if err != nil {
		t.Fatalf("Upgrade failed: %v", err)
	}

	// Read routing.yaml after upgrade
	postData, err := os.ReadFile(filepath.Join(home, "infrastructure", "routing.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	// Assert: stale-provider is gone (hub base overwrites, and it's not in the registry)
	if strings.Contains(string(postData), "stale-provider") {
		t.Errorf("stale-provider should have been removed after upgrade, but found in routing.yaml")
	}
	if strings.Contains(string(postData), "stale-model") {
		t.Errorf("stale-model should have been removed after upgrade, but found in routing.yaml")
	}

	// Verify anthropic is still there
	var cfg map[string]interface{}
	yaml.Unmarshal(postData, &cfg)
	providers, _ := cfg["providers"].(map[string]interface{})
	if _, ok := providers["anthropic"]; !ok {
		t.Errorf("anthropic provider should still be present after upgrade")
	}
}

func TestDiscoverFindsProviderComponent(t *testing.T) {
	home := t.TempDir()
	mgr := NewManager(home)

	// Write hub config defining a source
	os.WriteFile(filepath.Join(home, "config.yaml"), []byte("hub:\n  sources:\n    - name: default\n      url: https://example.com\n"), 0644)

	// Create provider cache dir and YAML using provider: as the identifier key
	providerDir := filepath.Join(home, "hub-cache", "default", "providers", "anthropic")
	os.MkdirAll(providerDir, 0755)
	os.WriteFile(filepath.Join(providerDir, "provider.yaml"),
		[]byte("provider: anthropic\nversion: \"1.0.0\"\ndescription: Anthropic Claude provider\n"), 0644)

	components := mgr.discover()

	var found *Component
	for i := range components {
		if components[i].Kind == "provider" && components[i].Name == "anthropic" {
			found = &components[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected to find provider component 'anthropic', got: %+v", components)
	}
	if found.Version != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %s", found.Version)
	}
}

func TestDiscoverFindsMarkdownSkillComponent(t *testing.T) {
	home := t.TempDir()
	mgr := NewManager(home)

	os.WriteFile(filepath.Join(home, "config.yaml"), []byte("hub:\n  sources:\n    - name: default\n      url: https://example.com\n"), 0644)

	skillDir := filepath.Join(home, "hub-cache", "default", "skills", "code-review")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(`---
name: code-review
version: "1.0"
description: Review code changes
---

# Code Review
`), 0644)

	results := mgr.Search("code-review", "skill")
	if len(results) != 1 {
		t.Fatalf("expected one skill search result, got %+v", results)
	}
	if results[0].Name != "code-review" || results[0].Kind != "skill" {
		t.Fatalf("unexpected skill result: %+v", results[0])
	}
	if results[0].Version != "1.0" {
		t.Fatalf("version = %q, want 1.0", results[0].Version)
	}
	if !strings.HasSuffix(results[0].Path, filepath.Join("skills", "code-review", "SKILL.md")) {
		t.Fatalf("path = %q, want SKILL.md path", results[0].Path)
	}
}
