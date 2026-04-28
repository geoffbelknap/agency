package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/geoffbelknap/agency/internal/config"
	hostruntimebackend "github.com/geoffbelknap/agency/internal/hostadapter/runtimebackend"
	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
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

func TestAdminDoctorAppleContainerReportsServiceAndHelperWarning(t *testing.T) {
	orig := appleContainerStatus
	origHelper := appleContainerHelperStatus
	origWaitHelper := appleContainerWaitHelperStatus
	appleContainerStatus = func(ctx context.Context, backendConfig map[string]string) error {
		return nil
	}
	appleContainerHelperStatus = func(ctx context.Context, backendConfig map[string]string) (runtimehost.AppleContainerHelperHealth, error) {
		return runtimehost.AppleContainerHelperHealth{}, fmt.Errorf("apple-container helper is not configured")
	}
	appleContainerWaitHelperStatus = func(ctx context.Context, backendConfig map[string]string) (runtimehost.AppleContainerHelperHealth, error) {
		return runtimehost.AppleContainerHelperHealth{}, fmt.Errorf("apple-container wait helper is not configured")
	}
	t.Cleanup(func() {
		appleContainerStatus = orig
		appleContainerHelperStatus = origHelper
		appleContainerWaitHelperStatus = origWaitHelper
	})

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
		t.Fatalf("expected no runtime checks for apple-container backend-only doctor checks, got %#v", report.RuntimeChecks)
	}
	seenPass := map[string]bool{}
	seenWarn := map[string]bool{}
	for _, check := range report.BackendChecks {
		if check.Backend == "apple-container" && check.Status == "pass" {
			seenPass[check.Name] = true
		}
		if check.Backend == "apple-container" && check.Status == "warn" {
			seenWarn[check.Name] = true
		}
	}
	if !seenPass["apple_container_service"] || !seenWarn["apple_container_helper"] || !seenWarn["apple_container_wait_helper"] {
		t.Fatalf("backend checks = %#v", report.BackendChecks)
	}
}

func TestAdminDoctorAppleContainerWaitHelperSatisfiesLifecycleEvents(t *testing.T) {
	orig := appleContainerStatus
	origHelper := appleContainerHelperStatus
	origWaitHelper := appleContainerWaitHelperStatus
	appleContainerStatus = func(ctx context.Context, backendConfig map[string]string) error {
		return nil
	}
	appleContainerHelperStatus = func(ctx context.Context, backendConfig map[string]string) (runtimehost.AppleContainerHelperHealth, error) {
		return runtimehost.AppleContainerHelperHealth{OK: true, Backend: "apple-container", EventSupport: "none"}, nil
	}
	appleContainerWaitHelperStatus = func(ctx context.Context, backendConfig map[string]string) (runtimehost.AppleContainerHelperHealth, error) {
		return runtimehost.AppleContainerHelperHealth{OK: true, Backend: "apple-container", EventSupport: "process_wait"}, nil
	}
	t.Cleanup(func() {
		appleContainerStatus = orig
		appleContainerHelperStatus = origHelper
		appleContainerWaitHelperStatus = origWaitHelper
	})

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
	seenWaitHelper := false
	seenEvents := false
	for _, check := range report.BackendChecks {
		if check.Name == "apple_container_wait_helper" {
			if check.Status != "pass" {
				t.Fatalf("wait helper check = %#v", check)
			}
			seenWaitHelper = true
		}
		if check.Name == "apple_container_helper_events" {
			if check.Status != "pass" || !strings.Contains(check.Detail, "process_wait") {
				t.Fatalf("event check = %#v", check)
			}
			seenEvents = true
		}
	}
	if !seenWaitHelper {
		t.Fatalf("missing apple_container_wait_helper check: %#v", report.BackendChecks)
	}
	if !seenEvents {
		t.Fatalf("missing apple_container_helper_events check: %#v", report.BackendChecks)
	}
}

