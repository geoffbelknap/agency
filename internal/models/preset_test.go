// agency-gateway/internal/models/preset_test.go
package models

import (
	"strings"
	"testing"
)

// minimalPresetConfig returns a valid PresetConfig for use as a test baseline.
func minimalPresetConfig() *PresetConfig {
	return &PresetConfig{
		Name:        "test-preset",
		Type:        "standard",
		Description: "A test preset",
		ModelTier:   "standard",
		Tools:       []string{},
		Capabilities: []string{},
		Identity: IdentityConfig{
			Purpose: "Testing",
			Body:    "You are a test agent.",
		},
		HardLimits: []HardLimit{},
		Escalation: EscalationConfig{},
		Responsiveness: PresetResponsivenessConfig{
			Default:  "mention-only",
			Channels: map[string]string{},
		},
	}
}

// TestPresetConfig_Validate_InvalidType tests that an invalid type is rejected.
func TestPresetConfig_Validate_InvalidType(t *testing.T) {
	cfg := minimalPresetConfig()
	cfg.Type = "unknown"

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid type, got nil")
	}
	if !strings.Contains(err.Error(), "type must be one of") {
		t.Errorf("expected error about type, got: %v", err)
	}
}

// TestPresetConfig_Validate_InvalidModelTier tests that an invalid model_tier is rejected.
func TestPresetConfig_Validate_InvalidModelTier(t *testing.T) {
	cfg := minimalPresetConfig()
	cfg.ModelTier = "ultra"

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid model_tier, got nil")
	}
	if !strings.Contains(err.Error(), "model_tier must be one of") {
		t.Errorf("expected error about model_tier, got: %v", err)
	}
}

// TestPresetConfig_Validate_Valid tests that a valid config passes.
func TestPresetConfig_Validate_Valid(t *testing.T) {
	cfg := minimalPresetConfig()
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected valid config, got error: %v", err)
	}
}

// TestPresetConfig_Validate_AllValidTypes tests each allowed type.
func TestPresetConfig_Validate_AllValidTypes(t *testing.T) {
	validTypes := []string{"standard", "coordinator", "function"}
	for _, typ := range validTypes {
		t.Run(typ, func(t *testing.T) {
			cfg := minimalPresetConfig()
			cfg.Type = typ
			if err := cfg.Validate(); err != nil {
				t.Errorf("expected valid type %q, got error: %v", typ, err)
			}
		})
	}
}

// TestPresetConfig_Validate_AllValidTiers tests each valid model tier.
func TestPresetConfig_Validate_AllValidTiers(t *testing.T) {
	for _, tier := range VALID_TIERS {
		t.Run(tier, func(t *testing.T) {
			cfg := minimalPresetConfig()
			cfg.ModelTier = tier
			if err := cfg.Validate(); err != nil {
				t.Errorf("expected valid tier %q, got error: %v", tier, err)
			}
		})
	}
}

// TestPresetConfig_DefaultModelTier tests that ModelTier defaults to "standard" via applyDefaults.
func TestPresetConfig_DefaultModelTier(t *testing.T) {
	cfg := &PresetConfig{}
	applyDefaults(cfg)
	if cfg.ModelTier != "standard" {
		t.Errorf("expected default model_tier 'standard', got %q", cfg.ModelTier)
	}
}

// TestPresetResponsivenessConfig_DefaultValue tests the default responsiveness setting.
func TestPresetResponsivenessConfig_DefaultValue(t *testing.T) {
	cfg := &PresetResponsivenessConfig{}
	applyDefaults(cfg)
	if cfg.Default != "mention-only" {
		t.Errorf("expected default responsiveness 'mention-only', got %q", cfg.Default)
	}
}

// TestPresetConfig_WithTriage tests that a preset with triage config is valid.
func TestPresetConfig_WithTriage(t *testing.T) {
	cfg := minimalPresetConfig()
	cfg.Triage = &TriageConfig{
		Domains: []string{"engineering", "ops"},
		Prompt:  "Route based on domain keywords.",
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected valid config with triage, got error: %v", err)
	}
}

// TestPresetConfig_WithHardLimitsAndEscalation tests that HardLimit and EscalationConfig
// from constraints.go are accepted by PresetConfig (integration check for type reuse).
func TestPresetConfig_WithHardLimitsAndEscalation(t *testing.T) {
	cfg := minimalPresetConfig()
	cfg.HardLimits = []HardLimit{
		{Rule: "No PII in responses", Reason: "Privacy compliance"},
	}
	cfg.Escalation = EscalationConfig{
		AlwaysEscalate:       []string{"legal decisions"},
		FlagBeforeProceeding: []string{"irreversible actions"},
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected valid config with limits/escalation, got error: %v", err)
	}
}
