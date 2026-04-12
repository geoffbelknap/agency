package hub

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func newTestRegistry(t *testing.T) (*Registry, string) {
	t.Helper()
	dir := t.TempDir()
	r := NewRegistry(dir)
	return r, dir
}

func TestRegistry_CreateInstance(t *testing.T) {
	r, _ := newTestRegistry(t)

	inst, err := r.Create("slack-incidents", "connector", "default/slack-events")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if inst.Name != "slack-incidents" {
		t.Errorf("expected name %q, got %q", "slack-incidents", inst.Name)
	}
	if inst.Kind != "connector" {
		t.Errorf("expected kind %q, got %q", "connector", inst.Kind)
	}
	if inst.Source != "default/slack-events" {
		t.Errorf("expected source %q, got %q", "default/slack-events", inst.Source)
	}
	if len(inst.ID) != 8 {
		t.Errorf("expected 8-char ID, got %q (len=%d)", inst.ID, len(inst.ID))
	}
	if inst.State != "installed" {
		t.Errorf("expected state %q, got %q", "installed", inst.State)
	}
	if inst.Created == "" {
		t.Error("expected Created to be set")
	}
}

func TestRegistry_ResolveByName(t *testing.T) {
	r, _ := newTestRegistry(t)

	inst, err := r.Create("slack-incidents", "connector", "default/slack-events")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	found := r.Resolve("slack-incidents")
	if found == nil {
		t.Fatal("Resolve by name returned nil")
	}
	if found.ID != inst.ID {
		t.Errorf("expected ID %q, got %q", inst.ID, found.ID)
	}
}

func TestRegistry_ResolveByID(t *testing.T) {
	r, _ := newTestRegistry(t)

	inst, err := r.Create("slack-incidents", "connector", "default/slack-events")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	found := r.Resolve(inst.ID)
	if found == nil {
		t.Fatal("Resolve by ID returned nil")
	}
	if found.Name != "slack-incidents" {
		t.Errorf("expected name %q, got %q", "slack-incidents", found.Name)
	}
}

func TestRegistry_ResolveNotFound(t *testing.T) {
	r, _ := newTestRegistry(t)

	found := r.Resolve("nonexistent")
	if found != nil {
		t.Errorf("expected nil, got %+v", found)
	}
}

func TestRegistry_DuplicateNameFails(t *testing.T) {
	r, _ := newTestRegistry(t)

	_, err := r.Create("slack-incidents", "connector", "default/slack-events")
	if err != nil {
		t.Fatalf("first Create failed: %v", err)
	}

	_, err = r.Create("slack-incidents", "connector", "default/slack-events")
	if err == nil {
		t.Error("expected error on duplicate name, got nil")
	}
}

func TestRegistry_Remove(t *testing.T) {
	r, home := newTestRegistry(t)

	inst, err := r.Create("slack-incidents", "connector", "default/slack-events")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Instance directory should exist
	instDir := filepath.Join(home, "connectors", inst.ID)
	if _, err := os.Stat(instDir); err != nil {
		t.Fatalf("instance directory not created: %v", err)
	}

	if err := r.Remove("slack-incidents"); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	// Should be gone from registry
	found := r.Resolve("slack-incidents")
	if found != nil {
		t.Error("instance still resolvable after Remove")
	}

	// Directory should be deleted
	if _, err := os.Stat(instDir); !os.IsNotExist(err) {
		t.Error("instance directory still exists after Remove")
	}
}

func TestRegistry_RemoveNotFound(t *testing.T) {
	r, _ := newTestRegistry(t)

	err := r.Remove("nonexistent")
	if err == nil {
		t.Error("expected error removing nonexistent instance, got nil")
	}
}

func TestRegistry_List(t *testing.T) {
	r, _ := newTestRegistry(t)

	r.Create("slack-incidents", "connector", "default/slack-events")
	r.Create("github-prs", "connector", "default/github")
	r.Create("my-preset", "preset", "default/base-preset")

	all := r.List("")
	if len(all) != 3 {
		t.Errorf("expected 3 instances, got %d", len(all))
	}
}

func TestRegistry_ListFilterByKind(t *testing.T) {
	r, _ := newTestRegistry(t)

	r.Create("slack-incidents", "connector", "default/slack-events")
	r.Create("github-prs", "connector", "default/github")
	r.Create("my-preset", "preset", "default/base-preset")

	connectors := r.List("connector")
	if len(connectors) != 2 {
		t.Errorf("expected 2 connectors, got %d", len(connectors))
	}
	for _, c := range connectors {
		if c.Kind != "connector" {
			t.Errorf("expected kind connector, got %q", c.Kind)
		}
	}

	presets := r.List("preset")
	if len(presets) != 1 {
		t.Errorf("expected 1 preset, got %d", len(presets))
	}
}

func TestRegistry_ListEmpty(t *testing.T) {
	r, _ := newTestRegistry(t)

	all := r.List("")
	if all == nil {
		// nil is acceptable for empty list, but len must be 0
	}
	if len(all) != 0 {
		t.Errorf("expected 0 instances, got %d", len(all))
	}
}

func TestRegistry_Persistence(t *testing.T) {
	r, home := newTestRegistry(t)

	inst, err := r.Create("slack-incidents", "connector", "default/slack-events")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Create a fresh registry pointing at the same home — simulates process restart
	r2 := NewRegistry(home)

	found := r2.Resolve("slack-incidents")
	if found == nil {
		t.Fatal("instance not found after reload")
	}
	if found.ID != inst.ID {
		t.Errorf("expected ID %q after reload, got %q", inst.ID, found.ID)
	}
	if found.Kind != "connector" {
		t.Errorf("expected kind %q after reload, got %q", "connector", found.Kind)
	}
}

