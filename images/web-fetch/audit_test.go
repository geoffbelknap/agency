package main

import (
	"bufio"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAuditLogger_WritesJSONL(t *testing.T) {
	// Create a temporary directory for the audit logs
	tmpDir := t.TempDir()
	hmacKey := "test-secret-key-12345"

	logger, err := NewAuditLogger(tmpDir, hmacKey)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	defer logger.Close()

	// Create a test entry with today's timestamp
	now := time.Now()
	entry := &AuditEntry{
		Timestamp:      now,
		Agent:          "test-agent",
		URL:            "https://example.com/test",
		FinalURL:       "https://example.com/test",
		Method:         "GET",
		Status:         200,
		ContentType:    "text/html",
		RawBytes:       5000,
		ExtractedBytes: 1000,
		Cached:         false,
		Blocked:        false,
		DNSHit:         false,
		Duration:       250,
		RequestID:      "req-12345",
	}

	// Log the entry
	if err := logger.Log(entry); err != nil {
		t.Fatalf("failed to log entry: %v", err)
	}

	// Flush to disk
	if err := logger.Flush(); err != nil {
		t.Fatalf("failed to flush: %v", err)
	}

	// Read back the file and verify
	dateStr := now.Format("2006-01-02")
	expectedFilename := filepath.Join(tmpDir, fmt.Sprintf("web-fetch-%s.jsonl", dateStr))
	file, err := os.Open(expectedFilename)
	if err != nil {
		t.Fatalf("failed to open audit file: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		t.Fatal("no lines in audit file")
	}

	var loggedEntry AuditEntry
	if err := json.Unmarshal(scanner.Bytes(), &loggedEntry); err != nil {
		t.Fatalf("failed to unmarshal logged entry: %v", err)
	}

	// Verify basic fields
	if loggedEntry.Agent != "test-agent" {
		t.Errorf("expected agent='test-agent', got '%s'", loggedEntry.Agent)
	}
	if loggedEntry.URL != "https://example.com/test" {
		t.Errorf("expected url='https://example.com/test', got '%s'", loggedEntry.URL)
	}
	if loggedEntry.Method != "GET" {
		t.Errorf("expected method='GET', got '%s'", loggedEntry.Method)
	}
	if loggedEntry.Status != 200 {
		t.Errorf("expected status=200, got %d", loggedEntry.Status)
	}
	if loggedEntry.RawBytes != 5000 {
		t.Errorf("expected raw_bytes=5000, got %d", loggedEntry.RawBytes)
	}
	if loggedEntry.ExtractedBytes != 1000 {
		t.Errorf("expected extracted_bytes=1000, got %d", loggedEntry.ExtractedBytes)
	}

	// Verify HMAC is present and valid
	if loggedEntry.HMAC == "" {
		t.Fatal("expected HMAC to be set")
	}

	// Verify HMAC correctness by recomputing it
	entryForVerify := loggedEntry
	entryForVerify.HMAC = ""
	jsonBytes, err := json.Marshal(entryForVerify)
	if err != nil {
		t.Fatalf("failed to marshal for verification: %v", err)
	}

	h := hmac.New(sha256.New, []byte(hmacKey))
	h.Write(jsonBytes)
	expectedHMAC := hex.EncodeToString(h.Sum(nil))

	if loggedEntry.HMAC != expectedHMAC {
		t.Errorf("HMAC mismatch: got '%s', expected '%s'", loggedEntry.HMAC, expectedHMAC)
	}
}

func TestAuditLogger_BlockedEntry(t *testing.T) {
	tmpDir := t.TempDir()
	hmacKey := "test-secret-key-12345"

	logger, err := NewAuditLogger(tmpDir, hmacKey)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	defer logger.Close()

	// Create a blocked entry with today's timestamp
	now := time.Now()
	entry := &AuditEntry{
		Timestamp:      now,
		Agent:          "test-agent",
		URL:            "https://evil.onion/malware",
		Method:         "GET",
		Status:         0,
		RawBytes:       0,
		ExtractedBytes: 0,
		Cached:         false,
		Blocked:        true,
		BlockReason:    "blocklist_match",
		DNSHit:         true,
		Duration:       10,
	}

	// Log the entry
	if err := logger.Log(entry); err != nil {
		t.Fatalf("failed to log entry: %v", err)
	}

	// Flush to disk
	if err := logger.Flush(); err != nil {
		t.Fatalf("failed to flush: %v", err)
	}

	// Read back and verify
	dateStr := now.Format("2006-01-02")
	expectedFilename := filepath.Join(tmpDir, fmt.Sprintf("web-fetch-%s.jsonl", dateStr))
	file, err := os.Open(expectedFilename)
	if err != nil {
		t.Fatalf("failed to open audit file: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		t.Fatal("no lines in audit file")
	}

	var loggedEntry AuditEntry
	if err := json.Unmarshal(scanner.Bytes(), &loggedEntry); err != nil {
		t.Fatalf("failed to unmarshal logged entry: %v", err)
	}

	// Verify blocked fields
	if !loggedEntry.Blocked {
		t.Error("expected blocked=true")
	}
	if loggedEntry.BlockReason != "blocklist_match" {
		t.Errorf("expected block_reason='blocklist_match', got '%s'", loggedEntry.BlockReason)
	}
	if !loggedEntry.DNSHit {
		t.Error("expected dns_blocklist_hit=true")
	}

	// Verify HMAC is still present
	if loggedEntry.HMAC == "" {
		t.Fatal("expected HMAC to be set")
	}
}
