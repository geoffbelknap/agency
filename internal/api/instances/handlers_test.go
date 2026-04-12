package instances

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/events"
	"github.com/geoffbelknap/agency/internal/hub"
	"github.com/geoffbelknap/agency/internal/hubclient"
	instancepkg "github.com/geoffbelknap/agency/internal/instances"
	runpkg "github.com/geoffbelknap/agency/internal/runtime"
	"github.com/go-chi/chi/v5"
	"log/slog"
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

func TestCreateInstanceFromAuthorityPackage(t *testing.T) {
	home := t.TempDir()
	s := instancepkg.NewStore(filepath.Join(home, "instances"))
	reg := hub.NewRegistry(filepath.Join(home, "hub-registry"))

	pkgDir := filepath.Join(home, "hub-registry", "connectors", "google-drive-admin")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(): %v", err)
	}
	pkgPath := filepath.Join(pkgDir, "connector.yaml")
	if err := os.WriteFile(pkgPath, []byte(`kind: connector
name: google-drive-admin
version: "1.0.0"
requires:
  credentials:
    - name: google-drive-admin
      scope: service-grant
      grant_name: google-drive-admin
  auth:
    type: google_service_account
    scopes:
      - https://www.googleapis.com/auth/drive
source:
  type: none
config:
  allow_whitelist_mutations:
    type: bool
    default: false
mcp:
  name: google-drive-admin
  credential: google-drive-admin
  api_base: https://www.googleapis.com
  tools:
    - name: drive_list_file_permissions
      method: GET
      path: /drive/v3/files/{file_id}/permissions
      parameters:
        file_id: {type: string}
        fields: {type: string}
      query_params: [fields]
`), 0o644); err != nil {
		t.Fatalf("WriteFile(): %v", err)
	}
	if err := reg.PutPackage(hub.InstalledPackage{
		Kind:      "connector",
		Name:      "google-drive-admin",
		Version:   "1.0.0",
		Trust:     "verified",
		Path:      pkgPath,
		Assurance: []string{"publisher_verified", "ask_partial"},
		Spec: map[string]any{
			"runtime": map[string]any{
				"executor": map[string]any{
					"kind":     "http_json",
					"base_url": "https://www.googleapis.com",
				},
			},
		},
	}); err != nil {
		t.Fatalf("PutPackage(): %v", err)
	}

	r := chi.NewRouter()
	RegisterRoutes(r, Deps{Store: s, Registry: reg})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances/from-package", strings.NewReader(`{"kind":"connector","name":"google-drive-admin","instance_name":"community-drive","config":{"consent_deployment_id":"dep-123","whitelist":["file:file-123","folder:folder-456"]}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("code = %d, want 201; body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"community-drive"`) {
		t.Fatalf("missing instance name: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"connector.authority"`) {
		t.Fatalf("missing authority node: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"google-drive-admin"`) {
		t.Fatalf("missing package reference: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"resource_whitelist":[{"id":"file-123","kind":"file"},{"id":"folder-456","kind":"folder"}]`) {
		t.Fatalf("missing derived resource whitelist: %s", rec.Body.String())
	}
}

func TestCreateInstanceFromIngressPackage(t *testing.T) {
	home := t.TempDir()
	s := instancepkg.NewStore(filepath.Join(home, "instances"))
	reg := hub.NewRegistry(filepath.Join(home, "hub-registry"))

	pkgDir := filepath.Join(home, "hub-registry", "connectors", "slack-interactivity")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(): %v", err)
	}
	pkgPath := filepath.Join(pkgDir, "connector.yaml")
	if err := os.WriteFile(pkgPath, []byte(`kind: connector
name: slack-interactivity
version: "1.0.0"
source:
  type: webhook
  path: /webhooks/slack-interactivity
config:
  interactivity_target_agent:
    type: string
routes:
  - match: {}
    target:
      agent: "${interactivity_target_agent}"
`), 0o644); err != nil {
		t.Fatalf("WriteFile(): %v", err)
	}
	if err := reg.PutPackage(hub.InstalledPackage{
		Kind:      "connector",
		Name:      "slack-interactivity",
		Version:   "1.0.0",
		Trust:     "verified",
		Path:      pkgPath,
		Assurance: []string{"publisher_verified", "ask_partial"},
		Spec: map[string]any{
			"runtime": map[string]any{
				"executor": map[string]any{
					"kind":     "http_json",
					"base_url": "https://slack.com",
				},
			},
		},
	}); err != nil {
		t.Fatalf("PutPackage(): %v", err)
	}

	r := chi.NewRouter()
	RegisterRoutes(r, Deps{Store: s, Registry: reg})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances/from-package", strings.NewReader(`{"kind":"connector","name":"slack-interactivity","config":{"interactivity_target_agent":"slack-bridge"}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("code = %d, want 201; body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"connector.ingress"`) {
		t.Fatalf("missing ingress node: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"connector.authority"`) {
		t.Fatalf("missing authority node: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"interactivity_target_agent":"slack-bridge"`) {
		t.Fatalf("missing instance config: %s", rec.Body.String())
	}
}

