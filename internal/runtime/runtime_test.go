package runtime

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	authzcore "github.com/geoffbelknap/agency/internal/authz"
	agencyconsent "github.com/geoffbelknap/agency/internal/consent"
	"github.com/geoffbelknap/agency/internal/credstore"
	"github.com/geoffbelknap/agency/internal/hub"
	instancepkg "github.com/geoffbelknap/agency/internal/instances"
	"golang.org/x/oauth2"
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
	if req := node.ConsentRequirements["add_viewer"]; req.OperationKind != "" {
		t.Fatalf("unexpected consent requirement without explicit metadata: %#v", req)
	}
	if node.Executor != nil {
		t.Fatalf("expected no executor, got %#v", node.Executor)
	}
}

func TestPlannerCompileAuthorityNodeWithExecutor(t *testing.T) {
	inst := testInstanceWithExecutor("https://drive.example.test")

	manifest, err := Planner{}.Compile(inst)
	if err != nil {
		t.Fatalf("Compile(): %v", err)
	}
	node := manifest.Runtime.Nodes[0]
	if node.Executor == nil {
		t.Fatal("expected executor")
	}
	if node.Executor.Kind != "http_json" {
		t.Fatalf("executor.kind = %q, want http_json", node.Executor.Kind)
	}
	if node.Executor.Auth == nil || node.Executor.Auth.Binding != "service_account_json" {
		t.Fatalf("executor.auth = %#v", node.Executor.Auth)
	}
	if len(node.ResourceWhitelist) != 2 {
		t.Fatalf("resource_whitelist = %#v", node.ResourceWhitelist)
	}
	if len(manifest.Runtime.Bindings) != 1 || manifest.Runtime.Bindings[0].Target != "credref:gdrive-admin" {
		t.Fatalf("bindings = %#v", manifest.Runtime.Bindings)
	}
}

