package hub

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSourceUpdateEmptyWhenNoChange(t *testing.T) {
	su := SourceUpdate{Name: "test", OldCommit: "abc1234", NewCommit: "abc1234"}
	if su.OldCommit != su.NewCommit {
		t.Fatal("expected same commit")
	}
	if su.CommitCount != 0 {
		t.Fatal("expected 0 commits")
	}
}

func TestOutdatedDetectsVersionDiff(t *testing.T) {
	home := t.TempDir()
	mgr := NewManager(home)

	// Create a fake installed instance with version 0.1.0
	inst, err := mgr.Registry.Create("test-connector", "connector", "default/test-connector")
	if err != nil {
		t.Fatal(err)
	}
	mgr.Registry.SetVersion(inst.Name, "0.1.0")

	// Write a fake component YAML in hub-cache with version 0.2.0
	cacheDir := filepath.Join(home, "hub-cache", "default", "connectors", "test-connector")
	os.MkdirAll(cacheDir, 0755)
	os.WriteFile(filepath.Join(cacheDir, "connector.yaml"), []byte("name: test-connector\nversion: \"0.2.0\"\n"), 0644)

	// Write hub config
	os.MkdirAll(home, 0755)
	os.WriteFile(filepath.Join(home, "config.yaml"), []byte("hub:\n  sources:\n    - name: default\n      url: https://example.com\n"), 0644)

	upgrades := mgr.Outdated()
	found := false
	for _, u := range upgrades {
		if u.Name == "test-connector" && u.InstalledVersion == "0.1.0" && u.AvailableVersion == "0.2.0" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected test-connector upgrade, got: %+v", upgrades)
	}
}

func TestUpdateReturnsReport(t *testing.T) {
	home := t.TempDir()
	mgr := NewManager(home)

	// No sources configured — should return empty report, no error
	os.MkdirAll(home, 0755)
	os.WriteFile(filepath.Join(home, "config.yaml"), []byte("hub:\n  sources: []\n"), 0644)

	report, err := mgr.Update()
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Sources) != 0 {
		t.Fatalf("expected 0 sources, got %d", len(report.Sources))
	}
}

func TestUpgradeAllSyncsManagedFiles(t *testing.T) {
	home := t.TempDir()
	mgr := NewManager(home)

	// Set up hub cache with an ontology file
	cacheDir := filepath.Join(home, "hub-cache", "default")
	os.MkdirAll(filepath.Join(cacheDir, "ontology"), 0755)
	os.WriteFile(filepath.Join(cacheDir, "ontology", "base-ontology.yaml"),
		[]byte("version: v2\nentity_types:\n  Host: {}\n  Software: {}\n"), 0644)

	// Set up local ontology (older version)
	os.MkdirAll(filepath.Join(home, "knowledge"), 0755)
	os.WriteFile(filepath.Join(home, "knowledge", "base-ontology.yaml"),
		[]byte("version: v1\nentity_types:\n  Host: {}\n"), 0644)

	// Config
	os.WriteFile(filepath.Join(home, "config.yaml"),
		[]byte("hub:\n  sources:\n    - name: default\n      url: https://example.com\n"), 0644)

	report, err := mgr.Upgrade(nil)
	if err != nil {
		t.Fatal(err)
	}

	foundOntology := false
	for _, f := range report.Files {
		if f.Category == "ontology" && f.Status == "upgraded" {
			foundOntology = true
		}
	}
	if !foundOntology {
		t.Fatalf("expected ontology upgrade, got: %+v", report.Files)
	}
}

func TestUpgradeSpecificComponent(t *testing.T) {
	home := t.TempDir()
	mgr := NewManager(home)

	// Create installed instance at version 0.1.0
	inst, _ := mgr.Registry.Create("test-conn", "connector", "default/test-conn")
	mgr.Registry.SetVersion(inst.Name, "0.1.0")

	// Write old YAML in instance dir
	instDir := mgr.Registry.InstanceDir(inst.Name)
	os.WriteFile(filepath.Join(instDir, "connector.yaml"),
		[]byte("name: test-conn\nversion: \"0.1.0\"\n"), 0644)

	// Write new version in hub cache
	cacheDir := filepath.Join(home, "hub-cache", "default", "connectors", "test-conn")
	os.MkdirAll(cacheDir, 0755)
	os.WriteFile(filepath.Join(cacheDir, "connector.yaml"),
		[]byte("name: test-conn\nversion: \"0.2.0\"\n"), 0644)

	// Config
	os.WriteFile(filepath.Join(home, "config.yaml"),
		[]byte("hub:\n  sources:\n    - name: default\n      url: https://example.com\n"), 0644)

	report, err := mgr.Upgrade([]string{"test-conn"})
	if err != nil {
		t.Fatal(err)
	}

	if len(report.Components) != 1 {
		t.Fatalf("expected 1 component, got %d", len(report.Components))
	}
	cu := report.Components[0]
	if cu.Status != "upgraded" || cu.OldVersion != "0.1.0" || cu.NewVersion != "0.2.0" {
		t.Fatalf("unexpected component upgrade: %+v", cu)
	}

	// Verify version was updated in registry
	updated := mgr.Registry.Resolve("test-conn")
	if updated.Version != "0.2.0" {
		t.Fatalf("expected version 0.2.0, got %s", updated.Version)
	}
}

func TestOutdatedNoChangeWhenVersionsMatch(t *testing.T) {
	home := t.TempDir()
	mgr := NewManager(home)

	inst, err := mgr.Registry.Create("test-connector", "connector", "default/test-connector")
	if err != nil {
		t.Fatal(err)
	}
	mgr.Registry.SetVersion(inst.Name, "0.1.0")

	cacheDir := filepath.Join(home, "hub-cache", "default", "connectors", "test-connector")
	os.MkdirAll(cacheDir, 0755)
	os.WriteFile(filepath.Join(cacheDir, "connector.yaml"), []byte("name: test-connector\nversion: \"0.1.0\"\n"), 0644)

	os.WriteFile(filepath.Join(home, "config.yaml"), []byte("hub:\n  sources:\n    - name: default\n      url: https://example.com\n"), 0644)

	upgrades := mgr.Outdated()
	if len(upgrades) != 0 {
		t.Fatalf("expected no upgrades, got: %+v", upgrades)
	}
}

func TestDiscoverFindsProviderComponent(t *testing.T) {
	home := t.TempDir()
	mgr := NewManager(home)

	// Write hub config defining a source
	os.WriteFile(filepath.Join(home, "config.yaml"), []byte("hub:\n  sources:\n    - name: default\n      url: https://example.com\n"), 0644)

	// Create provider cache dir and YAML using provider: as the identifier key
	providerDir := filepath.Join(home, "hub-cache", "default", "providers", "anthropic")
	os.MkdirAll(providerDir, 0755)
	os.WriteFile(filepath.Join(providerDir, "provider.yaml"),
		[]byte("provider: anthropic\nversion: \"1.0.0\"\ndescription: Anthropic Claude provider\n"), 0644)

	components := mgr.discover()

	var found *Component
	for i := range components {
		if components[i].Kind == "provider" && components[i].Name == "anthropic" {
			found = &components[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected to find provider component 'anthropic', got: %+v", components)
	}
	if found.Version != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %s", found.Version)
	}
}
