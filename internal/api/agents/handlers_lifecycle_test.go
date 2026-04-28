package agents

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/geoffbelknap/agency/internal/orchestrate"
	"github.com/go-chi/chi/v5"
)

type recordingCommsClient struct {
	calls     []string
	errs      map[string]error
	responses map[string][]byte
}

func (r *recordingCommsClient) CommsRequest(_ context.Context, method, path string, _ interface{}) ([]byte, error) {
	key := method + " " + path
	r.calls = append(r.calls, key)
	if err := r.errs[key]; err != nil {
		return nil, err
	}
	if data := r.responses[key]; data != nil {
		return data, nil
	}
	return []byte(`{"ok":true}`), nil
}

func TestEnsureAgentDMCreatesDirectChannelAndGrantsMembership(t *testing.T) {
	comms := &recordingCommsClient{}
	h := &handler{deps: Deps{Comms: comms}}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/henry/dm", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "henry")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	h.ensureAgentDM(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	wantCalls := []string{
		"POST /channels",
		"POST /channels/dm-henry/grant-access",
		"POST /channels/dm-henry/grant-access",
	}
	if len(comms.calls) != len(wantCalls) {
		t.Fatalf("expected %d comms calls, got %d: %v", len(wantCalls), len(comms.calls), comms.calls)
	}
	for i, want := range wantCalls {
		if comms.calls[i] != want {
			t.Fatalf("call %d = %q, want %q", i, comms.calls[i], want)
		}
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["channel"] != "dm-henry" || body["status"] != "ready" {
		t.Fatalf("unexpected response: %#v", body)
	}
}

func TestEnsureAgentDMIgnoresAlreadyExistsOnCreate(t *testing.T) {
	comms := &recordingCommsClient{
		errs: map[string]error{
			"POST /channels": errors.New("409 conflict"),
		},
	}
	h := &handler{deps: Deps{Comms: comms}}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/henry/dm", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "henry")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	h.ensureAgentDM(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	wantCalls := []string{
		"POST /channels",
		"GET /channels?member=_operator&state=all",
		"POST /channels/dm-henry/retire",
		"POST /channels",
		"POST /channels/dm-henry/grant-access",
		"POST /channels/dm-henry/grant-access",
	}
	if len(comms.calls) != len(wantCalls) {
		t.Fatalf("expected %d calls, got %d: %v", len(wantCalls), len(comms.calls), comms.calls)
	}
	for i, want := range wantCalls {
		if comms.calls[i] != want {
			t.Fatalf("call %d = %q, want %q", i, comms.calls[i], want)
		}
	}
}

func TestEnsureAgentDMDoesNotRetireActiveExistingChannel(t *testing.T) {
	comms := &recordingCommsClient{
		errs: map[string]error{
			"POST /channels": errors.New("409 conflict"),
		},
		responses: map[string][]byte{
			"GET /channels?member=_operator&state=all": []byte(`[{"name":"dm-henry","state":"active"}]`),
		},
	}
	h := &handler{deps: Deps{Comms: comms}}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/henry/dm", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "henry")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	h.ensureAgentDM(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	for _, call := range comms.calls {
		if call == "POST /channels/dm-henry/retire" {
			t.Fatalf("active DM channel should not be retired: %v", comms.calls)
		}
	}
}

func TestEnsureAgentDMReturnsErrorWhenGrantFails(t *testing.T) {
	comms := &recordingCommsClient{
		errs: map[string]error{
			"POST /channels/dm-henry/grant-access": errors.New("grant failed"),
		},
	}
	h := &handler{deps: Deps{Comms: comms}}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/henry/dm", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "henry")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	h.ensureAgentDM(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRuntimeInstanceIDFallsBackWithoutRuntimeInstanceClient(t *testing.T) {
	h := &handler{}
	if got := h.runtimeInstanceID(context.Background(), "henry", "workspace"); got != "henry:workspace" {
		t.Fatalf("runtimeInstanceID() = %q, want henry:workspace", got)
	}
}

func TestRuntimeLifecycleAvailableUsesRuntimeSupervisor(t *testing.T) {
	home := t.TempDir()
	agentDir := filepath.Join(home, "agents", "henry")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte("uuid: ag_123\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := &handler{deps: Deps{
		AgentManager: &orchestrate.AgentManager{
			Home:    home,
			Runtime: orchestrate.NewRuntimeSupervisor(home, "0.1.0", "", "build-1", "probe", nil, nil, nil, nil),
		},
	}}
	rec := httptest.NewRecorder()
	if !h.runtimeLifecycleAvailable(rec) {
		t.Fatalf("runtimeLifecycleAvailable() = false, body=%s", rec.Body.String())
	}
}
