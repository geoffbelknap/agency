package hub

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestMigrateGitSourceToOCI(t *testing.T) {
	t.Skip("OCI migration disabled until artifacts are published to GHCR")
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
	t.Skip("OCI migration disabled until artifacts are published to GHCR")
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
	t.Skip("OCI default disabled until artifacts are published to GHCR")
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

func TestOCIIndexParsesCatalogComponents(t *testing.T) {
	data := []byte(`
schema_version: 1
registry: "ghcr.io/geoffbelknap/agency-hub"
components:
  - kind: "connector"
    name: "limacharlie"
    version: "0.3.0"
    ref: "ghcr.io/geoffbelknap/agency-hub/connector/limacharlie:0.3.0"
    path: "connectors/limacharlie/connector.yaml"
    metadata_path: "connectors/limacharlie/metadata.yaml"
`)

	var index ociIndex
	if err := yaml.Unmarshal(data, &index); err != nil {
		t.Fatalf("unmarshal index: %v", err)
	}
	if index.SchemaVersion != 1 {
		t.Fatalf("schema version = %d, want 1", index.SchemaVersion)
	}
	if len(index.Components) != 1 {
		t.Fatalf("components = %d, want 1", len(index.Components))
	}
	component := index.Components[0]
	if component.Kind != "connector" || component.Name != "limacharlie" {
		t.Fatalf("unexpected component: %+v", component)
	}
	if component.Path != "connectors/limacharlie/connector.yaml" {
		t.Fatalf("path = %q", component.Path)
	}
}

func TestSplitOCIRef(t *testing.T) {
	repo, ref, err := splitOCIRef("ghcr.io/geoffbelknap/agency-hub/connector/limacharlie:0.3.0")
	if err != nil {
		t.Fatalf("split ref: %v", err)
	}
	if repo != "ghcr.io/geoffbelknap/agency-hub/connector/limacharlie" {
		t.Fatalf("repo = %q", repo)
	}
	if ref != "0.3.0" {
		t.Fatalf("ref = %q", ref)
	}

	repo, ref, err = splitOCIRef("localhost:5000/agency-hub/connector/limacharlie")
	if err != nil {
		t.Fatalf("split registry-with-port ref: %v", err)
	}
	if repo != "localhost:5000/agency-hub/connector/limacharlie" || ref != "" {
		t.Fatalf("unexpected registry-with-port split: repo=%q ref=%q", repo, ref)
	}
}

func TestSafeCachePathRejectsEscapes(t *testing.T) {
	root := t.TempDir()

	if _, err := safeCachePath(root, "../outside.yaml"); err == nil {
		t.Fatal("expected parent traversal to be rejected")
	}
	if _, err := safeCachePath(root, "/tmp/outside.yaml"); err == nil {
		t.Fatal("expected absolute path to be rejected")
	}

	path, err := safeCachePath(root, "connectors/limacharlie/connector.yaml")
	if err != nil {
		t.Fatalf("safe path rejected: %v", err)
	}
	if path != filepath.Join(root, "connectors", "limacharlie", "connector.yaml") {
		t.Fatalf("path = %q", path)
	}
}

func TestCopyPulledFilePreservesCatalogPath(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()
	srcFile := filepath.Join(src, "connectors", "limacharlie", "connector.yaml")
	if err := os.MkdirAll(filepath.Dir(srcFile), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(srcFile, []byte("name: limacharlie\n"), 0644); err != nil {
		t.Fatal(err)
	}

	destFile := filepath.Join(dest, "connectors", "limacharlie", "connector.yaml")
	if err := copyPulledFile(src, "connectors/limacharlie/connector.yaml", destFile); err != nil {
		t.Fatalf("copy pulled file: %v", err)
	}
	data, err := os.ReadFile(destFile)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(data) != "name: limacharlie\n" {
		t.Fatalf("dest data = %q", string(data))
	}
}
