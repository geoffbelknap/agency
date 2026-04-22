package agents

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/geoffbelknap/agency/internal/orchestrate"
	runtimecontract "github.com/geoffbelknap/agency/internal/runtime/contract"
)

func TestParseResultFrontmatter(t *testing.T) {
	metadata, found, err := parseResultFrontmatter([]byte(`---
task_id: task-123
agent: test-agent
pact:
  kind: current_info
  verdict: completed
  source_urls:
    - https://nodejs.org/en/blog/release/v24.15.0
---

Body
`))
	if err != nil {
		t.Fatalf("parseResultFrontmatter() returned error: %v", err)
	}
	if !found {
		t.Fatal("parseResultFrontmatter() found = false, want true")
	}
	if metadata["task_id"] != "task-123" {
		t.Fatalf("task_id = %#v, want task-123", metadata["task_id"])
	}
	pact, ok := metadata["pact"].(map[string]interface{})
	if !ok {
		t.Fatalf("pact metadata missing or wrong type: %#v", metadata["pact"])
	}
	if pact["verdict"] != "completed" {
		t.Fatalf("pact.verdict = %#v, want completed", pact["verdict"])
	}
}

func TestParseResultFrontmatterReportsMalformedMetadata(t *testing.T) {
	if _, found, err := parseResultFrontmatter([]byte("---\ntask_id: task-123\n\nBody")); !found || err == nil {
		t.Fatalf("parseResultFrontmatter() found=%v err=%v, want found=true with error", found, err)
	}
}

