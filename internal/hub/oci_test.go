package hub

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestSourceTypeDefault(t *testing.T) {
	s := Source{Name: "official", URL: "https://github.com/geoffbelknap/agency-hub.git"}
	if s.EffectiveType() != "git" {
		t.Errorf("expected git, got %s", s.EffectiveType())
	}
}

func TestSourceTypeOCI(t *testing.T) {
	s := Source{Name: "official", Type: "oci", Registry: "ghcr.io/geoffbelknap/agency-hub"}
	if s.EffectiveType() != "oci" {
		t.Errorf("expected oci, got %s", s.EffectiveType())
	}
}

func TestSourceOCIRef(t *testing.T) {
	s := Source{Name: "official", Type: "oci", Registry: "ghcr.io/geoffbelknap/agency-hub"}
	ref := s.ComponentRef("connector", "limacharlie", "0.5.0")
	expected := "ghcr.io/geoffbelknap/agency-hub/connector/limacharlie:0.5.0"
	if ref != expected {
		t.Errorf("expected %s, got %s", expected, ref)
	}
}

func TestSourceOCIRefLatest(t *testing.T) {
	s := Source{Name: "official", Type: "oci", Registry: "ghcr.io/geoffbelknap/agency-hub"}
	ref := s.ComponentRef("pack", "security-ops", "")
	expected := "ghcr.io/geoffbelknap/agency-hub/pack/security-ops:latest"
	if ref != expected {
		t.Errorf("expected %s, got %s", expected, ref)
	}
}

func TestExtractRegistryHost(t *testing.T) {
	tests := []struct {
		input, expected string
	}{
		{"ghcr.io/geoffbelknap/agency-hub", "ghcr.io"},
		{"registry.example.com/org/repo", "registry.example.com"},
		{"localhost:5000", "localhost:5000"},
	}
	for _, tt := range tests {
		got := extractRegistryHost(tt.input)
		if got != tt.expected {
			t.Errorf("extractRegistryHost(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestExtractRepoPrefix(t *testing.T) {
	tests := []struct {
		input, expected string
	}{
		{"ghcr.io/geoffbelknap/agency-hub", "geoffbelknap/agency-hub"},
		{"registry.example.com/org/repo", "org/repo"},
		{"localhost:5000", ""},
	}
	for _, tt := range tests {
		got := extractRepoPrefix(tt.input)
		if got != tt.expected {
			t.Errorf("extractRepoPrefix(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestOCICacheStructure(t *testing.T) {
	// Verify that pulled files land in the right cache structure for discover().
	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, "hub-cache")
	sourceName := "official"
	kind := "connector"
	name := "test-connector"

	destDir := filepath.Join(cacheDir, sourceName, kind+"s", name)
	os.MkdirAll(destDir, 0755)

	// Write a component file like pullComponent would.
	yamlContent := []byte("name: test-connector\nkind: connector\nversion: 0.1.0\n")
	os.WriteFile(filepath.Join(destDir, "connector.yaml"), yamlContent, 0644)

	// Verify discover() can find it by creating a Manager with this cache.
	m := NewManager(tmpDir)
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	os.WriteFile(cfgPath, []byte(fmt.Sprintf("hub:\n  sources:\n    - name: %s\n      type: oci\n      registry: ghcr.io/test\n", sourceName)), 0644)

	results := m.discover()
	found := false
	for _, c := range results {
		if c.Name == "test-connector" && c.Kind == "connector" {
			found = true
		}
	}
	if !found {
		t.Error("discover() should find component in OCI cache structure")
	}
}

func TestNewOCIClient(t *testing.T) {
	c := newOCIClient("ghcr.io/geoffbelknap/agency-hub")
	if c.registry != "ghcr.io/geoffbelknap/agency-hub" {
		t.Errorf("expected registry to be set, got %q", c.registry)
	}
}
