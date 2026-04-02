// agency-gateway/internal/models/pack_test.go
package models

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPackTeam_Validate_EmptyAgents tests that an empty agents list is rejected.
func TestPackTeam_Validate_EmptyAgents(t *testing.T) {
	pt := &PackTeam{
		Name:   "test-team",
		Agents: []PackAgent{},
	}
	err := pt.Validate()
	if err == nil {
		t.Fatal("expected error for empty agents, got nil")
	}
	if !strings.Contains(err.Error(), "at least one agent") {
		t.Errorf("expected error about agents, got: %v", err)
	}
}

// TestPackTeam_Validate_DuplicateAgents tests that duplicate agent names are rejected.
func TestPackTeam_Validate_DuplicateAgents(t *testing.T) {
	pt := &PackTeam{
		Name: "test-team",
		Agents: []PackAgent{
			{Name: "agent-1", Preset: "researcher"},
			{Name: "agent-1", Preset: "analyst"},
		},
	}
	err := pt.Validate()
	if err == nil {
		t.Fatal("expected error for duplicate agent names, got nil")
	}
	if !strings.Contains(err.Error(), "duplicate agent names") {
		t.Errorf("expected error about duplicate agent names, got: %v", err)
	}
}

// TestPackTeam_Validate_DuplicateChannels tests that duplicate channel names are rejected.
func TestPackTeam_Validate_DuplicateChannels(t *testing.T) {
	pt := &PackTeam{
		Name: "test-team",
		Agents: []PackAgent{
			{Name: "agent-1", Preset: "researcher"},
		},
		Channels: []PackChannel{
			{Name: "general"},
			{Name: "general"},
		},
	}
	err := pt.Validate()
	if err == nil {
		t.Fatal("expected error for duplicate channel names, got nil")
	}
	if !strings.Contains(err.Error(), "duplicate channel names") {
		t.Errorf("expected error about duplicate channel names, got: %v", err)
	}
}

// TestPackTeam_Validate_Valid tests that a valid team passes validation.
func TestPackTeam_Validate_Valid(t *testing.T) {
	pt := &PackTeam{
		Name: "test-team",
		Agents: []PackAgent{
			{Name: "agent-1", Preset: "researcher"},
			{Name: "agent-2", Preset: "analyst"},
		},
		Channels: []PackChannel{
			{Name: "general"},
			{Name: "reports"},
		},
	}
	if err := pt.Validate(); err != nil {
		t.Errorf("expected valid team, got error: %v", err)
	}
}

// TestPackConfig_Fixtures tests fixture-based loading via LoadAndValidate.
func TestPackConfig_Fixtures(t *testing.T) {
	tests := []struct {
		file    string
		wantErr string
	}{
		{"valid_minimal.yaml", ""},
		{"valid_full.yaml", ""},
		{"invalid_duplicate_agents.yaml", "duplicate agent names"},
		{"invalid_duplicate_channels.yaml", "duplicate channel names"},
		{"invalid_empty_agents.yaml", "at least one agent"},
	}

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			fixturePath := filepath.Join("testdata", "models", "pack", tt.file)
			data, err := os.ReadFile(fixturePath)
			if err != nil {
				t.Fatalf("fixture not found: %s", fixturePath)
			}

			dir := t.TempDir()
			packFile := filepath.Join(dir, "pack.yaml")
			if err := os.WriteFile(packFile, data, 0644); err != nil {
				t.Fatalf("failed to write temp file: %v", err)
			}

			err = LoadAndValidate(packFile)

			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("expected valid, got error: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.wantErr)
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got: %v", tt.wantErr, err)
				}
			}
		})
	}
}

// TestPackConfig_Defaults tests that default values are applied correctly.
func TestPackConfig_Defaults(t *testing.T) {
	dir := t.TempDir()
	packFile := filepath.Join(dir, "pack.yaml")
	data := []byte("kind: pack\nname: my-pack\nteam:\n  name: my-team\n  agents:\n    - name: a1\n      preset: researcher\n")
	if err := os.WriteFile(packFile, data, 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	var cfg PackConfig
	if err := Load(packFile, &cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Kind != "pack" {
		t.Errorf("expected default kind 'pack', got %q", cfg.Kind)
	}
	if cfg.Version != "1.0.0" {
		t.Errorf("expected default version '1.0.0', got %q", cfg.Version)
	}
}

// TestPackAgent_RoleDefault tests that PackAgent.Role defaults to "standard".
func TestPackAgent_RoleDefault(t *testing.T) {
	dir := t.TempDir()
	packFile := filepath.Join(dir, "pack.yaml")
	data := []byte("kind: pack\nname: my-pack\nteam:\n  name: my-team\n  agents:\n    - name: a1\n      preset: researcher\n")
	if err := os.WriteFile(packFile, data, 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	var cfg PackConfig
	if err := Load(packFile, &cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Team.Agents) == 0 {
		t.Fatal("expected at least one agent")
	}
	if cfg.Team.Agents[0].Role != "standard" {
		t.Errorf("expected default role 'standard', got %q", cfg.Team.Agents[0].Role)
	}
}
