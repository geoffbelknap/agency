package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/orchestrate"
	runtimecontract "github.com/geoffbelknap/agency/internal/runtime/contract"
	"gopkg.in/yaml.v3"
)

func TestSplitDoctorChecksSeparatesDockerBackendHygiene(t *testing.T) {
	t.Parallel()

	checks := []doctorCheckResult{
		{Name: "credentials_isolated", Agent: "henry", Status: "pass"},
		{Name: "host_capacity", Status: "pass"},
		{Name: "docker_dangling_images", Status: "warn"},
		{Name: "network_pool", Status: "warn"},
	}

	runtimeChecks, backendChecks := splitDoctorChecks(checks, "docker")

	if len(runtimeChecks) != 2 {
		t.Fatalf("runtimeChecks len = %d, want 2", len(runtimeChecks))
	}
	if len(backendChecks) != 2 {
		t.Fatalf("backendChecks len = %d, want 2", len(backendChecks))
	}
	if runtimeChecks[0].Name != "credentials_isolated" || runtimeChecks[1].Name != "host_capacity" {
		t.Fatalf("unexpected runtime checks: %#v", runtimeChecks)
	}
	if backendChecks[0].Name != "docker_dangling_images" || backendChecks[1].Name != "network_pool" {
		t.Fatalf("unexpected backend checks: %#v", backendChecks)
	}
}

func TestSplitDoctorChecksKeepsNetworkPoolOutOfPodmanBackendChecks(t *testing.T) {
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

func TestSplitDoctorChecksSeparatesContainerdBackendHygiene(t *testing.T) {
	t.Parallel()

	checks := []doctorCheckResult{
		{Name: "credentials_isolated", Agent: "henry", Status: "pass"},
		{Name: "containerd_dangling_images", Status: "warn"},
		{Name: "containerd_log_rotation", Status: "warn"},
		{Name: "network_pool", Status: "warn"},
	}

	runtimeChecks, backendChecks := splitDoctorChecks(checks, "containerd")

	if len(backendChecks) != 2 {
		t.Fatalf("backendChecks len = %d, want 2", len(backendChecks))
	}
	if len(runtimeChecks) != 2 {
		t.Fatalf("runtimeChecks len = %d, want 2", len(runtimeChecks))
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
