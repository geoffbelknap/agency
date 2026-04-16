package agents

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"

	"github.com/geoffbelknap/agency/internal/orchestrate"
	runtimecontract "github.com/geoffbelknap/agency/internal/runtime/contract"
)

func TestShowAgentUsesPersistedRuntimeStatusWhenBackendUnavailable(t *testing.T) {
	home := t.TempDir()
	agentDir := filepath.Join(home, "agents", "runtime-agent")
	stateDir := filepath.Join(agentDir, "state")
	runtimeDir := filepath.Join(agentDir, "runtime")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte("uuid: ag_runtime\ntype: standard\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "constraints.yaml"), []byte("identity:\n  role: assistant\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "token.yaml"), []byte("- key: \"abc\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	manifest := map[string]any{
		"spec": map[string]any{
			"runtimeId": "runtime-agent",
			"agentId":   "ag_runtime",
			"backend":   "docker",
			"transport": map[string]any{
				"enforcer": map[string]any{
					"type":     runtimecontract.TransportTypeLoopbackHTTP,
					"endpoint": "http://127.0.0.1:9999",
					"tokenRef": filepath.Join(stateDir, "token.yaml"),
				},
			},
			"storage": map[string]any{
				"configPath": agentDir,
				"statePath":  stateDir,
			},
		},
		"status": map[string]any{
			"runtimeId": "runtime-agent",
			"agentId":   "ag_runtime",
			"phase":     runtimecontract.RuntimePhaseDegraded,
			"healthy":   false,
			"backend":   "docker",
			"transport": map[string]any{
				"type":              runtimecontract.TransportTypeLoopbackHTTP,
				"endpoint":          "http://127.0.0.1:9999",
				"enforcerConnected": false,
				"lastError":         "lost mediation",
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

	am := &orchestrate.AgentManager{
		Home:    home,
		Runtime: orchestrate.NewRuntimeSupervisor(home, "0.1.0", "", "build-1", "docker", nil, nil, nil, nil),
	}
	h := &handler{deps: Deps{AgentManager: am}}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/runtime-agent", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "runtime-agent")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	h.showAgent(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "unhealthy" {
		t.Fatalf("status = %v, want unhealthy", body["status"])
	}
	if body["enforcer"] != "stopped" {
		t.Fatalf("enforcer = %v, want stopped", body["enforcer"])
	}
}

func TestShowAgentRuntimeStoppedWithActiveHaltReturnsHalted(t *testing.T) {
	home := t.TempDir()
	agentDir := filepath.Join(home, "agents", "halted-agent")
	stateDir := filepath.Join(agentDir, "state")
	runtimeDir := filepath.Join(agentDir, "runtime")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte("uuid: ag_halted\ntype: standard\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "constraints.yaml"), []byte("identity:\n  role: assistant\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "token.yaml"), []byte("- key: \"abc\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "active-halt.json"), []byte(`{"halt_id":"h1"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	manifest := map[string]any{
		"spec": map[string]any{
			"runtimeId": "halted-agent",
			"agentId":   "ag_halted",
			"backend":   "docker",
			"transport": map[string]any{
				"enforcer": map[string]any{
					"type":     runtimecontract.TransportTypeLoopbackHTTP,
					"endpoint": "http://127.0.0.1:9998",
					"tokenRef": filepath.Join(stateDir, "token.yaml"),
				},
			},
			"storage": map[string]any{
				"configPath": agentDir,
				"statePath":  stateDir,
			},
		},
		"status": map[string]any{
			"runtimeId": "halted-agent",
			"agentId":   "ag_halted",
			"phase":     runtimecontract.RuntimePhaseStopped,
			"healthy":   false,
			"backend":   "docker",
			"transport": map[string]any{
				"type":              runtimecontract.TransportTypeLoopbackHTTP,
				"endpoint":          "http://127.0.0.1:9998",
				"enforcerConnected": false,
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

	am := &orchestrate.AgentManager{
		Home:    home,
		Runtime: orchestrate.NewRuntimeSupervisor(home, "0.1.0", "", "build-1", "docker", nil, nil, nil, nil),
	}
	h := &handler{deps: Deps{AgentManager: am}}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/halted-agent", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "halted-agent")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	h.showAgent(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "halted" {
		t.Fatalf("status = %v, want halted", body["status"])
	}
}
