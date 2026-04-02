// agency-gateway/internal/models/collective_test.go
package models

import (
	"testing"
)

// TestTeamConfig_NameValidation tests the name field validator.
func TestTeamConfig_NameValidation(t *testing.T) {
	tests := []struct {
		name    string
		team    string
		wantErr bool
	}{
		{"valid simple", "my-team", false},
		{"valid alphanumeric", "team01", false},
		{"valid with hyphens", "dev-ops-team", false},
		{"too short", "a", true},
		{"single char", "x", true},
		{"uppercase", "MyTeam", true},
		{"trailing hyphen", "team-", true},
		{"leading hyphen", "-team", true},
		{"spaces", "my team", true},
		{"underscore", "my_team", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := &TeamConfig{Name: tt.team}
			err := tc.Validate()
			if tt.wantErr && err == nil {
				t.Errorf("expected error for name %q, got nil", tt.team)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("expected no error for name %q, got: %v", tt.team, err)
			}
		})
	}
}

// TestTeamConfig_GetMember tests the GetMember lookup method.
func TestTeamConfig_GetMember(t *testing.T) {
	tc := &TeamConfig{
		Name: "my-team",
		Members: []TeamMember{
			{Name: "alice", AgentType: "coordinator"},
			{Name: "bob", AgentType: "standard"},
		},
	}

	m := tc.GetMember("alice")
	if m == nil {
		t.Fatal("expected to find member 'alice', got nil")
	}
	if m.Name != "alice" {
		t.Errorf("expected name 'alice', got %q", m.Name)
	}

	m = tc.GetMember("charlie")
	if m != nil {
		t.Errorf("expected nil for unknown member 'charlie', got %+v", m)
	}
}

// TestTeamConfig_MemberNames tests the MemberNames method.
func TestTeamConfig_MemberNames(t *testing.T) {
	tc := &TeamConfig{
		Name: "my-team",
		Members: []TeamMember{
			{Name: "alice"},
			{Name: "bob"},
			{Name: "carol"},
		},
	}

	names := tc.MemberNames()
	if len(names) != 3 {
		t.Fatalf("expected 3 names, got %d", len(names))
	}
	expected := map[string]bool{"alice": true, "bob": true, "carol": true}
	for _, n := range names {
		if !expected[n] {
			t.Errorf("unexpected name: %q", n)
		}
	}
}

// TestTeamConfig_Coordinators tests the Coordinators method.
func TestTeamConfig_Coordinators(t *testing.T) {
	tc := &TeamConfig{
		Name: "my-team",
		Members: []TeamMember{
			{Name: "alice", AgentType: "coordinator"},
			{Name: "bob", AgentType: "standard"},
			{Name: "carol", AgentType: "coordinator"},
			{Name: "dave", AgentType: "function"},
		},
	}

	coords := tc.Coordinators()
	if len(coords) != 2 {
		t.Fatalf("expected 2 coordinators, got %d", len(coords))
	}
	for _, c := range coords {
		if c.AgentType != "coordinator" {
			t.Errorf("expected agent_type 'coordinator', got %q", c.AgentType)
		}
	}
}

// TestTeamConfig_FunctionAgents tests the FunctionAgents method.
func TestTeamConfig_FunctionAgents(t *testing.T) {
	tc := &TeamConfig{
		Name: "my-team",
		Members: []TeamMember{
			{Name: "alice", AgentType: "coordinator"},
			{Name: "bob", AgentType: "function"},
			{Name: "carol", AgentType: "standard"},
		},
	}

	fns := tc.FunctionAgents()
	if len(fns) != 1 {
		t.Fatalf("expected 1 function agent, got %d", len(fns))
	}
	if fns[0].Name != "bob" {
		t.Errorf("expected 'bob', got %q", fns[0].Name)
	}
}

// TestTeamMember_Defaults tests that TeamMember default fields are applied by Validate.
func TestTeamMember_Defaults(t *testing.T) {
	tc := &TeamConfig{
		Name: "my-team",
		Members: []TeamMember{
			{Name: "alice"},
		},
	}

	if err := tc.Validate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}

	m := tc.GetMember("alice")
	if m == nil {
		t.Fatal("member 'alice' not found after Validate")
	}
	if m.Type != "agent" {
		t.Errorf("expected default type 'agent', got %q", m.Type)
	}
	if m.AgentType != "standard" {
		t.Errorf("expected default agent_type 'standard', got %q", m.AgentType)
	}
}

// TestHaltAuthority_Defaults tests that HaltAuthority default halt_types are applied.
func TestHaltAuthority_Defaults(t *testing.T) {
	tc := &TeamConfig{
		Name: "my-team",
		Members: []TeamMember{
			{
				Name: "security-agent",
				HaltAuthority: &HaltAuthority{
					CanHalt: []string{"alice"},
					// HaltTypes intentionally left empty to test default
				},
			},
		},
	}

	if err := tc.Validate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}

	m := tc.GetMember("security-agent")
	if m == nil {
		t.Fatal("member 'security-agent' not found")
	}
	if m.HaltAuthority == nil {
		t.Fatal("halt_authority is nil after Validate")
	}

	ha := m.HaltAuthority
	if len(ha.HaltTypes) != 2 {
		t.Fatalf("expected 2 default halt_types, got %d: %v", len(ha.HaltTypes), ha.HaltTypes)
	}
	expectedTypes := map[string]bool{"supervised": true, "immediate": true}
	for _, ht := range ha.HaltTypes {
		if !expectedTypes[ht] {
			t.Errorf("unexpected halt_type: %q", ht)
		}
	}
}

// TestTeamConfig_EmptyMemberNames tests MemberNames on an empty team.
func TestTeamConfig_EmptyMemberNames(t *testing.T) {
	tc := &TeamConfig{Name: "my-team"}
	names := tc.MemberNames()
	if len(names) != 0 {
		t.Errorf("expected empty names, got %v", names)
	}
}

// TestTeamConfig_NoCoordinators tests Coordinators when none present.
func TestTeamConfig_NoCoordinators(t *testing.T) {
	tc := &TeamConfig{
		Name: "my-team",
		Members: []TeamMember{
			{Name: "alice", AgentType: "standard"},
		},
	}
	coords := tc.Coordinators()
	if coords != nil {
		t.Errorf("expected nil coordinators, got %v", coords)
	}
}