func TestCreateInstanceFromPackageRejectsInsufficientAssurance(t *testing.T) {
	home := t.TempDir()
	s := instancepkg.NewStore(filepath.Join(home, "instances"))
	reg := hub.NewRegistry(filepath.Join(home, "hub-registry"))

	pkgDir := filepath.Join(home, "hub-registry", "connectors", "google-drive-admin")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(): %v", err)
	}
	pkgPath := filepath.Join(pkgDir, "connector.yaml")
	if err := os.WriteFile(pkgPath, []byte("kind: connector\nname: google-drive-admin\nversion: \"1.0.0\"\nsource:\n  type: none\nmcp:\n  name: google-drive-admin\n  credential: google-drive-admin\n  api_base: https://www.googleapis.com\n  tools:\n    - name: drive_list_file_permissions\n      method: GET\n      path: /drive/v3/files/{file_id}/permissions\n      parameters:\n        file_id: {type: string}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(): %v", err)
	}
	if err := reg.PutPackage(hub.InstalledPackage{
		Kind:      "connector",
		Name:      "google-drive-admin",
		Version:   "1.0.0",
		Trust:     "verified",
		Path:      pkgPath,
		Assurance: []string{"publisher_verified"},
		Spec: map[string]any{
			"runtime": map[string]any{
				"executor": map[string]any{
					"kind":     "http_json",
					"base_url": "https://www.googleapis.com",
					"actions": map[string]any{
						"drive_list_file_permissions": map[string]any{
							"method": "GET",
							"path":   "/drive/v3/files/{file_id}/permissions",
						},
					},
				},
			},
		},
	}); err != nil {
		t.Fatalf("PutPackage(): %v", err)
	}

	r := chi.NewRouter()
	RegisterRoutes(r, Deps{Store: s, Registry: reg})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances/from-package", strings.NewReader(`{"kind":"connector","name":"google-drive-admin"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "insufficient package assurance") {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
}

func TestInstancesUpdate(t *testing.T) {
	s := instancepkg.NewStore(t.TempDir())
	inst := &instancepkg.Instance{
		ID:   "inst_123",
		Name: "community-admin",
		Source: instancepkg.InstanceSource{
			Template: instancepkg.PackageRef{Kind: "template", Name: "community-admin", Version: "1.0.0"},
		},
	}
	if err := s.Create(t.Context(), inst); err != nil {
		t.Fatalf("Create(): %v", err)
	}

	r := chi.NewRouter()
	RegisterRoutes(r, Deps{Store: s})

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/instances/inst_123", strings.NewReader(`{"name":"community-admin-v2","config":{"consent_deployment_id":"dep-123"}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update code = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	got, err := s.Get(t.Context(), "inst_123")
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	if got.Name != "community-admin-v2" {
		t.Fatalf("name = %q", got.Name)
	}
	if got.Config["consent_deployment_id"] != "dep-123" {
		t.Fatalf("config = %#v", got.Config)
	}
}

