package agents

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

type recordingCommsClient struct {
	calls []string
	errs  map[string]error
}

func (r *recordingCommsClient) CommsRequest(_ context.Context, method, path string, _ interface{}) ([]byte, error) {
	key := method + " " + path
	r.calls = append(r.calls, key)
	if err := r.errs[key]; err != nil {
		return nil, err
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
	if len(comms.calls) != 3 {
		t.Fatalf("expected create plus two grant calls, got %v", comms.calls)
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
