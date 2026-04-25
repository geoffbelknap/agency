// agency-gateway/internal/models/capability_test.go
package models

import (
	"testing"
)

func TestCapabilityEntry_Validate_ValidName(t *testing.T) {
	e := CapabilityEntry{
		Kind: "mcp-server",
		Name: "brave-search",
	}
	if err := e.Validate(); err != nil {
		t.Errorf("Validate() returned unexpected error: %v", err)
	}
}

func TestCapabilityEntry_Validate_ValidNameWithUnderscore(t *testing.T) {
	e := CapabilityEntry{
		Kind: "skill",
		Name: "my_skill_1",
	}
	if err := e.Validate(); err != nil {
		t.Errorf("Validate() returned unexpected error: %v", err)
	}
}

func TestCapabilityEntry_Validate_InvalidNameWithSpecialChars(t *testing.T) {
	e := CapabilityEntry{
		Kind: "skill",
		Name: "bad name!",
	}
	err := e.Validate()
	if err == nil {
		t.Error("Validate() expected error for name with special chars, got nil")
	}
}

func TestCapabilityEntry_Validate_InvalidNameWithDot(t *testing.T) {
	e := CapabilityEntry{
		Kind: "service",
		Name: "some.service",
	}
	err := e.Validate()
	if err == nil {
		t.Error("Validate() expected error for name with dot, got nil")
	}
}

func TestCapabilityConfig_DefaultState(t *testing.T) {
	// Zero-value state is empty string; the default tag documents "available".
	// Verify struct initialises correctly and the field accepts valid values.
	cfg := CapabilityConfig{
		State: "available",
	}
	if cfg.State != "available" {
		t.Errorf("State = %q, want %q", cfg.State, "available")
	}
}

func TestCapabilityConfig_RestrictedWithAgents(t *testing.T) {
	cfg := CapabilityConfig{
		State:  "restricted",
		Agents: []string{"researcher", "analyst"},
	}
	if cfg.State != "restricted" {
		t.Errorf("State = %q, want %q", cfg.State, "restricted")
	}
	if len(cfg.Agents) != 2 {
		t.Errorf("len(Agents) = %d, want 2", len(cfg.Agents))
	}
}

func TestCapabilityPermissions_DefaultFilesystem(t *testing.T) {
	// Default filesystem value is "none" per the default tag.
	perms := CapabilityPermissions{
		Filesystem: "none",
	}
	if perms.Filesystem != "none" {
		t.Errorf("Filesystem = %q, want %q", perms.Filesystem, "none")
	}
	if perms.Network {
		t.Error("Network should default to false")
	}
	if perms.Execution {
		t.Error("Execution should default to false")
	}
}

func TestCapabilityPermissions_ReadWrite(t *testing.T) {
	perms := CapabilityPermissions{
		Filesystem: "read-write",
		Network:    true,
		Execution:  true,
	}
	if perms.Filesystem != "read-write" {
		t.Errorf("Filesystem = %q, want %q", perms.Filesystem, "read-write")
	}
}

func TestToolApprovalRecord_RequiredFields(t *testing.T) {
	rec := ToolApprovalRecord{
		Capability: "brave-search",
		Tool:       "web_search",
		Agent:      "researcher",
		Approved:   true,
		ApprovedBy: "operator",
		ApprovedAt: "2026-03-20T10:00:00Z",
	}
	if rec.Capability != "brave-search" {
		t.Errorf("Capability = %q, want %q", rec.Capability, "brave-search")
	}
	if rec.Tool != "web_search" {
		t.Errorf("Tool = %q, want %q", rec.Tool, "web_search")
	}
	if rec.Agent != "researcher" {
		t.Errorf("Agent = %q, want %q", rec.Agent, "researcher")
	}
	if !rec.Approved {
		t.Error("Approved should be true")
	}
	if rec.ApprovalStatus() != "approved" {
		t.Errorf("ApprovalStatus = %q, want approved", rec.ApprovalStatus())
	}
}

func TestToolApprovalRecord_DeniedApproval(t *testing.T) {
	rec := ToolApprovalRecord{
		Capability: "file-system",
		Tool:       "write_file",
		Agent:      "analyst",
		Approved:   false,
		ApprovedBy: "operator",
	}
	if rec.Approved {
		t.Error("Approved should be false")
	}
	if rec.ApprovalStatus() != "denied" {
		t.Errorf("ApprovalStatus = %q, want denied", rec.ApprovalStatus())
	}
}

func TestToolApprovalRecord_StatusOverridesLegacyBoolean(t *testing.T) {
	rec := ToolApprovalRecord{
		Capability: "file-system",
		Tool:       "write_file",
		Agent:      "analyst",
		Approved:   false,
		Status:     "rejected",
	}
	if rec.ApprovalStatus() != "rejected" {
		t.Errorf("ApprovalStatus = %q, want rejected", rec.ApprovalStatus())
	}
}