func TestGetResultMetadataReturnsPACTFrontmatter(t *testing.T) {
	home := t.TempDir()
	workspaceDir := filepath.Join(home, "agents", "agent", "workspace")
	resultsDir := filepath.Join(workspaceDir, ".results")
	if err := os.MkdirAll(resultsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	rs := writeRuntimeManifest(t, home, "agent", workspaceDir)
	resultPath := filepath.Join(resultsDir, "task-123.md")
	if err := os.WriteFile(resultPath, []byte(`---
task_id: task-123
agent: agent
pact:
  kind: current_info
  verdict: completed
  source_urls:
    - https://nodejs.org/en/blog/release/v24.15.0
---

Verified answer.
`), 0o644); err != nil {
		t.Fatal(err)
	}

	h := &handler{deps: Deps{AgentManager: &orchestrate.AgentManager{
		Home:    home,
		Runtime: rs,
	}}}
	req := resultRequest(http.MethodGet, "/api/v1/agents/agent/results/task-123/metadata", "agent", "task-123")
	rec := httptest.NewRecorder()

	h.getResultMetadata(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["task_id"] != "task-123" {
		t.Fatalf("task_id = %#v, want task-123", body["task_id"])
	}
	if body["has_metadata"] != true {
		t.Fatalf("has_metadata = %#v, want true", body["has_metadata"])
	}
	pact, ok := body["pact"].(map[string]interface{})
	if !ok {
		t.Fatalf("pact missing or wrong type: %#v", body["pact"])
	}
	if pact["kind"] != "current_info" || pact["verdict"] != "completed" {
		t.Fatalf("unexpected pact metadata: %#v", pact)
	}
}

func TestGetResultMetadataRejectsMalformedFrontmatter(t *testing.T) {
	home := t.TempDir()
	workspaceDir := filepath.Join(home, "agents", "agent", "workspace")
	resultsDir := filepath.Join(workspaceDir, ".results")
	if err := os.MkdirAll(resultsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	rs := writeRuntimeManifest(t, home, "agent", workspaceDir)
	if err := os.WriteFile(filepath.Join(resultsDir, "task-123.md"), []byte("---\ntask_id: task-123\nBody"), 0o644); err != nil {
		t.Fatal(err)
	}

	h := &handler{deps: Deps{AgentManager: &orchestrate.AgentManager{
		Home:    home,
		Runtime: rs,
	}}}
	req := resultRequest(http.MethodGet, "/api/v1/agents/agent/results/task-123/metadata", "agent", "task-123")
	rec := httptest.NewRecorder()

	h.getResultMetadata(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestListResultsIncludesPACTMetadata(t *testing.T) {
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
  kind: current_info
  verdict: completed
  source_urls:
    - https://nodejs.org/en/blog/release/v24.15.0
---

Verified answer.
`), 0o644); err != nil {
		t.Fatal(err)
	}

	h := &handler{deps: Deps{AgentManager: &orchestrate.AgentManager{
		Home:    home,
		Runtime: rs,
	}}}
	req := resultRequest(http.MethodGet, "/api/v1/agents/agent/results", "agent", "")
	rec := httptest.NewRecorder()

	h.listResults(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body []map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body) != 1 {
		t.Fatalf("len(results) = %d, want 1: %#v", len(body), body)
	}
	if body[0]["task_id"] != "task-123" || body[0]["has_metadata"] != true {
		t.Fatalf("unexpected result item: %#v", body[0])
	}
	pact, ok := body[0]["pact"].(map[string]interface{})
	if !ok {
		t.Fatalf("pact missing or wrong type: %#v", body[0]["pact"])
	}
	if pact["verdict"] != "completed" {
		t.Fatalf("pact.verdict = %#v, want completed", pact["verdict"])
	}
}

func TestListResultsReportsMalformedMetadataWithoutFailing(t *testing.T) {
	home := t.TempDir()
	workspaceDir := filepath.Join(home, "agents", "agent", "workspace")
	resultsDir := filepath.Join(workspaceDir, ".results")
	if err := os.MkdirAll(resultsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	rs := writeRuntimeManifest(t, home, "agent", workspaceDir)
	if err := os.WriteFile(filepath.Join(resultsDir, "task-123.md"), []byte("---\ntask_id: task-123\nBody"), 0o644); err != nil {
		t.Fatal(err)
	}

	h := &handler{deps: Deps{AgentManager: &orchestrate.AgentManager{
		Home:    home,
		Runtime: rs,
	}}}
	req := resultRequest(http.MethodGet, "/api/v1/agents/agent/results", "agent", "")
	rec := httptest.NewRecorder()

	h.listResults(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body []map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body) != 1 {
		t.Fatalf("len(results) = %d, want 1: %#v", len(body), body)
	}
	if body[0]["metadata_error"] != "invalid result metadata" {
		t.Fatalf("metadata_error = %#v, want invalid result metadata", body[0]["metadata_error"])
	}
}

func resultRequest(method, target, agentName, taskID string) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", agentName)
	rctx.URLParams.Add("taskId", taskID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func writeRuntimeManifest(t *testing.T, home, agentName, workspaceDir string) *orchestrate.RuntimeSupervisor {
	t.Helper()
	agentDir := filepath.Join(home, "agents", agentName)
	stateDir := filepath.Join(agentDir, "state")
	runtimeDir := filepath.Join(agentDir, "runtime")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte("uuid: ag_"+agentName+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tokenFile := filepath.Join(stateDir, "token.yaml")
	if err := os.WriteFile(tokenFile, []byte("- key: \"abc\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rs := orchestrate.NewRuntimeSupervisor(home, "0.1.0", "", "build-1", "probe", nil, nil, nil, nil)
	spec := runtimecontract.RuntimeSpec{
		RuntimeID: agentName,
		AgentID:   "ag_" + agentName,
		Backend:   "probe",
		Transport: runtimecontract.RuntimeTransportSpec{
			Enforcer: runtimecontract.EnforcerTransportSpec{
				Type:     runtimecontract.TransportTypeLoopbackHTTP,
				Endpoint: "http://127.0.0.1:9911",
				TokenRef: tokenFile,
			},
		},
		Storage: runtimecontract.RuntimeStorageSpec{
			ConfigPath:    agentDir,
			StatePath:     stateDir,
			WorkspacePath: workspaceDir,
		},
	}
	if err := rs.Reconcile(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	return rs
}
