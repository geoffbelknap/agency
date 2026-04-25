// agency-gateway/internal/models/capability.go
package models

import (
	"fmt"
	"unicode"

	agencysecurity "github.com/geoffbelknap/agency/internal/security"
)

// CapabilityKind is the type of a capability entry.
// Valid values: "mcp-server", "skill", "service".
type CapabilityKind = string

// CapabilityState is the operational state of a capability.
// Valid values: "available", "restricted", "disabled".
type CapabilityState = string

// ToolApproval is the approval policy for a tool.
// Valid values: "available", "ask-once", "ask-always", "denied".
type ToolApproval = string

// CapabilityRequirements lists prerequisites for a capability.
type CapabilityRequirements struct {
	RuntimePackages []string `yaml:"runtime_packages"`
	Network         []string `yaml:"network"`
	Capabilities    []string `yaml:"capabilities"`
}

// CapabilityPermissions declares the permission level required by a capability.
type CapabilityPermissions struct {
	// Filesystem is one of "none", "read-only", "read-write". Defaults to "none".
	Filesystem string `yaml:"filesystem" validate:"oneof=none read-only read-write" default:"none"`
	Network    bool   `yaml:"network" default:"false"`
	Execution  bool   `yaml:"execution" default:"false"`
}

// CapabilityIntegrity holds integrity verification data for a capability.
type CapabilityIntegrity struct {
	SHA256     *string `yaml:"sha256"`
	SignedBy   *string `yaml:"signed_by"`
	Signature  *string `yaml:"signature"`
	VerifiedAt *string `yaml:"verified_at"`
}

// MCPServerSpec defines how to launch an MCP server capability.
type MCPServerSpec struct {
	Command string            `yaml:"command" validate:"required"`
	Args    []string          `yaml:"args"`
	Env     map[string]string `yaml:"env"`
}

// CapabilityEntry is the definition of a capability in the capability registry.
type CapabilityEntry struct {
	Kind        CapabilityKind         `yaml:"kind" validate:"required,oneof=mcp-server skill service"`
	Name        string                 `yaml:"name" validate:"required"`
	Version     string                 `yaml:"version" default:"0.1.0"`
	DisplayName string                 `yaml:"display_name"`
	Description string                 `yaml:"description"`
	Source      string                 `yaml:"source" default:"local"`
	Publisher   string                 `yaml:"publisher"`
	Integrity   CapabilityIntegrity    `yaml:"integrity"`
	Requires    CapabilityRequirements `yaml:"requires"`
	Permissions CapabilityPermissions  `yaml:"permissions"`
	Spec        *MCPServerSpec         `yaml:"spec"`
	ServiceRef  *string                `yaml:"service_ref"`
	SkillPath   *string                `yaml:"skill_path"`
	Tags        []string               `yaml:"tags"`
}

// Validate checks that the capability name is alphanumeric with hyphens or underscores.
func (c *CapabilityEntry) Validate() error {
	for _, r := range c.Name {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_' {
			return fmt.Errorf("capability name must be alphanumeric with hyphens or underscores")
		}
	}
	return nil
}

// ToolPolicy defines the approval policy for a specific tool.
type ToolPolicy struct {
	Approval ToolApproval `yaml:"approval" validate:"oneof=available ask-once ask-always denied" default:"available"`
}

// CapabilityAuth defines how to authenticate a capability.
type CapabilityAuth struct {
	Env      string            `yaml:"env" validate:"required"`
	InjectAs string            `yaml:"inject_as"`
	Agents   map[string]string `yaml:"agents"`
}

// CapabilityConfig is the runtime configuration for a capability in capabilities.yaml.
type CapabilityConfig struct {
	// State is one of "available", "restricted", "disabled". Defaults to "available".
	State  CapabilityState         `yaml:"state" validate:"oneof=available restricted disabled" default:"available"`
	Agents []string                `yaml:"agents"`
	Auth   *CapabilityAuth         `yaml:"auth"`
	Tools  map[string]ToolApproval `yaml:"tools"`
}

// CapabilitiesFile is the top-level schema for capabilities.yaml.
type CapabilitiesFile struct {
	Capabilities map[string]CapabilityConfig `yaml:"capabilities"`
}

// ToolApprovalRecord records an operator approval decision for a specific tool call.
type ToolApprovalRecord struct {
	Capability string                        `yaml:"capability" validate:"required"`
	Tool       string                        `yaml:"tool" validate:"required"`
	Agent      string                        `yaml:"agent" validate:"required"`
	Approved   bool                          `yaml:"approved"`
	Status     agencysecurity.ApprovalStatus `yaml:"status,omitempty"`
	ApprovedBy string                        `yaml:"approved_by" default:"operator"`
	ApprovedAt string                        `yaml:"approved_at"`
}

func (r ToolApprovalRecord) ApprovalStatus() agencysecurity.ApprovalStatus {
	if r.Status != "" {
		return r.Status
	}
	if r.Approved {
		return agencysecurity.ApprovalApproved
	}
	return agencysecurity.ApprovalDenied
}

// CapabilityPolicy declares which capabilities an agent is permitted to use.
type CapabilityPolicy struct {
	Required  []string `yaml:"required"`
	Available []string `yaml:"available"`
	Denied    []string `yaml:"denied"`
	Enabled   []string `yaml:"enabled"`
}
