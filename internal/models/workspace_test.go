// agency-gateway/internal/models/workspace_test.go
package models

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExtraMount_Validate checks absolute path enforcement.
func TestExtraMount_Validate(t *testing.T) {
	t.Run("both absolute — valid", func(t *testing.T) {
		m := &ExtraMount{Source: "/host/data", Target: "/workspace/data"}
		if err := m.Validate(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("relative source — invalid", func(t *testing.T) {
		m := &ExtraMount{Source: "relative/path", Target: "/workspace/data"}
		err := m.Validate()
		if err == nil {
			t.Error("expected error for relative source, got nil")
		} else if !strings.Contains(err.Error(), "source must be an absolute path") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("relative target — invalid", func(t *testing.T) {
		m := &ExtraMount{Source: "/host/data", Target: "relative/path"}
		err := m.Validate()
		if err == nil {
			t.Error("expected error for relative target, got nil")
		} else if !strings.Contains(err.Error(), "target must be an absolute path") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("empty source — invalid", func(t *testing.T) {
		m := &ExtraMount{Source: "", Target: "/workspace/data"}
		err := m.Validate()
		if err == nil {
			t.Error("expected error for empty source, got nil")
		} else if !strings.Contains(err.Error(), "source must be an absolute path") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("empty target — invalid", func(t *testing.T) {
		m := &ExtraMount{Source: "/host/data", Target: ""}
		err := m.Validate()
		if err == nil {
			t.Error("expected error for empty target, got nil")
		} else if !strings.Contains(err.Error(), "target must be an absolute path") {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}

// TestWorkspaceConfig_Defaults verifies default values are applied.
func TestWorkspaceConfig_Defaults(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "workspace.yaml")
	os.WriteFile(f, []byte("name: myworkspace\nbase:\n  image: agency-workspace:latest\n"), 0644)

	var cfg WorkspaceConfig
	if err := Load(f, &cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Version != "1.0" {
		t.Errorf("expected default version '1.0', got '%s'", cfg.Version)
	}
	if cfg.Base.User != "agent" {
		t.Errorf("expected default user 'agent', got '%s'", cfg.Base.User)
	}
	if cfg.Base.Filesystem != "readonly-root" {
		t.Errorf("expected default filesystem 'readonly-root', got '%s'", cfg.Base.Filesystem)
	}
	if cfg.Provides.Network != "mediated" {
		t.Errorf("expected default network 'mediated', got '%s'", cfg.Provides.Network)
	}
	if cfg.Resources.Memory != "2GB" {
		t.Errorf("expected default memory '2GB', got '%s'", cfg.Resources.Memory)
	}
	if cfg.Resources.CPU != "1.0" {
		t.Errorf("expected default cpu '1.0', got '%s'", cfg.Resources.CPU)
	}
	if cfg.Resources.Tmpfs != "512MB" {
		t.Errorf("expected default tmpfs '512MB', got '%s'", cfg.Resources.Tmpfs)
	}
	if cfg.Security.Capabilities != "none" {
		t.Errorf("expected default capabilities 'none', got '%s'", cfg.Security.Capabilities)
	}
	if cfg.Security.Seccomp != "default-strict" {
		t.Errorf("expected default seccomp 'default-strict', got '%s'", cfg.Security.Seccomp)
	}
	if !cfg.Security.NoNewPrivileges {
		t.Errorf("expected default no_new_privileges true, got false")
	}
}

// TestWorkspaceConfig_MissingRequired verifies that name and base.image are required.
func TestWorkspaceConfig_MissingRequired(t *testing.T) {
	t.Run("missing name", func(t *testing.T) {
		dir := t.TempDir()
		f := filepath.Join(dir, "workspace.yaml")
		os.WriteFile(f, []byte("base:\n  image: agency-workspace:latest\n"), 0644)

		var cfg WorkspaceConfig
		err := Load(f, &cfg)
		if err == nil {
			t.Error("expected error for missing name, got nil")
		}
	})

	t.Run("missing base image", func(t *testing.T) {
		dir := t.TempDir()
		f := filepath.Join(dir, "workspace.yaml")
		os.WriteFile(f, []byte("name: myworkspace\nbase:\n  user: agent\n"), 0644)

		var cfg WorkspaceConfig
		err := Load(f, &cfg)
		if err == nil {
			t.Error("expected error for missing base image, got nil")
		}
	})
}

// TestAgentWorkspaceConfig_Defaults verifies default values for agent workspace refs.
func TestAgentWorkspaceConfig_Defaults(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "workspace.yaml")
	os.WriteFile(f, []byte("agent: my-agent\nworkspace_ref: minimal\n"), 0644)

	var cfg AgentWorkspaceConfig
	if err := Load(f, &cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Version != "0.1" {
		t.Errorf("expected default version '0.1', got '%s'", cfg.Version)
	}
	if cfg.Agent != "my-agent" {
		t.Errorf("expected agent 'my-agent', got '%s'", cfg.Agent)
	}
	if cfg.WorkspaceRef != "minimal" {
		t.Errorf("expected workspace_ref 'minimal', got '%s'", cfg.WorkspaceRef)
	}
	if cfg.ProjectDir != nil {
		t.Errorf("expected nil project_dir, got %v", cfg.ProjectDir)
	}
	if len(cfg.ExtraMounts) != 0 {
		t.Errorf("expected empty extra_mounts, got %v", cfg.ExtraMounts)
	}
}

// TestAgentWorkspaceConfig_MissingRequired verifies that agent and workspace_ref are required.
func TestAgentWorkspaceConfig_MissingRequired(t *testing.T) {
	t.Run("missing agent", func(t *testing.T) {
		dir := t.TempDir()
		f := filepath.Join(dir, "workspace.yaml")
		os.WriteFile(f, []byte("workspace_ref: minimal\n"), 0644)

		var cfg AgentWorkspaceConfig
		err := Load(f, &cfg)
		if err == nil {
			t.Error("expected error for missing agent, got nil")
		}
	})

	t.Run("missing workspace_ref", func(t *testing.T) {
		dir := t.TempDir()
		f := filepath.Join(dir, "workspace.yaml")
		os.WriteFile(f, []byte("agent: my-agent\n"), 0644)

		var cfg AgentWorkspaceConfig
		err := Load(f, &cfg)
		if err == nil {
			t.Error("expected error for missing workspace_ref, got nil")
		}
	})
}

// TestDetectWorkspaceSchema verifies path-based schema detection.
func TestDetectWorkspaceSchema(t *testing.T) {
	t.Run("path with /agents/ returns AgentWorkspaceConfig", func(t *testing.T) {
		schema, err := detectWorkspaceSchema("/some/path/agents/myagent/workspace.yaml")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := schema.(*AgentWorkspaceConfig); !ok {
			t.Errorf("expected *AgentWorkspaceConfig, got %T", schema)
		}
	})

	t.Run("non-existent file without /agents/ returns AgentWorkspaceConfig", func(t *testing.T) {
		schema, err := detectWorkspaceSchema("/nonexistent/workspace.yaml")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := schema.(*AgentWorkspaceConfig); !ok {
			t.Errorf("expected *AgentWorkspaceConfig, got %T", schema)
		}
	})

	t.Run("file with base key returns WorkspaceConfig", func(t *testing.T) {
		dir := t.TempDir()
		f := filepath.Join(dir, "workspace.yaml")
		os.WriteFile(f, []byte("name: minimal\nbase:\n  image: agency-workspace:latest\n"), 0644)

		schema, err := detectWorkspaceSchema(f)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := schema.(*WorkspaceConfig); !ok {
			t.Errorf("expected *WorkspaceConfig, got %T", schema)
		}
	})

	t.Run("file without base key returns AgentWorkspaceConfig", func(t *testing.T) {
		dir := t.TempDir()
		f := filepath.Join(dir, "workspace.yaml")
		os.WriteFile(f, []byte("agent: my-agent\nworkspace_ref: minimal\n"), 0644)

		schema, err := detectWorkspaceSchema(f)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := schema.(*AgentWorkspaceConfig); !ok {
			t.Errorf("expected *AgentWorkspaceConfig, got %T", schema)
		}
	})
}

// TestWorkspaceConfig_LoadAndValidate tests fixture-based loading via LoadAndValidate.
func TestWorkspaceConfig_LoadAndValidate(t *testing.T) {
	tests := []struct {
		file      string
		agentPath bool
		wantErr   string
	}{
		{"valid_template.yaml", false, ""},
		{"valid_agent_ref.yaml", true, ""},
	}

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			fixturePath := filepath.Join("testdata", "models", "workspace", tt.file)
			data, err := os.ReadFile(fixturePath)
			if err != nil {
				t.Fatalf("fixture not found: %s", fixturePath)
			}

			dir := t.TempDir()
			var workspaceFile string
			if tt.agentPath {
				agentDir := filepath.Join(dir, "agents", "myagent")
				os.MkdirAll(agentDir, 0755)
				workspaceFile = filepath.Join(agentDir, "workspace.yaml")
			} else {
				workspaceFile = filepath.Join(dir, "workspace.yaml")
			}
			os.WriteFile(workspaceFile, data, 0644)

			err = LoadAndValidate(workspaceFile)

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
