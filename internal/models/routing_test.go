package models

import "testing"

func TestModelConfig_HasCapability(t *testing.T) {
	m := ModelConfig{
		Provider: "anthropic", ProviderModel: "claude-sonnet-4",
		Capabilities: []string{"tools", "vision", "streaming"},
	}
	if !m.HasCapability("tools") {
		t.Error("expected true for tools")
	}
	if !m.HasCapability("vision") {
		t.Error("expected true for vision")
	}
	if m.HasCapability("thinking") {
		t.Error("expected false for thinking")
	}
}

func TestModelConfig_HasCapabilityEmpty(t *testing.T) {
	m := ModelConfig{Provider: "ollama", ProviderModel: "llama3.1:8b"}
	if m.HasCapability("tools") {
		t.Error("expected false for empty caps")
	}
}

func TestRoutingConfig_TierCapabilities(t *testing.T) {
	rc := RoutingConfig{
		Models: map[string]ModelConfig{
			"claude-sonnet": {Provider: "anthropic", ProviderModel: "claude-sonnet-4", Capabilities: []string{"tools", "vision", "streaming"}},
			"gemini-flash":  {Provider: "gemini", ProviderModel: "gemini-2.5-flash", Capabilities: []string{"tools", "vision", "streaming"}},
		},
		Tiers: TierConfig{
			Standard: []TierEntry{{Model: "claude-sonnet"}, {Model: "gemini-flash"}},
		},
	}
	caps := rc.TierCapabilities("standard")
	if len(caps) != 3 {
		t.Errorf("expected 3 caps, got %d: %v", len(caps), caps)
	}
}

func TestRoutingConfig_TierCapabilitiesIntersection(t *testing.T) {
	rc := RoutingConfig{
		Models: map[string]ModelConfig{
			"claude-sonnet": {Provider: "anthropic", ProviderModel: "cs4", Capabilities: []string{"tools", "vision", "streaming"}},
			"llama-8b":      {Provider: "ollama", ProviderModel: "llama", Capabilities: []string{"streaming"}},
		},
		Tiers: TierConfig{Mini: []TierEntry{{Model: "claude-sonnet"}, {Model: "llama-8b"}}},
	}
	caps := rc.TierCapabilities("mini")
	if len(caps) != 1 || caps[0] != "streaming" {
		t.Errorf("expected [streaming], got %v", caps)
	}
}

func TestRoutingConfig_TierCapabilitiesEmpty(t *testing.T) {
	rc := RoutingConfig{}
	if caps := rc.TierCapabilities("frontier"); len(caps) != 0 {
		t.Errorf("expected nil, got %v", caps)
	}
}

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
			Standard: []TierEntry{{Model: "claude-sonnet"}},
			Mini:     []TierEntry{{Model: "llama-8b"}},
		},
		Settings: RoutingSettings{TierStrategy: "best_effort"},
	}

	// Mini + tools → should fallback to standard
	pc, mc, tier := rc.ResolveTierWithCapabilities("mini", []string{"tools"}, nil)
	if pc == nil || mc == nil {
		t.Fatal("expected resolution")
	}
	if mc.ProviderModel != "claude-sonnet-4" {
		t.Errorf("expected claude-sonnet-4, got %s", mc.ProviderModel)
	}
	if tier != "standard" {
		t.Errorf("expected standard fallback, got %s", tier)
	}

	// Mini + streaming → stays on mini
	_, mc2, tier2 := rc.ResolveTierWithCapabilities("mini", []string{"streaming"}, nil)
	if mc2.ProviderModel != "llama3.1:8b" {
		t.Errorf("expected llama, got %s", mc2.ProviderModel)
	}
	if tier2 != "mini" {
		t.Errorf("expected mini, got %s", tier2)
	}
}

func TestResolveTierWithCapabilitiesNoMatch(t *testing.T) {
	rc := RoutingConfig{
		Providers: map[string]ProviderConfig{"ollama": {APIBase: "http://localhost:11434/v1"}},
		Models:    map[string]ModelConfig{"llama": {Provider: "ollama", ProviderModel: "llama", Capabilities: []string{"streaming"}}},
		Tiers:     TierConfig{Mini: []TierEntry{{Model: "llama"}}},
		Settings:  RoutingSettings{TierStrategy: "best_effort"},
	}
	pc, mc, _ := rc.ResolveTierWithCapabilities("mini", []string{"vision"}, nil)
	if pc != nil || mc != nil {
		t.Error("expected nil for unsatisfiable caps")
	}
}
