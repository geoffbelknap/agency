package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	auditFlushInterval = 2 * time.Second
	auditBatchSize     = 50
)

// AuditEntry represents a single audit log entry.
type AuditEntry struct {
	Timestamp     string `json:"ts"`
	Type          string `json:"type"`
	Agent         string `json:"agent,omitempty"`
	LifecycleID   string `json:"lifecycle_id,omitempty"`
	Method        string `json:"method,omitempty"`
	URL           string `json:"url,omitempty"`
	Host          string `json:"host,omitempty"`
	Status        int    `json:"status,omitempty"`
	Model         string `json:"model,omitempty"`
	ProviderModel string `json:"provider_model,omitempty"`
	CorrelationID string `json:"correlation_id,omitempty"`
	Service       string `json:"service,omitempty"`
	EventID       string `json:"event_id,omitempty"`
	Error         string `json:"error,omitempty"`
	DurationMs    int64  `json:"duration_ms,omitempty"`
	InputTokens   int    `json:"input_tokens,omitempty"`
	OutputTokens  int    `json:"output_tokens,omitempty"`
	Sig           string            `json:"sig,omitempty"`
	Extra         map[string]string `json:"extra,omitempty"`
}

// AuditLogger is an async buffered JSONL logger that writes audit entries
// to date-rotated files.
type AuditLogger struct {
	dir         string
	agent       string
	lifecycleID string
	hmacKey     []byte

	mu       sync.Mutex
	file     *os.File
	fileDate string
	ch       chan AuditEntry
	done     chan struct{}
}

// NewAuditLogger creates and starts an audit logger.
func NewAuditLogger(dir string, agent string) *AuditLogger {
	al := &AuditLogger{
		dir:   dir,
		agent: agent,
		ch:    make(chan AuditEntry, 256),
		done:  make(chan struct{}),
	}
	if keyHex := os.Getenv("ENFORCER_AUDIT_HMAC_KEY"); keyHex != "" {
		key, err := hex.DecodeString(keyHex)
		if err != nil {
			log.Printf("audit: invalid ENFORCER_AUDIT_HMAC_KEY (must be hex): %v", err)
		} else {
			al.hmacKey = key
		}
	}
	go al.run()
	return al
}

// SetLifecycleID sets the lifecycle ID to be injected into every audit entry.
func (al *AuditLogger) SetLifecycleID(id string) {
	al.lifecycleID = id
}

// Log queues an audit entry for writing.
func (al *AuditLogger) Log(entry AuditEntry) {
	if entry.Timestamp == "" {
		entry.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if entry.Agent == "" {
		entry.Agent = al.agent
	}
	if entry.LifecycleID == "" && al.lifecycleID != "" {
		entry.LifecycleID = al.lifecycleID
	}
	if len(al.hmacKey) > 0 {
		entryJSON, _ := json.Marshal(entry)
		mac := hmac.New(sha256.New, al.hmacKey)
		mac.Write(entryJSON)
		entry.Sig = hex.EncodeToString(mac.Sum(nil))
	}
	select {
	case al.ch <- entry:
	default:
		// Channel full, drop entry (should not happen in practice)
		log.Printf("audit: dropped entry (buffer full)")
	}
}

// Close flushes remaining entries and closes the logger.
func (al *AuditLogger) Close() {
	close(al.ch)
	<-al.done
}

// run is the background goroutine that batches writes.
func (al *AuditLogger) run() {
	defer close(al.done)

	ticker := time.NewTicker(auditFlushInterval)
	defer ticker.Stop()

	var batch []AuditEntry

	for {
		select {
		case entry, ok := <-al.ch:
			if !ok {
				// Channel closed, flush remaining
				if len(batch) > 0 {
					al.flush(batch)
				}
				al.mu.Lock()
				if al.file != nil {
					al.file.Close()
				}
				al.mu.Unlock()
				return
			}
			batch = append(batch, entry)
			if len(batch) >= auditBatchSize {
				al.flush(batch)
				batch = nil
			}
		case <-ticker.C:
			if len(batch) > 0 {
				al.flush(batch)
				batch = nil
			}
		}
	}
}

// flush writes a batch of entries to the current log file.
func (al *AuditLogger) flush(entries []AuditEntry) {
	al.mu.Lock()
	defer al.mu.Unlock()

	today := time.Now().UTC().Format("2006-01-02")
	if al.file == nil || al.fileDate != today {
		if al.file != nil {
			al.file.Close()
		}
		if err := os.MkdirAll(al.dir, 0755); err != nil {
			log.Printf("audit: mkdir error: %v", err)
			return
		}
		filename := filepath.Join(al.dir, fmt.Sprintf("enforcer-%s.jsonl", today))
		f, err := os.OpenFile(filename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			log.Printf("audit: open error: %v", err)
			return
		}
		al.file = f
		al.fileDate = today
	}

	for _, entry := range entries {
		data, err := json.Marshal(entry)
		if err != nil {
			log.Printf("audit: marshal error: %v", err)
			continue
		}
		data = append(data, '\n')
		if _, err := al.file.Write(data); err != nil {
			log.Printf("audit: write error: %v", err)
		}
	}
	al.file.Sync()
}
