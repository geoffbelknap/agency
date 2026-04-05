package hub

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultSourceIsOCI(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(""), 0644)

	m := NewManager(tmpDir)
	cfg := m.loadConfig()
	if len(cfg.Hub.Sources) != 1 {
		t.Fatalf("expected 1 default source, got %d", len(cfg.Hub.Sources))
	}
	if cfg.Hub.Sources[0].EffectiveType() != "oci" {
		t.Errorf("expected oci default source, got %s", cfg.Hub.Sources[0].EffectiveType())
	}
	if cfg.Hub.Sources[0].Registry != "ghcr.io/geoffbelknap/agency-hub" {
		t.Errorf("unexpected registry: %s", cfg.Hub.Sources[0].Registry)
	}
}