func TestPlannerCompileAuthorityNodeWithConsentRequirement(t *testing.T) {
	inst := testInstanceWithConsentRequirement()

	manifest, err := Planner{}.Compile(inst)
	if err != nil {
		t.Fatalf("Compile(): %v", err)
	}
	if manifest.Source.ConsentDeploymentID != "dep-123" {
		t.Fatalf("consent deployment id = %q", manifest.Source.ConsentDeploymentID)
	}
	req := manifest.Runtime.Nodes[0].ConsentRequirements["add_viewer"]
	if req.OperationKind != "grant_drive_viewer" {
		t.Fatalf("consent requirement = %#v", req)
	}
	if req.TokenInputField != "consent_token" || req.TargetInputField != "drive_id" {
		t.Fatalf("consent requirement fields = %#v", req)
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

func TestPlannerCompileIngressNode(t *testing.T) {
	home := t.TempDir()
	reg := hub.NewRegistry(filepath.Join(home, "hub-registry"))
	pkgDir := filepath.Join(home, "hub-registry", "connectors", "slack-interactivity")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
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
    target:
      agent: "${interactivity_target_agent}"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := reg.PutPackage(hub.InstalledPackage{
		Kind: "connector", Name: "slack-interactivity", Version: "1.0.0", Path: pkgPath,
	}); err != nil {
		t.Fatal(err)
	}
	inst := &instancepkg.Instance{
		ID:   "inst_123",
		Name: "slack-interactivity",
		Source: instancepkg.InstanceSource{
			Package: instancepkg.PackageRef{Kind: "connector", Name: "slack-interactivity", Version: "1.0.0"},
		},
		Config: map[string]any{"interactivity_target_agent": "slack-bridge"},
		Nodes: []instancepkg.Node{{
			ID:      "slack_interactivity",
			Kind:    "connector.ingress",
			Package: instancepkg.PackageRef{Kind: "connector", Name: "slack-interactivity", Version: "1.0.0"},
		}},
	}
	manifest, err := Planner{Packages: reg}.Compile(inst)
	if err != nil {
		t.Fatalf("Compile(): %v", err)
	}
	if len(manifest.Runtime.Nodes) != 1 {
		t.Fatalf("len(nodes) = %d, want 1", len(manifest.Runtime.Nodes))
	}
	node := manifest.Runtime.Nodes[0]
	if node.Ingress == nil {
		t.Fatal("expected ingress spec")
	}
	if node.Ingress.PublishedName != "slack-interactivity" {
		t.Fatalf("published_name = %q", node.Ingress.PublishedName)
	}
	if !strings.Contains(node.Ingress.ConnectorYAML, "slack-bridge") {
		t.Fatalf("connector yaml missing rendered target: %s", node.Ingress.ConnectorYAML)
	}
	if !strings.Contains(node.Ingress.ConnectorYAML, "name: slack-interactivity") {
		t.Fatalf("connector yaml missing published name: %s", node.Ingress.ConnectorYAML)
	}
}

func TestReconcilerPublishesIngressConnector(t *testing.T) {
	home := t.TempDir()
	instanceDir := filepath.Join(home, "instances", "inst_123")
	store := NewStore(instanceDir)
	manifest := &Manifest{
		APIVersion: ManifestAPIVersion,
		Kind:       ManifestKind,
		Metadata: ManifestMeta{
			ManifestID:   "mf_test",
			InstanceID:   "inst_123",
			InstanceName: "slack-interactivity",
			CompiledAt:   time.Now().UTC(),
			Planner:      PlannerVersion,
		},
		Runtime: RuntimeSpec{
			Nodes: []RuntimeNode{{
				NodeID:          "slack_interactivity",
				Kind:            "connector.ingress",
				Materialization: "ingress/slack_interactivity.yaml",
				Ingress: &RuntimeIngressSpec{
					PublishedName: "slack-interactivity",
					ConnectorYAML: "kind: connector\nname: slack-interactivity\nsource:\n  type: webhook\n",
				},
			}},
		},
	}
	if err := store.SaveManifest(manifest); err != nil {
		t.Fatalf("SaveManifest(): %v", err)
	}
	if err := (Reconciler{Home: home}).Reconcile(store, manifest); err != nil {
		t.Fatalf("Reconcile(): %v", err)
	}
	if _, err := os.Stat(filepath.Join(instanceDir, "runtime", "ingress", "slack_interactivity.yaml")); err != nil {
		t.Fatalf("expected ingress materialization: %v", err)
	}
	published, err := os.ReadFile(filepath.Join(home, "connectors", "slack-interactivity.yaml"))
	if err != nil {
		t.Fatalf("read published connector: %v", err)
	}
	if !strings.Contains(string(published), "name: slack-interactivity") {
		t.Fatalf("unexpected published connector: %s", string(published))
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

func TestAuthorityHandlerInvokeExecutesHTTPJSON(t *testing.T) {
	t.Setenv("AGENCY_HOME", t.TempDir())
	putTestCredential(t, os.Getenv("AGENCY_HOME"), "gdrive-admin", "secret-token")

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret-token" {
			t.Fatalf("Authorization = %q, want Bearer secret-token", got)
		}
		if r.URL.Path != "/permissions/add" {
			t.Fatalf("path = %q, want /permissions/add", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["email"] != "person@example.com" {
			t.Fatalf("body = %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer upstream.Close()

	manifest, err := Planner{}.Compile(testInstanceWithExecutor(upstream.URL))
	if err != nil {
		t.Fatalf("Compile(): %v", err)
	}
	handler := AuthorityHandler{Manifest: manifest, Resolver: authzcore.Resolver{}}

	req := httptest.NewRequest(http.MethodPost, "/invoke", bytes.NewBufferString(`{"subject":"agent:community-admin/coordinator","node_id":"drive_admin","action":"add_viewer","consent_provided":true,"input":{"email":"person@example.com"}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"execution":"executed"`) {
		t.Fatalf("expected executed response: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"status_code":200`) {
		t.Fatalf("expected status code in response: %s", rec.Body.String())
	}
}

func TestAuthorityHandlerInvokeExecutesHTTPJSONWithPathQueryAndBodyMapping(t *testing.T) {
	t.Setenv("AGENCY_HOME", t.TempDir())
	putTestCredential(t, os.Getenv("AGENCY_HOME"), "gdrive-admin", "secret-token")

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret-token" {
			t.Fatalf("Authorization = %q, want Bearer secret-token", got)
		}
		if r.URL.Path != "/drive/v3/files/folder-123/permissions" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("sendNotificationEmail"); got != "false" {
			t.Fatalf("sendNotificationEmail = %q, want false", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["emailAddress"] != "person@example.com" || body["role"] != "commenter" || body["type"] != "user" {
			t.Fatalf("body = %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer upstream.Close()

	manifest, err := Planner{}.Compile(testInstanceWithTemplatedExecutor(upstream.URL))
	if err != nil {
		t.Fatalf("Compile(): %v", err)
	}
	handler := AuthorityHandler{Manifest: manifest, Resolver: authzcore.Resolver{}}

	req := httptest.NewRequest(http.MethodPost, "/invoke", bytes.NewBufferString(`{"subject":"agent:community-admin/coordinator","node_id":"drive_admin","action":"add_viewer","consent_provided":true,"input":{"folder_id":"folder-123","email":"person@example.com","role":"commenter","notify":false,"permission_type":"user"}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
}

func TestAuthorityHandlerInvokeRejectsNonWhitelistedResource(t *testing.T) {
	t.Setenv("AGENCY_HOME", t.TempDir())
	putTestCredential(t, os.Getenv("AGENCY_HOME"), "gdrive-admin", "secret-token")

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("upstream should not be called for non-whitelisted resource")
	}))
	defer upstream.Close()

	manifest, err := Planner{}.Compile(testInstanceWithWhitelistedExecutor(upstream.URL))
	if err != nil {
		t.Fatalf("Compile(): %v", err)
	}
	handler := AuthorityHandler{Manifest: manifest, Resolver: authzcore.Resolver{}}

	req := httptest.NewRequest(http.MethodPost, "/invoke", bytes.NewBufferString(`{"subject":"agent:community-admin/coordinator","node_id":"drive_admin","action":"add_viewer","consent_provided":true,"input":{"file_id":"file-blocked"}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("code = %d, want 502; body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "not whitelisted") {
		t.Fatalf("expected whitelist error: %s", rec.Body.String())
	}
}

func TestAuthorityHandlerInvokeExecutesHTTPJSONWithGoogleServiceAccountAuth(t *testing.T) {
	t.Setenv("AGENCY_HOME", t.TempDir())
	tokenSourceMu.Lock()
	tokenSourceCache = map[string]oauth2.TokenSource{}
	tokenSourceMu.Unlock()
	serviceAccountJSON := mustGoogleServiceAccountJSON(t)

	var tokenRequests int
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenRequests++
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm(): %v", err)
		}
		if got := r.Form.Get("grant_type"); got != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
			t.Fatalf("grant_type = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "google-access-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer tokenServer.Close()

	serviceAccountJSON = strings.ReplaceAll(serviceAccountJSON, "__TOKEN_URI__", tokenServer.URL)
	putTestCredential(t, os.Getenv("AGENCY_HOME"), "gdrive-admin", serviceAccountJSON)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer google-access-token" {
			t.Fatalf("Authorization = %q, want Bearer google-access-token", got)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	manifest, err := Planner{}.Compile(testInstanceWithGoogleExecutor(upstream.URL))
	if err != nil {
		t.Fatalf("Compile(): %v", err)
	}
	handler := AuthorityHandler{Manifest: manifest, Resolver: authzcore.Resolver{}}

	for range 2 {
		req := httptest.NewRequest(http.MethodPost, "/invoke", bytes.NewBufferString(`{"subject":"agent:community-admin/coordinator","node_id":"drive_admin","action":"add_viewer","consent_provided":true,"input":{"file_id":"file-123"}}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("code = %d, want 200; body = %s", rec.Code, rec.Body.String())
		}
	}
	if tokenRequests != 1 {
		t.Fatalf("token requests = %d, want 1", tokenRequests)
	}
}

func TestAuthorityHandlerInvokeValidatesConsentToken(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	now := time.Now().UTC()
	token := agencyconsent.Token{
		Version:         1,
		DeploymentID:    "dep-123",
		OperationKind:   "grant_drive_viewer",
		OperationTarget: []byte("drive-abc"),
		Issuer:          "slack-interactivity",
		Witnesses:       []string{"U1"},
		IssuedAt:        now.UnixMilli(),
		ExpiresAt:       now.Add(5 * time.Minute).UnixMilli(),
		Nonce:           []byte("0123456789abcdef"),
		SigningKeyID:    "dep-123:v1",
	}
	raw, err := token.MarshalCanonical()
	if err != nil {
		t.Fatalf("marshal token: %v", err)
	}
	encoded, err := agencyconsent.EncodeSignedToken(agencyconsent.SignedToken{
		Token:     token,
		Signature: ed25519.Sign(priv, raw),
	})
	if err != nil {
		t.Fatalf("encode token: %v", err)
	}

	manifest := &Manifest{
		APIVersion: ManifestAPIVersion,
		Kind:       ManifestKind,
		Metadata: ManifestMeta{
			ManifestID:   "mf_test",
			InstanceID:   "inst_123",
			InstanceName: "community-admin",
			CompiledAt:   now,
			Planner:      PlannerVersion,
		},
		Source: ManifestSource{ConsentDeploymentID: "dep-123"},
		Runtime: RuntimeSpec{
			Nodes: []RuntimeNode{{
				NodeID:         "drive_admin",
				Kind:           "connector.authority",
				Tools:          []string{"add_viewer"},
				GrantSubjects:  []string{"agent:community-admin/coordinator"},
				ConsentActions: []string{"add_viewer"},
				ConsentRequirements: map[string]agencyconsent.Requirement{
					"add_viewer": {
						OperationKind:    "grant_drive_viewer",
						TokenInputField:  "consent_token",
						TargetInputField: "drive_id",
					},
				},
			}},
		},
	}
	handler := AuthorityHandler{
		Manifest: manifest,
		Resolver: authzcore.Resolver{},
		ConsentValidator: agencyconsent.NewValidator("dep-123", map[string]ed25519.PublicKey{
			"dep-123:v1": pub,
		}, 15*time.Minute, 30*time.Second),
	}

	req := httptest.NewRequest(http.MethodPost, "/invoke", bytes.NewBufferString(`{"subject":"agent:community-admin/coordinator","node_id":"drive_admin","action":"add_viewer","input":{"drive_id":"drive-abc","consent_token":"`+encoded+`"}}`))
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
	return testInstanceWithConfig(nil)
}

func testInstanceWithExecutor(baseURL string) *instancepkg.Instance {
	return testInstanceWithConfig(map[string]any{
		"executor": map[string]any{
			"kind":     "http_json",
			"base_url": baseURL,
			"actions": map[string]any{
				"add_viewer": map[string]any{
					"method": "POST",
					"path":   "/permissions/add",
				},
			},
			"auth": map[string]any{
				"type":    "bearer",
				"binding": "service_account_json",
			},
		},
	})
}

func testInstanceWithTemplatedExecutor(baseURL string) *instancepkg.Instance {
	return testInstanceWithConfig(map[string]any{
		"executor": map[string]any{
			"kind":     "http_json",
			"base_url": baseURL,
			"actions": map[string]any{
				"add_viewer": map[string]any{
					"method": "POST",
					"path":   "/drive/v3/files/{folder_id}/permissions",
					"query": map[string]any{
						"sendNotificationEmail": "notify",
					},
					"body": map[string]any{
						"emailAddress": "email",
						"role":         "role",
						"type":         "permission_type",
					},
				},
			},
			"auth": map[string]any{
				"type":    "bearer",
				"binding": "service_account_json",
			},
		},
	})
}

func testInstanceWithWhitelistedExecutor(baseURL string) *instancepkg.Instance {
	return testInstanceWithConfig(map[string]any{
		"executor": map[string]any{
			"kind":     "http_json",
			"base_url": baseURL,
			"actions": map[string]any{
				"add_viewer": map[string]any{
					"method":          "GET",
					"path":            "/drive/v3/files/{file_id}",
					"whitelist_field": "file_id",
				},
			},
			"auth": map[string]any{
				"type":    "bearer",
				"binding": "service_account_json",
			},
		},
	})
}

func testInstanceWithGoogleExecutor(baseURL string) *instancepkg.Instance {
	return testInstanceWithConfig(map[string]any{
		"executor": map[string]any{
			"kind":     "http_json",
			"base_url": baseURL,
			"actions": map[string]any{
				"add_viewer": map[string]any{
					"method": "GET",
					"path":   "/drive/v3/files/{file_id}",
				},
			},
			"auth": map[string]any{
				"type":    "google_service_account",
				"binding": "service_account_json",
				"scopes":  []any{"https://www.googleapis.com/auth/drive"},
			},
		},
	})
}

func testInstanceWithConfig(extra map[string]any) *instancepkg.Instance {
	config := map[string]any{
		"tools":               []any{"add_viewer", "remove_viewer", "list_permissions"},
		"credential_bindings": []any{"service_account_json"},
		"resource_whitelist": []any{
			map[string]any{"kind": "file", "drive_id": "file-123"},
			map[string]any{"kind": "folder", "drive_id": "folder-123"},
		},
	}
	for key, value := range extra {
		config[key] = value
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
				Config: config,
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
	return inst
}

func testInstanceWithConsentRequirement() *instancepkg.Instance {
	inst := testInstance()
	inst.Config = map[string]any{"consent_deployment_id": "dep-123"}
	inst.Grants[0].Config = map[string]any{
		"consent_required":   true,
		"operation_kind":     "grant_drive_viewer",
		"token_input_field":  "consent_token",
		"target_input_field": "drive_id",
	}
	return inst
}

func putTestCredential(t *testing.T, home, name, value string) {
	t.Helper()
	backend, err := credstore.NewFileBackend(
		filepath.Join(home, "credentials", "store.enc"),
		filepath.Join(home, "credentials", ".key"),
	)
	if err != nil {
		t.Fatalf("NewFileBackend(): %v", err)
	}
	store := credstore.NewStore(backend, home)
	if err := store.Put(credstore.Entry{Name: name, Value: value}); err != nil {
		t.Fatalf("Put(): %v", err)
	}
}

func mustGoogleServiceAccountJSON(t *testing.T) string {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey(): %v", err)
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey(): %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8})
	return fmt.Sprintf(`{
  "type": "service_account",
  "project_id": "agency-test",
  "private_key_id": "test-key",
  "private_key": %q,
  "client_email": "agency-test@agency-test.iam.gserviceaccount.com",
  "client_id": "1234567890",
  "token_uri": "__TOKEN_URI__"
}`, string(pemBytes))
}
