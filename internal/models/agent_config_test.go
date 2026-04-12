// agency-gateway/internal/models/agent_config_test.go
package models

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// minimalAgentConfig returns a valid AgentConfig for use as a test baseline.
func minimalAgentConfig() *AgentConfig {
	return &AgentConfig{
		Version: "0.1",
		Name:    "my-agent",
		Role:    "software engineer",
		Tier:    "standard",
		Type:    "standard",
		Body: AgentBodyConfig{
			Runtime: "body",
			Version: "1.0",
		},
		Workspace: AgentWorkspaceRef{
			Ref: "default",
		},
	}
}

// strPtr is a helper to get a *string from a literal.
func strPtr(s string) *string {
	return &s
}

// --- Name validation ---

// TestAgentConfig_Name_Valid tests that a valid name passes.
func TestAgentConfig_Name_Valid(t *testing.T) {
	cfg := minimalAgentConfig()
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected valid config, got error: %v", err)
	}
}

// TestAgentConfig_Name_TooShort tests that a single-character name is rejected.
func TestAgentConfig_Name_TooShort(t *testing.T) {
	cfg := minimalAgentConfig()
	cfg.Name = "a"
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for name too short, got nil")
	}
	if !strings.Contains(err.Error(), "at least 2 characters") {
		t.Errorf("expected 'at least 2 characters' in error, got: %v", err)
	}
}

// TestAgentConfig_Name_InvalidChars tests that names with invalid characters are rejected.
func TestAgentConfig_Name_InvalidChars(t *testing.T) {
	cases := []struct {
		name  string
		value string
	}{
		{"uppercase", "MyAgent"},
		{"leading hyphen", "-agent"},
		{"trailing hyphen", "agent-"},
		{"underscore", "my_agent"},
		{"spaces", "my agent"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := minimalAgentConfig()
			cfg.Name = tc.value
			err := cfg.Validate()
			if err == nil {
				t.Errorf("expected error for name %q, got nil", tc.value)
			}
		})
	}
}

// TestAgentConfig_Name_ValidFormats tests several valid name formats.
func TestAgentConfig_Name_ValidFormats(t *testing.T) {
	cases := []string{
		"ab",
		"my-agent",
		"agent01",
		"dev-ops-agent",
		"a1",
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := minimalAgentConfig()
			cfg.Name = name
			if err := cfg.Validate(); err != nil {
				t.Errorf("expected valid name %q, got error: %v", name, err)
			}
		})
	}
}

// --- model_tier validation ---

// TestAgentConfig_ModelTier_Nil tests that a nil model_tier is valid.
func TestAgentConfig_ModelTier_Nil(t *testing.T) {
	cfg := minimalAgentConfig()
	cfg.ModelTier = nil
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected nil model_tier to be valid, got error: %v", err)
	}
}

// TestAgentConfig_ModelTier_ValidTiers tests each valid routing tier.
func TestAgentConfig_ModelTier_ValidTiers(t *testing.T) {
	for _, tier := range VALID_TIERS {
		t.Run(tier, func(t *testing.T) {
			cfg := minimalAgentConfig()
			cfg.ModelTier = strPtr(tier)
			if err := cfg.Validate(); err != nil {
				t.Errorf("expected valid tier %q, got error: %v", tier, err)
			}
		})
	}
}

// TestAgentConfig_ModelTier_InvalidTier tests that an unrecognised tier is rejected.
func TestAgentConfig_ModelTier_InvalidTier(t *testing.T) {
	cfg := minimalAgentConfig()
	cfg.ModelTier = strPtr("ultra")
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid model_tier, got nil")
	}
	if !strings.Contains(err.Error(), "model_tier must be one of") {
		t.Errorf("expected 'model_tier must be one of' in error, got: %v", err)
	}
}

// --- Struct defaults ---

// TestAgentConfig_Defaults_Version tests that version defaults to "0.1" via applyDefaults.
func TestAgentConfig_Defaults_Version(t *testing.T) {
	cfg := &AgentConfig{}
	applyDefaults(cfg)
	if cfg.Version != "0.1" {
		t.Errorf("expected default version '0.1', got %q", cfg.Version)
	}
}

