// agency-gateway/internal/models/constraints.go
package models

// HardLimit is a named rule that the agent must never violate.
// Also used by preset models (preset.go will reference this type).
type HardLimit struct {
	Rule   string `yaml:"rule" validate:"required"`
	Reason string `yaml:"reason" validate:"required"`
}

// EscalationConfig controls when the agent must escalate to a human.
type EscalationConfig struct {
	AlwaysEscalate        []string `yaml:"always_escalate"`
	FlagBeforeProceeding  []string `yaml:"flag_before_proceeding"`
}

// NotifyConfig specifies notification targets for budget events.
type NotifyConfig struct {
	Webhook string `yaml:"webhook"`
	Email   string `yaml:"email"`
	Log     bool   `yaml:"log" default:"true"`
}

// BudgetConfig controls cost budget enforcement.
type BudgetConfig struct {
	Mode                string      `yaml:"mode" validate:"oneof=hard soft notify" default:"notify"`
	SoftLimit           float64     `yaml:"soft_limit" validate:"gte=0"`
	HardLimit           float64     `yaml:"hard_limit" validate:"gte=0"`
	MaxDailyUSD         float64     `yaml:"max_daily_usd" validate:"gte=0"`
	MaxSessionUSD       float64     `yaml:"max_session_usd" validate:"gte=0"`
	MaxTotalUSD         float64     `yaml:"max_total_usd" validate:"gte=0"`
	WarningThresholdPct int         `yaml:"warning_threshold_pct" validate:"gte=1,lte=100" default:"80"`
	Notify              NotifyConfig `yaml:"notify"`
}

// MCPPolicy controls which MCP servers and tools the agent may use.
type MCPPolicy struct {
	Mode           string            `yaml:"mode" validate:"oneof=allowlist denylist" default:"denylist"`
	AllowedServers []string          `yaml:"allowed_servers"`
	DeniedServers  []string          `yaml:"denied_servers"`
	AllowedTools   []string          `yaml:"allowed_tools"`
	DeniedTools    []string          `yaml:"denied_tools"`
	PinnedHashes   map[string]string `yaml:"pinned_hashes"`
}

// IsServerAllowed returns true if the named server is permitted by this policy.
func (p *MCPPolicy) IsServerAllowed(serverName string) bool {
	if p.Mode == "allowlist" {
		for _, s := range p.AllowedServers {
			if s == serverName {
				return true
			}
		}
		return false
	}
	// denylist mode
	for _, s := range p.DeniedServers {
		if s == serverName {
			return false
		}
	}
	return true
}

// IsToolAllowed returns true if the named tool is permitted by this policy.
func (p *MCPPolicy) IsToolAllowed(toolName string) bool {
	for _, t := range p.DeniedTools {
		if t == toolName {
			return false
		}
	}
	if len(p.AllowedTools) > 0 {
		for _, t := range p.AllowedTools {
			if t == toolName {
				return true
			}
		}
		return false
	}
	return true
}

// NetworkConfig controls network egress behaviour for the agent.
type NetworkConfig struct {
	EgressMode string `yaml:"egress_mode" validate:"oneof=denylist allowlist supervised-strict supervised-permissive" default:"denylist"`
}

// IdentityConstraint records the agent's declared role and purpose.
type IdentityConstraint struct {
	Role    string `yaml:"role" validate:"required"`
	Purpose string `yaml:"purpose" validate:"required"`
}

// ConstraintsConfig is the schema for constraints.yaml.
type ConstraintsConfig struct {
	Version    string             `yaml:"version" default:"0.1"`
	Agent      string             `yaml:"agent" validate:"required"`
	Identity   IdentityConstraint `yaml:"identity" validate:"required"`
	HardLimits []HardLimit        `yaml:"hard_limits"`
	Escalation EscalationConfig   `yaml:"escalation"`
	Network    NetworkConfig      `yaml:"network"`
	Budget     BudgetConfig       `yaml:"budget"`
	MCP        MCPPolicy          `yaml:"mcp"`
}
