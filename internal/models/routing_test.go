package models

import "testing"

func TestModelConfig_HasCapability(t *testing.T) {
	m := ModelConfig{
		Provider: "provider-a", ProviderModel: "provider-a-model-v1",
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
			"standard": {Provider: "provider-a", ProviderModel: "provider-a-model-v1", Capabilities: []string{"tools", "vision", "streaming"}},
			"fast":     {Provider: "provider-b", ProviderModel: "provider-b-model-v1", Capabilities: []string{"tools", "vision", "streaming"}},
		},
		Tiers: TierConfig{
			Standard: []TierEntry{{Model: "standard"}, {Model: "fast"}},
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
			"standard": {Provider: "provider-a", ProviderModel: "provider-a-model-v1", Capabilities: []string{"tools", "vision", "streaming"}},
			"local":    {Provider: "local", ProviderModel: "local-model", Capabilities: []string{"streaming"}},
		},
		Tiers: TierConfig{Mini: []TierEntry{{Model: "standard"}, {Model: "local"}}},
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
			"provider-a": {APIBase: "https://provider-a.example.com/v1"},
			"local":      {APIBase: "http://localhost:11434/v1"},
		},
		Models: map[string]ModelConfig{
			"standard": {Provider: "provider-a", ProviderModel: "provider-a-model-v1", Capabilities: []string{"tools", "vision", "streaming"}},
			"local":    {Provider: "local", ProviderModel: "local-model-v1", Capabilities: []string{"streaming"}},
		},
		Tiers: TierConfig{
			Standard: []TierEntry{{Model: "standard"}},
			Mini:     []TierEntry{{Model: "local"}},
		},
		Settings: RoutingSettings{TierStrategy: "best_effort"},
	}

	// Mini + tools → should fallback to standard
	pc, mc, tier := rc.ResolveTierWithCapabilities("mini", []string{"tools"}, nil)
	if pc == nil || mc == nil {
		t.Fatal("expected resolution")
	}
	if mc.ProviderModel != "provider-a-model-v1" {
		t.Errorf("expected provider-a-model-v1, got %s", mc.ProviderModel)
	}
	if tier != "standard" {
		t.Errorf("expected standard fallback, got %s", tier)
	}

	// Mini + streaming → stays on mini
	_, mc2, tier2 := rc.ResolveTierWithCapabilities("mini", []string{"streaming"}, nil)
	if mc2.ProviderModel != "local-model-v1" {
		t.Errorf("expected local model, got %s", mc2.ProviderModel)
	}
	if tier2 != "mini" {
		t.Errorf("expected mini, got %s", tier2)
	}
}

func TestResolveTierWithCapabilitiesNoMatch(t *testing.T) {
	rc := RoutingConfig{
		Providers: map[string]ProviderConfig{"local": {APIBase: "http://localhost:11434/v1"}},
		Models:    map[string]ModelConfig{"local": {Provider: "local", ProviderModel: "local-model-v1", Capabilities: []string{"streaming"}}},
		Tiers:     TierConfig{Mini: []TierEntry{{Model: "local"}}},
		Settings:  RoutingSettings{TierStrategy: "best_effort"},
	}
	pc, mc, _ := rc.ResolveTierWithCapabilities("mini", []string{"vision"}, nil)
	if pc != nil || mc != nil {
		t.Error("expected nil for unsatisfiable caps")
	}
}

func TestRoutingConfigValidateRejectsUnknownProviderToolCapability(t *testing.T) {
	rc := RoutingConfig{
		Providers: map[string]ProviderConfig{
			"provider-a": {APIBase: "https://provider-a.example.com/v1"},
		},
		Models: map[string]ModelConfig{
			"provider-a-standard": {
				Provider:                 "provider-a",
				ProviderModel:            "provider-a-standard",
				ProviderToolCapabilities: []string{"provider-web-search", "provider-unknown-tool"},
			},
		},
	}
	if err := rc.Validate(); err == nil {
		t.Fatal("expected unknown provider tool capability error")
	}
}

func TestRoutingConfigValidateRejectsUnknownProviderToolPricingCapability(t *testing.T) {
	rc := RoutingConfig{
		Providers: map[string]ProviderConfig{
			"provider-a": {APIBase: "https://provider-a.example.com/v1"},
		},
		Models: map[string]ModelConfig{
			"provider-a-test": {
				Provider:      "provider-a",
				ProviderModel: "provider-a-model-v1",
				ProviderToolPricing: map[string]ProviderToolPrice{
					"provider-unknown-tool": {Unit: "tool_call", Confidence: "unknown"},
				},
			},
		},
	}
	if err := rc.Validate(); err == nil {
		t.Fatal("expected unknown provider tool pricing capability error")
	}
}

func TestRoutingConfigValidateAllowsProviderNameIndependentOfAPIFormat(t *testing.T) {
	rc := RoutingConfig{
		Providers: map[string]ProviderConfig{
			"provider-a": {APIBase: "https://provider-a.example.com/v1beta", APIFormat: "gemini"},
		},
		Models: map[string]ModelConfig{
			"provider-a-fast": {Provider: "provider-a", ProviderModel: "provider-a-model-v1"},
		},
	}
	if err := rc.Validate(); err != nil {
		t.Fatalf("provider name should be independent of api_format: %v", err)
	}
}
