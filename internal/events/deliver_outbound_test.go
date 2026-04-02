package events

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geoffbelknap/agency/internal/models"
)

func TestOutboundDelivery_Success(t *testing.T) {
	var received models.Event
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &received)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	od := NewOutboundDelivery()
	sub := &Subscription{
		Destination: Destination{Type: DestWebhook, Target: server.URL},
	}
	event := &models.Event{
		ID:         "evt-out1",
		SourceType: "platform",
		SourceName: "gateway",
		EventType:  "agent_started",
		Timestamp:  time.Now().UTC(),
		Data:       map[string]interface{}{"agent": "test"},
	}

	err := od.Deliver(sub, event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if received.ID != "evt-out1" {
		t.Errorf("expected event ID evt-out1, got %s", received.ID)
	}
	if received.EventType != "agent_started" {
		t.Errorf("expected event_type agent_started, got %s", received.EventType)
	}
}

func TestOutboundDelivery_RetryOn500(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&attempts, 1)
		if count == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	od := NewOutboundDelivery()
	sub := &Subscription{
		Destination: Destination{Type: DestWebhook, Target: server.URL},
	}
	event := &models.Event{
		ID:         "evt-retry",
		SourceType: "platform",
		SourceName: "gateway",
		EventType:  "test",
		Timestamp:  time.Now().UTC(),
		Data:       map[string]interface{}{},
	}

	err := od.Deliver(sub, event)
	if err != nil {
		t.Fatalf("expected success after retry, got: %v", err)
	}

	if atomic.LoadInt32(&attempts) != 2 {
		t.Errorf("expected 2 attempts, got %d", atomic.LoadInt32(&attempts))
	}
}

func TestOutboundDelivery_BothAttemptsFail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	od := NewOutboundDelivery()
	sub := &Subscription{
		Destination: Destination{Type: DestWebhook, Target: server.URL},
	}
	event := &models.Event{
		ID:         "evt-fail",
		SourceType: "platform",
		SourceName: "gateway",
		EventType:  "test",
		Timestamp:  time.Now().UTC(),
		Data:       map[string]interface{}{},
	}

	err := od.Deliver(sub, event)
	if err == nil {
		t.Fatal("expected error when both attempts fail")
	}
}

func TestOutboundDelivery_CustomHeaders(t *testing.T) {
	var receivedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	od := NewOutboundDelivery()
	od.Headers[server.URL] = map[string]string{
		"Authorization": "Bearer test-token",
	}

	sub := &Subscription{
		Destination: Destination{Type: DestWebhook, Target: server.URL},
	}
	event := &models.Event{
		ID:         "evt-hdr",
		SourceType: "platform",
		SourceName: "gateway",
		EventType:  "test",
		Timestamp:  time.Now().UTC(),
		Data:       map[string]interface{}{},
	}

	err := od.Deliver(sub, event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedAuth != "Bearer test-token" {
		t.Errorf("expected Authorization header, got: %s", receivedAuth)
	}
}

func TestOutbound_NtfyPayloadFormat(t *testing.T) {
	event := &models.Event{
		ID:         "evt-ntfy1",
		SourceType: "platform",
		SourceName: "gateway",
		EventType:  "agent_stopped",
		Timestamp:  time.Now().UTC(),
		Data:       map[string]interface{}{"agent": "monitor", "reason": "completed"},
		Metadata:   map[string]interface{}{"priority": "P1"},
	}

	payload := formatNtfy("https://ntfy.example.com/agency-alerts", event)

	if payload.Topic != "agency-alerts" {
		t.Errorf("expected topic agency-alerts, got %s", payload.Topic)
	}
	if payload.Title != "Agency: agent_stopped" {
		t.Errorf("expected title 'Agency: agent_stopped', got %s", payload.Title)
	}
	if payload.Priority != 5 {
		t.Errorf("expected priority 5 for P1, got %d", payload.Priority)
	}
}

func TestOutbound_NtfyPriorityMapping(t *testing.T) {
	tests := []struct {
		priority string
		expected int
	}{
		{"P1", 5},
		{"P2", 4},
		{"P3", 3},
	}

	for _, tt := range tests {
		event := &models.Event{
			ID:         "evt-pri",
			SourceType: "platform",
			SourceName: "gw",
			EventType:  "test",
			Timestamp:  time.Now().UTC(),
			Data:       map[string]interface{}{},
			Metadata:   map[string]interface{}{"priority": tt.priority},
		}
		payload := formatNtfy("https://ntfy.sh/test", event)
		if payload.Priority != tt.expected {
			t.Errorf("priority %s: expected %d, got %d", tt.priority, tt.expected, payload.Priority)
		}
	}
}

func TestOutbound_NtfyDefaultPriority(t *testing.T) {
	event := &models.Event{
		ID:         "evt-defpri",
		SourceType: "platform",
		SourceName: "gw",
		EventType:  "test",
		Timestamp:  time.Now().UTC(),
		Data:       map[string]interface{}{},
	}
	payload := formatNtfy("https://ntfy.sh/test", event)
	if payload.Priority != 3 {
		t.Errorf("expected default priority 3, got %d", payload.Priority)
	}
}

func TestFormatNtfySeverityPriority(t *testing.T) {
	tests := []struct {
		severity string
		want     int
	}{
		{"critical", 5},
		{"warning", 3},
		{"info", 2},
		{"", 3},        // default
		{"unknown", 3}, // unknown defaults to 3
	}

	for _, tt := range tests {
		event := models.NewEvent(models.EventSourcePlatform, "gateway", "operator_alert", map[string]interface{}{
			"severity": tt.severity,
			"message":  "test alert",
		})
		payload := formatNtfy("https://ntfy.sh/test-topic", event)
		if payload.Priority != tt.want {
			t.Errorf("severity=%q: got priority %d, want %d", tt.severity, payload.Priority, tt.want)
		}
	}
}

func TestOutbound_NtfyDelivery(t *testing.T) {
	var received NtfyPayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &received)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	nd := NewNtfyDelivery()
	sub := &Subscription{
		Destination: Destination{Type: DestNtfy, Target: server.URL + "/alerts"},
	}
	event := &models.Event{
		ID:         "evt-ntfydel",
		SourceType: "platform",
		SourceName: "gateway",
		EventType:  "agent_error",
		Timestamp:  time.Now().UTC(),
		Data:       map[string]interface{}{"error": "OOM"},
	}

	err := nd.Deliver(sub, event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if received.Topic != "alerts" {
		t.Errorf("expected topic alerts, got %s", received.Topic)
	}
}
