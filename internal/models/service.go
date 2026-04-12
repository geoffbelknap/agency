// agency-gateway/internal/models/service.go
package models

import (
	"fmt"
	"strings"
	"unicode"
)

// ServiceCredentialConfig defines how a service authenticates.
type ServiceCredentialConfig struct {
	EnvVar       string  `yaml:"env_var" validate:"required"`
	Header       string  `yaml:"header" validate:"required"`
	Format       *string `yaml:"format"`
	ScopedPrefix string  `yaml:"scoped_prefix" validate:"required"`
}

// Validate checks scoped_prefix starts with "agency-scoped-".
func (s *ServiceCredentialConfig) Validate() error {
	if !strings.HasPrefix(s.ScopedPrefix, "agency-scoped-") {
		return fmt.Errorf("scoped_prefix must start with 'agency-scoped-'")
	}
	return nil
}

// ServiceToolParameter defines a parameter for a service tool.
type ServiceToolParameter struct {
	Name        string  `yaml:"name" validate:"required"`
	Type        string  `yaml:"type" default:"string"`
	Description string  `yaml:"description" validate:"required"`
	Required    bool    `yaml:"required" default:"true"`
	Default     *string `yaml:"default"`
}

type ConsentRequirement struct {
	OperationKind    string `yaml:"operation_kind"`
	TokenInputField  string `yaml:"token_input_field"`
	TargetInputField string `yaml:"target_input_field"`
	MinWitnesses     int    `yaml:"min_witnesses"`
}

// ServiceTool defines an MCP-exposed tool for a service.
type ServiceTool struct {
	Name                 string                 `yaml:"name" validate:"required"`
	Description          string                 `yaml:"description" validate:"required"`
	Scope                string                 `yaml:"scope"`
	Parameters           []ServiceToolParameter `yaml:"parameters"`
	Method               string                 `yaml:"method" default:"GET"`
	Path                 string                 `yaml:"path" validate:"required"`
	QueryParams          map[string]string      `yaml:"query_params"`
	BodyTemplate         map[string]interface{} `yaml:"body_template"`
	ResponsePath         *string                `yaml:"response_path"`
	RequiresConsentToken *ConsentRequirement    `yaml:"requires_consent_token"`
}

// ServiceDefinition is a service that can be granted to agents.
type ServiceDefinition struct {
	Service      string                  `yaml:"service" validate:"required"`
	DisplayName  string                  `yaml:"display_name" validate:"required"`
	APIBase      string                  `yaml:"api_base" validate:"required"`
	Description  string                  `yaml:"description"`
	Author       string                  `yaml:"author,omitempty"`
	License      string                  `yaml:"license,omitempty"`
	Credential   ServiceCredentialConfig `yaml:"credential"`
	UsageExample *string                 `yaml:"usage_example"`
	Tools        []ServiceTool           `yaml:"tools"`
}

// Validate checks service name is alphanumeric with hyphens/underscores,
// and validates the credential config.
func (s *ServiceDefinition) Validate() error {
	for _, r := range s.Service {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_' {
			return fmt.Errorf("service name must be alphanumeric with hyphens or underscores")
		}
	}
	if err := s.Credential.Validate(); err != nil {
		return err
	}
	for _, tool := range s.Tools {
		if tool.RequiresConsentToken == nil {
			continue
		}
		if tool.RequiresConsentToken.OperationKind == "" {
			return fmt.Errorf("tool %q requires_consent_token.operation_kind is required", tool.Name)
		}
		if tool.RequiresConsentToken.TokenInputField == "" {
			return fmt.Errorf("tool %q requires_consent_token.token_input_field is required", tool.Name)
		}
		if tool.RequiresConsentToken.TargetInputField == "" {
			return fmt.Errorf("tool %q requires_consent_token.target_input_field is required", tool.Name)
		}
		params := make(map[string]bool, len(tool.Parameters))
		for _, param := range tool.Parameters {
			params[param.Name] = true
		}
		if !params[tool.RequiresConsentToken.TokenInputField] {
			return fmt.Errorf("tool %q requires_consent_token references unknown token_input_field %q", tool.Name, tool.RequiresConsentToken.TokenInputField)
		}
		if !params[tool.RequiresConsentToken.TargetInputField] {
			return fmt.Errorf("tool %q requires_consent_token references unknown target_input_field %q", tool.Name, tool.RequiresConsentToken.TargetInputField)
		}
	}
	return nil
}

// ServiceGrant records that a service was granted to an agent.
type ServiceGrant struct {
	Service       string   `yaml:"service" validate:"required"`
	GrantedAt     string   `yaml:"granted_at" validate:"required"`
	GrantedBy     string   `yaml:"granted_by" validate:"required"`
	AllowedScopes []string `yaml:"allowed_scopes,omitempty"`
}

// Validate checks granted_at is not empty.
func (g *ServiceGrant) Validate() error {
	if g.GrantedAt == "" {
		return fmt.Errorf("granted_at must not be empty")
	}
	return nil
}

// AgentServiceGrants holds all service grants for an agent.
type AgentServiceGrants struct {
	Agent  string         `yaml:"agent" validate:"required"`
	Grants []ServiceGrant `yaml:"grants"`
}
