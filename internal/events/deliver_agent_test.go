package events

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/geoffbelknap/agency/internal/models"
)

func TestAgentDelivery_FormatMessage(t *testing.T) {
	sub := &Subscription{
		ID:          "sub-1",
		SourceType:  "connector",
		SourceName:  "github",
		EventType:   "pull_request",
		Destination: Destination{Type: DestAgent, Target: "reviewer"},
		Origin:      OriginMission,
		OriginRef:   "review-prs",
		Active:      true,
	}

	event := &models.Event{
		ID:         "evt-abc123",
		SourceType: "connector",
		SourceName: "github",
		EventType:  "pull_request",
		Timestamp:  time.Now().UTC(),
		Data: map[string]interface{}{
			"repo":   "agency",
			"action": "opened",
			"number": 42,
		},
	}

	msg := formatAgentMessage(sub, event)

	// Check header
	if !strings.Contains(msg, "[Mission trigger: connector github / pull_request]") {
		t.Errorf("missing mission trigger header, got: %s", msg)
	}

	// Check source
	if !strings.Contains(msg, "New event from github:") {
		t.Errorf("missing source line, got: %s", msg)
	}

	// Check data fields (sorted)
	if !strings.Contains(msg, "  action: opened") {
		t.Errorf("missing action field, got: %s", msg)
	}
	if !strings.Contains(msg, "  repo: agency") {
		t.Errorf("missing repo field, got: %s", msg)
	}

	// Check footer
	if !strings.Contains(msg, "Process this according to your mission instructions.") {
		t.Errorf("missing footer, got: %s", msg)
	}
}

func TestAgentDelivery_Deliver(t *testing.T) {
	var receivedBody map[string]interface{}
	var receivedPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ad := NewAgentDelivery(server.URL)

	sub := &Subscription{
		ID:          "sub-1",
		SourceType:  "connector",
		SourceName:  "github",
		EventType:   "push",
		Destination: Destination{Type: DestAgent, Target: "deployer"},
		Origin:      OriginMission,
		OriginRef:   "auto-deploy",
		Active:      true,
	}

	event := &models.Event{
		ID:         "evt-xyz789",
		SourceType: "connector",
		SourceName: "github",
		EventType:  "push",
		Timestamp:  time.Now().UTC(),
		Data:       map[string]interface{}{"branch": "main"},
	}

	err := ad.Deliver(sub, event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify path
	expected := "/channels/dm-deployer/messages"
	if receivedPath != expected {
		t.Errorf("expected path %s, got %s", expected, receivedPath)
	}

	// Verify author
	author, _ := receivedBody["author"].(string)
	if author != "_gateway" {
		t.Errorf("expected author _gateway, got %s", author)
	}

	// Verify content contains trigger info
	content, _ := receivedBody["content"].(string)
	if !strings.Contains(content, "[Mission trigger:") {
		t.Errorf("content missing trigger header: %s", content)
	}

	// Verify metadata contains event_id
	metadata, ok := receivedBody["metadata"].(map[string]interface{})
	if !ok {
		t.Fatal("expected metadata field in body")
	}
	eventID, _ := metadata["event_id"].(string)
	if eventID != "evt-xyz789" {
		t.Errorf("expected event_id evt-xyz789, got %s", eventID)
	}
}

func TestAgentDelivery_CommsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	ad := NewAgentDelivery(server.URL)

	sub := &Subscription{
		Destination: Destination{Type: DestAgent, Target: "test-agent"},
	}
	event := &models.Event{
		ID:         "evt-err",
		SourceType: "platform",
		SourceName: "gateway",
		EventType:  "error",
		Timestamp:  time.Now().UTC(),
		Data:       map[string]interface{}{},
	}

	err := ad.Deliver(sub, event)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected 500 in error, got: %v", err)
	}
}

func TestAgentDelivery_EmptyData(t *testing.T) {
	sub := &Subscription{
		Destination: Destination{Type: DestAgent, Target: "agent"},
	}
	event := &models.Event{
		ID:         "evt-empty",
		SourceType: "schedule",
		SourceName: "daily-check",
		EventType:  "fired",
		Timestamp:  time.Now().UTC(),
		Data:       map[string]interface{}{},
	}

	msg := formatAgentMessage(sub, event)
	if !strings.Contains(msg, "New event from daily-check:") {
		t.Errorf("missing source line for empty data event: %s", msg)
	}
}
