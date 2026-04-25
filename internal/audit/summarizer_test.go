package audit

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"log/slog"
)

func TestParseAuditLine(t *testing.T) {
	line := `{"ts":"2026-03-27T10:00:00Z","type":"LLM_DIRECT","agent":"researcher","model":"standard","event_id":"evt-abc123","input_tokens":1000,"output_tokens":500}`
	entry, err := parseAuditLine([]byte(line))
	if err != nil {
		t.Fatalf("parseAuditLine: %v", err)
	}
	if entry.Agent != "researcher" {
		t.Errorf("Agent=%q want researcher", entry.Agent)
	}
	if entry.EventID != "evt-abc123" {
		t.Errorf("EventID=%q want evt-abc123", entry.EventID)
	}
	if entry.InputTokens != 1000 {
		t.Errorf("InputTokens=%d want 1000", entry.InputTokens)
	}
}

func TestParseAuditLineMalformed(t *testing.T) {
	_, err := parseAuditLine([]byte("not json"))
	if err == nil {
		t.Error("expected error")
	}
}

func TestEstimateCostKnown(t *testing.T) {
	cost := EstimateCostWithPricing(map[string]ModelPrice{
		"standard": {InputPer1M: 3.0, OutputPer1M: 15.0},
	}, "standard", 1_000_000, 1_000_000)
	if cost != 18.0 {
		t.Errorf("cost=%f want 18.0", cost)
	}
}

func TestEstimateCostUnknown(t *testing.T) {
	cost := EstimateCost("unknown-model", 1000, 500)
	if cost != 0 {
		t.Errorf("cost=%f want 0", cost)
	}
}

func TestSummarizeDate(t *testing.T) {
	tmp := t.TempDir()
	agentDir := filepath.Join(tmp, "audit", "researcher")
	os.MkdirAll(agentDir, 0755)
	lines := []string{
		`{"ts":"2026-03-27T10:00:00Z","type":"LLM_DIRECT","agent":"researcher","model":"standard","event_id":"evt-001","input_tokens":1000,"output_tokens":500}`,
		`{"ts":"2026-03-27T10:01:00Z","type":"LLM_DIRECT","agent":"researcher","model":"standard","event_id":"evt-001","input_tokens":2000,"output_tokens":1000}`,
		`{"ts":"2026-03-27T10:02:00Z","type":"LLM_DIRECT","agent":"researcher","model":"standard","event_id":"evt-002","input_tokens":500,"output_tokens":200}`,
	}
	f, _ := os.Create(filepath.Join(agentDir, "enforcer-2026-03-27.jsonl"))
	for _, l := range lines {
		f.WriteString(l + "\n")
	}
	f.Close()

	s := &AuditSummarizer{homeDir: tmp, missionMap: map[string]string{"researcher": "soc-triage"}}
	metrics, err := s.summarizeDate("2026-03-27")
	if err != nil {
		t.Fatalf("summarizeDate: %v", err)
	}
	if len(metrics) != 1 {
		t.Fatalf("got %d metrics want 1", len(metrics))
	}
	m := metrics[0]
	if m.Mission != "soc-triage" {
		t.Errorf("Mission=%q want soc-triage", m.Mission)
	}
	if m.Activations != 2 {
		t.Errorf("Activations=%d want 2", m.Activations)
	}
	if m.TotalInputTokens != 3500 {
		t.Errorf("TotalInputTokens=%d want 3500", m.TotalInputTokens)
	}
	if m.TotalOutputTokens != 1700 {
		t.Errorf("TotalOutputTokens=%d want 1700", m.TotalOutputTokens)
	}
	if m.Model != "standard" {
		t.Errorf("Model=%q want standard", m.Model)
	}
}

