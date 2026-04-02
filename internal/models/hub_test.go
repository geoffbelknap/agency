// agency-gateway/internal/models/hub_test.go
package models

import (
	"os"
	"path/filepath"
	"testing"
)

// --- HubSource validation ---

func TestHubSource_Validate(t *testing.T) {
	tests := []struct {
		name    string
		src     HubSource
		wantErr bool
	}{
		{
			name:    "valid source",
			src:     HubSource{Name: "official", Type: "git", URL: "https://github.com/agency-hub/registry", Branch: "main"},
			wantErr: false,
		},
		{
			name:    "missing name",
			src:     HubSource{Type: "git", URL: "https://github.com/agency-hub/registry"},
			wantErr: true,
		},
		{
			name:    "missing url",
			src:     HubSource{Name: "official", Type: "git"},
			wantErr: true,
		},
		{
			name:    "invalid type",
			src:     HubSource{Name: "official", Type: "svn", URL: "https://github.com/agency-hub/registry"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validate.Struct(tt.src)
			if (err != nil) != tt.wantErr {
				t.Errorf("validate.Struct() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// --- HubConfig with nil/empty sources ---

func TestHubConfig_NilSources(t *testing.T) {
	// nil slice is valid — no validate:"required" on Sources
	cfg := HubConfig{}
	err := validate.Struct(cfg)
	if err != nil {
		t.Errorf("HubConfig with nil sources should be valid, got: %v", err)
	}
}

func TestHubConfig_EmptySources(t *testing.T) {
	cfg := HubConfig{Sources: []HubSource{}}
	err := validate.Struct(cfg)
	if err != nil {
		t.Errorf("HubConfig with empty sources should be valid, got: %v", err)
	}
}

// --- HubInstalledEntry validation ---

func TestHubInstalledEntry_Validate(t *testing.T) {
	tests := []struct {
		name    string
		entry   HubInstalledEntry
		wantErr bool
	}{
		{
			name: "valid entry",
			entry: HubInstalledEntry{
				Component:   "security-pack",
				Kind:        "pack",
				Source:      "official",
				CommitSHA:   "abc123def456",
				InstalledAt: "2026-03-20T00:00:00Z",
			},
			wantErr: false,
		},
		{
			name: "missing component",
			entry: HubInstalledEntry{
				Kind:        "pack",
				Source:      "official",
				CommitSHA:   "abc123",
				InstalledAt: "2026-03-20T00:00:00Z",
			},
			wantErr: true,
		},
		{
			name: "missing kind",
			entry: HubInstalledEntry{
				Component:   "security-pack",
				Source:      "official",
				CommitSHA:   "abc123",
				InstalledAt: "2026-03-20T00:00:00Z",
			},
			wantErr: true,
		},
		{
			name: "missing commit_sha",
			entry: HubInstalledEntry{
				Component:   "security-pack",
				Kind:        "pack",
				Source:      "official",
				InstalledAt: "2026-03-20T00:00:00Z",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validate.Struct(tt.entry)
			if (err != nil) != tt.wantErr {
				t.Errorf("validate.Struct() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// --- AgencyConfig fixtures ---

func TestAgencyConfig_Fixtures(t *testing.T) {
	tests := []struct {
		file    string
		wantErr bool
	}{
		{"valid_minimal.yaml", false},
		{"valid_full.yaml", false},
	}

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			src := filepath.Join("testdata", "models", "hub", tt.file)
			data, err := os.ReadFile(src)
			if err != nil {
				t.Fatalf("fixture not found: %s", src)
			}

			var cfg AgencyConfig
			if err := decodeStrict(data, &cfg); err != nil {
				if !tt.wantErr {
					t.Errorf("expected valid, got decode error: %v", err)
				}
				return
			}

			applyDefaults(&cfg)

			err = validate.Struct(cfg)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("expected valid, got error: %v", err)
			}
		})
	}
}

// --- HubSource defaults ---

func TestHubSource_Defaults(t *testing.T) {
	src := HubSource{Name: "test", URL: "https://github.com/example/repo"}
	applyDefaults(&src)

	if src.Type != "git" {
		t.Errorf("expected default type 'git', got '%s'", src.Type)
	}
	if src.Branch != "main" {
		t.Errorf("expected default branch 'main', got '%s'", src.Branch)
	}
}