func TestRegistry_SetState(t *testing.T) {
	r, _ := newTestRegistry(t)

	_, err := r.Create("slack-incidents", "connector", "default/slack-events")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if err := r.SetState("slack-incidents", "active"); err != nil {
		t.Fatalf("SetState failed: %v", err)
	}

	found := r.Resolve("slack-incidents")
	if found == nil {
		t.Fatal("instance not found after SetState")
	}
	if found.State != "active" {
		t.Errorf("expected state %q, got %q", "active", found.State)
	}
}

func TestRegistry_SetStateByID(t *testing.T) {
	r, _ := newTestRegistry(t)

	inst, err := r.Create("slack-incidents", "connector", "default/slack-events")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if err := r.SetState(inst.ID, "inactive"); err != nil {
		t.Fatalf("SetState by ID failed: %v", err)
	}

	found := r.Resolve(inst.ID)
	if found.State != "inactive" {
		t.Errorf("expected state %q, got %q", "inactive", found.State)
	}
}

func TestRegistry_SetStateNotFound(t *testing.T) {
	r, _ := newTestRegistry(t)

	err := r.SetState("nonexistent", "active")
	if err == nil {
		t.Error("expected error for nonexistent instance, got nil")
	}
}

func TestRegistry_InstanceDir(t *testing.T) {
	r, home := newTestRegistry(t)

	inst, err := r.Create("slack-incidents", "connector", "default/slack-events")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	dir := r.InstanceDir("slack-incidents")
	expected := filepath.Join(home, "connectors", inst.ID)
	if dir != expected {
		t.Errorf("expected dir %q, got %q", expected, dir)
	}

	// Also verify by ID
	dirByID := r.InstanceDir(inst.ID)
	if dirByID != expected {
		t.Errorf("expected dir %q by ID, got %q", expected, dirByID)
	}
}

func TestRegistry_InstanceDirNotFound(t *testing.T) {
	r, _ := newTestRegistry(t)

	dir := r.InstanceDir("nonexistent")
	if dir != "" {
		t.Errorf("expected empty string for nonexistent instance, got %q", dir)
	}
}

func TestRegistry_IDIsHex(t *testing.T) {
	r, _ := newTestRegistry(t)

	inst, err := r.Create("test-inst", "connector", "default/test")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	for _, ch := range inst.ID {
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f')) {
			t.Errorf("ID %q contains non-hex character %q", inst.ID, ch)
		}
	}
}

func TestRegistry_MigrateFromFlatFiles(t *testing.T) {
	dir := t.TempDir()

	// Create flat connector file (old format)
	connectorsDir := filepath.Join(dir, "connectors")
	os.MkdirAll(connectorsDir, 0755)
	os.WriteFile(filepath.Join(connectorsDir, "slack-events.yaml"),
		[]byte("name: slack-events\nkind: connector\nactive: true\n"), 0644)

	// Create old provenance file
	prov := []map[string]string{
		{"name": "slack-events", "kind": "connector", "source": "default", "installed_at": "2026-03-20T00:00:00Z"},
	}
	provJSON, _ := json.Marshal(prov)
	os.WriteFile(filepath.Join(dir, "hub-installed.json"), provJSON, 0644)

	reg := NewRegistry(dir)
	migrated, err := reg.MigrateIfNeeded()
	if err != nil {
		t.Fatal(err)
	}
	if migrated != 1 {
		t.Errorf("expected 1 migration, got %d", migrated)
	}

	// Instance exists in registry
	inst := reg.Resolve("slack-events")
	if inst == nil {
		t.Fatal("migrated instance not found")
	}
	if inst.Kind != "connector" {
		t.Errorf("kind = %q, want connector", inst.Kind)
	}
	if inst.Source != "default" {
		t.Errorf("source = %q, want default", inst.Source)
	}

	// File moved into instance directory
	instDir := reg.InstanceDir("slack-events")
	if _, err := os.Stat(filepath.Join(instDir, "connector.yaml")); err != nil {
		t.Error("connector.yaml not in instance directory")
	}

	// Old flat file removed
	if _, err := os.Stat(filepath.Join(connectorsDir, "slack-events.yaml")); err == nil {
		t.Error("old flat file should be removed")
	}

	// Old provenance renamed
	if _, err := os.Stat(filepath.Join(dir, "hub-installed.json.migrated")); err != nil {
		t.Error("hub-installed.json not renamed to .migrated")
	}
}

func TestRegistry_MigrateSkipsIfAlreadyMigrated(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir)
	reg.Create("existing", "connector", "test")

	migrated, err := reg.MigrateIfNeeded()
	if err != nil {
		t.Fatal(err)
	}
	if migrated != 0 {
		t.Errorf("expected 0 migrations (already migrated), got %d", migrated)
	}
}

func TestRegistry_MigrateMultipleKinds(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "connectors"), 0755)
	os.MkdirAll(filepath.Join(dir, "services"), 0755)
	os.WriteFile(filepath.Join(dir, "connectors", "slack.yaml"), []byte("name: slack\n"), 0644)
	os.WriteFile(filepath.Join(dir, "services", "github.yaml"), []byte("name: github\n"), 0644)

	reg := NewRegistry(dir)
	migrated, _ := reg.MigrateIfNeeded()
	if migrated != 2 {
		t.Errorf("expected 2 migrations, got %d", migrated)
	}
}

func TestRegistry_InstanceDirectoryCreated(t *testing.T) {
	r, home := newTestRegistry(t)

	inst, err := r.Create("slack-incidents", "connector", "default/slack-events")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	instDir := filepath.Join(home, "connectors", inst.ID)
	info, err := os.Stat(instDir)
	if err != nil {
		t.Fatalf("instance directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("instance path is not a directory")
	}
}
