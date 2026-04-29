package infra

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/orchestrate"
)

func TestInfraStatus_NonDockerBackendReturnsBackendAwareStatus(t *testing.T) {
	h := &handler{deps: Deps{
		Config: &config.Config{
			Home:    t.TempDir(),
			Version: "test",
			Hub: config.HubConfig{
				DeploymentBackend: "probe",
			},
		},
	}}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/infra/status", nil)
	rec := httptest.NewRecorder()

	h.infraStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := body["backend"]; got != "probe" {
		t.Fatalf("backend = %v, want probe", got)
	}
	if got := body["backend_endpoint"]; got != "" {
		t.Fatalf("backend_endpoint = %v, want empty", got)
	}
	if got := body["backend_mode"]; got != "" {
		t.Fatalf("backend_mode = %v, want empty", got)
	}
	if got := body["container_backend"]; got != "not_applicable" {
		t.Fatalf("container_backend = %v, want not_applicable", got)
	}
	if got := body["runtime_backend_state"]; got != "not_applicable" {
		t.Fatalf("runtime_backend_state = %v, want not_applicable", got)
	}
	if got := body["infra_control_available"]; got != false {
		t.Fatalf("infra_control_available = %v, want false", got)
	}
}

func TestInfraUp_NonDockerBackendReturnsBackendSpecificUnavailable(t *testing.T) {
	h := &handler{deps: Deps{
		Config: &config.Config{
			Home: t.TempDir(),
			Hub: config.HubConfig{
				DeploymentBackend: "probe",
			},
		},
	}}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/infra/up", nil)
	rec := httptest.NewRecorder()

	h.infraUp(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["backend"] != "probe" {
		t.Fatalf("backend = %q, want probe", body["backend"])
	}
	if body["error"] == "" {
		t.Fatal("expected non-empty error")
	}
}

func TestInfraLogs_MicroVMBackendsReadHostInfraLog(t *testing.T) {
	for _, backend := range []string{"firecracker", "apple-vf-microvm"} {
		t.Run(backend, func(t *testing.T) {
			testInfraLogsMicroVMBackendReadsHostInfraLog(t, backend)
		})
	}
}

func testInfraLogsMicroVMBackendReadsHostInfraLog(t *testing.T, backend string) {
	home := t.TempDir()
	logPath := filepath.Join(home, "logs", "infra", "comms.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		t.Fatalf("mkdir log dir: %v", err)
	}
	if err := os.WriteFile(logPath, []byte("alpha\nbeta\ngamma\n"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	runDir := filepath.Join(home, "run")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatalf("mkdir run dir: %v", err)
	}
	metadata := `{
  "component": "comms",
  "service": "agency-infra-comms",
  "pid": 123,
  "pid_file": "` + filepath.Join(runDir, "agency-infra-comms.pid") + `",
  "command": ["comms"],
  "log_file": "` + logPath + `",
  "health_url": "http://127.0.0.1:8202/health",
  "started_at": "2026-04-30T00:00:00Z"
}
`
	if err := os.WriteFile(filepath.Join(runDir, "agency-infra-comms.json"), []byte(metadata), 0o644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}

	h := &handler{deps: Deps{
		Infra: &orchestrate.Infra{Home: home, RuntimeBackendName: backend},
		Config: &config.Config{
			Home: home,
			Hub: config.HubConfig{
				DeploymentBackend: backend,
			},
		},
	}}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/infra/services/comms/logs?tail=2", nil)
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("component", "comms")
	req = req.WithContext(contextWithRouteContext(req.Context(), routeContext))
	rec := httptest.NewRecorder()

	h.infraLogs(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Component string `json:"component"`
		Tail      int    `json:"tail"`
		Logs      string `json:"logs"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Component != "comms" || body.Tail != 2 {
		t.Fatalf("response metadata = %+v", body)
	}
	if body.Logs != "beta\ngamma\n" {
		t.Fatalf("logs = %q, want host log tail", body.Logs)
	}
}

func contextWithRouteContext(ctx context.Context, routeContext *chi.Context) context.Context {
	return context.WithValue(ctx, chi.RouteCtxKey, routeContext)
}