func TestCreateInstanceFromPackageAllowsStructuredAssurance(t *testing.T) {
	home := t.TempDir()
	s := instancepkg.NewStore(filepath.Join(home, "instances"))
	reg := hub.NewRegistry(filepath.Join(home, "hub-registry"))

	pkgDir := filepath.Join(home, "hub-registry", "connectors", "google-drive-admin")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(): %v", err)
	}
	pkgPath := filepath.Join(pkgDir, "connector.yaml")
	if err := os.WriteFile(pkgPath, []byte("kind: connector\nname: google-drive-admin\nversion: \"1.0.0\"\nsource:\n  type: none\nmcp:\n  name: google-drive-admin\n  credential: google-drive-admin\n  api_base: https://www.googleapis.com\n  tools:\n    - name: drive_list_file_permissions\n      method: GET\n      path: /drive/v3/files/{file_id}/permissions\n      parameters:\n        file_id: {type: string}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(): %v", err)
	}
	if err := reg.PutPackage(hub.InstalledPackage{
		Kind:    "connector",
		Name:    "google-drive-admin",
		Version: "1.0.0",
		Trust:   "verified",
		Path:    pkgPath,
		Spec: map[string]any{
			"runtime": map[string]any{
				"executor": map[string]any{
					"kind":     "http_json",
					"base_url": "https://www.googleapis.com",
					"actions": map[string]any{
						"drive_list_file_permissions": map[string]any{
							"method": "GET",
							"path":   "/drive/v3/files/{file_id}/permissions",
						},
					},
				},
			},
		},
		AssuranceStatements: []hubclient.AssuranceStatement{
			{
				StatementType: "ask_reviewed",
				Result:        "ASK-Partial",
				ReviewScope:   "package-change",
				ReviewerType:  "automated",
			},
		},
	}); err != nil {
		t.Fatalf("PutPackage(): %v", err)
	}

	r := chi.NewRouter()
	RegisterRoutes(r, Deps{Store: s, Registry: reg})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances/from-package", strings.NewReader(`{"kind":"connector","name":"google-drive-admin"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("code = %d, want 201; body = %s", rec.Code, rec.Body.String())
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
	RegisterRoutes(r, Deps{Store: s, RuntimeManager: stubRuntimeManager{}})

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

	statusReq := httptest.NewRequest(http.MethodGet, "/api/v1/instances/inst_123/runtime/nodes/drive_admin", nil)
	statusRec := httptest.NewRecorder()
	r.ServeHTTP(statusRec, statusReq)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("status code = %d, want 200; body = %s", statusRec.Code, statusRec.Body.String())
	}
	if !strings.Contains(statusRec.Body.String(), `"materialized"`) {
		t.Fatalf("expected materialized status: %s", statusRec.Body.String())
	}

	startReq := httptest.NewRequest(http.MethodPost, "/api/v1/instances/inst_123/runtime/nodes/drive_admin/start", nil)
	startRec := httptest.NewRecorder()
	r.ServeHTTP(startRec, startReq)
	if startRec.Code != http.StatusOK {
		t.Fatalf("start code = %d, want 200; body = %s", startRec.Code, startRec.Body.String())
	}
	if !strings.Contains(startRec.Body.String(), `"active"`) {
		t.Fatalf("expected active status: %s", startRec.Body.String())
	}

	stopReq := httptest.NewRequest(http.MethodPost, "/api/v1/instances/inst_123/runtime/nodes/drive_admin/stop", nil)
	stopRec := httptest.NewRecorder()
	r.ServeHTTP(stopRec, stopReq)
	if stopRec.Code != http.StatusOK {
		t.Fatalf("stop code = %d, want 200; body = %s", stopRec.Code, stopRec.Body.String())
	}
	if !strings.Contains(stopRec.Body.String(), `"stopped"`) {
		t.Fatalf("expected stopped status: %s", stopRec.Body.String())
	}
}

