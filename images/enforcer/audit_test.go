package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAuditLogEntryFields(t *testing.T) {
	dir := t.TempDir()
	al := NewAuditLogger(dir, "test-agent")

	al.Log(AuditEntry{
		Type:   "HTTP_PROXY",
		Method: "GET",
		URL:    "https://example.com",
		Status: 200,
	})

	al.Close()

	today := time.Now().UTC().Format("2006-01-02")
	data, err := os.ReadFile(filepath.Join(dir, "enforcer-"+today+".jsonl"))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	var entry AuditEntry
	if err := json.Unmarshal(data[:len(data)-1], &entry); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if entry.Type != "HTTP_PROXY" {
		t.Errorf("wrong type: %s", entry.Type)
	}
	if entry.Agent != "test-agent" {
		t.Errorf("wrong agent: %s", entry.Agent)
	}
	if entry.Timestamp == "" {
		t.Error("timestamp should be set")
	}
	if entry.Method != "GET" {
		t.Errorf("wrong method: %s", entry.Method)
	}
	if entry.Status != 200 {
		t.Errorf("wrong status: %d", entry.Status)
	}
}

func TestAuditFlushWritesJSONL(t *testing.T) {
	dir := t.TempDir()
	al := NewAuditLogger(dir, "test-agent")

	for i := 0; i < 5; i++ {
		al.Log(AuditEntry{
			Type: "HTTP_PROXY",
			URL:  "https://example.com",
		})
	}

	al.Close()

	today := time.Now().UTC().Format("2006-01-02")
	data, err := os.ReadFile(filepath.Join(dir, "enforcer-"+today+".jsonl"))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 5 {
		t.Errorf("expected 5 lines, got %d", len(lines))
	}

	// Each line should be valid JSON
	for i, line := range lines {
		var entry AuditEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Errorf("line %d invalid JSON: %v", i, err)
		}
	}
}

func TestAuditConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	al := NewAuditLogger(dir, "test-agent")

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				al.Log(AuditEntry{
					Type: "HTTP_PROXY",
					URL:  "https://example.com",
				})
			}
		}()
	}
	wg.Wait()
	al.Close()

	today := time.Now().UTC().Format("2006-01-02")
	data, err := os.ReadFile(filepath.Join(dir, "enforcer-"+today+".jsonl"))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 100 {
		t.Errorf("expected 100 lines, got %d", len(lines))
	}
}

func TestAuditBatchFlush(t *testing.T) {
	dir := t.TempDir()
	al := NewAuditLogger(dir, "test-agent")

	// Write more than batch size (50) to trigger immediate flush
	for i := 0; i < 60; i++ {
		al.Log(AuditEntry{
			Type: "HTTP_PROXY",
			URL:  "https://example.com",
		})
	}

	al.Close()

	today := time.Now().UTC().Format("2006-01-02")
	data, err := os.ReadFile(filepath.Join(dir, "enforcer-"+today+".jsonl"))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 60 {
		t.Errorf("expected 60 lines, got %d", len(lines))
	}
}

func TestAuditLLMEntry(t *testing.T) {
	dir := t.TempDir()
	al := NewAuditLogger(dir, "test-agent")

	al.Log(AuditEntry{
		Type:          "LLM_DIRECT",
		Model:         "standard",
		ProviderModel: "provider-a-standard",
		Status:        200,
		CorrelationID: "test-corr-123",
		InputTokens:   100,
		OutputTokens:  500,
		DurationMs:    1234,
	})

	al.Close()

	today := time.Now().UTC().Format("2006-01-02")
	data, err := os.ReadFile(filepath.Join(dir, "enforcer-"+today+".jsonl"))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	var entry AuditEntry
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &entry); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if entry.Type != "LLM_DIRECT" {
		t.Errorf("wrong type: %s", entry.Type)
	}
	if entry.Model != "standard" {
		t.Errorf("wrong model: %s", entry.Model)
	}
	if entry.CorrelationID != "test-corr-123" {
		t.Errorf("wrong correlation_id: %s", entry.CorrelationID)
	}
	if entry.InputTokens != 100 {
		t.Errorf("wrong input_tokens: %d", entry.InputTokens)
	}
}

func TestAuditEntry_EconomicsFields(t *testing.T) {
	valid := true
	entry := AuditEntry{
		Type:          "LLM_DIRECT_STREAM",
		TTFTMs:        380,
		TPOTMs:        28.5,
		ContextTokens: 4200,
		StepIndex:     3,
		ToolCallValid: &valid,
		RetryOf:       "corr-123",
	}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]interface{}
	json.Unmarshal(data, &parsed)

	if parsed["ttft_ms"] != float64(380) { t.Errorf("ttft_ms: %v", parsed["ttft_ms"]) }
	if parsed["step_index"] != float64(3) { t.Errorf("step_index: %v", parsed["step_index"]) }
	if parsed["tool_call_valid"] != true { t.Errorf("tool_call_valid: %v", parsed["tool_call_valid"]) }
	if parsed["retry_of"] != "corr-123" { t.Errorf("retry_of: %v", parsed["retry_of"]) }
}
