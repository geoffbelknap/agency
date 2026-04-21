package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/logs"
	"github.com/geoffbelknap/agency/internal/orchestrate"
	runtimecontract "github.com/geoffbelknap/agency/internal/runtime/contract"
	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"
)

func TestSplitDoctorChecksSeparatesBackendScopedChecks(t *testing.T) {
	t.Parallel()

	checks := []doctorCheckResult{
		{Name: "credentials_isolated", Agent: "henry", Status: "pass"},
		{Name: "host_capacity", Status: "pass"},
		{Name: "pid_limits", Scope: "backend", Backend: "docker", Status: "pass"},
	}

	runtimeChecks, backendChecks := splitDoctorChecks(checks, "docker")

	if len(runtimeChecks) != 2 {
		t.Fatalf("runtimeChecks len = %d, want 2", len(runtimeChecks))
	}
	if len(backendChecks) != 1 {
		t.Fatalf("backendChecks len = %d, want 1", len(backendChecks))
	}
	if runtimeChecks[0].Name != "credentials_isolated" || runtimeChecks[1].Name != "host_capacity" {
		t.Fatalf("unexpected runtime checks: %#v", runtimeChecks)
	}
	if backendChecks[0].Name != "pid_limits" || backendChecks[0].Backend != "docker" {
		t.Fatalf("unexpected backend checks: %#v", backendChecks)
	}
}

func TestSplitDoctorChecksTreatsNetworkPoolAsRuntimeAdvisoryWithoutScope(t *testing.T) {
	t.Parallel()

	checks := []doctorCheckResult{
		{Name: "credentials_isolated", Agent: "henry", Status: "pass"},
		{Name: "host_capacity", Status: "pass"},
		{Name: "network_pool", Status: "warn"},
	}

	runtimeChecks, backendChecks := splitDoctorChecks(checks, "podman")

	if len(backendChecks) != 0 {
		t.Fatalf("backendChecks len = %d, want 0", len(backendChecks))
	}
	if len(runtimeChecks) != 3 {
		t.Fatalf("runtimeChecks len = %d, want 3", len(runtimeChecks))
	}
}

func TestSplitDoctorChecksKeepsLegacyPrefixedBackendChecksGrouped(t *testing.T) {
	t.Parallel()

	checks := []doctorCheckResult{
		{Name: "credentials_isolated", Agent: "henry", Status: "pass"},
		{Name: "containerd_dangling_images", Status: "warn"},
		{Name: "containerd_log_rotation", Status: "warn"},
	}

	runtimeChecks, backendChecks := splitDoctorChecks(checks, "containerd")

	if len(backendChecks) != 2 {
		t.Fatalf("backendChecks len = %d, want 2", len(backendChecks))
	}
	if len(runtimeChecks) != 1 {
		t.Fatalf("runtimeChecks len = %d, want 1", len(runtimeChecks))
	}
	if backendChecks[0].Name != "containerd_dangling_images" || backendChecks[1].Name != "containerd_log_rotation" {
		t.Fatalf("unexpected backend checks: %#v", backendChecks)
	}
}

func TestConfiguredRuntimeBackendDefaultsToDocker(t *testing.T) {
	t.Parallel()

	if got := configuredRuntimeBackend(nil); got != "docker" {
		t.Fatalf("configuredRuntimeBackend(nil) = %q, want docker", got)
	}
}