func TestAdminDoctorFirecrackerReportsHostChecksWhenExperimental(t *testing.T) {
	t.Setenv("AGENCY_EXPERIMENTAL_SURFACES", "1")
	restoreFirecrackerDoctorHooks(t)
	firecrackerOpenReadWrite = func(path string) error { return nil }
	firecrackerStat = func(path string) (os.FileInfo, error) {
		if path == "/sys/module/kvm" {
			return fakeFileInfo{name: "kvm", mode: os.ModeDir | 0o755}, nil
		}
		return os.Stat(path)
	}

	home := t.TempDir()
	binaryPath := filepath.Join(home, "firecracker")
	kernelPath := filepath.Join(home, "vmlinux")
	enforcerPath := filepath.Join(home, "enforcer")
	bridgePath := filepath.Join(home, "agency-vsock-http-bridge")
	if err := os.WriteFile(binaryPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(enforcerPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bridgePath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kernelPath, []byte{0x7f, 'E', 'L', 'F', 0x02}, 0o644); err != nil {
		t.Fatal(err)
	}
	h := &handler{deps: Deps{
		Config: &config.Config{
			Home: home,
			Hub: config.HubConfig{
				DeploymentBackend: hostruntimebackend.BackendFirecracker,
				DeploymentBackendConfig: map[string]string{
					"binary_path":              binaryPath,
					"kernel_path":              kernelPath,
					"enforcer_binary_path":     enforcerPath,
					"vsock_bridge_binary_path": bridgePath,
				},
			},
		},
		AgentManager: &orchestrate.AgentManager{
			Home:    home,
			Runtime: orchestrate.NewRuntimeSupervisor(home, "0.1.0", "", "build-1", hostruntimebackend.BackendFirecracker, nil, nil, nil, nil),
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
	for _, name := range []string{"firecracker_kvm_device", "firecracker_vsock_device", "firecracker_kvm_module", "firecracker_binary", "firecracker_kernel", "firecracker_enforcer_binary", "firecracker_vsock_bridge_binary"} {
		check, ok := findDoctorCheck(report.BackendChecks, name)
		if !ok {
			t.Fatalf("missing %s in %#v", name, report.BackendChecks)
		}
		if check.Status != "pass" || check.Backend != hostruntimebackend.BackendFirecracker || check.Scope != "backend" {
			t.Fatalf("%s check = %#v", name, check)
		}
	}
}

func TestFirecrackerDoctorChecksReportRemediationHints(t *testing.T) {
	restoreFirecrackerDoctorHooks(t)
	firecrackerOpenReadWrite = func(path string) error {
		if path == "/dev/kvm" {
			return os.ErrPermission
		}
		return nil
	}
	firecrackerStat = func(path string) (os.FileInfo, error) {
		if path == "/sys/module/kvm" {
			return nil, os.ErrNotExist
		}
		return os.Stat(path)
	}

	home := t.TempDir()
	binaryPath := filepath.Join(home, "firecracker")
	kernelPath := filepath.Join(home, "vmlinux")
	enforcerPath := filepath.Join(home, "enforcer")
	if err := os.WriteFile(binaryPath, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(enforcerPath, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kernelPath, []byte("not-elf"), 0o644); err != nil {
		t.Fatal(err)
	}
	report := doctorReport{AllPassed: true, Backend: hostruntimebackend.BackendFirecracker}
	appendFirecrackerDoctorChecks(&report, &config.Config{
		Hub: config.HubConfig{DeploymentBackendConfig: map[string]string{
			"binary_path":          binaryPath,
			"kernel_path":          kernelPath,
			"enforcer_binary_path": enforcerPath,
		}},
	})

	if report.AllPassed {
		t.Fatal("expected all_passed=false")
	}
	for _, tt := range []struct {
		name string
		want string
	}{
		{"firecracker_kvm_device", "setfacl"},
		{"firecracker_kvm_module", "modprobe kvm"},
		{"firecracker_binary", "chmod +x"},
		{"firecracker_kernel", "vmlinux"},
		{"firecracker_enforcer_binary", "chmod +x"},
		{"firecracker_vsock_bridge_binary", "make firecracker-helpers"},
	} {
		check, ok := findDoctorCheck(report.Checks, tt.name)
		if !ok {
			t.Fatalf("missing %s in %#v", tt.name, report.Checks)
		}
		if check.Status != "fail" || !strings.Contains(check.Fix, tt.want) {
			t.Fatalf("%s check = %#v", tt.name, check)
		}
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

func restoreFirecrackerDoctorHooks(t *testing.T) {
	t.Helper()
	origOpen := firecrackerOpenReadWrite
	origStat := firecrackerStat
	origLookPath := firecrackerLookPath
	t.Cleanup(func() {
		firecrackerOpenReadWrite = origOpen
		firecrackerStat = origStat
		firecrackerLookPath = origLookPath
	})
}

func findDoctorCheck(checks []doctorCheckResult, name string) (doctorCheckResult, bool) {
	for _, check := range checks {
		if check.Name == name {
			return check, true
		}
	}
	return doctorCheckResult{}, false
}

type fakeFileInfo struct {
	name string
	mode os.FileMode
}

func (f fakeFileInfo) Name() string       { return f.name }
func (f fakeFileInfo) Size() int64        { return 0 }
func (f fakeFileInfo) Mode() os.FileMode  { return f.mode }
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool        { return f.mode.IsDir() }
func (f fakeFileInfo) Sys() any           { return nil }

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
	change, ok := approved["change"].(map[string]interface{})
	if !ok {
		t.Fatalf("change = %#v, want map", approved["change"])
	}
	if change["action"] != "approve_domain" || change["scope"] != "egress" || change["status"] != "applied" {
		t.Fatalf("unexpected change: %#v", change)
	}
	egress, ok := approved["egress"].(map[string]interface{})
	if !ok {
		t.Fatalf("egress = %#v, want map", approved["egress"])
	}
	if egress["mode"] != "allowlist" {
		t.Fatalf("mode = %v, want allowlist", egress["mode"])
	}
	domains, ok := egress["domains"].([]interface{})
	if !ok || len(domains) != 1 {
		t.Fatalf("domains = %#v, want one entry", egress["domains"])
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

func TestAdminAuditAnnotatesPACTResultArtifacts(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	workspaceDir := filepath.Join(home, "agents", "agent", "workspace")
	resultsDir := filepath.Join(workspaceDir, ".results")
	if err := os.MkdirAll(resultsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resultsDir, "task-123.md"), []byte("result"), 0o644); err != nil {
		t.Fatal(err)
	}
	rs := writeAdminRuntimeManifest(t, home, "agent", workspaceDir)
	audit := logs.NewWriter(home)
	if err := audit.Write("agent", "agent_signal_pact_verdict", map[string]interface{}{
		"task_id": "task-123",
		"verdict": "completed",
		"evidence_entries": []map[string]interface{}{
			{"kind": "changed_file", "producer": "write_file", "value": "parser.py"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	router := chi.NewRouter()
	RegisterRoutes(router, Deps{
		Config: &config.Config{Home: home},
		Audit:  audit,
		AgentManager: &orchestrate.AgentManager{
			Home:    home,
			Runtime: rs,
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/audit?agent=agent", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var events []map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &events); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events len = %d, want 1: %#v", len(events), events)
	}
	if events[0]["has_result"] != true {
		t.Fatalf("has_result = %#v, want true", events[0]["has_result"])
	}
	result := events[0]["result"].(map[string]interface{})
	if result["url"] != "/api/v1/agents/agent/results/task-123" {
		t.Fatalf("result.url = %#v", result["url"])
	}
	evidenceEntries := events[0]["evidence_entries"].([]interface{})
	if len(evidenceEntries) != 1 {
		t.Fatalf("evidence_entries len = %d, want 1", len(evidenceEntries))
	}
}

func writeAdminRuntimeManifest(t *testing.T, home, agentName, workspaceDir string) *orchestrate.RuntimeSupervisor {
	t.Helper()
	agentDir := filepath.Join(home, "agents", agentName)
	stateDir := filepath.Join(agentDir, "state")
	runtimeDir := filepath.Join(agentDir, "runtime")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte("uuid: ag_"+agentName+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tokenFile := filepath.Join(stateDir, "token.yaml")
	if err := os.WriteFile(tokenFile, []byte("- key: \"abc\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rs := orchestrate.NewRuntimeSupervisor(home, "0.1.0", "", "build-1", "probe", nil, nil, nil, nil)
	spec := runtimecontract.RuntimeSpec{
		RuntimeID: agentName,
		AgentID:   "ag_" + agentName,
		Backend:   "probe",
		Transport: runtimecontract.RuntimeTransportSpec{
			Enforcer: runtimecontract.EnforcerTransportSpec{
				Type:     runtimecontract.TransportTypeLoopbackHTTP,
				Endpoint: "http://127.0.0.1:9911",
				TokenRef: tokenFile,
			},
		},
		Storage: runtimecontract.RuntimeStorageSpec{
			ConfigPath:    agentDir,
			StatePath:     stateDir,
			WorkspacePath: workspaceDir,
		},
	}
	if err := rs.Reconcile(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	return rs
}
