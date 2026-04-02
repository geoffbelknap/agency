package context

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/geoffbelknap/agency/internal/logs"
)

// agentState holds per-agent constraint tracking data.
type agentState struct {
	version     int
	constraints map[string]interface{}
	latest      *ConstraintChange
	history     []ConstraintChange
}

// Manager tracks constraint pushes, ack state, and change history per agent.
// It satisfies ASK tenets 6 and 7: atomic constraint changes and immutable history.
type Manager struct {
	mu        sync.RWMutex
	agents    map[string]*agentState
	audit     *logs.Writer
	alertFunc func(agent, message string)

	wsMu      sync.RWMutex
	wsClients map[string]*WSClient                          // agent → WS client
	haltFunc  func(agent, changeID, reason string) error    // injected halt callback
}

// NewManager creates a new Manager. audit may be nil (e.g. in tests).
func NewManager(audit *logs.Writer) *Manager {
	return &Manager{
		agents:    make(map[string]*agentState),
		audit:     audit,
		wsClients: make(map[string]*WSClient),
	}
}

// getOrCreate returns existing agentState or allocates a new one (caller must hold write lock).
func (m *Manager) getOrCreate(agent string) *agentState {
	as, ok := m.agents[agent]
	if !ok {
		as = &agentState{
			constraints: make(map[string]interface{}),
		}
		m.agents[agent] = as
	}
	return as
}

// Push creates a new ConstraintChange for agent, auto-classifies severity,
// applies any override escalation, computes the body hash, increments the
// version counter, appends to history, and writes an audit log entry.
func (m *Manager) Push(
	agent string,
	constraints map[string]interface{},
	severityOverride, reason, initiator string,
) (*ConstraintChange, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	as := m.getOrCreate(agent)

	// Classify and optionally escalate severity.
	auto := ClassifySeverity(as.constraints, constraints)
	sev, err := ApplyEscalation(auto, severityOverride)
	if err != nil {
		return nil, fmt.Errorf("severity escalation: %w", err)
	}

	as.version++
	hash := HashConstraints(constraints)

	change := &ConstraintChange{
		ChangeID:    uuid.New().String(),
		Agent:       agent,
		Version:     as.version,
		Severity:    sev,
		Constraints: constraints,
		Hash:        hash,
		Reason:      reason,
		Initiator:   initiator,
		Timestamp:   time.Now().UTC(),
		Status:      StatusPending,
	}

	as.latest = change
	as.history = append(as.history, *change)

	// Write audit log (ASK tenet 2: every action leaves a trace).
	if m.audit != nil {
		_ = m.audit.Write(agent, "constraint_push", map[string]interface{}{
			"change_id": change.ChangeID,
			"version":   change.Version,
			"severity":  sev.String(),
			"reason":    reason,
			"initiator": initiator,
			"hash":      hash,
		})
	}

	copy := *change
	return &copy, nil
}

// Ack verifies the agent's hash and marks the latest change as acked (or hash_mismatch).
// Returns an error on hash mismatch (status is still updated to hash_mismatch).
func (m *Manager) Ack(agent, changeID string, version int, bodyHash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	as, ok := m.agents[agent]
	if !ok || as.latest == nil {
		return fmt.Errorf("no pending change for agent %q", agent)
	}

	if as.latest.ChangeID != changeID {
		return fmt.Errorf("change_id mismatch: got %q, want %q", changeID, as.latest.ChangeID)
	}

	if as.latest.Hash != bodyHash {
		as.latest.Status = StatusHashMismatch
		// Propagate status update into history.
		m.syncLatestToHistory(as)

		if m.audit != nil {
			_ = m.audit.Write(agent, "constraint_ack_hash_mismatch", map[string]interface{}{
				"change_id":     changeID,
				"version":       version,
				"expected_hash": as.latest.Hash,
				"received_hash": bodyHash,
			})
		}
		return fmt.Errorf("hash mismatch: expected %q got %q", as.latest.Hash, bodyHash)
	}

	now := time.Now().UTC()
	as.latest.Status = StatusAcked
	as.latest.AckedAt = &now
	m.syncLatestToHistory(as)

	// Update current constraints to newly acked set.
	as.constraints = as.latest.Constraints

	if m.audit != nil {
		_ = m.audit.Write(agent, "constraint_ack", map[string]interface{}{
			"change_id": changeID,
			"version":   version,
		})
	}

	return nil
}

// syncLatestToHistory updates the last history entry to match as.latest.
// Called after any status mutation so history stays consistent.
func (m *Manager) syncLatestToHistory(as *agentState) {
	if len(as.history) == 0 {
		return
	}
	as.history[len(as.history)-1] = *as.latest
}

// MarkTimeout marks the latest change for agent as timed out.
func (m *Manager) MarkTimeout(agent, changeID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	as, ok := m.agents[agent]
	if !ok || as.latest == nil || as.latest.ChangeID != changeID {
		return
	}
	as.latest.Status = StatusTimeout
	m.syncLatestToHistory(as)

	if m.audit != nil {
		_ = m.audit.Write(agent, "constraint_ack_timeout", map[string]interface{}{
			"change_id": changeID,
		})
	}
}

// MarkHalted marks the latest change for agent as halted.
func (m *Manager) MarkHalted(agent, changeID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	as, ok := m.agents[agent]
	if !ok || as.latest == nil || as.latest.ChangeID != changeID {
		return
	}
	as.latest.Status = StatusHalted
	m.syncLatestToHistory(as)

	if m.audit != nil {
		_ = m.audit.Write(agent, "constraint_halted", map[string]interface{}{
			"change_id": changeID,
		})
	}
}