// TestAgentConfig_Defaults_Tier tests that tier defaults to "standard" via applyDefaults.
func TestAgentConfig_Defaults_Tier(t *testing.T) {
	cfg := &AgentConfig{}
	applyDefaults(cfg)
	if cfg.Tier != "standard" {
		t.Errorf("expected default tier 'standard', got %q", cfg.Tier)
	}
}

// TestAgentConfig_Defaults_Type tests that type defaults to "standard" via applyDefaults.
func TestAgentConfig_Defaults_Type(t *testing.T) {
	cfg := &AgentConfig{}
	applyDefaults(cfg)
	if cfg.Type != "standard" {
		t.Errorf("expected default type 'standard', got %q", cfg.Type)
	}
}

// TestAgentResponsivenessConfig_Default tests that responsiveness defaults to "mention-only".
func TestAgentResponsivenessConfig_Default(t *testing.T) {
	cfg := &AgentResponsivenessConfig{}
	applyDefaults(cfg)
	if cfg.Default != "mention-only" {
		t.Errorf("expected default responsiveness 'mention-only', got %q", cfg.Default)
	}
}

// TestAgentConfig_Validate_DefaultsResponsiveness tests that Validate() applies the
// responsiveness default when the field is empty.
func TestAgentConfig_Validate_DefaultsResponsiveness(t *testing.T) {
	cfg := minimalAgentConfig()
	cfg.Responsiveness.Default = ""
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Responsiveness.Default != "mention-only" {
		t.Errorf("expected responsiveness default applied, got %q", cfg.Responsiveness.Default)
	}
}

// --- Required fields ---

// TestAgentConfig_Required_Name tests that a missing name is rejected.
func TestAgentConfig_Required_Name(t *testing.T) {
	cfg := minimalAgentConfig()
	cfg.Name = ""
	err := validate.Struct(cfg)
	if err == nil {
		t.Fatal("expected validation error for missing name, got nil")
	}
}

// TestAgentConfig_Required_Role tests that a missing role is rejected.
func TestAgentConfig_Required_Role(t *testing.T) {
	cfg := minimalAgentConfig()
	cfg.Role = ""
	err := validate.Struct(cfg)
	if err == nil {
		t.Fatal("expected validation error for missing role, got nil")
	}
}

// TestAgentConfig_Required_Body_Runtime tests that a missing body runtime is rejected.
func TestAgentConfig_Required_Body_Runtime(t *testing.T) {
	cfg := minimalAgentConfig()
	cfg.Body.Runtime = ""
	err := validate.Struct(cfg)
	if err == nil {
		t.Fatal("expected validation error for missing body.runtime, got nil")
	}
}

// TestAgentConfig_Required_Body_Version tests that a missing body version is rejected.
func TestAgentConfig_Required_Body_Version(t *testing.T) {
	cfg := minimalAgentConfig()
	cfg.Body.Version = ""
	err := validate.Struct(cfg)
	if err == nil {
		t.Fatal("expected validation error for missing body.version, got nil")
	}
}

// TestAgentConfig_Required_Workspace_Ref tests that a missing workspace ref is rejected.
func TestAgentConfig_Required_Workspace_Ref(t *testing.T) {
	cfg := minimalAgentConfig()
	cfg.Workspace.Ref = ""
	err := validate.Struct(cfg)
	if err == nil {
		t.Fatal("expected validation error for missing workspace.ref, got nil")
	}
}

// --- Tier / Type validation ---

// TestAgentConfig_Tier_Invalid tests that an invalid tier is rejected.
func TestAgentConfig_Tier_Invalid(t *testing.T) {
	cfg := minimalAgentConfig()
	cfg.Tier = "admin"
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid tier, got nil")
	}
	if !strings.Contains(err.Error(), "tier must be one of") {
		t.Errorf("expected 'tier must be one of' in error, got: %v", err)
	}
}

// TestAgentConfig_Tier_ValidValues tests all valid tier values.
func TestAgentConfig_Tier_ValidValues(t *testing.T) {
	for _, tier := range []string{"standard", "elevated", "function"} {
		t.Run(tier, func(t *testing.T) {
			cfg := minimalAgentConfig()
			cfg.Tier = tier
			if err := cfg.Validate(); err != nil {
				t.Errorf("expected valid tier %q, got error: %v", tier, err)
			}
		})
	}
}

