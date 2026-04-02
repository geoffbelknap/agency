package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// AuditEntry represents a single logged fetch operation.
type AuditEntry struct {
	Timestamp      time.Time `json:"ts"`
	Agent          string    `json:"agent,omitempty"`
	URL            string    `json:"url"`
	FinalURL       string    `json:"final_url,omitempty"`
	Method         string    `json:"method"`
	Status         int       `json:"status_code"`
	ContentType    string    `json:"content_type,omitempty"`
	RawBytes       int64     `json:"raw_bytes"`
	ExtractedBytes int64     `json:"extracted_bytes"`
	Cached         bool      `json:"cached"`
	Blocked        bool      `json:"blocked"`
	BlockReason    string    `json:"block_reason,omitempty"`
	XPIAFlags      []string  `json:"xpia_flags,omitempty"`
	DNSHit         bool      `json:"dns_blocklist_hit"`
	Duration       int64     `json:"duration_ms"`
	RequestID      string    `json:"request_id,omitempty"`
	HMAC           string    `json:"hmac"`
}

// AuditLogger writes HMAC-signed audit entries to date-rotated JSONL files.
type AuditLogger struct {
	dir      string
	hmacKey  []byte
	mu       sync.Mutex
	file     *os.File
	date     string
}

// NewAuditLogger creates an audit logger with the given directory and HMAC key.
func NewAuditLogger(dir, hmacKey string) (*AuditLogger, error) {
	// Ensure directory exists - don't fail if already exists
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create audit dir %s: %w", dir, err)
	}

	logger := &AuditLogger{
		dir:     dir,
		hmacKey: []byte(hmacKey),
		date:    "", // Will be set by ensureFile
	}

	if err := logger.ensureFile(); err != nil {
		return nil, err
	}

	return logger, nil
}

// ensureFile opens or rotates the audit log file based on current date.
func (al *AuditLogger) ensureFile() error {
	now := time.Now()
	dateStr := now.Format("2006-01-02")

	// If file is already open for today, no action needed
	if al.file != nil && al.date == dateStr {
		return nil
	}

	// Close existing file if open
	if al.file != nil {
		al.file.Close()
	}

	// Ensure the directory exists (in case it was deleted or permissions changed)
	if err := os.MkdirAll(al.dir, 0755); err != nil {
		return fmt.Errorf("failed to ensure audit dir %s: %w", al.dir, err)
	}

	// Open or create the date-rotated file
	filename := filepath.Join(al.dir, fmt.Sprintf("web-fetch-%s.jsonl", dateStr))
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open audit file %s: %w", filename, err)
	}

	al.file = f
	al.date = dateStr
	return nil
}

// Log writes an audit entry to the log file.
func (al *AuditLogger) Log(entry *AuditEntry) error {
	al.mu.Lock()
	defer al.mu.Unlock()

	// Set timestamp if not already set
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}

	// Compute HMAC over the entry (before adding the HMAC field itself)
	// Marshal entry to JSON for HMAC computation
	entryForHMAC := *entry
	entryForHMAC.HMAC = ""
	jsonBytes, err := json.Marshal(entryForHMAC)
	if err != nil {
		return fmt.Errorf("failed to marshal entry for HMAC: %w", err)
	}

	// Compute HMAC-SHA256
	h := hmac.New(sha256.New, al.hmacKey)
	h.Write(jsonBytes)
	entry.HMAC = hex.EncodeToString(h.Sum(nil))

	// Ensure file is rotated if necessary
	if err := al.ensureFile(); err != nil {
		return err
	}

	// Marshal the complete entry (with HMAC)
	jsonWithHMAC, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal entry: %w", err)
	}

	// Write JSON line
	if _, err := al.file.Write(append(jsonWithHMAC, '\n')); err != nil {
		return fmt.Errorf("failed to write audit entry: %w", err)
	}

	return nil
}

// Flush syncs the file to disk.
func (al *AuditLogger) Flush() error {
	al.mu.Lock()
	defer al.mu.Unlock()

	if al.file == nil {
		return nil
	}

	return al.file.Sync()
}

// Close closes the audit log file.
func (al *AuditLogger) Close() error {
	al.mu.Lock()
	defer al.mu.Unlock()

	if al.file == nil {
		return nil
	}

	err := al.file.Close()
	al.file = nil
	return err
}
