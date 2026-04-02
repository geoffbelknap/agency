// agency-gateway/internal/models/policy_schema_test.go
package models

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCommsScanningConfig_Validate verifies that no_credentials is always injected.
func TestCommsScanningConfig_Validate(t *testing.T) {
	t.Run("nil rules gets no_credentials", func(t *testing.T) {
		c := &CommsScanningConfig{}
		if err := c.Validate(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(c.Rules) != 1 || c.Rules[0] != "no_credentials" {
			t.Errorf("expected [no_credentials], got %v", c.Rules)
		}
	})

	t.Run("no_credentials already present — no change", func(t *testing.T) {
		c := &CommsScanningConfig{Rules: []string{"no_credentials", "no_pii"}}
		if err := c.Validate(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if c.Rules[0] != "no_credentials" {
			t.Errorf("expected no_credentials first, got %v", c.Rules)
		}
		if len(c.Rules) != 2 {
			t.Errorf("expected 2 rules, got %d", len(c.Rules))
		}
	})

	t.Run("missing no_credentials is prepended", func(t *testing.T) {
		c := &CommsScanningConfig{Rules: []string{"no_pii", "no_secrets"}}
		if err := c.Validate(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if c.Rules[0] != "no_credentials" {
			t.Errorf("expected no_credentials prepended, got %v", c.Rules)
		}
		if len(c.Rules) != 3 {
			t.Errorf("expected 3 rules, got %d", len(c.Rules))
		}
	})
}

// TestPolicyConfig_Defaults verifies default values are applied.
func TestPolicyConfig_Defaults(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "policy.yaml")
	os.WriteFile(f, []byte("bundle: default\n"), 0644)

	var cfg PolicyConfig
	if err := Load(f, &cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Version != "0.1" {
		t.Errorf("expected default version '0.1', got '%s'", cfg.Version)
	}
}

// TestAgentPolicyConfig_Defaults verifies default values are applied.
func TestAgentPolicyConfig_Defaults(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "policy.yaml")
	os.WriteFile(f, []byte("inherits_from: departments/engineering\n"), 0644)

	var cfg AgentPolicyConfig
	if err := Load(f, &cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Version != "0.1" {
		t.Errorf("expected default version '0.1', got '%s'", cfg.Version)
	}
	if cfg.InheritsFrom == nil || *cfg.InheritsFrom != "departments/engineering" {
		t.Errorf("expected inherits_from 'departments/engineering', got %v", cfg.InheritsFrom)
	}
}

// TestDetectPolicySchema verifies path-based schema detection.
func TestDetectPolicySchema(t *testing.T) {
	t.Run("path with /agents/ returns AgentPolicyConfig", func(t *testing.T) {
		schema, err := detectPolicySchema("/some/path/agents/myagent/policy.yaml")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := schema.(*AgentPolicyConfig); !ok {
			t.Errorf("expected *AgentPolicyConfig, got %T", schema)
		}
	})

	t.Run("non-existent file without /agents/ returns AgentPolicyConfig", func(t *testing.T) {
		schema, err := detectPolicySchema("/nonexistent/policy.yaml")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := schema.(*AgentPolicyConfig); !ok {
			t.Errorf("expected *AgentPolicyConfig, got %T", schema)
		}
	})

	t.Run("file with bundle key returns PolicyConfig", func(t *testing.T) {
		dir := t.TempDir()
		f := filepath.Join(dir, "policy.yaml")
		os.WriteFile(f, []byte("version: \"0.1\"\nbundle: default\n"), 0644)

		schema, err := detectPolicySchema(f)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := schema.(*PolicyConfig); !ok {
			t.Errorf("expected *PolicyConfig, got %T", schema)
		}
	})

	t.Run("file without bundle key returns AgentPolicyConfig", func(t *testing.T) {
		dir := t.TempDir()
		f := filepath.Join(dir, "policy.yaml")
		os.WriteFile(f, []byte("version: \"0.1\"\ninherits_from: departments/eng\n"), 0644)

		schema, err := detectPolicySchema(f)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := schema.(*AgentPolicyConfig); !ok {
			t.Errorf("expected *AgentPolicyConfig, got %T", schema)
		}
	})
}

// TestPolicyConfig_LoadAndValidate tests fixture-based loading via LoadAndValidate.
func TestPolicyConfig_LoadAndValidate(t *testing.T) {
	tests := []struct {
		file      string
		agentPath bool
		wantErr   string
	}{
		{"valid_org.yaml", false, ""},
		{"valid_agent.yaml", true, ""},
		{"invalid_extra_field.yaml", false, "not found"},
	}

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			fixturePath := filepath.Join("testdata", "models", "policy", tt.file)
			data, err := os.ReadFile(fixturePath)
			if err != nil {
				t.Fatalf("fixture not found: %s", fixturePath)
			}

			dir := t.TempDir()
			var policyFile string
			if tt.agentPath {
				agentDir := filepath.Join(dir, "agents", "myagent")
				os.MkdirAll(agentDir, 0755)
				policyFile = filepath.Join(agentDir, "policy.yaml")
			} else {
				policyFile = filepath.Join(dir, "policy.yaml")
			}
			os.WriteFile(policyFile, data, 0644)

			err = LoadAndValidate(policyFile)

			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("expected valid, got error: %v", err)
				}
			} else {
				if err == nil {
					t.Error("expected error, got nil")
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got: %v", tt.wantErr, err)
				}
			}
		})
	}
}
