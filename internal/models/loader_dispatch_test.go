package models

import (
	"reflect"
	"testing"
)

// TestDetectSchemaAll tests basic schema detection for all 10 file types.
// Verifies that detectSchema returns appropriate schema types for all required file names.
func TestDetectSchemaAll(t *testing.T) {
	tests := []struct {
		filename string
		wantType string // empty = nil (unknown file)
	}{
		{"org.yaml", "*models.OrgConfig"},
		{"agent.yaml", "*models.AgentConfig"},
		{"constraints.yaml", "*models.ConstraintsConfig"},
		{"principals.yaml", "*models.PrincipalsConfig"},
		{"pack.yaml", "*models.PackConfig"},
		{"connector.yaml", "*models.ConnectorConfig"},
		{"routing.yaml", "*models.RoutingConfig"},
		{"egress-domains.yaml", "*models.AgentEgressConfig"},
		{"unknown.yaml", ""},
		{"readme.md", ""},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			got, err := detectSchema("/" + tt.filename)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantType == "" {
				if got != nil {
					t.Errorf("expected nil, got %T", got)
				}
			} else {
				if got == nil {
					t.Fatalf("expected %s, got nil", tt.wantType)
				}
				gotType := reflect.TypeOf(got).String()
				if gotType != tt.wantType {
					t.Errorf("expected %s, got %s", tt.wantType, gotType)
				}
			}
		})
	}
}

// TestDetectSchemaCaseSensitivity verifies that detectSchema is case-sensitive.
// Only lowercase filenames should match; uppercase variants should return nil.
func TestDetectSchemaCaseSensitivity(t *testing.T) {
	tests := []struct {
		filename string
		wantNil  bool
	}{
		{"org.yaml", false},
		{"ORG.yaml", true},
		{"Org.yaml", true},
		{"agent.yaml", false},
		{"Agent.yaml", true},
		{"constraints.yaml", false},
		{"Constraints.yaml", true},
		{"principals.yaml", false},
		{"Principals.yaml", true},
		{"pack.yaml", false},
		{"Pack.yaml", true},
		{"connector.yaml", false},
		{"Connector.yaml", true},
		{"routing.yaml", false},
		{"Routing.yaml", true},
		{"egress-domains.yaml", false},
		{"Egress-domains.yaml", true},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			got, err := detectSchema("/" + tt.filename)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantNil && got != nil {
				t.Errorf("expected nil for %s, got %T", tt.filename, got)
			}
			if !tt.wantNil && got == nil {
				t.Errorf("expected schema for %s, got nil", tt.filename)
			}
		})
	}
}

// TestDetectSchemaCompletenessCoverage verifies all 10 Python SCHEMA_MAP entries are covered.
// Python SCHEMA_MAP (from models/__init__.py):
// - principals.yaml → PrincipalsConfig
// - agent.yaml → AgentConfig
// - constraints.yaml → ConstraintsConfig
// - policy.yaml → PolicyConfig or AgentPolicyConfig (path-aware, tested in policy_schema_test.go)
// - pack.yaml → PackConfig
// - connector.yaml → ConnectorConfig
// - workspace.yaml → WorkspaceConfig or AgentWorkspaceConfig (path-aware, tested in workspace_test.go)
// - org.yaml → OrgConfig
// - egress-domains.yaml → AgentEgressConfig
// - routing.yaml → RoutingConfig
func TestDetectSchemaCompletenessCoverage(t *testing.T) {
	expectedFiles := []string{
		"principals.yaml",
		"agent.yaml",
		"constraints.yaml",
		"policy.yaml",
		"pack.yaml",
		"connector.yaml",
		"workspace.yaml",
		"org.yaml",
		"egress-domains.yaml",
		"routing.yaml",
	}

	for _, filename := range expectedFiles {
		t.Run(filename, func(t *testing.T) {
			got, err := detectSchema("/" + filename)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got == nil {
				t.Fatalf("detectSchema returned nil for required file type: %s", filename)
			}
		})
	}
}

// TestDetectSchemaUnknownFiles verifies that unknown files return nil.
// This ensures that detectSchema only processes expected configuration files.
func TestDetectSchemaUnknownFiles(t *testing.T) {
	unknownFiles := []string{
		"readme.md",
		"README.md",
		"docker-compose.yml",
		"agents/my-agent/config.json",
		"policies/custom.txt",
		"workflow.yaml",
		"requirements.txt",
		".gitignore",
	}

	for _, filename := range unknownFiles {
		t.Run(filename, func(t *testing.T) {
			got, err := detectSchema("/" + filename)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != nil {
				t.Errorf("expected nil for unknown file %s, got %T", filename, got)
			}
		})
	}
}
