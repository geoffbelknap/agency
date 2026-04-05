package hub

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func TestCosignInstalled(t *testing.T) {
	// Just verify the function doesn't panic — actual availability varies
	_ = cosignInstalled()
}

func TestVerifySignatureNoCosign(t *testing.T) {
	// If cosign is not installed, verify we get a clear error
	if cosignInstalled() {
		t.Skip("cosign is installed, skipping missing-cosign test")
	}
	err := verifySignature(context.Background(), "ghcr.io/test/fake:latest")
	if err == nil {
		t.Error("expected error when cosign not installed")
	}
	if !strings.Contains(err.Error(), "cosign not installed") {
		t.Errorf("expected 'cosign not installed' error, got: %s", err)
	}
}

func TestManagerUpdateDispatchesOCI(t *testing.T) {
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	// Write config with an OCI source pointing to a non-routable address to avoid network hangs
	config := []byte("hub:\n  sources:\n    - name: official\n      type: oci\n      registry: localhost:1/agency-hub\n")
	os.WriteFile(filepath.Join(tmpDir, "config.yaml"), config, 0644)

	// Update will fail to reach the registry (no network in unit test)
	// but should attempt OCI sync, not git sync
	report, _ := m.Update()
	// Should have a warning about the OCI source (registry unreachable), not about git
	if len(report.Sources) != 1 || report.Sources[0].Name != "official" {
		t.Errorf("expected 1 source named 'official', got %v", report.Sources)
	}
	// Verify the warning is OCI-related, not git-related
	if len(report.Warnings) == 0 {
		t.Log("no warnings (unexpected but acceptable if localhost resolved)")
	} else {
		for _, w := range report.Warnings {
			if strings.Contains(w, "git") && !strings.Contains(w, "oci") {
				t.Errorf("expected OCI warning, got git-related: %s", w)
			}
		}
	}
}

func TestFindSourceByName(t *testing.T) {
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	config := []byte("hub:\n  sources:\n    - name: test-source\n      type: oci\n      registry: ghcr.io/test\n")
	os.WriteFile(filepath.Join(tmpDir, "config.yaml"), config, 0644)

	src := m.findSourceByName("test-source")
	if src == nil {
		t.Fatal("expected to find source")
	}
	if src.EffectiveType() != "oci" {
		t.Errorf("expected oci, got %s", src.EffectiveType())
	}

	missing := m.findSourceByName("nonexistent")
	if missing != nil {
		t.Error("expected nil for missing source")
	}
}
