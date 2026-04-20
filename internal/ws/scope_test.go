package ws

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"log/slog"

	"github.com/geoffbelknap/agency/internal/authz"
	"github.com/geoffbelknap/agency/internal/registry"
)

// recordingAuditor captures WriteSystem calls for assertions.
type recordingAuditor struct {
	mu      sync.Mutex
	entries []auditEntry
}

type auditEntry struct {
	event  string
	detail map[string]interface{}
}

func (r *recordingAuditor) WriteSystem(event string, detail map[string]interface{}) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	copied := make(map[string]interface{}, len(detail))
	for k, v := range detail {
		copied[k] = v
	}
	r.entries = append(r.entries, auditEntry{event: event, detail: copied})
	return nil
}

func (r *recordingAuditor) find(target, name string) *auditEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.entries {
		e := &r.entries[i]
		if e.event != "ws_subscribe_denied" {
			continue
		}
		if e.detail["target"] == target && e.detail["name"] == name {
			return e
		}
	}
	return nil
}

// registerInHub inserts a client into the hub's clients map synchronously
// so tests do not depend on the register-channel goroutine timing.
func registerInHub(h *Hub, c *Client) {
	h.mu.Lock()
	h.clients[c] = true
	h.mu.Unlock()
}

// drainOne reads a single broadcast from the client's send channel or fails
// if no event arrives within the timeout.
func drainOne(t *testing.T, ch chan []byte) Event {
	t.Helper()
	select {
	case data := <-ch:
		var ev Event
		if err := json.Unmarshal(data, &ev); err != nil {
			t.Fatalf("unmarshal broadcast: %v", err)
		}
		return ev
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for event")
		return Event{}
	}
}

// expectNoEvent asserts nothing is delivered within the window.
func expectNoEvent(t *testing.T, ch chan []byte, window time.Duration) {
	t.Helper()
	select {
	case data := <-ch:
		t.Fatalf("unexpected event delivered: %s", string(data))
	case <-time.After(window):
	}
}

func TestScope_AgentEmptySubscriptionOnlyGetsOwnAgent(t *testing.T) {
	h := NewHub(slog.Default())
	sendCh := make(chan []byte, 8)

	scope := &authz.Scope{
		Agents:   map[string]bool{"bob": true},
		Channels: map[string]bool{"dm-bob": true},
	}
	client := &Client{
		hub:          h,
		send:         sendCh,
		log:          slog.Default(),
		principal:    &registry.Principal{Type: "agent", Name: "bob"},
		scope:        scope,
		subscription: &Subscription{}, // empty — "all within scope"
	}
	registerInHub(h, client)

	// Event for bob — should be delivered.
	h.Broadcast(Event{Type: "agent_status", Agent: "bob", Status: "running"})
	got := drainOne(t, sendCh)
	if got.Agent != "bob" {
		t.Errorf("expected event for bob, got %q", got.Agent)
	}

	// Event for a different agent — must be dropped by scope.
	h.Broadcast(Event{Type: "agent_status", Agent: "eve", Status: "running"})
	expectNoEvent(t, sendCh, 100*time.Millisecond)

	// Message in bob's DM — delivered.
	h.Broadcast(Event{Type: "message", Channel: "dm-bob"})
	got = drainOne(t, sendCh)
	if got.Channel != "dm-bob" {
		t.Errorf("expected dm-bob, got %q", got.Channel)
	}

	// Message in someone else's DM — dropped.
	h.Broadcast(Event{Type: "message", Channel: "dm-eve"})
	expectNoEvent(t, sendCh, 100*time.Millisecond)

	// Infra event — dropped (agents don't see infra).
	h.Broadcast(Event{Type: "infra_status", Component: "enforcer"})
	expectNoEvent(t, sendCh, 100*time.Millisecond)

	// Agent signal for bob — delivered.
	h.Broadcast(Event{Type: "agent_signal_error", Agent: "bob"})
	got = drainOne(t, sendCh)
	if got.Type != "agent_signal_error" {
		t.Errorf("expected agent_signal_error, got %q", got.Type)
	}

	// Agent signal for eve — dropped.
	h.Broadcast(Event{Type: "agent_signal_error", Agent: "eve"})
	expectNoEvent(t, sendCh, 100*time.Millisecond)
}

