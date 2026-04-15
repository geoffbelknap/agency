package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadRoutingConfig(t *testing.T) {
	dir := t.TempDir()
	routingFile := filepath.Join(dir, "routing.yaml")
	os.WriteFile(routingFile, []byte(`
version: "0.1"
providers:
  anthropic:
    api_base: https://api.anthropic.com/v1/
models:
  claude-sonnet:
    provider: anthropic
    provider_model: claude-sonnet-4-20250514
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
	if rc.Providers["anthropic"].APIBase != "https://api.anthropic.com/v1/" {
		t.Errorf("wrong api_base: %s", rc.Providers["anthropic"].APIBase)
	}
	if rc.Models["claude-sonnet"].Provider != "anthropic" {
		t.Errorf("wrong provider: %s", rc.Models["claude-sonnet"].Provider)
	}
	if rc.Models["claude-sonnet"].CostIn != 3.0 {
		t.Errorf("wrong cost_in: %f", rc.Models["claude-sonnet"].CostIn)
	}
	if rc.Models["claude-sonnet"].CostOut != 15.0 {
		t.Errorf("wrong cost_out: %f", rc.Models["claude-sonnet"].CostOut)
	}
	price, ok := rc.Models["claude-sonnet"].ProviderToolPriceFor(capProviderWebSearch)
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
			"anthropic": {
				APIBase: "https://api.anthropic.com/v1/",
			},
		},
		Models: map[string]Model{
			"claude-sonnet": {
				Provider:      "anthropic",
				ProviderModel: "claude-sonnet-4-20250514",
				CostIn:        3.0,
				CostOut:       15.0,
			},
		},
	}

	target, providerModel, providerName, err := rc.ResolveModel("claude-sonnet")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target != "https://api.anthropic.com/v1/messages" {
		t.Errorf("wrong target: %s", target)
	}
	if providerModel != "claude-sonnet-4-20250514" {
		t.Errorf("wrong provider model: %s", providerModel)
	}
	if providerName != "anthropic" {
		t.Errorf("wrong provider name: %s", providerName)
	}
}

func TestResolveModelWithDifferentProvider(t *testing.T) {
	rc := &RoutingConfig{
		Providers: map[string]Provider{
			"openai": {
				APIBase: "https://api.openai.com/v1/",
			},
		},
		Models: map[string]Model{
			"gpt-4o": {
				Provider:      "openai",
				ProviderModel: "gpt-4o",
			},
		},
	}

	target, providerModel, providerName, err := rc.ResolveModel("gpt-4o")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target != "https://api.openai.com/v1/chat/completions" {
		t.Errorf("wrong target: %s", target)
	}
	if providerModel != "gpt-4o" {
		t.Errorf("wrong provider model: %s", providerModel)
	}
	if providerName != "openai" {
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
