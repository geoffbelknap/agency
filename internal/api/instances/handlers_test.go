package instances

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	instancepkg "github.com/geoffbelknap/agency/internal/instances"
	"github.com/go-chi/chi/v5"
)

func TestInstancesCreateAndShow(t *testing.T) {
	s := instancepkg.NewStore(t.TempDir())
	r := chi.NewRouter()
	RegisterRoutes(r, Deps{Store: s})

	body := strings.NewReader(`{"name":"community-admin","source":{"template":{"kind":"template","name":"community-admin","version":"1.0.0"}}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create code = %d, want 201; body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"community-admin"`) {
		t.Fatalf("missing instance in create response: %s", rec.Body.String())
	}

	showReq := httptest.NewRequest(http.MethodGet, extractID(rec.Body.String()), nil)
	showRec := httptest.NewRecorder()
	r.ServeHTTP(showRec, showReq)
	if showRec.Code != http.StatusOK {
		t.Fatalf("show code = %d, want 200; body = %s", showRec.Code, showRec.Body.String())
	}
}

func TestInstancesCompileShowAndReconcileRuntimeManifest(t *testing.T) {
	s := instancepkg.NewStore(t.TempDir())
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
					"tools":               []any{"add_viewer", "list_permissions"},
					"credential_bindings": []any{"service_account_json"},
				},
			},
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
	if err := s.Create(t.Context(), inst); err != nil {
		t.Fatalf("Create(): %v", err)
	}

	r := chi.NewRouter()
	RegisterRoutes(r, Deps{Store: s})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances/inst_123/runtime/manifest", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("compile code = %d, want 201; body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"runtime_manifest"`) {
		t.Fatalf("missing runtime manifest in response: %s", rec.Body.String())
	}

	showReq := httptest.NewRequest(http.MethodGet, "/api/v1/instances/inst_123/runtime/manifest", nil)
	showRec := httptest.NewRecorder()
	r.ServeHTTP(showRec, showReq)
	if showRec.Code != http.StatusOK {
		t.Fatalf("show code = %d, want 200; body = %s", showRec.Code, showRec.Body.String())
	}

	reconcileReq := httptest.NewRequest(http.MethodPost, "/api/v1/instances/inst_123/runtime/reconcile", nil)
	reconcileRec := httptest.NewRecorder()
	r.ServeHTTP(reconcileRec, reconcileReq)
	if reconcileRec.Code != http.StatusOK {
		t.Fatalf("reconcile code = %d, want 200; body = %s", reconcileRec.Code, reconcileRec.Body.String())
	}

	instanceDir, err := s.InstanceDir("inst_123")
	if err != nil {
		t.Fatalf("InstanceDir(): %v", err)
	}
	authorityPath := filepath.Join(instanceDir, "runtime", "authority", "drive_admin.yaml")
	if _, err := os.Stat(authorityPath); err != nil {
		t.Fatalf("expected authority config at %s: %v", authorityPath, err)
	}
}

func extractID(body string) string {
	const marker = `"id":"`
	idx := strings.Index(body, marker)
	if idx == -1 {
		return "/api/v1/instances/missing"
	}
	start := idx + len(marker)
	end := strings.Index(body[start:], `"`)
	if end == -1 {
		return "/api/v1/instances/missing"
	}
	return "/api/v1/instances/" + body[start:start+end]
}
