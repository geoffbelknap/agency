package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	instancepkg "github.com/geoffbelknap/agency/internal/instances"
)

func TestPlannerCompileAuthorityNode(t *testing.T) {
	inst := testInstance()

	manifest, err := Planner{}.Compile(inst)
	if err != nil {
		t.Fatalf("Compile(): %v", err)
	}
	if manifest.Kind != ManifestKind {
		t.Fatalf("Kind = %q, want %q", manifest.Kind, ManifestKind)
	}
	if len(manifest.Runtime.Nodes) != 1 {
		t.Fatalf("len(nodes) = %d, want 1", len(manifest.Runtime.Nodes))
	}
	node := manifest.Runtime.Nodes[0]
	if node.NodeID != "drive_admin" {
		t.Fatalf("node_id = %q, want drive_admin", node.NodeID)
	}
	if len(node.GrantSubjects) != 1 || node.GrantSubjects[0] != "agent:community-admin/coordinator" {
		t.Fatalf("grant_subjects = %v", node.GrantSubjects)
	}
	if len(node.ConsentActions) != 1 || node.ConsentActions[0] != "add_viewer" {
		t.Fatalf("consent_actions = %v", node.ConsentActions)
	}
}

func TestStoreSaveLoadManifest(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	manifest, err := Planner{}.Compile(testInstance())
	if err != nil {
		t.Fatalf("Compile(): %v", err)
	}
	if err := store.SaveManifest(manifest); err != nil {
		t.Fatalf("SaveManifest(): %v", err)
	}
	got, err := store.LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest(): %v", err)
	}
	if got.Metadata.InstanceID != manifest.Metadata.InstanceID {
		t.Fatalf("InstanceID = %q, want %q", got.Metadata.InstanceID, manifest.Metadata.InstanceID)
	}
}

func TestReconcilerMaterializesAuthorityConfig(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	manifest, err := Planner{}.Compile(testInstance())
	if err != nil {
		t.Fatalf("Compile(): %v", err)
	}
	if err := store.SaveManifest(manifest); err != nil {
		t.Fatalf("SaveManifest(): %v", err)
	}
	if err := (Reconciler{}).Reconcile(store, manifest); err != nil {
		t.Fatalf("Reconcile(): %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "runtime", "authority", "drive_admin.yaml"))
	if err != nil {
		t.Fatalf("read authority config: %v", err)
	}
	if !strings.Contains(string(data), "add_viewer") {
		t.Fatalf("authority config missing tool: %s", string(data))
	}
	got, err := store.LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest(): %v", err)
	}
	if got.Status.ReconcileState != ReconcileStateMaterialized {
		t.Fatalf("reconcile_state = %q, want materialized", got.Status.ReconcileState)
	}
}

func testInstance() *instancepkg.Instance {
	store := instancepkg.NewStore("/tmp/unused")
	inst := &instancepkg.Instance{
		ID:   "inst_123",
		Name: "community-admin",
		Source: instancepkg.InstanceSource{
			Template: instancepkg.PackageRef{Kind: "template", Name: "community-admin", Version: "1.0.0"},
		},
		Nodes: []instancepkg.Node{
			{
				ID:   "drive_admin",
				Kind: "connector.authority",
				Package: instancepkg.PackageRef{
					Kind:    "connector",
					Name:    "google-drive-admin",
					Version: "1.0.0",
				},
				Config: map[string]any{
					"tools":               []any{"add_viewer", "remove_viewer", "list_permissions"},
					"credential_bindings": []any{"service_account_json"},
				},
			},
		},
		Credentials: map[string]instancepkg.Binding{
			"service_account_json": {Type: "credref", Target: "credref:gdrive-admin"},
		},
		Grants: []instancepkg.GrantBinding{
			{
				Principal: "agent:community-admin/coordinator",
				Action:    "add_viewer",
				Resource:  "drive_admin",
				Config:    map[string]any{"consent_required": true},
			},
		},
	}
	_ = store
	return inst
}
