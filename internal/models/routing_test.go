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