func TestInstancesApplyRegistersRuntimeSubscriptions(t *testing.T) {
	home := t.TempDir()
	s := instancepkg.NewStore(filepath.Join(home, "instances"))
	reg := hub.NewRegistry(filepath.Join(home, "hub-registry"))
	bus := events.NewBus(slog.Default(), nil)

	pkgDir := filepath.Join(home, "hub-registry", "connectors", "slack-interactivity")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(): %v", err)
	}
	pkgPath := filepath.Join(pkgDir, "connector.yaml")
	if err := os.WriteFile(pkgPath, []byte(`kind: connector
name: slack-interactivity
version: "1.0.0"
source:
  type: webhook
  path: /webhooks/slack-interactivity
  webhook_auth:
    type: hmac_sha256
    secret_credref: slack_signing_secret
routes:
  - match:
      payload_type: block_actions
      action_id: consent_approve
    target:
      runtime_node: slack_authority
      runtime_event: approval_action
  - match:
      payload_type: block_actions
    target:
      agent: "${interactivity_target_agent}"
runtime:
  executor:
    kind: slack_interactivity
    base_url: https://slack.com
`), 0o644); err != nil {
		t.Fatalf("WriteFile(): %v", err)
	}
	if err := reg.PutPackage(hub.InstalledPackage{
		Kind:      "connector",
		Name:      "slack-interactivity",
		Version:   "1.0.0",
		Path:      pkgPath,
		Assurance: []string{"publisher_verified", "ask_partial"},
		Spec: map[string]any{
			"runtime": map[string]any{
				"executor": map[string]any{
					"kind":     "slack_interactivity",
					"base_url": "https://slack.com",
				},
			},
		},
	}); err != nil {
		t.Fatalf("PutPackage(): %v", err)
	}

	inst := &instancepkg.Instance{
		ID:   "inst_123",
		Name: "slack-interactivity",
		Source: instancepkg.InstanceSource{
			Package: instancepkg.PackageRef{Kind: "connector", Name: "slack-interactivity", Version: "1.0.0"},
		},
		Config: map[string]any{"interactivity_target_agent": "slack-bridge"},
		Nodes: []instancepkg.Node{
			{ID: "slack_ingress", Kind: "connector.ingress", Package: instancepkg.PackageRef{Kind: "connector", Name: "slack-interactivity", Version: "1.0.0"}},
			{ID: "slack_authority", Kind: "connector.authority", Package: instancepkg.PackageRef{Kind: "connector", Name: "slack-interactivity", Version: "1.0.0"}, Config: map[string]any{"tools": []any{"consent_open_approval_card"}}},
		},
	}
	if err := s.Create(t.Context(), inst); err != nil {
		t.Fatalf("Create(): %v", err)
	}

	r := chi.NewRouter()
	RegisterRoutes(r, Deps{
		Config:   &config.Config{Home: home},
		Store:    s,
		Registry: reg,
		EventBus: bus,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances/inst_123/apply", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("apply code = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	var found bool
	for _, sub := range bus.Subscriptions().List() {
		if sub.Origin == events.OriginInstance && sub.OriginRef == "inst_123" {
			found = true
			if sub.Destination.Type != events.DestRuntime || sub.Destination.Target != "inst_123/slack_authority" {
				t.Fatalf("subscription destination = %#v", sub.Destination)
			}
			if sub.EventType != "approval_action" || sub.SourceName != "slack-interactivity" {
				t.Fatalf("subscription = %#v", sub)
			}
		}
	}
	if !found {
		t.Fatal("expected runtime subscription to be registered")
	}
}

func TestApplyInstance(t *testing.T) {
	home := t.TempDir()
	s := instancepkg.NewStore(filepath.Join(home, "instances"))
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
					"tools": []any{"list_permissions"},
					"executor": map[string]any{
						"kind":     "http_json",
						"base_url": "https://drive.example.test",
						"actions": map[string]any{
							"list_permissions": map[string]any{"path": "/permissions/list", "method": "POST"},
						},
					},
				},
			},
		},
	}
	if err := s.Create(t.Context(), inst); err != nil {
		t.Fatalf("Create(): %v", err)
	}

	r := chi.NewRouter()
	RegisterRoutes(r, Deps{
		Store:          s,
		Config:         &config.Config{Home: home},
		RuntimeManager: stubRuntimeManager{},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances/inst_123/apply", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("apply code = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"status":"applied"`) {
		t.Fatalf("unexpected apply response: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"materialized"`) {
		t.Fatalf("expected materialized node state: %s", rec.Body.String())
	}
}