// GetStatus returns a copy of the latest ConstraintChange for agent, or nil if none.
func (m *Manager) GetStatus(agent string) *ConstraintChange {
	m.mu.RLock()
	defer m.mu.RUnlock()

	as, ok := m.agents[agent]
	if !ok || as.latest == nil {
		return nil
	}
	copy := *as.latest
	return &copy
}

// Changes returns a copy of the full change history for agent.
func (m *Manager) Changes(agent string) []ConstraintChange {
	m.mu.RLock()
	defer m.mu.RUnlock()

	as, ok := m.agents[agent]
	if !ok {
		return nil
	}
	result := make([]ConstraintChange, len(as.history))
	copy(result, as.history)
	return result
}

// CurrentConstraints returns a shallow copy of the current (acked) constraints for agent.
func (m *Manager) CurrentConstraints(agent string) map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	as, ok := m.agents[agent]
	if !ok {
		return nil
	}
	out := make(map[string]interface{}, len(as.constraints))
	for k, v := range as.constraints {
		out[k] = v
	}
	return out
}

// SetInitialConstraints sets the baseline constraints for an agent at startup,
// before any push/ack cycle has occurred.
func (m *Manager) SetInitialConstraints(agent string, constraints map[string]interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()

	as := m.getOrCreate(agent)
	as.constraints = constraints
}

// RemoveAgent removes all state for agent (e.g. after agent teardown).
func (m *Manager) RemoveAgent(agent string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.agents, agent)
}

// SetAlertFunc registers a callback invoked when enforcer connectivity events occur.
func (m *Manager) SetAlertFunc(fn func(agent, message string)) {
	m.alertFunc = fn
}

// HandleEnforcerDisconnect audits and alerts when the enforcer WebSocket for agent disconnects.
// Per ASK tenet 2 every event is logged; per tenet 10 authority-level events are monitored.
func (m *Manager) HandleEnforcerDisconnect(agent string) {
	msg := fmt.Sprintf("Enforcer for agent %s unreachable — constraint delivery unavailable", agent)
	if m.audit != nil {
		m.audit.Write(agent, "enforcer_ws_disconnected", map[string]interface{}{
			"message": msg,
		})
	}
	if m.alertFunc != nil {
		m.alertFunc(agent, msg)
	}
}

// HandleEnforcerReconnect audits when the enforcer WebSocket for agent reconnects.
func (m *Manager) HandleEnforcerReconnect(agent string) {
	if m.audit != nil {
		m.audit.Write(agent, "enforcer_ws_reconnected", map[string]interface{}{})
	}
}

// RegisterWSClient associates a WebSocket client with an agent for constraint delivery.
func (m *Manager) RegisterWSClient(agent string, client *WSClient) {
	m.wsMu.Lock()
	defer m.wsMu.Unlock()
	m.wsClients[agent] = client
}

// UnregisterWSClient removes the WebSocket client for an agent.
func (m *Manager) UnregisterWSClient(agent string) {
	m.wsMu.Lock()
	defer m.wsMu.Unlock()
	delete(m.wsClients, agent)
}

// SetHaltFunc registers a callback invoked when an agent must be halted due to
// unacknowledged constraint changes. ASK tenet 6: unacknowledged changes are
// treated as potential compromise.
func (m *Manager) SetHaltFunc(fn func(agent, changeID, reason string) error) {
	m.haltFunc = fn
}

// DeliverAsync initiates asynchronous two-stage delivery of a constraint change
// to the agent's enforcer via WebSocket. Stage 1 waits the ack timeout, alerting
// the operator on failure. Stage 2 waits the remaining halt timeout, then auto-halts.
//
// ASK tenet 6: constraint changes are atomic and acknowledged.
// ASK tenet 8: halts are always auditable and reversible.
func (m *Manager) DeliverAsync(change *ConstraintChange) {
	go m.deliverAsync(change)
}

func (m *Manager) deliverAsync(change *ConstraintChange) {
	m.wsMu.RLock()
	client := m.wsClients[change.Agent]
	m.wsMu.RUnlock()

	if client == nil {
		if m.audit != nil {
			_ = m.audit.Write(change.Agent, "constraint_delivery_failed", map[string]interface{}{
				"change_id": change.ChangeID,
				"reason":    "no websocket client registered",
			})
		}
		return
	}

	// Stage 1: Push with ack timeout (alert stage).
	ack, err := client.Push(change, change.Severity.AckTimeout())
	if err != nil {
		// Alert operator that ack window expired.
		if m.alertFunc != nil {
			m.alertFunc(change.Agent, fmt.Sprintf(
				"Constraint change %s (severity %s) not acknowledged within %s",
				change.ChangeID, change.Severity, change.Severity.AckTimeout()))
		}

		if m.audit != nil {
			_ = m.audit.Write(change.Agent, "constraint_ack_alert", map[string]interface{}{
				"change_id": change.ChangeID,
				"severity":  change.Severity.String(),
				"stage":     "ack_timeout",
			})
		}

		// Stage 2: Wait remaining time until halt timeout.
		remaining := change.Severity.HaltTimeout() - change.Severity.AckTimeout()
		if remaining <= 0 {
			remaining = change.Severity.AckTimeout()
		}

		ack, err = client.Push(change, remaining)
		if err != nil {
			// Auto-halt: tenet 6 — unacknowledged constraint changes trigger halt.
			m.MarkTimeout(change.Agent, change.ChangeID)
			if m.haltFunc != nil {
				if haltErr := m.haltFunc(change.Agent, change.ChangeID, "unacked constraint change"); haltErr == nil {
					m.MarkHalted(change.Agent, change.ChangeID)
				}
			}
			return
		}
	}

	// Process successful ack.
	m.Ack(change.Agent, ack.ChangeID, ack.Version, ack.BodyHash)
}
