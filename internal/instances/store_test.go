package instances

import (
	"context"
	"testing"
)

func TestStore_ValidateRejectsDuplicateNodeIDs(t *testing.T) {
	inst := &Instance{
		ID:   "inst_123",
		Name: "community-admin",
		Source: InstanceSource{
			Template: PackageRef{Kind: "template", Name: "community-admin", Version: "1.0.0"},
		},
		Nodes: []Node{
			{ID: "drive_admin", Kind: "connector.authority"},
			{ID: "drive_admin", Kind: "connector.authority"},
		},
	}
	if err := ValidateInstance(inst); err == nil {
		t.Fatal("expected duplicate node ID validation error")
	}
}

func TestStore_ValidateRejectsConsentGrantWithoutDeploymentID(t *testing.T) {
	inst := &Instance{
		ID:   "inst_123",
		Name: "community-admin",
		Source: InstanceSource{
			Template: PackageRef{Kind: "template", Name: "community-admin", Version: "1.0.0"},
		},
		Grants: []GrantBinding{{
			Principal: "agent:community-admin/coordinator",
			Action:    "add_viewer",
			Resource:  "drive_admin",
			Config: map[string]any{
				"operation_kind":     "grant_drive_viewer",
				"token_input_field":  "consent_token",
				"target_input_field": "drive_id",
			},
		}},
	}
	if err := ValidateInstance(inst); err == nil {
		t.Fatal("expected consent deployment id validation error")
	}
}

func TestStore_ValidateAcceptsConsentGrantWithDeploymentID(t *testing.T) {
	inst := &Instance{
		ID:   "inst_123",
		Name: "community-admin",
		Source: InstanceSource{
			Template: PackageRef{Kind: "template", Name: "community-admin", Version: "1.0.0"},
		},
		Config: map[string]any{"consent_deployment_id": "dep-123"},
		Grants: []GrantBinding{{
			Principal: "agent:community-admin/coordinator",
			Action:    "add_viewer",
			Resource:  "drive_admin",
			Config: map[string]any{
				"operation_kind":     "grant_drive_viewer",
				"token_input_field":  "consent_token",
				"target_input_field": "drive_id",
			},
		}},
	}
	if err := ValidateInstance(inst); err != nil {
		t.Fatalf("ValidateInstance(): %v", err)
	}
}

func TestStore_CreateAndGetInstance(t *testing.T) {
	s := NewStore(t.TempDir())
	inst := &Instance{
		ID:   "inst_123",
		Name: "community-admin",
		Source: InstanceSource{
			Template: PackageRef{Kind: "template", Name: "community-admin", Version: "1.0.0"},
		},
	}
	if err := s.Create(context.Background(), inst); err != nil {
		t.Fatalf("Create(): %v", err)
	}
	got, err := s.Get(context.Background(), "inst_123")
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	if got.Name != "community-admin" {
		t.Fatalf("Name = %q, want community-admin", got.Name)
	}
}

func TestStore_ListInstances(t *testing.T) {
	s := NewStore(t.TempDir())

	for _, id := range []string{"inst_123", "inst_456"} {
		if err := s.Create(context.Background(), &Instance{
			ID:   id,
			Name: id,
			Source: InstanceSource{
				Template: PackageRef{Kind: "template", Name: "community-admin", Version: "1.0.0"},
			},
		}); err != nil {
			t.Fatalf("Create(%s): %v", id, err)
		}
	}

	items, err := s.List(context.Background())
	if err != nil {
		t.Fatalf("List(): %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(List()) = %d, want 2", len(items))
	}
}

func TestStore_UpdateInstance(t *testing.T) {
	s := NewStore(t.TempDir())
	inst := &Instance{
		ID:   "inst_123",
		Name: "community-admin",
		Source: InstanceSource{
			Template: PackageRef{Kind: "template", Name: "community-admin", Version: "1.0.0"},
		},
	}
	if err := s.Create(context.Background(), inst); err != nil {
		t.Fatalf("Create(): %v", err)
	}

	if err := s.Update(context.Background(), "inst_123", func(inst *Instance) error {
		inst.Config = map[string]any{"workspace_id": "T123"}
		return nil
	}); err != nil {
		t.Fatalf("Update(): %v", err)
	}

	got, err := s.Get(context.Background(), "inst_123")
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	if got.Config["workspace_id"] != "T123" {
		t.Fatalf("workspace_id = %#v, want T123", got.Config["workspace_id"])
	}
}

func TestStore_ClaimAndRelease(t *testing.T) {
	s := NewStore(t.TempDir())
	inst := &Instance{
		ID:   "inst_123",
		Name: "community-admin",
		Source: InstanceSource{
			Template: PackageRef{Kind: "template", Name: "community-admin", Version: "1.0.0"},
		},
	}
	if err := s.Create(context.Background(), inst); err != nil {
		t.Fatalf("Create(): %v", err)
	}
	if err := s.Claim(context.Background(), "inst_123", "agency-local"); err != nil {
		t.Fatalf("Claim(): %v", err)
	}
	got, err := s.Get(context.Background(), "inst_123")
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	if got.Claim == nil || got.Claim.Owner != "agency-local" {
		t.Fatalf("Claim owner = %#v, want agency-local", got.Claim)
	}
	if err := s.Release(context.Background(), "inst_123"); err != nil {
		t.Fatalf("Release(): %v", err)
	}
	got, err = s.Get(context.Background(), "inst_123")
	if err != nil {
		t.Fatalf("Get() after release: %v", err)
	}
	if got.Claim != nil {
		t.Fatalf("Claim = %#v, want nil", got.Claim)
	}
}