func TestSummarizeDateCountsSecurityFindings(t *testing.T) {
	tmp := t.TempDir()
	agentDir := filepath.Join(tmp, "audit", "researcher")
	os.MkdirAll(agentDir, 0755)
	lines := []string{
		`{"ts":"2026-03-27T10:00:00Z","type":"LLM_DIRECT","agent":"researcher","model":"standard","event_id":"evt-001","input_tokens":1000,"output_tokens":500}`,
		`{"ts":"2026-03-27T10:01:00Z","event":"security_scan_flagged","agent":"researcher","event_id":"scan-001","finding_count":3}`,
		`{"ts":"2026-03-27T10:02:00Z","event":"agent_signal_finding","agent":"researcher","event_id":"finding-001"}`,
	}
	f, _ := os.Create(filepath.Join(agentDir, "enforcer-2026-03-27.jsonl"))
	for _, l := range lines {
		f.WriteString(l + "\n")
	}
	f.Close()

	s := &AuditSummarizer{homeDir: tmp, missionMap: map[string]string{"researcher": "soc-triage"}}
	metrics, err := s.summarizeDate("2026-03-27")
	if err != nil {
		t.Fatalf("summarizeDate: %v", err)
	}
	if len(metrics) != 1 {
		t.Fatalf("got %d metrics want 1", len(metrics))
	}
	if metrics[0].FindingsCount == nil {
		t.Fatal("FindingsCount is nil")
	}
	if *metrics[0].FindingsCount != 4 {
		t.Errorf("FindingsCount=%d want 4", *metrics[0].FindingsCount)
	}
}

func TestSummarizeUpsertsToKnowledge(t *testing.T) {
	tmp := t.TempDir()
	agentDir := filepath.Join(tmp, "audit", "researcher")
	os.MkdirAll(agentDir, 0755)
	lines := []string{
		`{"ts":"2026-03-27T10:00:00Z","type":"LLM_DIRECT","agent":"researcher","model":"standard","event_id":"evt-001","input_tokens":1000,"output_tokens":500}`,
	}
	today := "2026-03-27"
	f, _ := os.Create(filepath.Join(agentDir, "enforcer-"+today+".jsonl"))
	for _, l := range lines {
		f.WriteString(l + "\n")
	}
	f.Close()

	// Mission YAML
	missionDir := filepath.Join(tmp, "missions")
	os.MkdirAll(missionDir, 0755)
	os.WriteFile(filepath.Join(missionDir, "soc-triage.yaml"), []byte("name: soc-triage\nagents:\n  - researcher\n"), 0644)

	// Mock knowledge server
	var receivedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(200)
		w.Write([]byte(`{"count": 1}`))
	}))
	defer srv.Close()

	logger := slog.Default()
	s := NewAuditSummarizer(tmp, srv.URL, logger)
	// Override mission map directly for the test date
	s.missionMap = map[string]string{"researcher": "soc-triage"}

	metrics := []MissionMetric{{
		Mission: "soc-triage", Date: today, Activations: 1,
		TotalInputTokens: 1000, TotalOutputTokens: 500,
		EstimatedCostUSD: 0.006, Model: "standard",
	}}
	s.upsertMetricsToKnowledge(metrics)

	if len(receivedBody) == 0 {
		t.Fatal("expected knowledge server to receive a POST")
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(receivedBody, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	nodes, ok := payload["nodes"].([]interface{})
	if !ok || len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %v", payload)
	}
	node := nodes[0].(map[string]interface{})
	if node["kind"] != "MissionMetrics" {
		t.Errorf("kind=%v want MissionMetrics", node["kind"])
	}
	if node["label"] != "soc-triage:2026-03-27" {
		t.Errorf("label=%v want soc-triage:2026-03-27", node["label"])
	}
	if node["source_type"] != "rule" {
		t.Errorf("source_type=%v want rule", node["source_type"])
	}
	props := node["properties"].(map[string]interface{})
	if props["mission"] != "soc-triage" {
		t.Errorf("properties.mission=%v want soc-triage", props["mission"])
	}
	if props["findings_count"] != float64(0) {
		t.Errorf("properties.findings_count=%v want 0", props["findings_count"])
	}
}
