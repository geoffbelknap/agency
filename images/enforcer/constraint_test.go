package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func newTestConstraintHandler(t *testing.T) *ConstraintHandler {
	t.Helper()
	dir := t.TempDir()
	audit := NewAuditLogger(dir, "test-agent")
	t.Cleanup(func() { audit.Close() })
	return NewConstraintHandler("test-agent", audit, "")
}

func TestConstraintGetEmpty(t *testing.T) {
	ch := newTestConstraintHandler(t)

	mux := http.NewServeMux()
	ch.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/constraints", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var state ConstraintState
	if err := json.Unmarshal(rr.Body.Bytes(), &state); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if state.Version != 0 {
		t.Errorf("expected version 0, got %d", state.Version)
	}
	if state.Hash == "" {
		t.Error("expected non-empty hash")
	}
}

func TestConstraintGetMethodNotAllowed(t *testing.T) {
	ch := newTestConstraintHandler(t)

	mux := http.NewServeMux()
	ch.RegisterRoutes(mux)

	req := httptest.NewRequest("POST", "/constraints", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rr.Code)
	}
}

func TestConstraintBodyAckMismatch(t *testing.T) {
	ch := newTestConstraintHandler(t)

	mux := http.NewServeMux()
	ch.RegisterRoutes(mux)

	body := `{"change_id":"abc","version":1,"hash":"wrong-hash"}`
	req := httptest.NewRequest("POST", "/constraints/ack", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestConstraintBodyAckSuccess(t *testing.T) {
	ch := newTestConstraintHandler(t)

	// Get the current hash from the empty state.
	state := ch.state.Load()

	mux := http.NewServeMux()
	ch.RegisterRoutes(mux)

	body, _ := json.Marshal(map[string]interface{}{
		"change_id": "test",
		"version":   0,
		"hash":      state.Hash,
	})
	req := httptest.NewRequest("POST", "/constraints/ack", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestConstraintWSPushAndAck(t *testing.T) {
	ch := newTestConstraintHandler(t)

	mux := http.NewServeMux()
	ch.RegisterRoutes(mux)

	server := httptest.NewServer(mux)
	defer server.Close()

	// Connect via WebSocket.
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	constraints := map[string]interface{}{
		"hard_limits": []interface{}{
			map[string]interface{}{
				"rule":   "no-delete-production",
				"reason": "safety",
			},
		},
	}
	hash := hashConstraints(constraints)

	push := wsPushMessage{
		Type:        "constraint_push",
		Agent:       "test-agent",
		ChangeID:    "change-001",
		Version:     1,
		Severity:    "MEDIUM",
		Constraints: constraints,
		Hash:        hash,
		Reason:      "test push",
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}

	if err := conn.WriteJSON(push); err != nil {
		t.Fatalf("write push: %v", err)
	}

	var ack ackReport
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if err := conn.ReadJSON(&ack); err != nil {
		t.Fatalf("read ack: %v", err)
	}

	if ack.Status != "acked" {
		t.Errorf("expected acked, got %s", ack.Status)
	}
	if ack.ChangeID != "change-001" {
		t.Errorf("expected change-001, got %s", ack.ChangeID)
	}
	if ack.BodyHash != hash {
		t.Errorf("expected hash %s, got %s", hash, ack.BodyHash)
	}

	// Verify state was updated.
	state := ch.state.Load()
	if state.Version != 1 {
		t.Errorf("expected version 1, got %d", state.Version)
	}
	if state.Hash != hash {
		t.Errorf("expected hash %s, got %s", hash, state.Hash)
	}

	// Verify GET /constraints returns updated state.
	req := httptest.NewRequest("GET", "/constraints", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	var getState ConstraintState
	json.Unmarshal(rr.Body.Bytes(), &getState)
	if getState.Version != 1 {
		t.Errorf("GET /constraints version = %d, want 1", getState.Version)
	}
}

func TestConstraintWSPushHashMismatch(t *testing.T) {
	ch := newTestConstraintHandler(t)

	mux := http.NewServeMux()
	ch.RegisterRoutes(mux)

	server := httptest.NewServer(mux)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	push := wsPushMessage{
		Type:        "constraint_push",
		Agent:       "test-agent",
		ChangeID:    "change-bad",
		Version:     1,
		Severity:    "HIGH",
		Constraints: map[string]interface{}{"foo": "bar"},
		Hash:        "intentionally-wrong-hash",
		Reason:      "test bad hash",
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}

	if err := conn.WriteJSON(push); err != nil {
		t.Fatalf("write push: %v", err)
	}

	var ack ackReport
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if err := conn.ReadJSON(&ack); err != nil {
		t.Fatalf("read ack: %v", err)
	}

	if ack.Status != "hash_mismatch" {
		t.Errorf("expected hash_mismatch, got %s", ack.Status)
	}

	// State should NOT have been updated (still version 0).
	state := ch.state.Load()
	if state.Version != 0 {
		t.Errorf("state should not have been updated, got version %d", state.Version)
	}
}

func TestConstraintWSBodyNotification(t *testing.T) {
	// Set up a fake Body webhook endpoint.
	var received bool
	var receivedChangeID string
	bodyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]interface{}
		json.NewDecoder(r.Body).Decode(&payload)
		received = true
		if cid, ok := payload["change_id"].(string); ok {
			receivedChangeID = cid
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer bodyServer.Close()

	dir := t.TempDir()
	audit := NewAuditLogger(dir, "test-agent")
	t.Cleanup(func() { audit.Close() })

	ch := NewConstraintHandler("test-agent", audit, bodyServer.URL)

	mux := http.NewServeMux()
	ch.RegisterRoutes(mux)

	server := httptest.NewServer(mux)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	constraints := map[string]interface{}{"test": true}
	hash := hashConstraints(constraints)

	push := wsPushMessage{
		Type:        "constraint_push",
		Agent:       "test-agent",
		ChangeID:    "change-notify",
		Version:     1,
		Severity:    "LOW",
		Constraints: constraints,
		Hash:        hash,
		Reason:      "test notify",
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}

	conn.WriteJSON(push)

	var ack ackReport
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	conn.ReadJSON(&ack)

	// Give async notification a moment.
	time.Sleep(200 * time.Millisecond)

	if !received {
		t.Error("Body notification was not received")
	}
	if receivedChangeID != "change-notify" {
		t.Errorf("expected change_id change-notify, got %s", receivedChangeID)
	}
}
