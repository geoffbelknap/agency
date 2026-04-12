package runtime

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	authzcore "github.com/geoffbelknap/agency/internal/authz"
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
	status, err := store.LoadNodeStatus("drive_admin")
	if err != nil {
		t.Fatalf("LoadNodeStatus(): %v", err)
	}
	if status.State != NodeStateMaterialized {
		t.Fatalf("node state = %q, want materialized", status.State)
	}
}

func TestManagerStartStopAuthority(t *testing.T) {
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

	origStarter := authorityProcessStarter
	origStopper := authorityProcessStopper
	t.Cleanup(func() {
		authorityProcessStarter = origStarter
		authorityProcessStopper = origStopper
	})
	authorityProcessStarter = func(instanceDir string, manifest *Manifest, nodeID string) (int, int, string, error) {
		return 4321, 18888, "http://127.0.0.1:18888", nil
	}
	authorityProcessStopper = func(pid int) error { return nil }

	manager := Manager{}
	started, err := manager.StartAuthority(store, manifest, "drive_admin")
	if err != nil {
		t.Fatalf("StartAuthority(): %v", err)
	}
	if started.State != NodeStateActive {
		t.Fatalf("started state = %q, want active", started.State)
	}
	if started.PID != 4321 || started.Port != 18888 {
		t.Fatalf("unexpected runtime process info: %#v", started)
	}

	stopped, err := manager.StopAuthority(store, manifest, "drive_admin")
	if err != nil {
		t.Fatalf("StopAuthority(): %v", err)
	}
	if stopped.State != NodeStateStopped {
		t.Fatalf("stopped state = %q, want stopped", stopped.State)
	}
}

func TestAuthorityHandlerInvokeEnforcesConsent(t *testing.T) {
	manifest, err := Planner{}.Compile(testInstance())
	if err != nil {
		t.Fatalf("Compile(): %v", err)
	}
	handler := AuthorityHandler{Manifest: manifest, Resolver: authzcore.Resolver{}}

	req := httptest.NewRequest(http.MethodPost, "/invoke", bytes.NewBufferString(`{"subject":"agent:community-admin/coordinator","node_id":"drive_admin","action":"add_viewer"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("code = %d, want 403; body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"consent_needed":true`) {
		t.Fatalf("expected consent_needed response: %s", rec.Body.String())
	}
}

func TestAuthorityHandlerInvokeAllowsAuthorizedAction(t *testing.T) {
	manifest, err := Planner{}.Compile(testInstance())
	if err != nil {
		t.Fatalf("Compile(): %v", err)
	}
	handler := AuthorityHandler{Manifest: manifest, Resolver: authzcore.Resolver{}}

	req := httptest.NewRequest(http.MethodPost, "/invoke", bytes.NewBufferString(`{"subject":"agent:community-admin/coordinator","node_id":"drive_admin","action":"add_viewer","consent_provided":true}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("code = %d, want 501; body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"allowed":true`) {
		t.Fatalf("expected allowed response: %s", rec.Body.String())
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