func TestApplyInstanceRejectsInsufficientPackageAssurance(t *testing.T) {
	home := t.TempDir()
	s := instancepkg.NewStore(filepath.Join(home, "instances"))
	reg := hub.NewRegistry(filepath.Join(home, "hub-registry"))
	if err := reg.PutPackage(hub.InstalledPackage{
		Kind:      "connector",
		Name:      "google-drive-admin",
		Version:   "1.0.0",
		Trust:     "verified",
		Path:      filepath.Join(home, "google-drive-admin.yaml"),
		Assurance: []string{"publisher_verified"},
		Spec: map[string]any{
			"runtime": map[string]any{
				"executor": map[string]any{
					"kind":     "http_json",
					"base_url": "https://www.googleapis.com",
					"actions": map[string]any{
						"drive_list_file_permissions": map[string]any{
							"method": "GET",
							"path":   "/drive/v3/files/{file_id}/permissions",
						},
					},
				},
			},
		},
	}); err != nil {
		t.Fatalf("PutPackage(): %v", err)
	}
	inst := &instancepkg.Instance{
		ID:   "inst_123",
		Name: "community-admin",
		Source: instancepkg.InstanceSource{
			Package: instancepkg.PackageRef{Kind: "connector", Name: "google-drive-admin", Version: "1.0.0"},
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
					"tools": []any{"drive_list_file_permissions"},
					"executor": map[string]any{
						"kind":     "http_json",
						"base_url": "https://www.googleapis.com",
						"actions": map[string]any{
							"drive_list_file_permissions": map[string]any{
								"method": "GET",
								"path":   "/drive/v3/files/{file_id}/permissions",
							},
						},
					},
				},
			},
		},
	}
	if err := s.Create(t.Context(), inst); err != nil {
		t.Fatalf("Create(): %v", err)
	}

	r := chi.NewRouter()
	RegisterRoutes(r, Deps{
		Store:          s,
		Registry:       reg,
		Config:         &config.Config{Home: home},
		RuntimeManager: stubRuntimeManager{},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances/inst_123/apply", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("apply code = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "insufficient package assurance") {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
}

func TestInstancesCompileManifestFromPackageRuntimeSpec(t *testing.T) {
	home := t.TempDir()
	s := instancepkg.NewStore(filepath.Join(home, "instances"))
	reg := hub.NewRegistry(filepath.Join(home, "hub-registry"))
	if err := reg.PutPackage(hub.InstalledPackage{
		Kind:    "connector",
		Name:    "google-drive-admin",
		Version: "1.0.0",
		Trust:   "verified",
		Spec: map[string]any{
			"runtime": map[string]any{
				"executor": map[string]any{
					"kind":     "http_json",
					"base_url": "https://www.googleapis.com",
					"actions": map[string]any{
						"drive_list_file_permissions": map[string]any{
							"method":          "GET",
							"path":            "/drive/v3/files/{file_id}/permissions",
							"whitelist_field": "file_id",
						},
					},
					"auth": map[string]any{
						"type":    "google_service_account",
						"binding": "service_account_json",
						"scopes":  []any{"https://www.googleapis.com/auth/drive"},
					},
				},
			},
		},
	}); err != nil {
		t.Fatalf("PutPackage(): %v", err)
	}

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
					"tools":               []any{"drive_list_file_permissions"},
					"credential_bindings": []any{"service_account_json"},
					"resource_whitelist": []any{
						map[string]any{"kind": "file", "drive_id": "file-123"},
					},
				},
			},
		},
		Credentials: map[string]instancepkg.Binding{
			"service_account_json": {Type: "credref", Target: "credref:gdrive-admin"},
		},
		Grants: []instancepkg.GrantBinding{
			{Principal: "agent:community-admin/coordinator", Action: "drive_list_file_permissions", Resource: "drive_admin"},
		},
	}
	if err := s.Create(t.Context(), inst); err != nil {
		t.Fatalf("Create(): %v", err)
	}

	r := chi.NewRouter()
	RegisterRoutes(r, Deps{Store: s, Registry: reg, RuntimeManager: stubRuntimeManager{}})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances/inst_123/runtime/manifest", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("compile code = %d, want 201; body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"google_service_account"`) {
		t.Fatalf("missing package-backed executor auth: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"drive_list_file_permissions"`) {
		t.Fatalf("missing package-backed action: %s", rec.Body.String())
	}
}

