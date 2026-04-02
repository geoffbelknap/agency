// agency-gateway/internal/models/preset.go
package models

import "fmt"

// PresetResponsivenessConfig controls how responsive the agent is in channels.
type PresetResponsivenessConfig struct {
	Default  string            `yaml:"default" default:"mention-only"`
	Channels map[string]string `yaml:"channels"`
}

// PresetExpertiseConfig describes the agent's area of expertise.
type PresetExpertiseConfig struct {
	Description string   `yaml:"description"`
	Keywords    []string `yaml:"keywords"`
}

// TriageConfig defines how the agent triages incoming work.
type TriageConfig struct {
	Domains []string `yaml:"domains" validate:"required"`
	Prompt  string   `yaml:"prompt" validate:"required"`
}

// IdentityConfig describes the agent's identity and purpose.
type IdentityConfig struct {
	Purpose string `yaml:"purpose" validate:"required"`
	Body    string `yaml:"body" validate:"required"`
}

// PresetConfig is the schema for preset YAML files.
// HardLimit and EscalationConfig are reused from constraints.go.
type PresetConfig struct {
	Name         string                     `yaml:"name" validate:"required"`
	Type         string                     `yaml:"type" validate:"required"`
	Description  string                     `yaml:"description"`
	Author       string                     `yaml:"author,omitempty"`
	License      string                     `yaml:"license,omitempty"`
	Model        *string                    `yaml:"model"`
	ModelTier    string                     `yaml:"model_tier" default:"standard"`
	Tools        []string                   `yaml:"tools"`
	Capabilities []string                   `yaml:"capabilities"`
	Identity     IdentityConfig             `yaml:"identity" validate:"required"`
	HardLimits   []HardLimit                `yaml:"hard_limits"`
	Escalation   EscalationConfig           `yaml:"escalation"`
	Triage       *TriageConfig              `yaml:"triage"`
	Responsiveness PresetResponsivenessConfig `yaml:"responsiveness"`
	Expertise    PresetExpertiseConfig      `yaml:"expertise"`
}

// Validate checks that Type is one of the allowed preset types and that
// ModelTier is a recognised routing tier.
func (p *PresetConfig) Validate() error {
	allowedTypes := map[string]bool{
		"standard":    true,
		"coordinator": true,
		"function":    true,
	}
	if !allowedTypes[p.Type] {
		return fmt.Errorf("type must be one of [standard coordinator function], got %q", p.Type)
	}

	for _, t := range VALID_TIERS {
		if p.ModelTier == t {
			return nil
		}
	}
	return fmt.Errorf("model_tier must be one of %v, got %q", VALID_TIERS, p.ModelTier)
}
