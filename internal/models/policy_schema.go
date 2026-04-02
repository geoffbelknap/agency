// agency-gateway/internal/models/policy_schema.go
package models

// CommsScanningConfig controls message content scanning.
type CommsScanningConfig struct {
	Enabled bool     `yaml:"enabled" default:"true"`
	Rules   []string `yaml:"rules"`
}

// Validate ensures "no_credentials" is always in rules.
func (c *CommsScanningConfig) Validate() error {
	if c.Rules == nil {
		c.Rules = []string{"no_credentials"}
		return nil
	}
	for _, r := range c.Rules {
		if r == "no_credentials" {
			return nil
		}
	}
	c.Rules = append([]string{"no_credentials"}, c.Rules...)
	return nil
}

// CommsBridgingConfig controls cross-platform message bridging.
type CommsBridgingConfig struct {
	Enabled          bool     `yaml:"enabled"`
	AllowedPlatforms []string `yaml:"allowed_platforms"`
}

// CommunicationPolicy groups comms scanning and bridging policies.
type CommunicationPolicy struct {
	Scanning CommsScanningConfig `yaml:"scanning"`
	Bridging CommsBridgingConfig `yaml:"bridging"`
}

// PolicyConfig is the schema for org-level policy.yaml.
type PolicyConfig struct {
	Version       string              `yaml:"version" default:"0.1"`
	Bundle        *string             `yaml:"bundle"`
	Additions     []string            `yaml:"additions"`
	Restrictions  []string            `yaml:"restrictions"`
	Communication CommunicationPolicy `yaml:"communication"`
}

// AgentPolicyConfig is the schema for agent-level policy.yaml.
type AgentPolicyConfig struct {
	Version      string   `yaml:"version" default:"0.1"`
	InheritsFrom *string  `yaml:"inherits_from"`
	Additions    []string `yaml:"additions"`
	Restrictions []string `yaml:"restrictions"`
}