func TestCompileRuntimeManifestRefreshesAttachedAgentManifest(t *testing.T) {
	home := t.TempDir()
	s := instancepkg.NewStore(filepath.Join(home, "instances"))
	inst := &instancepkg.Instance{
		ID:   "inst_123",
		Name: "community-admin",
		Source: instancepkg.InstanceSource{
			Template: instancepkg.PackageRef{Kind: "template", Name: "community-admin", Version: "1.0.0"},
		},
		Nodes: []instancepkg.Node{{
			ID:   "drive_admin",
			Kind: "connector.authority",
			Package: instancepkg.PackageRef{
				Kind:    "connector",
				Name:    "google-drive-admin",
				Version: "1.0.0",
			},
			Config: map[string]any{
				"tools": []any{"list_permissions"},
				"executor": map[string]any{
					"kind":     "http_json",
					"base_url": "https://drive.example.test",
					"actions": map[string]any{
						"list_permissions": map[string]any{"path": "/permissions/list", "method": "POST"},
					},
				},
			},
		}},
	}
	if err := s.Create(t.Context(), inst); err != nil {
		t.Fatalf("Create(): %v", err)
	}

	agentDir := filepath.Join(home, "agents", "coordinator")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "constraints.yaml"), []byte("agent: coordinator\ngranted_capabilities: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	agentYAML := `
version: "0.1"
name: coordinator
role: test
body:
  runtime: body
  version: "1.0"
workspace:
  ref: default
instances:
  attach:
    - instance_id: inst_123
      node_id: drive_admin
`
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(agentYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	r := chi.NewRouter()
	RegisterRoutes(r, Deps{
		Store:          s,
		Config:         &config.Config{Home: home},
		RuntimeManager: stubRuntimeManager{},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances/inst_123/runtime/manifest", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("compile code = %d, want 201; body = %s", rec.Code, rec.Body.String())
	}

	data, err := os.ReadFile(filepath.Join(agentDir, "services-manifest.json"))
	if err != nil {
		t.Fatalf("read services-manifest.json: %v", err)
	}
	if !strings.Contains(string(data), "instance_community_admin_drive_admin_list_permissions") {
		t.Fatalf("expected projected runtime tool in services-manifest.json: %s", string(data))
	}
}

func TestInvokeRuntimeNode(t *testing.T) {
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
					"tools": []any{"add_viewer"},
				},
			},
		},
		Grants: []instancepkg.GrantBinding{
			{
				Principal: "agent:community-admin/coordinator",
				Action:    "add_viewer",
				Resource:  "drive_admin",
			},
		},
	}
	if err := s.Create(t.Context(), inst); err != nil {
		t.Fatalf("Create(): %v", err)
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/invoke" {
			t.Fatalf("path = %q, want /invoke", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["action"] != "add_viewer" {
			t.Fatalf("body = %#v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"allowed": true, "execution": "executed"})
	}))
	defer upstream.Close()

	r := chi.NewRouter()
	RegisterRoutes(r, Deps{Store: s, RuntimeManager: invokeStubRuntimeManager{url: upstream.URL}})

	compileReq := httptest.NewRequest(http.MethodPost, "/api/v1/instances/inst_123/runtime/manifest", nil)
	compileRec := httptest.NewRecorder()
	r.ServeHTTP(compileRec, compileReq)
	if compileRec.Code != http.StatusCreated {
		t.Fatalf("compile code = %d, want 201; body = %s", compileRec.Code, compileRec.Body.String())
	}

	reconcileReq := httptest.NewRequest(http.MethodPost, "/api/v1/instances/inst_123/runtime/reconcile", nil)
	reconcileRec := httptest.NewRecorder()
	r.ServeHTTP(reconcileRec, reconcileReq)
	if reconcileRec.Code != http.StatusOK {
		t.Fatalf("reconcile code = %d, want 200; body = %s", reconcileRec.Code, reconcileRec.Body.String())
	}

	startReq := httptest.NewRequest(http.MethodPost, "/api/v1/instances/inst_123/runtime/nodes/drive_admin/start", nil)
	startRec := httptest.NewRecorder()
	r.ServeHTTP(startRec, startReq)
	if startRec.Code != http.StatusOK {
		t.Fatalf("start code = %d, want 200; body = %s", startRec.Code, startRec.Body.String())
	}

	invokeReq := httptest.NewRequest(http.MethodPost, "/api/v1/instances/inst_123/runtime/nodes/drive_admin/invoke", strings.NewReader(`{"subject":"agent:community-admin/coordinator","node_id":"drive_admin","action":"add_viewer"}`))
	invokeReq.Header.Set("Content-Type", "application/json")
	invokeRec := httptest.NewRecorder()
	r.ServeHTTP(invokeRec, invokeReq)
	if invokeRec.Code != http.StatusOK {
		t.Fatalf("invoke code = %d, want 200; body = %s", invokeRec.Code, invokeRec.Body.String())
	}
	if !strings.Contains(invokeRec.Body.String(), `"execution":"executed"`) {
		t.Fatalf("unexpected invoke response: %s", invokeRec.Body.String())
	}
}

func TestInvokeRuntimeActionDerivesSubjectFromAgentHeader(t *testing.T) {
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
					"tools": []any{"add_viewer"},
				},
			},
		},
	}
	if err := s.Create(t.Context(), inst); err != nil {
		t.Fatalf("Create(): %v", err)
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["action"] != "add_viewer" {
			t.Fatalf("body = %#v", body)
		}
		if body["subject"] != "agent:community-admin/coordinator" {
			t.Fatalf("subject = %#v", body["subject"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"allowed": true, "execution": "executed"})
	}))
	defer upstream.Close()

	r := chi.NewRouter()
	RegisterRoutes(r, Deps{Store: s, RuntimeManager: invokeStubRuntimeManager{url: upstream.URL}})

	for _, path := range []string{
		"/api/v1/instances/inst_123/runtime/manifest",
		"/api/v1/instances/inst_123/runtime/reconcile",
		"/api/v1/instances/inst_123/runtime/nodes/drive_admin/start",
	} {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code < 200 || rec.Code >= 300 {
			t.Fatalf("%s code = %d body=%s", path, rec.Code, rec.Body.String())
		}
	}

	invokeReq := httptest.NewRequest(http.MethodPost, "/api/v1/instances/inst_123/runtime/nodes/drive_admin/actions/add_viewer", strings.NewReader(`{"email":"person@example.com"}`))
	invokeReq.Header.Set("Content-Type", "application/json")
	invokeReq.Header.Set("X-Agency-Agent", "coordinator")
	invokeRec := httptest.NewRecorder()
	r.ServeHTTP(invokeRec, invokeReq)
	if invokeRec.Code != http.StatusOK {
		t.Fatalf("invoke code = %d, want 200; body = %s", invokeRec.Code, invokeRec.Body.String())
	}
}

type stubRuntimeManager struct{}

func (stubRuntimeManager) Status(store *runpkg.Store, manifest *runpkg.Manifest, nodeID string) (*runpkg.NodeStatus, error) {
	return runpkg.Manager{}.Status(store, manifest, nodeID)
}

func (stubRuntimeManager) StartAuthority(store *runpkg.Store, manifest *runpkg.Manifest, nodeID string) (*runpkg.NodeStatus, error) {
	node, err := runpkg.Manager{}.Status(store, manifest, nodeID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	node.State = runpkg.NodeStateActive
	node.UpdatedAt = now
	node.StartedAt = &now
	node.PID = 4321
	node.Port = 18888
	node.URL = "http://127.0.0.1:18888"
	if err := store.SaveNodeStatus(*node); err != nil {
		return nil, err
	}
	return node, nil
}

func (stubRuntimeManager) StopAuthority(store *runpkg.Store, manifest *runpkg.Manifest, nodeID string) (*runpkg.NodeStatus, error) {
	node, err := store.LoadNodeStatus(nodeID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	node.State = runpkg.NodeStateStopped
	node.UpdatedAt = now
	node.StoppedAt = &now
	if err := store.SaveNodeStatus(*node); err != nil {
		return nil, err
	}
	return node, nil
}

type invokeStubRuntimeManager struct {
	url string
}

func (m invokeStubRuntimeManager) Status(store *runpkg.Store, manifest *runpkg.Manifest, nodeID string) (*runpkg.NodeStatus, error) {
	return runpkg.Manager{}.Status(store, manifest, nodeID)
}

func (m invokeStubRuntimeManager) StartAuthority(store *runpkg.Store, manifest *runpkg.Manifest, nodeID string) (*runpkg.NodeStatus, error) {
	node, err := runpkg.Manager{}.Status(store, manifest, nodeID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	node.State = runpkg.NodeStateActive
	node.UpdatedAt = now
	node.StartedAt = &now
	node.URL = m.url
	if err := store.SaveNodeStatus(*node); err != nil {
		return nil, err
	}
	return node, nil
}

func (m invokeStubRuntimeManager) StopAuthority(store *runpkg.Store, manifest *runpkg.Manifest, nodeID string) (*runpkg.NodeStatus, error) {
	return stubRuntimeManager{}.StopAuthority(store, manifest, nodeID)
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
