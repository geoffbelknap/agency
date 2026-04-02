// agency-gateway/internal/models/principal_test.go
package models

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-playground/validator/v10"
)

func TestPrincipalsConfig(t *testing.T) {
	tests := []struct {
		file    string
		wantErr string
	}{
		{"valid_minimal.yaml", ""},
		{"valid_full.yaml", ""},
		{"invalid_extra_field.yaml", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			path := filepath.Join("testdata", "models", "principal", tt.file)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("fixture not found: %s", path)
			}
			dir := t.TempDir()
			principalFile := filepath.Join(dir, "principals.yaml")
			os.WriteFile(principalFile, data, 0644)

			err = LoadAndValidate(principalFile)

			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("expected valid, got error: %v", err)
				}
			} else {
				if err == nil {
					t.Error("expected error, got nil")
				}
			}
		})
	}
}

func TestPrincipalsConfig_Defaults(t *testing.T) {
	var cfg PrincipalsConfig
	data := []byte("humans:\n  - id: admin\n    name: Admin\n    roles: [op]\n    created: now\n")
	dir := t.TempDir()
	f := filepath.Join(dir, "principals.yaml")
	os.WriteFile(f, data, 0644)

	Load(f, &cfg)

	if cfg.Version != "0.1" {
		t.Errorf("expected default version '0.1', got '%s'", cfg.Version)
	}
}

func TestHumanPrincipal_Defaults(t *testing.T) {
	var cfg PrincipalsConfig
	data := []byte("humans:\n  - id: admin\n    name: Admin\n    roles: [op]\n    created: now\n")
	dir := t.TempDir()
	f := filepath.Join(dir, "principals.yaml")
	os.WriteFile(f, data, 0644)

	Load(f, &cfg)

	if len(cfg.Humans) != 1 {
		t.Fatalf("expected 1 human, got %d", len(cfg.Humans))
	}

	if cfg.Humans[0].Status != "active" {
		t.Errorf("expected default status 'active', got '%s'", cfg.Humans[0].Status)
	}
}

func TestAgentPrincipal_Defaults(t *testing.T) {
	var cfg PrincipalsConfig
	data := []byte("agents:\n  - id: agent-1\n    name: Agent\n")
	dir := t.TempDir()
	f := filepath.Join(dir, "principals.yaml")
	os.WriteFile(f, data, 0644)

	Load(f, &cfg)

	if len(cfg.Agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(cfg.Agents))
	}

	if cfg.Agents[0].Type != "standard" {
		t.Errorf("expected default type 'standard', got '%s'", cfg.Agents[0].Type)
	}
	if cfg.Agents[0].Status != "active" {
		t.Errorf("expected default status 'active', got '%s'", cfg.Agents[0].Status)
	}
}

func TestHumanPrincipal_RequiredFields(t *testing.T) {
	var human HumanPrincipal
	validate := validator.New()

	// Missing ID should fail validation
	human = HumanPrincipal{Name: "Test", Roles: []string{"op"}, Created: "now"}
	err := validate.Struct(human)
	if err == nil {
		t.Error("expected validation error for missing id field, got nil")
	}

	// Missing Name should fail validation
	human = HumanPrincipal{ID: "test", Roles: []string{"op"}, Created: "now"}
	err = validate.Struct(human)
	if err == nil {
		t.Error("expected validation error for missing name field, got nil")
	}
}

func TestExceptionRoute_Validation(t *testing.T) {
	data := []byte("exception_routes:\n  - domain: security\n    approvers: [admin]\n")
	dir := t.TempDir()
	f := filepath.Join(dir, "principals.yaml")
	os.WriteFile(f, data, 0644)

	err := LoadAndValidate(f)
	if err != nil {
		t.Errorf("expected valid, got error: %v", err)
	}
}

func TestExceptionRoute_DualApprovalDefault(t *testing.T) {
	var cfg PrincipalsConfig
	data := []byte("exception_routes:\n  - domain: security\n    approvers: [admin]\n")
	dir := t.TempDir()
	f := filepath.Join(dir, "principals.yaml")
	os.WriteFile(f, data, 0644)

	Load(f, &cfg)

	if len(cfg.ExceptionRoutes) != 1 {
		t.Fatalf("expected 1 exception route, got %d", len(cfg.ExceptionRoutes))
	}

	if cfg.ExceptionRoutes[0].RequiresDualApproval {
		t.Error("expected default requires_dual_approval to be false")
	}
}
