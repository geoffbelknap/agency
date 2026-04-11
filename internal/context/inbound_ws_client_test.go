package context

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestInboundWSClientPush(t *testing.T) {
	upgrader := websocket.Upgrader{}
	pushReceived := make(chan WSPushMessage, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatal(err)
		}

		client := NewInboundWSClient("test-agent", conn, nil)
		defer client.Close()

		change := &ConstraintChange{
			ChangeID:    "chg-inbound",
			Agent:       "test-agent",
			Version:     3,
			Severity:    SeverityMedium,
			Constraints: map[string]interface{}{"foo": "bar"},
			Hash:        "abc123",
			Reason:      "test",
			Timestamp:   time.Now().UTC(),
		}

		ack, err := client.Push(change, 5*time.Second)
		if err != nil {
			t.Errorf("push failed: %v", err)
			return
		}
		if ack.ChangeID != change.ChangeID {
			t.Errorf("ack change_id = %q, want %q", ack.ChangeID, change.ChangeID)
		}
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	var push WSPushMessage
	if err := conn.ReadJSON(&push); err != nil {
		t.Fatal(err)
	}
	pushReceived <- push
	if err := conn.WriteJSON(AckReport{
		Type:      "constraint_ack",
		Agent:     push.Agent,
		ChangeID:  push.ChangeID,
		Version:   push.Version,
		Status:    StatusAcked,
		BodyHash:  push.Hash,
		Timestamp: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	got := <-pushReceived
	if got.ChangeID != "chg-inbound" {
		t.Fatalf("received change_id = %q, want chg-inbound", got.ChangeID)
	}
}
