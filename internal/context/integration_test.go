package context

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestIntegrationPushAckFlow(t *testing.T) {
	var mu sync.Mutex
	var receivedPush WSPushMessage

	upgrader := websocket.Upgrader{}
	enforcer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()

		var push WSPushMessage
		if err := conn.ReadJSON(&push); err != nil {
			return
		}
		mu.Lock()
		receivedPush = push
		mu.Unlock()

		time.Sleep(50 * time.Millisecond)

		conn.WriteJSON(AckReport{
			Type:      "constraint_ack",
			Agent:     push.Agent,
			ChangeID:  push.ChangeID,
			Version:   push.Version,
			Status:    StatusAcked,
			BodyHash:  push.Hash,
			Timestamp: time.Now().UTC(),
		})
	}))
	defer enforcer.Close()

	mgr := NewManager(nil)
	mgr.SetInitialConstraints("test-agent", map[string]interface{}{"old": true})

	wsURL := "ws" + strings.TrimPrefix(enforcer.URL, "http") + "/ws"
	client := NewWSClient("test-agent", wsURL, nil)
	if err := client.Connect(); err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	change, err := mgr.Push("test-agent", map[string]interface{}{"new": true}, "", "integration test", "operator")
	if err != nil {
		t.Fatal(err)
	}

	ack, err := client.Push(change, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	mgr.Ack("test-agent", ack.ChangeID, ack.Version, ack.BodyHash)

	status := mgr.GetStatus("test-agent")
	if status.Status != StatusAcked {
		t.Errorf("final status = %s, want acked", status.Status)
	}

	mu.Lock()
	if receivedPush.ChangeID != change.ChangeID {
		t.Errorf("enforcer received change_id = %s, want %s", receivedPush.ChangeID, change.ChangeID)
	}
	mu.Unlock()

	history := mgr.Changes("test-agent")
	if len(history) != 1 {
		t.Fatalf("history len = %d, want 1", len(history))
	}
	if history[0].Status != StatusAcked {
		t.Errorf("history status = %s, want acked", history[0].Status)
	}
}

// TestDeliverAsyncSuccess verifies the full async delivery path: Manager creates
// a change, DeliverAsync sends it via the registered WSClient, the mock enforcer
// acks, and the Manager marks it acked.
func TestDeliverAsyncSuccess(t *testing.T) {
	upgrader := websocket.Upgrader{}
	enforcer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		var push WSPushMessage
		if err := conn.ReadJSON(&push); err != nil {
			return
		}
		conn.WriteJSON(AckReport{
			Type:     "constraint_ack",
			Agent:    push.Agent,
			ChangeID: push.ChangeID,
			Version:  push.Version,
			Status:   StatusAcked,
			BodyHash: push.Hash,
		})
	}))
	defer enforcer.Close()

	mgr := NewManager(nil)
	mgr.SetInitialConstraints("async-agent", map[string]interface{}{"old": true})

	wsURL := "ws" + strings.TrimPrefix(enforcer.URL, "http") + "/ws"
	client := NewWSClient("async-agent", wsURL, nil)
	if err := client.Connect(); err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	mgr.RegisterWSClient("async-agent", client)

	change, err := mgr.Push("async-agent", map[string]interface{}{"new": true}, "", "async test", "operator")
	if err != nil {
		t.Fatal(err)
	}

	mgr.DeliverAsync(change)

	// Wait for the async goroutine to complete.
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for async ack")
		default:
		}
		status := mgr.GetStatus("async-agent")
		if status != nil && status.Status == StatusAcked {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestDeliverAsyncTimeout verifies that when the enforcer never acks, the
// Manager marks the change as timed out, calls the halt function, and marks halted.
func TestDeliverAsyncTimeout(t *testing.T) {
	upgrader := websocket.Upgrader{}
	// Mock enforcer that accepts the connection but never sends an ack.
	enforcer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		// Read the push but never respond — let the timeout fire.
		var push WSPushMessage
		conn.ReadJSON(&push)
		// Hold connection open.
		time.Sleep(10 * time.Second)
	}))
	defer enforcer.Close()

	mgr := NewManager(nil)
	mgr.SetInitialConstraints("timeout-agent", map[string]interface{}{"old": true})

	var mu sync.Mutex
	var haltedAgent, haltedChangeID string
	mgr.SetHaltFunc(func(agent, changeID, reason string) error {
		mu.Lock()
		haltedAgent = agent
		haltedChangeID = changeID
		mu.Unlock()
		return nil
	})

	var alertCalled bool
	mgr.SetAlertFunc(func(agent, message string) {
		mu.Lock()
		alertCalled = true
		mu.Unlock()
	})

	wsURL := "ws" + strings.TrimPrefix(enforcer.URL, "http") + "/ws"
	client := NewWSClient("timeout-agent", wsURL, nil)
	if err := client.Connect(); err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	mgr.RegisterWSClient("timeout-agent", client)

	// Use CRITICAL severity for fastest timeout (5s ack + 10s remaining).
	change, err := mgr.Push("timeout-agent", map[string]interface{}{"critical_change": true}, "CRITICAL", "critical test", "operator")
	if err != nil {
		t.Fatal(err)
	}

	mgr.DeliverAsync(change)

	// Wait for the async goroutine to hit both timeouts and halt.
	deadline := time.After(30 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for halt")
		default:
		}
		status := mgr.GetStatus("timeout-agent")
		if status != nil && status.Status == StatusHalted {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if haltedAgent != "timeout-agent" {
		t.Errorf("halted agent = %q, want timeout-agent", haltedAgent)
	}
	if haltedChangeID != change.ChangeID {
		t.Errorf("halted change_id = %q, want %q", haltedChangeID, change.ChangeID)
	}
	if !alertCalled {
		t.Error("alert function should have been called")
	}
}

// TestRegisterUnregisterWSClient verifies the WSClient registry on Manager.
func TestRegisterUnregisterWSClient(t *testing.T) {
	mgr := NewManager(nil)

	client := NewWSClient("reg-agent", "ws://localhost:9999/ws", nil)
	mgr.RegisterWSClient("reg-agent", client)

	// Verify delivery with no connection returns gracefully (no panic).
	change := &ConstraintChange{
		ChangeID: "chg_reg",
		Agent:    "reg-agent",
		Severity: SeverityLow,
	}
	// deliverAsync should handle the "not connected" error without panic.
	mgr.deliverAsync(change)

	mgr.UnregisterWSClient("reg-agent")

	// After unregister, deliverAsync should log and return (no client).
	mgr.deliverAsync(change)
}
