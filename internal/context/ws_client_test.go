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

func TestWSClientConnectAndPush(t *testing.T) {
	var received WSPushMessage
	var mu sync.Mutex
	upgrader := websocket.Upgrader{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		var msg WSPushMessage
		if err := conn.ReadJSON(&msg); err != nil {
			return
		}
		mu.Lock()
		received = msg
		mu.Unlock()
		conn.WriteJSON(AckReport{
			Type:     "constraint_ack",
			Agent:    msg.Agent,
			ChangeID: msg.ChangeID,
			Version:  msg.Version,
			Status:   StatusAcked,
			BodyHash: msg.Hash,
		})
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	client := NewWSClient("test-agent", wsURL, nil)
	if err := client.Connect(); err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	change := &ConstraintChange{
		ChangeID:    "chg_test1",
		Agent:       "test-agent",
		Version:     1,
		Severity:    SeverityMedium,
		Constraints: map[string]interface{}{"foo": "bar"},
		Hash:        "abc123",
		Reason:      "test",
		Timestamp:   time.Now().UTC(),
	}

	ack, err := client.Push(change, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if ack.Status != StatusAcked {
		t.Errorf("ack status = %s, want acked", ack.Status)
	}

	mu.Lock()
	if received.ChangeID != "chg_test1" {
		t.Errorf("received change_id = %q, want chg_test1", received.ChangeID)
	}
	mu.Unlock()
}

func TestWSClientReconnect(t *testing.T) {
	connectCount := 0
	var mu sync.Mutex
	upgrader := websocket.Upgrader{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		connectCount++
		count := connectCount
		mu.Unlock()

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		if count == 1 {
			conn.Close()
			return
		}
		defer conn.Close()
		time.Sleep(2 * time.Second)
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	client := NewWSClient("test-agent", wsURL, nil)
	client.reconnectMin = 100 * time.Millisecond
	client.reconnectMax = 200 * time.Millisecond
	client.pingInterval = 50 * time.Millisecond
	client.writeTimeout = 50 * time.Millisecond
	go client.ConnectWithReconnect()
	defer client.Close()

	time.Sleep(500 * time.Millisecond)

	mu.Lock()
	if connectCount < 2 {
		t.Errorf("connect count = %d, want >= 2", connectCount)
	}
	mu.Unlock()
}

func TestWSClientKeepaliveDoesNotReconnectOnNormalPongs(t *testing.T) {
	connectCount := 0
	var mu sync.Mutex
	upgrader := websocket.Upgrader{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		connectCount++
		mu.Unlock()

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		_, _, _ = conn.ReadMessage()
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	client := NewWSClient("test-agent", wsURL, nil)
	client.reconnectMin = 25 * time.Millisecond
	client.reconnectMax = 25 * time.Millisecond
	client.pingInterval = 25 * time.Millisecond
	client.writeTimeout = 25 * time.Millisecond
	go client.ConnectWithReconnect()
	defer client.Close()

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if connectCount != 1 {
		t.Fatalf("connect count = %d, want 1", connectCount)
	}
}