func TestScope_OperatorSeesEverything(t *testing.T) {
	h := NewHub(slog.Default())
	sendCh := make(chan []byte, 8)

	client := &Client{
		hub:          h,
		send:         sendCh,
		log:          slog.Default(),
		principal:    &registry.Principal{Type: "operator", Name: "alice"},
		scope:        authz.AllowAll(),
		subscription: &Subscription{},
	}
	registerInHub(h, client)

	h.Broadcast(Event{Type: "agent_status", Agent: "bob"})
	h.Broadcast(Event{Type: "agent_status", Agent: "eve"})
	h.Broadcast(Event{Type: "infra_status", Component: "enforcer"})

	// Drain three events; all should land.
	for i := 0; i < 3; i++ {
		drainOne(t, sendCh)
	}
}

func TestScope_NilScopeAllowsAllForBackwardCompat(t *testing.T) {
	// Confirms that clients that predate the scope field (e.g., constructed
	// in pre-existing tests without a principal/registry) keep working.
	h := NewHub(slog.Default())
	sendCh := make(chan []byte, 2)

	client := &Client{
		hub:          h,
		send:         sendCh,
		log:          slog.Default(),
		subscription: &Subscription{},
		// scope left nil
	}
	registerInHub(h, client)

	h.Broadcast(Event{Type: "agent_status", Agent: "anything"})
	drainOne(t, sendCh)
}

func TestSubscribeAudit_DeniesOutOfScopeAndAudits(t *testing.T) {
	h := NewHub(slog.Default())
	aud := &recordingAuditor{}
	h.SetAuditor(aud)
	sendCh := make(chan []byte, 8)

	scope := &authz.Scope{
		Agents:   map[string]bool{"bob": true},
		Channels: map[string]bool{"dm-bob": true},
	}
	client := &Client{
		hub:       h,
		send:      sendCh,
		log:       slog.Default(),
		principal: &registry.Principal{Type: "agent", Name: "bob"},
		scope:     scope,
	}
	registerInHub(h, client)

	// Simulate the subscribe handler path: bob asks to follow dm-eve and eve.
	allowedChannels, deniedChannels := filterByScope([]string{"dm-bob", "dm-eve"}, client.scope, scopeChannel)
	allowedAgents, deniedAgents := filterByScope([]string{"bob", "eve"}, client.scope, scopeAgent)

	for _, n := range deniedChannels {
		client.auditSubscribeDenial("channel", n)
	}
	for _, n := range deniedAgents {
		client.auditSubscribeDenial("agent", n)
	}

	if len(allowedChannels) != 1 || allowedChannels[0] != "dm-bob" {
		t.Errorf("allowedChannels = %v, want [dm-bob]", allowedChannels)
	}
	if len(allowedAgents) != 1 || allowedAgents[0] != "bob" {
		t.Errorf("allowedAgents = %v, want [bob]", allowedAgents)
	}
	if aud.find("channel", "dm-eve") == nil {
		t.Error("expected ws_subscribe_denied audit entry for channel dm-eve")
	}
	if aud.find("agent", "eve") == nil {
		t.Error("expected ws_subscribe_denied audit entry for agent eve")
	}
	entry := aud.find("channel", "dm-eve")
	if entry.detail["principal_type"] != "agent" || entry.detail["principal_name"] != "bob" {
		t.Errorf("audit entry missing principal identification: %+v", entry.detail)
	}
}

func TestFilterByScope_AllowAllPassesEverything(t *testing.T) {
	allowed, denied := filterByScope([]string{"anything", "else"}, authz.AllowAll(), scopeAgent)
	if len(allowed) != 2 {
		t.Errorf("allowed = %v, want both entries", allowed)
	}
	if len(denied) != 0 {
		t.Errorf("denied = %v, want empty", denied)
	}
}

func TestFilterByScope_NilScopePassesEverything(t *testing.T) {
	allowed, denied := filterByScope([]string{"anything"}, nil, scopeAgent)
	if len(allowed) != 1 || len(denied) != 0 {
		t.Errorf("nil scope should pass through: allowed=%v denied=%v", allowed, denied)
	}
}
