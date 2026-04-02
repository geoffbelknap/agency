// agency-gateway/internal/models/agent_config.go
package models

import "fmt"

// AgentMCPServerConfig describes an MCP server mounted into an agent's body.
// Structurally mirrors MCPServerSpec in capability.go but is kept separate to
// match the Python source — this is the agent.yaml body.mcp_servers entry.
type AgentMCPServerConfig struct {
	Command string            `yaml:"command" validate:"required"`
	Args    []string          `yaml:"args"`
	Env     map[string]string `yaml:"env"`
}

// AgentBodyConfig describes the agent runtime and its skill/MCP dependencies.
type AgentBodyConfig struct {
	Runtime    string                          `yaml:"runtime" validate:"required"`
	Version    string                          `yaml:"version" validate:"required"`
	SkillsDirs []string                        `yaml:"skills_dirs"`
	MCPServers map[string]AgentMCPServerConfig `yaml:"mcp_servers"`
}

// AgentWorkspaceRef is a reference to the workspace this agent uses.
type AgentWorkspaceRef struct {
	Ref string `yaml:"ref" validate:"required"`
}

// AgentRequires lists the tools, capabilities, and models that an agent requires.
type AgentRequires struct {
	Tools        []string `yaml:"tools"`
	Capabilities []string `yaml:"capabilities"`
	Models       []string `yaml:"models"`
}

// AgentPolicyRef points to the policy the agent inherits or references directly.
type AgentPolicyRef struct {
	InheritsFrom *string `yaml:"inherits_from"`
	Ref          *string `yaml:"ref"`
}

// AgentTriageConfig controls how the agent triages incoming work.
type AgentTriageConfig struct {
	Domains []string `yaml:"domains"`
	Prompt  string   `yaml:"prompt"`
}

// AgentResponsivenessConfig controls how responsive the agent is in channels.
// Kept separate from PresetResponsivenessConfig to match the Python source.
type AgentResponsivenessConfig struct {
	Default  string            `yaml:"default" default:"mention-only"`
	Channels map[string]string `yaml:"channels"`
}

// AgentExpertiseConfig describes the agent's area of expertise.
// Kept separate from PresetExpertiseConfig to match the Python source.
type AgentExpertiseConfig struct {
	Description string   `yaml:"description"`
	Keywords    []string `yaml:"keywords"`
}

// AgentConfig is the top-level schema for agent.yaml files.
type AgentConfig struct {
	Version        string                    `yaml:"version" default:"0.1"`
	Name           string                    `yaml:"name" validate:"required"`
	LifecycleID    string                    `yaml:"lifecycle_id"`
	Role           string                    `yaml:"role" validate:"required"`
	Tier           string                    `yaml:"tier" default:"standard"`
	Type           string                    `yaml:"type" default:"standard"`
	ModelTier      *string                   `yaml:"model_tier"`
	Body           AgentBodyConfig           `yaml:"body" validate:"required"`
	Workspace      AgentWorkspaceRef         `yaml:"workspace" validate:"required"`
	Requires       AgentRequires             `yaml:"requires"`
	Policy         AgentPolicyRef            `yaml:"policy"`
	Triage         *AgentTriageConfig        `yaml:"triage"`
	Responsiveness AgentResponsivenessConfig `yaml:"responsiveness"`
	Expertise      AgentExpertiseConfig      `yaml:"expertise"`
}

// Validate performs cross-field validation for AgentConfig.
// It checks:
//   - name must be at least 2 characters and match the hierarchy name pattern
//   - model_tier, if set, must be one of the valid routing tiers
//   - tier must be one of: standard, elevated, function
//   - type must be one of: standard, coordinator, function
func (a *AgentConfig) Validate() error {
	// Apply default for Responsiveness.Default if still empty (nested struct
	// default tags are not applied by the top-level applyDefaults pass).
	if a.Responsiveness.Default == "" {
		a.Responsiveness.Default = "mention-only"
	}

	// Apply defaults for Tier and Type if zero-valued.
	if a.Tier == "" {
		a.Tier = "standard"
	}
	if a.Type == "" {
		a.Type = "standard"
	}

	// Validate name length and format.
	if len(a.Name) < 2 {
		return fmt.Errorf("agent name must be at least 2 characters")
	}
	if !ValidateHierarchyName(a.Name) {
		return fmt.Errorf(
			"agent name must be lowercase alphanumeric with hyphens (no leading/trailing hyphens), got: %s",
			a.Name,
		)
	}

	// Validate tier.
	allowedTiers := map[string]bool{
		"standard": true,
		"elevated": true,
		"function": true,
	}
	if !allowedTiers[a.Tier] {
		return fmt.Errorf("tier must be one of [standard elevated function], got %q", a.Tier)
	}

	// Validate type.
	allowedTypes := map[string]bool{
		"standard":    true,
		"coordinator": true,
		"function":    true,
	}
	if !allowedTypes[a.Type] {
		return fmt.Errorf("type must be one of [standard coordinator function], got %q", a.Type)
	}

	// Validate model_tier if provided.
	if a.ModelTier != nil {
		valid := false
		for _, t := range VALID_TIERS {
			if *a.ModelTier == t {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("model_tier must be one of %v, got %q", VALID_TIERS, *a.ModelTier)
		}
	}

	return nil
}