func TestAdminDoctorUsesRuntimeContractForProbeBackend(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	agentDir := filepath.Join(home, "agents", "probe-agent")
	stateDir := filepath.Join(agentDir, "state")
	runtimeDir := filepath.Join(agentDir, "runtime")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte("uuid: ag_probe\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tokenFile := filepath.Join(stateDir, "token.yaml")
	if err := os.WriteFile(tokenFile, []byte("- key: \"abc\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	manifest := map[string]any{
		"spec": map[string]any{
			"runtimeId": "probe-agent",
			"agentId":   "ag_probe",
			"backend":   "probe",
			"transport": map[string]any{
				"enforcer": map[string]any{
					"type":     runtimecontract.TransportTypeLoopbackHTTP,
					"endpoint": "http://127.0.0.1:9999",
					"tokenRef": tokenFile,
				},
			},
			"storage": map[string]any{
				"configPath": agentDir,
				"statePath":  stateDir,
			},
		},
		"status": map[string]any{
			"runtimeId": "probe-agent",
			"agentId":   "ag_probe",
			"phase":     runtimecontract.RuntimePhaseRunning,
			"healthy":   true,
			"backend":   "probe",
			"transport": map[string]any{
				"type":              runtimecontract.TransportTypeLoopbackHTTP,
				"endpoint":          "http://127.0.0.1:9999",
				"enforcerConnected": true,
			},
		},
		"compiledAt": time.Now().UTC(),
		"updatedAt":  time.Now().UTC(),
	}
	data, err := yaml.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runtimeDir, "manifest.yaml"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "runtime-probe-state.json"), []byte(`{"workspace_state":"running","enforcer_state":"running"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	h := &handler{deps: Deps{
		Config: &config.Config{
			Home: home,
			Hub:  config.HubConfig{DeploymentBackend: "probe"},
		},
		AgentManager: &orchestrate.AgentManager{
			Home:    home,
			Runtime: orchestrate.NewRuntimeSupervisor(home, "0.1.0", "", "build-1", "probe", nil, nil, nil, nil),
		},
	}}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/doctor", nil)
	rec := httptest.NewRecorder()
	h.adminDoctor(rec, req.WithContext(context.Background()))

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var report doctorReport
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if !report.AllPassed {
		t.Fatalf("expected all_passed, got false: %s", rec.Body.String())
	}
	if report.Backend != "probe" {
		t.Fatalf("backend = %q, want probe", report.Backend)
	}
	if len(report.BackendChecks) != 0 {
		t.Fatalf("expected no backend checks, got %#v", report.BackendChecks)
	}
	if len(report.TestedAgents) != 1 || report.TestedAgents[0] != "probe-agent" {
		t.Fatalf("tested_agents = %#v", report.TestedAgents)
	}
}

func TestBackendConnectionDetailsIncludesContainerdEndpointAndMode(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Home: t.TempDir(),
		Hub: config.HubConfig{
			DeploymentBackend: "containerd",
			DeploymentBackendConfig: map[string]string{
				"native_socket": "/run/user/1000/containerd/containerd.sock",
			},
		},
	}
	endpoint, mode := backendConnectionDetails(cfg)
	if endpoint != "unix:///run/user/1000/containerd/containerd.sock" {
		t.Fatalf("endpoint = %q", endpoint)
	}
	if mode != "rootless" {
		t.Fatalf("mode = %q", mode)
	}
}

func TestBackendConnectionDetailsIncludesAppleContainerEndpointAndMode(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Home: t.TempDir(),
		Hub:  config.HubConfig{DeploymentBackend: "apple-container"},
	}
	endpoint, mode := backendConnectionDetails(cfg)
	if endpoint != "container://local" {
		t.Fatalf("endpoint = %q", endpoint)
	}
	if mode != "macos-vm" {
		t.Fatalf("mode = %q", mode)
	}
}

func TestAdminDoctorAppleContainerReportsServiceAndGatedRuntime(t *testing.T) {
	orig := appleContainerStatus
	appleContainerStatus = func(ctx context.Context, backendConfig map[string]string) error {
		return nil
	}
	t.Cleanup(func() { appleContainerStatus = orig })

	h := &handler{deps: Deps{
		Config: &config.Config{
			Home: t.TempDir(),
			Hub:  config.HubConfig{DeploymentBackend: "apple-container"},
		},
	}}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/doctor", nil)
	rec := httptest.NewRecorder()
	h.adminDoctor(rec, req.WithContext(context.Background()))

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var report doctorReport
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if !report.AllPassed {
		t.Fatalf("expected all_passed, got false: %s", rec.Body.String())
	}
	if report.Backend != "apple-container" || report.BackendEndpoint != "container://local" || report.BackendMode != "macos-vm" {
		t.Fatalf("unexpected backend fields: %#v", report)
	}
	if len(report.RuntimeChecks) != 0 {
		t.Fatalf("expected no runtime checks before lifecycle support, got %#v", report.RuntimeChecks)
	}
	seen := map[string]bool{}
	for _, check := range report.BackendChecks {
		if check.Backend == "apple-container" && check.Status == "pass" {
			seen[check.Name] = true
		}
	}
	if !seen["apple_container_service"] || !seen["apple_container_runtime_gated"] {
		t.Fatalf("backend checks = %#v", report.BackendChecks)
	}
}

func TestSyntheticReadinessAgentIsIgnoredForUnscopedAudit(t *testing.T) {
	t.Parallel()

	if !isSyntheticReadinessAgent("podman-readiness-123") {
		t.Fatal("expected podman readiness agent to be treated as synthetic")
	}
	if !isSyntheticReadinessAgent("containerd-readiness-123") {
		t.Fatal("expected containerd readiness agent to be treated as synthetic")
	}
	if !isSyntheticReadinessAgent("docker-readiness-123") {
		t.Fatal("expected docker readiness agent to be treated as synthetic")
	}
	if isSyntheticReadinessAgent("real-agent") {
		t.Fatal("did not expect normal agent to be treated as synthetic")
	}
}

func TestAdminPruneImagesNonDockerBackendUnavailable(t *testing.T) {
	t.Parallel()

	h := &handler{deps: Deps{
		Config: &config.Config{
			Home: t.TempDir(),
			Hub:  config.HubConfig{DeploymentBackend: "probe"},
		},
	}}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/prune-images", nil)
	rec := httptest.NewRecorder()
	h.adminPruneImages(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d, want 503", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["backend"] != "probe" {
		t.Fatalf("backend = %q, want probe", body["backend"])
	}
}

func TestAdminDestroyNonDockerBackendUnavailable(t *testing.T) {
	t.Parallel()

	h := &handler{deps: Deps{
		Config: &config.Config{
			Home: t.TempDir(),
			Hub:  config.HubConfig{DeploymentBackend: "probe"},
		},
	}}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/destroy", nil)
	rec := httptest.NewRecorder()
	h.adminDestroy(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d, want 503", rec.Code)
	}
}

func TestAdminEgressRESTApproveRevokeAndMode(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	router := chi.NewRouter()
	RegisterRoutes(router, Deps{
		Config: &config.Config{Home: home},
		Audit:  logs.NewWriter(home),
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/egress/test-agent/domains", strings.NewReader(`{"domain":"API.Example.COM.","reason":"provider access"}`))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve code = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var approved map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &approved); err != nil {
		t.Fatal(err)
	}
	if approved["mode"] != "allowlist" {
		t.Fatalf("mode = %v, want allowlist", approved["mode"])
	}
	domains, ok := approved["domains"].([]interface{})
	if !ok || len(domains) != 1 {
		t.Fatalf("domains = %#v, want one entry", approved["domains"])
	}
	entry, ok := domains[0].(map[string]interface{})
	if !ok {
		t.Fatalf("domain entry = %#v, want map", domains[0])
	}
	if entry["domain"] != "api.example.com" || entry["reason"] != "provider access" || entry["approved_by"] != "operator" {
		t.Fatalf("unexpected approved entry: %#v", entry)
	}

	req = httptest.NewRequest(http.MethodPut, "/api/v1/admin/egress/test-agent/mode", strings.NewReader(`{"mode":"supervised-strict"}`))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("mode code = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/admin/egress/test-agent/domains/API.Example.COM.", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke code = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	raw, err := os.ReadFile(filepath.Join(home, "agents", "test-agent", "egress.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var onDisk map[string]interface{}
	if err := yaml.Unmarshal(raw, &onDisk); err != nil {
		t.Fatal(err)
	}
	if onDisk["mode"] != "supervised-strict" {
		t.Fatalf("on-disk mode = %v, want supervised-strict", onDisk["mode"])
	}
	if got := onDisk["domains"].([]interface{}); len(got) != 0 {
		t.Fatalf("on-disk domains = %#v, want empty", got)
	}

	auditRaw, err := os.ReadFile(filepath.Join(home, "audit", "test-agent", "gateway.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	audit := string(auditRaw)
	for _, event := range []string{"egress_domain_approved", "egress_mode_changed", "egress_domain_revoked"} {
		if !strings.Contains(audit, `"event":"`+event+`"`) {
			t.Fatalf("audit log missing %s: %s", event, audit)
		}
	}
}

func TestAdminEgressRESTRejectsInvalidInputs(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	router := chi.NewRouter()
	RegisterRoutes(router, Deps{
		Config: &config.Config{Home: home},
		Audit:  logs.NewWriter(home),
	})

	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "invalid agent", method: http.MethodPost, path: "/api/v1/admin/egress/Bad_Agent/domains", body: `{"domain":"api.example.com"}`},
		{name: "empty domain", method: http.MethodPost, path: "/api/v1/admin/egress/test-agent/domains", body: `{"domain":" "}`},
		{name: "invalid domain", method: http.MethodPost, path: "/api/v1/admin/egress/test-agent/domains", body: `{"domain":"https://api.example.com/path"}`},
		{name: "open mode", method: http.MethodPut, path: "/api/v1/admin/egress/test-agent/mode", body: `{"mode":"open"}`},
		{name: "unknown mode", method: http.MethodPut, path: "/api/v1/admin/egress/test-agent/mode", body: `{"mode":"monitor"}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("code = %d, want 400: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestAdminAuditAllReturnsEmptyWhenNoAuditLogs(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	router := chi.NewRouter()
	RegisterRoutes(router, Deps{
		Config: &config.Config{Home: home},
		Audit:  logs.NewWriter(home),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/audit", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var events []map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &events); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("events len = %d, want 0", len(events))
	}
}
