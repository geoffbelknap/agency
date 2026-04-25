package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRoutingConfig(t *testing.T) {
	dir := t.TempDir()
	routingFile := filepath.Join(dir, "routing.yaml")
	os.WriteFile(routingFile, []byte(`
version: "0.1"
providers:
  provider-a:
    api_base: https://provider-a.example.com/v1/
    api_format: openai
models:
  standard:
    provider: provider-a
    provider_model: provider-a-standard
    cost_per_mtok_in: 3.0
    cost_per_mtok_out: 15.0
    provider_tool_pricing:
      provider-web-search:
        unit: search
        usd_per_unit: 0.01
        source: test
        confidence: exact
settings:
  xpia_scan: true
  default_timeout: 300
`), 0644)

	rc, err := LoadRoutingConfig(routingFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rc.Providers["provider-a"].APIBase != "https://provider-a.example.com/v1/" {
		t.Errorf("wrong api_base: %s", rc.Providers["provider-a"].APIBase)
	}
	if rc.Models["standard"].Provider != "provider-a" {
		t.Errorf("wrong provider: %s", rc.Models["standard"].Provider)
	}
	if rc.Models["standard"].CostIn != 3.0 {
		t.Errorf("wrong cost_in: %f", rc.Models["standard"].CostIn)
	}
	if rc.Models["standard"].CostOut != 15.0 {
		t.Errorf("wrong cost_out: %f", rc.Models["standard"].CostOut)
	}
	price, ok := rc.Models["standard"].ProviderToolPriceFor(capProviderWebSearch)
	if !ok {
		t.Fatal("expected provider tool price")
	}
	if price.Unit != "search" || price.USDPerUnit != 0.01 || price.Confidence != "exact" {
		t.Fatalf("unexpected provider tool price: %#v", price)
	}
	if !rc.Settings.XPIAScan {
		t.Error("expected xpia_scan to be true")
	}
	if rc.Settings.DefaultTimeout != 300 {
		t.Errorf("wrong default_timeout: %d", rc.Settings.DefaultTimeout)
	}
}

func TestModelProviderToolPriceForLegacyCosts(t *testing.T) {
	model := Model{ProviderToolCosts: map[string]float64{capProviderWebSearch: 0.01}}
	price, ok := model.ProviderToolPriceFor(capProviderWebSearch)
	if !ok {
		t.Fatal("expected legacy provider tool cost to be available as price")
	}
	if price.Unit != "tool_call" || price.USDPerUnit != 0.01 || price.Confidence != "estimated" {
		t.Fatalf("unexpected legacy price: %#v", price)
	}
}

func TestLoadRoutingConfigRejectsUnknownProviderToolCapability(t *testing.T) {
	dir := t.TempDir()
	routingFile := filepath.Join(dir, "routing.yaml")
	os.WriteFile(routingFile, []byte(`
version: "0.1"
providers:
  provider-b:
    api_base: https://provider-b.example.com/v1/
models:
  provider-b-test:
    provider: provider-b
    provider_model: provider-b-test
    provider_tool_capabilities: [provider-web-search, provider-unknown-tool]
settings:
  xpia_scan: true
  default_timeout: 300
`), 0644)

	_, err := LoadRoutingConfig(routingFile)
	if err == nil {
		t.Fatal("expected unknown provider tool capability error")
	}
	if !strings.Contains(err.Error(), "provider-unknown-tool") {
		t.Fatalf("expected capability name in error, got %v", err)
	}
}

func TestLoadRoutingConfigMissing(t *testing.T) {
	_, err := LoadRoutingConfig("/nonexistent/routing.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadAPIKeys(t *testing.T) {
	dir := t.TempDir()
	keysFile := filepath.Join(dir, "api_keys.yaml")
	os.WriteFile(keysFile, []byte(`- key: "test-key-123"
  name: "agency-workspace"
`), 0644)

	keys, err := LoadAPIKeys(keysFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 1 || keys[0].Key != "test-key-123" {
		t.Errorf("unexpected keys: %v", keys)
	}
	if keys[0].Name != "agency-workspace" {
		t.Errorf("unexpected name: %s", keys[0].Name)
	}
}

func TestResolveModel(t *testing.T) {
	rc := &RoutingConfig{
		Providers: map[string]Provider{
			"provider-a": {
				APIBase:   "https://provider-a.example.com/v1/",
				APIFormat: "openai",
			},
		},
		Models: map[string]Model{
			"standard": {
				Provider:      "provider-a",
				ProviderModel: "provider-a-standard",
				CostIn:        3.0,
				CostOut:       15.0,
			},
		},
	}

	target, providerModel, providerName, err := rc.ResolveModel("standard")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target != "https://provider-a.example.com/v1/chat/completions" {
		t.Errorf("wrong target: %s", target)
	}
	if providerModel != "provider-a-standard" {
		t.Errorf("wrong provider model: %s", providerModel)
	}
	if providerName != "provider-a" {
		t.Errorf("wrong provider name: %s", providerName)
	}
}

func TestResolveModelWithDifferentProvider(t *testing.T) {
	rc := &RoutingConfig{
		Providers: map[string]Provider{
			"provider-b": {
				APIBase: "https://provider-b.example.com/v1/",
			},
		},
		Models: map[string]Model{
			"provider-b-standard": {
				Provider:      "provider-b",
				ProviderModel: "provider-b-standard",
			},
		},
	}

	target, providerModel, providerName, err := rc.ResolveModel("provider-b-standard")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target != "https://provider-b.example.com/v1/chat/completions" {
		t.Errorf("wrong target: %s", target)
	}
	if providerModel != "provider-b-standard" {
		t.Errorf("wrong provider model: %s", providerModel)
	}
	if providerName != "provider-b" {
		t.Errorf("wrong provider name: %s", providerName)
	}
}

func TestResolveModelGeminiNative(t *testing.T) {
	rc := &RoutingConfig{
		Providers: map[string]Provider{
			"google": {
				APIBase:   "https://generativelanguage.googleapis.com/v1beta",
				APIFormat: "gemini",
			},
		},
		Models: map[string]Model{
			"gemini-flash": {
				Provider:      "google",
				ProviderModel: "gemini-2.5-flash",
			},
		},
	}

	target, providerModel, providerName, err := rc.ResolveModel("gemini-flash")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target != "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:generateContent" {
		t.Errorf("wrong target: %s", target)
	}
	if providerModel != "gemini-2.5-flash" {
		t.Errorf("wrong provider model: %s", providerModel)
	}
	if providerName != "google" {
		t.Errorf("wrong provider name: %s", providerName)
	}
}

func TestResolveModelUnknown(t *testing.T) {
	rc := &RoutingConfig{
		Providers: map[string]Provider{},
		Models:    map[string]Model{},
	}
	_, _, _, err := rc.ResolveModel("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown model")
	}
}

func TestResolveModelUnknownProvider(t *testing.T) {
	rc := &RoutingConfig{
		Providers: map[string]Provider{},
		Models: map[string]Model{
			"test": {Provider: "nonexistent"},
		},
	}
	_, _, _, err := rc.ResolveModel("test")
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestResolveModelLocalProvider(t *testing.T) {
	rc := &RoutingConfig{
		Providers: map[string]Provider{
			"ollama": {
				APIBase: "http://localhost:11434/v1/",
			},
		},
		Models: map[string]Model{
			"llama": {Provider: "ollama", ProviderModel: "llama3.2"},
		},
	}
	target, providerModel, providerName, err := rc.ResolveModel("llama")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target != "http://localhost:11434/v1/chat/completions" {
		t.Errorf("wrong target: %s", target)
	}
	if providerModel != "llama3.2" {
		t.Errorf("wrong provider model: %s", providerModel)
	}
	if providerName != "ollama" {
		t.Errorf("wrong provider name: %s", providerName)
	}
}

func TestLoadServerConfig(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "server-config.yaml")
	os.WriteFile(configFile, []byte(`
server:
  listen: 0.0.0.0:18080
auth:
  type: api_key
  api_key:
    keys_file: /agency/enforcer/auth/api_keys.yaml
policy:
  path: /agency/enforcer/policy
audit:
  path: /agency/enforcer/audit
  format: json
  flush_interval: 5s
`), 0644)

	sc, err := LoadServerConfig(configFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sc.Server.Listen != "0.0.0.0:18080" {
		t.Errorf("wrong listen: %s", sc.Server.Listen)
	}
	if sc.Auth.Type != "api_key" {
		t.Errorf("wrong auth type: %s", sc.Auth.Type)
	}
	if sc.Audit.Path != "/agency/enforcer/audit" {
		t.Errorf("wrong audit path: %s", sc.Audit.Path)
	}
}
