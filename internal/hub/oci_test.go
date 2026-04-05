package hub

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateGitSourceToOCI(t *testing.T) {
	tmpDir := t.TempDir()

	// Simulate old git-based config
	config := []byte("hub:\n  sources:\n    - name: official\n      url: https://github.com/geoffbelknap/agency-hub.git\n      branch: main\n")
	os.WriteFile(filepath.Join(tmpDir, "config.yaml"), config, 0644)

	m := NewManager(tmpDir)
	migrated := m.migrateDefaultSourceToOCI()

	if !migrated {
		t.Error("expected migration to occur")
	}

	// Re-read config and verify
	cfg := m.loadConfig()
	if len(cfg.Hub.Sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(cfg.Hub.Sources))
	}
	if cfg.Hub.Sources[0].EffectiveType() != "oci" {
		t.Errorf("expected oci, got %s", cfg.Hub.Sources[0].EffectiveType())
	}
	if cfg.Hub.Sources[0].Registry != "ghcr.io/geoffbelknap/agency-hub" {
		t.Errorf("unexpected registry: %s", cfg.Hub.Sources[0].Registry)
	}
}

func TestMigrateNoOpForOCISource(t *testing.T) {
	tmpDir := t.TempDir()

	// Already OCI — no migration needed
	config := []byte("hub:\n  sources:\n    - name: official\n      type: oci\n      registry: ghcr.io/geoffbelknap/agency-hub\n")
	os.WriteFile(filepath.Join(tmpDir, "config.yaml"), config, 0644)

	m := NewManager(tmpDir)
	migrated := m.migrateDefaultSourceToOCI()

	if migrated {
		t.Error("expected no migration for already-OCI source")
	}
}

func TestMigratePreservesOtherSources(t *testing.T) {
	tmpDir := t.TempDir()

	// Official (git) + custom source — only official should migrate
	config := []byte("hub:\n  sources:\n    - name: official\n      url: https://github.com/geoffbelknap/agency-hub.git\n      branch: main\n    - name: my-corp\n      url: https://github.com/my-corp/hub.git\n      branch: main\n")
	os.WriteFile(filepath.Join(tmpDir, "config.yaml"), config, 0644)

	m := NewManager(tmpDir)
	migrated := m.migrateDefaultSourceToOCI()

	if !migrated {
		t.Error("expected migration to occur")
	}

	cfg := m.loadConfig()
	if len(cfg.Hub.Sources) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(cfg.Hub.Sources))
	}
	// Official should be OCI now
	if cfg.Hub.Sources[0].EffectiveType() != "oci" {
		t.Errorf("official should be oci, got %s", cfg.Hub.Sources[0].EffectiveType())
	}
	// Custom should still be git
	if cfg.Hub.Sources[1].EffectiveType() != "git" {
		t.Errorf("my-corp should still be git, got %s", cfg.Hub.Sources[1].EffectiveType())
	}
}

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
