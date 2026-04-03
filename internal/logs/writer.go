package logs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Writer appends JSONL audit events to per-agent log files.
// Events are written by the gateway (mediation layer), not by agents.
// This satisfies ASK tenet 2: every action leaves a trace.
type Writer struct {
	Home       string
	mu         sync.Mutex
	lifecycles map[string]string // agent name → lifecycle_id
}

// NewWriter creates a new audit log writer rooted at the agency home directory.
func NewWriter(home string) *Writer {
	return &Writer{Home: home}
}

// SetLifecycleID registers a lifecycle_id for an agent so it is injected into
// all subsequent audit events written for that agent.
func (w *Writer) SetLifecycleID(agent, id string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.lifecycles == nil {
		w.lifecycles = map[string]string{}
	}
	w.lifecycles[agent] = id
}

// Write appends an audit event to the agent's gateway.jsonl file.
func (w *Writer) Write(agent, event string, detail map[string]interface{}) (err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	auditDir := filepath.Join(w.Home, "audit", agent)
	if err := os.MkdirAll(auditDir, 0700); err != nil {
		return fmt.Errorf("create audit dir: %w", err)
	}

	entry := map[string]interface{}{
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"source":    "gateway",
		"event":     event,
		"agent":     agent,
	}
	if id, ok := w.lifecycles[agent]; ok && id != "" {
		entry["lifecycle_id"] = id
	}
	for k, v := range detail {
		entry[k] = v
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	f, err := os.OpenFile(
		filepath.Join(auditDir, "gateway.jsonl"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600,
	)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	_, err = f.Write(data)
	return err
}

// WriteSystem appends an audit event to the system-level log (not agent-specific).
func (w *Writer) WriteSystem(event string, detail map[string]interface{}) (err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	auditDir := filepath.Join(w.Home, "audit", "system")
	if err := os.MkdirAll(auditDir, 0700); err != nil {
		return fmt.Errorf("create audit dir: %w", err)
	}

	entry := map[string]interface{}{
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"source":    "gateway",
		"event":     event,
	}
	for k, v := range detail {
		entry[k] = v
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	f, err := os.OpenFile(
		filepath.Join(auditDir, "gateway.jsonl"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600,
	)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	_, err = f.Write(data)
	return err
}