// TestAgentConfig_Type_Invalid tests that an invalid type is rejected.
func TestAgentConfig_Type_Invalid(t *testing.T) {
	cfg := minimalAgentConfig()
	cfg.Type = "unknown"
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid type, got nil")
	}
	if !strings.Contains(err.Error(), "type must be one of") {
		t.Errorf("expected 'type must be one of' in error, got: %v", err)
	}
}

// TestAgentConfig_Type_ValidValues tests all valid type values.
func TestAgentConfig_Type_ValidValues(t *testing.T) {
	for _, typ := range []string{"standard", "coordinator", "function"} {
		t.Run(typ, func(t *testing.T) {
			cfg := minimalAgentConfig()
			cfg.Type = typ
			if err := cfg.Validate(); err != nil {
				t.Errorf("expected valid type %q, got error: %v", typ, err)
			}
		})
	}
}

// --- Optional nested fields ---

// TestAgentConfig_WithTriage tests that a config with triage is valid.
func TestAgentConfig_WithTriage(t *testing.T) {
	cfg := minimalAgentConfig()
	cfg.Triage = &AgentTriageConfig{
		Domains: []string{"engineering"},
		Prompt:  "Route to engineering.",
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected valid config with triage, got error: %v", err)
	}
}

// TestAgentConfig_WithRequires tests that a config with requires fields is valid.
func TestAgentConfig_WithRequires(t *testing.T) {
	cfg := minimalAgentConfig()
	cfg.Requires = AgentRequires{
		Tools:        []string{"read_file"},
		Capabilities: []string{"brave-search"},
		Models:       []string{"claude-3-5-sonnet"},
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected valid config with requires, got error: %v", err)
	}
}

// TestAgentConfig_WithPolicy tests that a config with a policy ref is valid.
func TestAgentConfig_WithPolicy(t *testing.T) {
	cfg := minimalAgentConfig()
	cfg.Policy = AgentPolicyRef{
		InheritsFrom: strPtr("org-default"),
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected valid config with policy, got error: %v", err)
	}
}

// TestAgentMCPServerConfig_Fields tests that MCP server config fields are populated correctly.
func TestAgentMCPServerConfig_Fields(t *testing.T) {
	cfg := minimalAgentConfig()
	cfg.Body.MCPServers = map[string]AgentMCPServerConfig{
		"filesystem": {
			Command: "npx",
			Args:    []string{"-y", "@modelcontextprotocol/server-filesystem", "/workspace"},
			Env:     map[string]string{"NODE_ENV": "production"},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected valid config with MCP servers, got error: %v", err)
	}
}

func TestAgentConfig_LifecycleID(t *testing.T) {
	yamlData := []byte(`
version: "0.1"
name: testbot
lifecycle_id: "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
role: assistant
tier: standard
type: standard
body:
  image: agency-body:latest
workspace:
  ref: ubuntu-default
`)
	var cfg AgentConfig
	if err := yaml.Unmarshal(yamlData, &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.LifecycleID != "a1b2c3d4-e5f6-7890-abcd-ef1234567890" {
		t.Errorf("LifecycleID = %q, want a1b2c3d4-e5f6-7890-abcd-ef1234567890", cfg.LifecycleID)
	}
}

func TestAgentConfig_WithInstanceAttachment(t *testing.T) {
	cfg := minimalAgentConfig()
	cfg.Instances = AgentInstancesConfig{
		Attach: []AgentInstanceAttachment{{
			InstanceID: "inst_1234abcd",
			NodeID:     "drive_admin",
			Actions:    []string{"list_permissions"},
		}},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config with instance attachment, got %v", err)
	}
}

func TestAgentConfig_DuplicateInstanceAttachment(t *testing.T) {
	cfg := minimalAgentConfig()
	cfg.Instances = AgentInstancesConfig{
		Attach: []AgentInstanceAttachment{
			{InstanceID: "inst_1234abcd", NodeID: "drive_admin"},
			{InstanceID: "inst_1234abcd", NodeID: "drive_admin"},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected duplicate instance attachment error, got nil")
	}
	if !strings.Contains(err.Error(), "duplicate instance attachment") {
		t.Fatalf("unexpected error: %v", err)
	}
}
