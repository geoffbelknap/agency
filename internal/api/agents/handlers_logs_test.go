package agents

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/logs"
	"github.com/geoffbelknap/agency/internal/orchestrate"
)

func TestAgentLogsAnnotatesMatchingResultArtifact(t *testing.T) {
	home := t.TempDir()
	workspaceDir := filepath.Join(home, "agents", "agent", "workspace")
	resultsDir := filepath.Join(workspaceDir, ".results")
	if err := os.MkdirAll(resultsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	rs := writeRuntimeManifest(t, home, "agent", workspaceDir)
	if err := os.WriteFile(filepath.Join(resultsDir, "task-123.md"), []byte("---\ntask_id: task-123\n---\n\nResult"), 0o644); err != nil {
		t.Fatal(err)
	}
	auditDir := filepath.Join(home, "audit", "agent")
	if err := os.MkdirAll(auditDir, 0o755); err != nil {
		t.Fatal(err)
	}
	event := `{"timestamp":"2026-04-22T08:00:00Z","event":"agent_signal_pact_verdict","agent":"agent","task_id":"task-123","verdict":"completed"}`
	if err := os.WriteFile(filepath.Join(auditDir, "events.jsonl"), []byte(event+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	h := &handler{deps: Deps{
		Config: &config.Config{Home: home},
		AgentManager: &orchestrate.AgentManager{
			Home:    home,
			Runtime: rs,
		},
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/agent/logs", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "agent")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	h.agentLogs(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body []map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body) != 1 {
		t.Fatalf("len(logs) = %d, want 1: %#v", len(body), body)
	}
	if body[0]["has_result"] != true {
		t.Fatalf("has_result = %#v, want true", body[0]["has_result"])
	}
	result, ok := body[0]["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("result missing or wrong type: %#v", body[0]["result"])
	}
	if result["task_id"] != "task-123" {
		t.Fatalf("result.task_id = %#v, want task-123", result["task_id"])
	}
	if result["url"] != "/api/v1/agents/agent/results/task-123" {
		t.Fatalf("result.url = %#v", result["url"])
	}
}

func TestIngestEnforcerAuditWritesHostVisibleLog(t *testing.T) {
	home := t.TempDir()
	h := &handler{deps: Deps{
		Config: &config.Config{Home: home},
		Audit:  logs.NewWriter(home),
	}}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/agent/logs/enforcer", strings.NewReader(`{"type":"MEDIATION_PROXY","agent":"agent"}`))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "agent")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	h.ingestEnforcerAudit(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
	events, err := logs.NewReader(home).ReadAgentLog("agent", "", "")
	if err != nil {
		t.Fatalf("ReadAgentLog returned error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events len = %d, want 1: %#v", len(events), events)
	}
	if events[0]["type"] != "MEDIATION_PROXY" || events[0]["source"] != "enforcer" {
		t.Fatalf("unexpected event: %#v", events[0])
	}
}