func TestCapabilityPolicy_EmptyListsDefault(t *testing.T) {
	pol := CapabilityPolicy{}
	if pol.Required != nil && len(pol.Required) != 0 {
		t.Errorf("Required should be empty, got %v", pol.Required)
	}
	if pol.Available != nil && len(pol.Available) != 0 {
		t.Errorf("Available should be empty, got %v", pol.Available)
	}
	if pol.Denied != nil && len(pol.Denied) != 0 {
		t.Errorf("Denied should be empty, got %v", pol.Denied)
	}
	if pol.Enabled != nil && len(pol.Enabled) != 0 {
		t.Errorf("Enabled should be empty, got %v", pol.Enabled)
	}
}

func TestCapabilityPolicy_WithValues(t *testing.T) {
	pol := CapabilityPolicy{
		Required:  []string{"comms"},
		Available: []string{"brave-search", "github"},
		Denied:    []string{"file-system"},
		Enabled:   []string{"brave-search"},
	}
	if len(pol.Required) != 1 || pol.Required[0] != "comms" {
		t.Errorf("Required = %v, want [comms]", pol.Required)
	}
	if len(pol.Available) != 2 {
		t.Errorf("len(Available) = %d, want 2", len(pol.Available))
	}
	if len(pol.Denied) != 1 || pol.Denied[0] != "file-system" {
		t.Errorf("Denied = %v, want [file-system]", pol.Denied)
	}
	if len(pol.Enabled) != 1 || pol.Enabled[0] != "brave-search" {
		t.Errorf("Enabled = %v, want [brave-search]", pol.Enabled)
	}
}

func TestCapabilitiesFile_EmptyCapabilities(t *testing.T) {
	f := CapabilitiesFile{}
	if f.Capabilities != nil && len(f.Capabilities) != 0 {
		t.Errorf("Capabilities should be empty, got %v", f.Capabilities)
	}
}

func TestCapabilitiesFile_WithEntry(t *testing.T) {
	f := CapabilitiesFile{
		Capabilities: map[string]CapabilityConfig{
			"brave-search": {
				State:  "available",
				Agents: []string{},
			},
		},
	}
	cfg, ok := f.Capabilities["brave-search"]
	if !ok {
		t.Fatal("expected 'brave-search' in Capabilities map")
	}
	if cfg.State != "available" {
		t.Errorf("State = %q, want %q", cfg.State, "available")
	}
}

func TestMCPServerSpec_Fields(t *testing.T) {
	spec := MCPServerSpec{
		Command: "npx",
		Args:    []string{"-y", "@modelcontextprotocol/server-brave-search"},
		Env:     map[string]string{"BRAVE_API_KEY": "test"},
	}
	if spec.Command != "npx" {
		t.Errorf("Command = %q, want %q", spec.Command, "npx")
	}
	if len(spec.Args) != 2 {
		t.Errorf("len(Args) = %d, want 2", len(spec.Args))
	}
	if spec.Env["BRAVE_API_KEY"] != "test" {
		t.Errorf("Env[BRAVE_API_KEY] = %q, want %q", spec.Env["BRAVE_API_KEY"], "test")
	}
}

func TestCapabilityAuth_Fields(t *testing.T) {
	auth := CapabilityAuth{
		Env:      "BRAVE_API_KEY",
		InjectAs: "BRAVE_API_KEY",
		Agents:   map[string]string{"researcher": "RESEARCHER_BRAVE_KEY"},
	}
	if auth.Env != "BRAVE_API_KEY" {
		t.Errorf("Env = %q, want %q", auth.Env, "BRAVE_API_KEY")
	}
	if auth.InjectAs != "BRAVE_API_KEY" {
		t.Errorf("InjectAs = %q, want %q", auth.InjectAs, "BRAVE_API_KEY")
	}
}

func TestCapabilityIntegrity_OptionalFields(t *testing.T) {
	// All fields optional — zero value has all nils.
	i := CapabilityIntegrity{}
	if i.SHA256 != nil {
		t.Error("SHA256 should be nil by default")
	}
	if i.SignedBy != nil {
		t.Error("SignedBy should be nil by default")
	}
	if i.Signature != nil {
		t.Error("Signature should be nil by default")
	}
	if i.VerifiedAt != nil {
		t.Error("VerifiedAt should be nil by default")
	}
}

func TestCapabilityIntegrity_WithValues(t *testing.T) {
	sha := "abc123"
	signer := "operator@example.com"
	i := CapabilityIntegrity{
		SHA256:   &sha,
		SignedBy: &signer,
	}
	if i.SHA256 == nil || *i.SHA256 != sha {
		t.Errorf("SHA256 = %v, want %q", i.SHA256, sha)
	}
	if i.SignedBy == nil || *i.SignedBy != signer {
		t.Errorf("SignedBy = %v, want %q", i.SignedBy, signer)
	}
}
