package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/geoffbelknap/agency/internal/config"
)

func TestRegisterAll_NonDockerInfraRoutesDoNotPanic(t *testing.T) {
	cfg := &config.Config{
		Home:    t.TempDir(),
		Version: "test",
		Token:   "test-token",
		Hub: config.HubConfig{
			DeploymentBackend: "probe",
		},
	}
	startup, err := Startup(cfg, nil, nil)
	if err != nil {
		t.Fatalf("Startup() error = %v", err)
	}

	r := chi.NewRouter()
	RegisterAll(r, cfg, nil, nil, startup, RouteOptions{})

	tests := []struct {
		method string
		path   string
		want   int
	}{
		{method: http.MethodGet, path: "/api/v1/infra/status", want: http.StatusOK},
		{method: http.MethodPost, path: "/api/v1/infra/up", want: http.StatusServiceUnavailable},
	}

	for _, tc := range tests {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Fatalf("%s %s = %d, want %d", tc.method, tc.path, rec.Code, tc.want)
		}
	}
}

func TestRegisterAll_NonDockerAgentDMUsesHostComms(t *testing.T) {
	var calls []string
	comms := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
	}))
	defer comms.Close()

	u, err := url.Parse(comms.URL)
	if err != nil {
		t.Fatalf("parse comms URL: %v", err)
	}
	_, port, ok := strings.Cut(u.Host, ":")
	if !ok {
		t.Fatalf("test comms URL missing port: %s", comms.URL)
	}
	t.Setenv("AGENCY_GATEWAY_PROXY_PORT", port)

	cfg := &config.Config{
		Home:    t.TempDir(),
		Version: "test",
		Token:   "test-token",
		Hub: config.HubConfig{
			DeploymentBackend: "probe",
		},
	}
	startup, err := Startup(cfg, nil, nil)
	if err != nil {
		t.Fatalf("Startup() error = %v", err)
	}

	r := chi.NewRouter()
	RegisterAll(r, cfg, nil, nil, startup, RouteOptions{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/henry/dm", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/v1/agents/henry/dm = %d, body %s", rec.Code, rec.Body.String())
	}
	want := []string{
		"POST /channels",
		"POST /channels/dm-henry/grant-access",
		"POST /channels/dm-henry/grant-access",
	}
	if strings.Join(calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("comms calls = %#v, want %#v", calls, want)
	}
}

func TestMCPInfraTools_NonDockerBackendReturnBackendSpecificUnavailable(t *testing.T) {
	reg := NewMCPToolRegistry()
	registerMCPTools(reg)

	deps := &mcpDeps{
		cfg: &config.Config{
			Home:    t.TempDir(),
			Version: "test",
			Hub: config.HubConfig{
				DeploymentBackend: "probe",
			},
		},
	}

	text, isErr := reg.Call("agency_infra_status", deps, nil)
	if !isErr {
		t.Fatal("agency_infra_status should fail for non-docker backend")
	}
	if !strings.Contains(text, "Current backend: probe") {
		t.Fatalf("unexpected response: %q", text)
	}
}

func TestMCPAdminDoctor_NonDockerBackendUsesRuntimeSummary(t *testing.T) {
	reg := NewMCPToolRegistry()
	registerMCPTools(reg)

	cfg := &config.Config{
		Home:    t.TempDir(),
		Version: "test",
		Hub: config.HubConfig{
			DeploymentBackend: "probe",
		},
	}
	startup, err := Startup(cfg, nil, nil)
	if err != nil {
		t.Fatalf("Startup() error = %v", err)
	}

	text, isErr := reg.Call("agency_admin_doctor", &mcpDeps{
		cfg:    cfg,
		agents: startup.AgentManager,
	}, nil)
	if isErr {
		t.Fatalf("agency_admin_doctor returned error: %q", text)
	}
	if !strings.Contains(text, "No managed agents to check (backend: probe)") {
		t.Fatalf("unexpected response: %q", text)
	}
}
