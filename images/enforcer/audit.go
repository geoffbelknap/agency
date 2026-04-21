package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
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
	Timestamp                string            `json:"ts"`
	Type                     string            `json:"type"`
	Agent                    string            `json:"agent,omitempty"`
	LifecycleID              string            `json:"lifecycle_id,omitempty"`
	Method                   string            `json:"method,omitempty"`
	URL                      string            `json:"url,omitempty"`
	Host                     string            `json:"host,omitempty"`
	Status                   int               `json:"status,omitempty"`
	Model                    string            `json:"model,omitempty"`
	ProviderModel            string            `json:"provider_model,omitempty"`
	CorrelationID            string            `json:"correlation_id,omitempty"`
	Service                  string            `json:"service,omitempty"`
	EventID                  string            `json:"event_id,omitempty"`
	Error                    string            `json:"error,omitempty"`
	DurationMs               int64             `json:"duration_ms,omitempty"`
	ScanType                 string            `json:"scan_type,omitempty"`
	ScanSurface              string            `json:"scan_surface,omitempty"`
	ScanAction               string            `json:"scan_action,omitempty"`
	ScanMode                 string            `json:"scan_mode,omitempty"`
	FindingCount             *int              `json:"finding_count,omitempty"`
	Findings                 []string          `json:"findings,omitempty"`
	ContentSHA256            string            `json:"content_sha256,omitempty"`
	ContentBytes             int               `json:"content_bytes,omitempty"`
	ContentCount             int               `json:"content_count,omitempty"`
	InputTokens              int               `json:"input_tokens,omitempty"`
	OutputTokens             int               `json:"output_tokens,omitempty"`
	CachedTokens             int               `json:"cached_tokens,omitempty"`
	CacheCreationInputTokens int               `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int               `json:"cache_read_input_tokens,omitempty"`
	TTFTMs                   int64             `json:"ttft_ms,omitempty"`
	TPOTMs                   float64           `json:"tpot_ms,omitempty"`
	ContextTokens            int64             `json:"context_tokens,omitempty"`
	StepIndex                int               `json:"step_index,omitempty"`
	ToolCallValid            *bool             `json:"tool_call_valid,omitempty"`
	RetryOf                  string            `json:"retry_of,omitempty"`
	Sig                      string            `json:"sig,omitempty"`
	Extra                    map[string]string `json:"extra,omitempty"`
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
			slog.Warn("audit: invalid ENFORCER_AUDIT_HMAC_KEY (must be hex)", "error", err)
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
		slog.Warn("audit: dropped entry (buffer full)")
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
					if err := al.file.Close(); err != nil {
						slog.Warn("audit: close error", "error", err)
					}
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
			if err := al.file.Close(); err != nil {
				slog.Warn("audit: close error", "error", err)
			}
		}
		if err := os.MkdirAll(al.dir, 0755); err != nil {
			slog.Warn("audit: mkdir error", "error", err)
			return
		}
		filename := filepath.Join(al.dir, fmt.Sprintf("enforcer-%s.jsonl", today))
		f, err := os.OpenFile(filename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			slog.Warn("audit: open error", "error", err)
			return
		}
		al.file = f
		al.fileDate = today
	}

	for _, entry := range entries {
		data, err := json.Marshal(entry)
		if err != nil {
			slog.Warn("audit: marshal error", "error", err)
			continue
		}
		data = append(data, '\n')
		if _, err := al.file.Write(data); err != nil {
			slog.Warn("audit: write error", "error", err)
		}
	}
	al.file.Sync()
}
