package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	agencyctx "github.com/geoffbelknap/agency/internal/context"
)

func setupContextTestRouter() (*chi.Mux, *agencyctx.Manager) {
	mgr := agencyctx.NewManager(nil)
	r := chi.NewRouter()
	h := &contextHandler{mgr: mgr}
	r.Route("/api/v1/agents/{name}/context", func(r chi.Router) {
		r.Get("/constraints", h.getConstraints)
		r.Get("/policy", h.getPolicy)
		r.Get("/exceptions", h.getExceptions)
		r.Get("/changes", h.getChanges)
		r.Post("/push", h.push)
		r.Get("/status", h.getStatus)
	})
	return r, mgr
}

func TestContextPush(t *testing.T) {
	r, _ := setupContextTestRouter()

	body := map[string]interface{}{
		"constraints": map[string]interface{}{"budget": map[string]interface{}{"max_daily_usd": 5.0}},
		"reason":      "test push",
	}
	b, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/api/v1/agents/test-agent/context/push", bytes.NewReader(b))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("status = %d, want 202", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != "pending" {
		t.Errorf("status = %v, want pending", resp["status"])
	}
}

func TestContextPushRejectsDowngrade(t *testing.T) {
	r, mgr := setupContextTestRouter()
	mgr.SetInitialConstraints("test-agent", map[string]interface{}{
		"granted_capabilities": []interface{}{"web_search", "code_exec"},
	})

	body := map[string]interface{}{
		"constraints":       map[string]interface{}{"granted_capabilities": []interface{}{"web_search"}},
		"severity_override": "LOW",
		"reason":            "should fail",
	}
	b, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/api/v1/agents/test-agent/context/push", bytes.NewReader(b))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestContextStatus(t *testing.T) {
	r, mgr := setupContextTestRouter()
	mgr.Push("test-agent", map[string]interface{}{"foo": "bar"}, "", "r", "op")

	req := httptest.NewRequest("GET", "/api/v1/agents/test-agent/context/status", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestContextChanges(t *testing.T) {
	r, mgr := setupContextTestRouter()
	mgr.Push("test-agent", map[string]interface{}{"a": 1}, "", "r1", "op")
	mgr.Push("test-agent", map[string]interface{}{"b": 2}, "", "r2", "op")

	req := httptest.NewRequest("GET", "/api/v1/agents/test-agent/context/changes", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var changes []map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &changes)
	if len(changes) != 2 {
		t.Errorf("len = %d, want 2", len(changes))
	}
}
