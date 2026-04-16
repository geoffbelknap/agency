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

func TestShowRuntimeManifest(t *testing.T) {
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
	tokenFile := filepath.Join(stateDir, "token.yaml")
	if err := os.WriteFile(tokenFile, []byte("- key: \"abc\"\n"), 0o644); err != nil {
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
					"tokenRef": tokenFile,
				},
			},
		},
		"status": map[string]any{
			"runtimeId": "runtime-agent",
			"agentId":   "ag_runtime",
			"phase":     runtimecontract.RuntimePhaseReconciled,
			"healthy":   false,
			"backend":   "docker",
			"transport": map[string]any{
				"type":     runtimecontract.TransportTypeLoopbackHTTP,
				"endpoint": "http://127.0.0.1:9999",
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

	h := &handler{deps: Deps{
		AgentManager: &orchestrate.AgentManager{
			Home:    home,
			Runtime: orchestrate.NewRuntimeSupervisor(home, "0.1.0", "", "build-1", "docker", nil, nil, nil, nil),
		},
	}}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/runtime-agent/runtime/manifest", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "runtime-agent")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	h.showRuntimeManifest(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	spec, ok := body["spec"].(map[string]any)
	if !ok || spec["runtimeId"] != "runtime-agent" {
		t.Fatalf("unexpected manifest body: %#v", body)
	}
}

func TestShowRuntimeStatusAndValidate(t *testing.T) {
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
	tokenFile := filepath.Join(stateDir, "token.yaml")
	if err := os.WriteFile(tokenFile, []byte("- key: \"abc\"\n"), 0o644); err != nil {
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
					"tokenRef": tokenFile,
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
			"phase":     runtimecontract.RuntimePhaseStopped,
			"healthy":   false,
			"backend":   "docker",
			"transport": map[string]any{
				"type":     runtimecontract.TransportTypeLoopbackHTTP,
				"endpoint": "http://127.0.0.1:9999",
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

	h := &handler{deps: Deps{
		AgentManager: &orchestrate.AgentManager{
			Home:    home,
			Runtime: orchestrate.NewRuntimeSupervisor(home, "0.1.0", "", "build-1", "docker", nil, nil, nil, nil),
		},
	}}

	statusReq := httptest.NewRequest(http.MethodGet, "/api/v1/agents/runtime-agent/runtime/status", nil)
	statusCtx := chi.NewRouteContext()
	statusCtx.URLParams.Add("name", "runtime-agent")
	statusReq = statusReq.WithContext(context.WithValue(statusReq.Context(), chi.RouteCtxKey, statusCtx))
	statusRec := httptest.NewRecorder()
	h.showRuntimeStatus(statusRec, statusReq)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", statusRec.Code, statusRec.Body.String())
	}

	validateReq := httptest.NewRequest(http.MethodPost, "/api/v1/agents/runtime-agent/runtime/validate", nil)
	validateCtx := chi.NewRouteContext()
	validateCtx.URLParams.Add("name", "runtime-agent")
	validateReq = validateReq.WithContext(context.WithValue(validateReq.Context(), chi.RouteCtxKey, validateCtx))
	validateRec := httptest.NewRecorder()
	h.validateRuntime(validateRec, validateReq)
	if validateRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", validateRec.Code, validateRec.Body.String())
	}
}
