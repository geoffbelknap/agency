// agency-gateway/internal/models/org_test.go
package models

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOrgConfig(t *testing.T) {
	tests := []struct {
		file    string
		wantErr string
	}{
		{"valid_minimal.yaml", ""},
		{"valid_full.yaml", ""},
		{"invalid_extra_field.yaml", "unknown"},
		{"invalid_missing_name.yaml", "Name"},
	}

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			path := filepath.Join("testdata", "models", "org", tt.file)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("fixture not found: %s", path)
			}
			dir := t.TempDir()
			orgFile := filepath.Join(dir, "org.yaml")
			os.WriteFile(orgFile, data, 0644)

			err = LoadAndValidate(orgFile)

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

func TestOrgConfig_Defaults(t *testing.T) {
	var cfg OrgConfig
	data := []byte("name: test\noperator: op\ncreated: now\n")
	dir := t.TempDir()
	f := filepath.Join(dir, "org.yaml")
	os.WriteFile(f, data, 0644)

	Load(f, &cfg)

	if cfg.Version != "0.1" {
		t.Errorf("expected default version '0.1', got '%s'", cfg.Version)
	}
	if cfg.DeploymentMode != "standalone" {
		t.Errorf("expected default deployment_mode 'standalone', got '%s'", cfg.DeploymentMode)
	}
}
