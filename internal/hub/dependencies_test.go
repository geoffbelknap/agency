package hub

import (
	"os"
	"path/filepath"
	"testing"
)

func writeInstanceTemplate(t *testing.T, mgr *Manager, name, kind, content string) {
	t.Helper()
	instDir := mgr.Registry.InstanceDir(name)
	if instDir == "" {
		t.Fatalf("instance dir missing for %s", name)
	}
	if err := os.WriteFile(filepath.Join(instDir, kind+".yaml"), []byte(content), 0644); err != nil {
		t.Fatalf("write template for %s: %v", name, err)
	}
}

func TestManagerRemoveWithDependenciesRemovesOrphanAutoInstalledDependencies(t *testing.T) {
	home := t.TempDir()
	mgr := NewManager(home)

	pack, err := mgr.Registry.Create("ops-pack", "pack", "default/ops-pack")
	if err != nil {
		t.Fatalf("create pack: %v", err)
	}
	svc, err := mgr.Registry.Create("jira", "service", "default/jira")
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	conn, err := mgr.Registry.Create("jira-ops", "connector", "default/jira-ops")
	if err != nil {
		t.Fatalf("create connector: %v", err)
	}

	writeInstanceTemplate(t, mgr, pack.Name, "pack", `kind: pack
name: ops-pack
requires:
  services:
    - jira
  connectors:
    - jira-ops
`)
	writeInstanceTemplate(t, mgr, svc.Name, "service", "kind: service\nname: jira\n")
	writeInstanceTemplate(t, mgr, conn.Name, "connector", "kind: connector\nname: jira-ops\n")

	if err := mgr.Registry.MarkAutoInstalled(svc.Name, true); err != nil {
		t.Fatalf("mark service auto-installed: %v", err)
	}
	if err := mgr.Registry.MarkAutoInstalled(conn.Name, true); err != nil {
		t.Fatalf("mark connector auto-installed: %v", err)
	}
	if err := mgr.Registry.AddRequiredBy(svc.Name, pack.Name); err != nil {
		t.Fatalf("link service dependency: %v", err)
	}
	if err := mgr.Registry.AddRequiredBy(conn.Name, pack.Name); err != nil {
		t.Fatalf("link connector dependency: %v", err)
	}

	removed, err := mgr.RemoveWithDependencies(pack.Name)
	if err != nil {
		t.Fatalf("remove with dependencies: %v", err)
	}
	if len(removed) != 3 {
		t.Fatalf("removed count = %d, want 3", len(removed))
	}
	if mgr.Registry.Resolve(pack.Name) != nil || mgr.Registry.Resolve(svc.Name) != nil || mgr.Registry.Resolve(conn.Name) != nil {
		t.Fatal("expected parent and auto-installed dependencies to be removed")
	}
}

func TestManagerRemoveWithDependenciesKeepsExplicitDependency(t *testing.T) {
	home := t.TempDir()
	mgr := NewManager(home)

	pack, err := mgr.Registry.Create("ops-pack", "pack", "default/ops-pack")
	if err != nil {
		t.Fatalf("create pack: %v", err)
	}
	conn, err := mgr.Registry.Create("shared-connector", "connector", "default/shared-connector")
	if err != nil {
		t.Fatalf("create connector: %v", err)
	}

	writeInstanceTemplate(t, mgr, pack.Name, "pack", `kind: pack
name: ops-pack
requires:
  connectors:
    - shared-connector
`)
	writeInstanceTemplate(t, mgr, conn.Name, "connector", "kind: connector\nname: shared-connector\n")

	if err := mgr.Registry.AddRequiredBy(conn.Name, pack.Name); err != nil {
		t.Fatalf("link connector dependency: %v", err)
	}

	removed, err := mgr.RemoveWithDependencies(pack.Name)
	if err != nil {
		t.Fatalf("remove with dependencies: %v", err)
	}
	if len(removed) != 1 {
		t.Fatalf("removed count = %d, want 1", len(removed))
	}
	child := mgr.Registry.Resolve(conn.Name)
	if child == nil {
		t.Fatal("explicit dependency should remain installed")
	}
	if len(child.RequiredBy) != 0 {
		t.Fatalf("required_by = %v, want empty", child.RequiredBy)
	}
}

func TestDependencyRefsFromYAMLIncludesPackMissionAssignmentsAndRequires(t *testing.T) {
	data := []byte(`kind: pack
name: community-admin
requires:
  services:
    - slack
  presets:
    - community-administrator
  connectors:
    - slack-events
    - slack-interactivity
    - google-drive-admin
mission_assignments:
  - mission: community-vote-close
    agent: admin-coordinator
  - mission: community-memory-distill
    agent: admin-coordinator
`)

	deps := DependencyRefsFromYAML(data)
	got := map[string]string{}
	for _, dep := range deps {
		got[dep.Kind+":"+dep.Name] = dep.Kind
	}

	for _, key := range []string{
		"service:slack",
		"preset:community-administrator",
		"connector:slack-events",
		"connector:slack-interactivity",
		"connector:google-drive-admin",
		"mission:community-vote-close",
		"mission:community-memory-distill",
	} {
		if _, ok := got[key]; !ok {
			t.Fatalf("missing dependency ref %s in %#v", key, deps)
		}
	}
}
