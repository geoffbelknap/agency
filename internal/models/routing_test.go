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
