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

	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/orchestrate"
)

func TestGetPactRunProjectsArtifactAndAuditEvidence(t *testing.T) {
	home := t.TempDir()
	workspaceDir := filepath.Join(home, "agents", "agent", "workspace")
	resultsDir := filepath.Join(workspaceDir, ".results")
	if err := os.MkdirAll(resultsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	rs := writeRuntimeManifest(t, home, "agent", workspaceDir)
	if err := os.WriteFile(filepath.Join(resultsDir, "task-123.md"), []byte(`---
task_id: task-123
agent: agent
pact:
  kind: code_change
  verdict: completed
  required_evidence:
    - code_change_result_or_blocker
    - tests_or_blocker
  answer_requirements:
    - files_changed
    - tests_run_or_blocker
  changed_files:
    - parser.py
  validation_results:
    - command: pytest tests/test_parser.py
      ok: true
  evidence_entries:
    - kind: changed_file
      producer: write_file
      value: parser.py
pact_activation:
  content: Fix the parser test
  match_type: direct
  source: dm
  channel: dm-agent
  author: operator
  mission_active: false
---

Changed parser.py.
`), 0o644); err != nil {
		t.Fatal(err)
	}
	auditDir := filepath.Join(home, "audit", "agent")
	if err := os.MkdirAll(auditDir, 0o755); err != nil {
		t.Fatal(err)
	}
	event := `{"timestamp":"2026-04-22T08:00:00Z","event":"agent_signal_pact_verdict","agent":"agent","task_id":"task-123","kind":"code_change","verdict":"completed","changed_files":["parser.py"]}`
	if err := os.WriteFile(filepath.Join(auditDir, "gateway.jsonl"), []byte(event+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	h := &handler{deps: Deps{
		Config: &config.Config{Home: home},
		AgentManager: &orchestrate.AgentManager{
			Home:    home,
			Runtime: rs,
		},
	}}
	req := pactRunRequest(http.MethodGet, "/api/v1/agents/agent/pact/runs/task-123", "agent", "task-123")
	rec := httptest.NewRecorder()

	h.getPactRun(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["task_id"] != "task-123" || body["agent"] != "agent" {
		t.Fatalf("unexpected identity fields: %#v", body)
	}
	if body["outcome"] != "completed" {
		t.Fatalf("outcome = %#v, want completed", body["outcome"])
	}
	contract := body["contract"].(map[string]interface{})
	if contract["kind"] != "code_change" {
		t.Fatalf("contract.kind = %#v, want code_change", contract["kind"])
	}
	activation := body["activation"].(map[string]interface{})
	if activation["channel"] != "dm-agent" || activation["mission_active"] != false {
		t.Fatalf("unexpected activation: %#v", activation)
	}
	evidence := body["evidence"].(map[string]interface{})
	changedFiles := evidence["changed_files"].([]interface{})
	if len(changedFiles) != 1 || changedFiles[0] != "parser.py" {
		t.Fatalf("changed_files = %#v", changedFiles)
	}
	evidenceEntries := evidence["evidence_entries"].([]interface{})
	if len(evidenceEntries) != 1 {
		t.Fatalf("evidence_entries len = %d, want 1", len(evidenceEntries))
	}
	artifact := body["artifact"].(map[string]interface{})
	if artifact["task_id"] != "task-123" {
		t.Fatalf("artifact.task_id = %#v, want task-123", artifact["task_id"])
	}
	if artifact["url"] != "/api/v1/agents/agent/results/task-123" {
		t.Fatalf("artifact.url = %#v", artifact["url"])
	}
	auditEvents := body["audit_events"].([]interface{})
	if len(auditEvents) != 1 {
		t.Fatalf("audit_events len = %d, want 1", len(auditEvents))
	}
}

func TestGetPactRunCanProjectAuditOnlyRun(t *testing.T) {
	home := t.TempDir()
	auditDir := filepath.Join(home, "audit", "agent")
	if err := os.MkdirAll(auditDir, 0o755); err != nil {
		t.Fatal(err)
	}
	event := `{"timestamp":"2026-04-22T08:00:00Z","event":"agent_signal_pact_verdict","agent":"agent","task_id":"task-123","kind":"operator_blocked","verdict":"blocked","missing_evidence":[]}`
	if err := os.WriteFile(filepath.Join(auditDir, "gateway.jsonl"), []byte(event+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	h := &handler{deps: Deps{Config: &config.Config{Home: home}}}
	req := pactRunRequest(http.MethodGet, "/api/v1/agents/agent/pact/runs/task-123", "agent", "task-123")
	rec := httptest.NewRecorder()

	h.getPactRun(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["outcome"] != "blocked" {
		t.Fatalf("outcome = %#v, want blocked", body["outcome"])
	}
	verdict := body["verdict"].(map[string]interface{})
	if verdict["verdict"] != "blocked" {
		t.Fatalf("verdict.verdict = %#v, want blocked", verdict["verdict"])
	}
	if _, ok := body["artifact"]; ok {
		t.Fatalf("artifact should be omitted for audit-only run: %#v", body["artifact"])
	}
}

func TestGetPactAuditReportIncludesStableIntegrity(t *testing.T) {
	home := t.TempDir()
	workspaceDir := filepath.Join(home, "agents", "agent", "workspace")
	resultsDir := filepath.Join(workspaceDir, ".results")
	if err := os.MkdirAll(resultsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	rs := writeRuntimeManifest(t, home, "agent", workspaceDir)
	if err := os.WriteFile(filepath.Join(resultsDir, "task-123.md"), []byte(`---
task_id: task-123
agent: agent
pact:
  kind: code_change
  verdict: completed
  changed_files:
    - parser.py
  evidence_entries:
    - kind: changed_file
      producer: write_file
      value: parser.py
---

Changed parser.py.
`), 0o644); err != nil {
		t.Fatal(err)
	}
	auditDir := filepath.Join(home, "audit", "agent")
	if err := os.MkdirAll(auditDir, 0o755); err != nil {
		t.Fatal(err)
	}
	event := `{"timestamp":"2026-04-22T08:00:00Z","event":"agent_signal_pact_verdict","agent":"agent","task_id":"task-123","kind":"code_change","verdict":"completed","evidence_entries":[{"kind":"changed_file","producer":"write_file","value":"parser.py"}]}`
	if err := os.WriteFile(filepath.Join(auditDir, "gateway.jsonl"), []byte(event+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	h := &handler{deps: Deps{
		Config: &config.Config{Home: home},
		AgentManager: &orchestrate.AgentManager{
			Home:    home,
			Runtime: rs,
		},
	}}
	req := pactRunRequest(http.MethodGet, "/api/v1/agents/agent/pact/runs/task-123/audit-report", "agent", "task-123")
	rec := httptest.NewRecorder()

	h.getPactAuditReport(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["report_id"] == "" || body["agent"] != "agent" || body["task_id"] != "task-123" {
		t.Fatalf("unexpected report identity: %#v", body)
	}
	integrity := body["integrity"].(map[string]interface{})
	if integrity["algorithm"] != "sha256" {
		t.Fatalf("integrity.algorithm = %#v, want sha256", integrity["algorithm"])
	}
	hash, _ := integrity["hash"].(string)
	if len(hash) != 64 {
		t.Fatalf("integrity.hash length = %d, want 64", len(hash))
	}
	entries := body["evidence_entries"].([]interface{})
	if len(entries) != 1 {
		t.Fatalf("evidence_entries len = %d, want 1", len(entries))
	}
	artifacts := body["artifact_refs"].([]interface{})
	if len(artifacts) != 1 {
		t.Fatalf("artifact_refs len = %d, want 1", len(artifacts))
	}
	events := body["audit_events"].([]interface{})
	if len(events) != 1 {
		t.Fatalf("audit_events len = %d, want 1", len(events))
	}

	run, found := h.buildPactRunProjection(context.Background(), "agent", "task-123")
	if !found {
		t.Fatal("buildPactRunProjection() found = false")
	}
	reportA, err := pactAuditReportFromRun("agent", "task-123", run, time.Date(2026, 4, 22, 1, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	reportB, err := pactAuditReportFromRun("agent", "task-123", run, time.Date(2026, 4, 22, 2, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if reportA.Integrity.Hash != reportB.Integrity.Hash || reportA.ReportID != reportB.ReportID {
		t.Fatalf("report integrity should be stable across generated_at: %#v %#v", reportA.Integrity, reportB.Integrity)
	}
	if reportA.GeneratedAt == reportB.GeneratedAt {
		t.Fatalf("generated_at should reflect report generation time")
	}
}

func TestVerifyPactAuditReportChecksHash(t *testing.T) {
	home := t.TempDir()
	workspaceDir := filepath.Join(home, "agents", "agent", "workspace")
	resultsDir := filepath.Join(workspaceDir, ".results")
	if err := os.MkdirAll(resultsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	rs := writeRuntimeManifest(t, home, "agent", workspaceDir)
	if err := os.WriteFile(filepath.Join(resultsDir, "task-123.md"), []byte(`---
task_id: task-123
agent: agent
pact:
  kind: code_change
  verdict: completed
  evidence_entries:
    - kind: changed_file
      producer: write_file
      value: parser.py
---

Changed parser.py.
`), 0o644); err != nil {
		t.Fatal(err)
	}
	auditDir := filepath.Join(home, "audit", "agent")
	if err := os.MkdirAll(auditDir, 0o755); err != nil {
		t.Fatal(err)
	}
	event := `{"timestamp":"2026-04-22T08:00:00Z","event":"agent_signal_pact_verdict","agent":"agent","task_id":"task-123","kind":"code_change","verdict":"completed"}`
	if err := os.WriteFile(filepath.Join(auditDir, "gateway.jsonl"), []byte(event+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	h := &handler{deps: Deps{
		Config: &config.Config{Home: home},
		AgentManager: &orchestrate.AgentManager{
			Home:    home,
			Runtime: rs,
		},
	}}
	run, found := h.buildPactRunProjection(context.Background(), "agent", "task-123")
	if !found {
		t.Fatal("buildPactRunProjection() found = false")
	}
	report, err := pactAuditReportFromRun("agent", "task-123", run, time.Date(2026, 4, 22, 1, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}

	req := pactRunRequest(http.MethodPost, "/api/v1/agents/agent/pact/runs/task-123/audit-report/verify?hash="+report.Integrity.Hash, "agent", "task-123")
	rec := httptest.NewRecorder()
	h.verifyPactAuditReport(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var validBody map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &validBody); err != nil {
		t.Fatal(err)
	}
	if validBody["valid"] != true || validBody["actual_hash"] != report.Integrity.Hash {
		t.Fatalf("unexpected valid verification: %#v", validBody)
	}

	req = pactRunRequest(http.MethodPost, "/api/v1/agents/agent/pact/runs/task-123/audit-report/verify?hash=bad", "agent", "task-123")
	rec = httptest.NewRecorder()
	h.verifyPactAuditReport(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var invalidBody map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &invalidBody); err != nil {
		t.Fatal(err)
	}
	if invalidBody["valid"] != false || invalidBody["reason"] != "hash_mismatch" {
		t.Fatalf("unexpected invalid verification: %#v", invalidBody)
	}
}

func TestGetPactRunRejectsInvalidTaskID(t *testing.T) {
	h := &handler{}
	req := pactRunRequest(http.MethodGet, "/api/v1/agents/agent/pact/runs/../secret", "agent", "../secret")
	rec := httptest.NewRecorder()

	h.getPactRun(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetPactAuditReportNotFoundWithoutRun(t *testing.T) {
	home := t.TempDir()
	h := &handler{deps: Deps{Config: &config.Config{Home: home}}}
	req := pactRunRequest(http.MethodGet, "/api/v1/agents/agent/pact/runs/task-123/audit-report", "agent", "task-123")
	rec := httptest.NewRecorder()

	h.getPactAuditReport(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetPactRunNotFoundWithoutArtifactOrAudit(t *testing.T) {
	home := t.TempDir()
	h := &handler{deps: Deps{Config: &config.Config{Home: home}}}
	req := pactRunRequest(http.MethodGet, "/api/v1/agents/agent/pact/runs/task-123", "agent", "task-123")
	rec := httptest.NewRecorder()

	h.getPactRun(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func pactRunRequest(method, target, agentName, taskID string) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", agentName)
	rctx.URLParams.Add("taskId", taskID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}
