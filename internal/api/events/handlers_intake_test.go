package events

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"log/slog"

	eventbus "github.com/geoffbelknap/agency/internal/events"
)

func TestIntakeWebhookRoutesToNamedConnectorAndPublishesEvent(t *testing.T) {
	previousBaseURL := intakeBaseURL
	defer func() { intakeBaseURL = previousBaseURL }()

	var receivedPath string
	var receivedBody []byte
	intake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		receivedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"status":"ok","delivered":true}`))
	}))
	defer intake.Close()
	intakeBaseURL = intake.URL

	bus := eventbus.NewBus(slog.Default(), nil)
	h := &handler{deps: Deps{EventBus: bus}}

	body := []byte(`{"connector":"fixture-connector","event_type":"local_check","kind":"local-intake-check"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/events/intake/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.intakeWebhook(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if receivedPath != "/webhooks/fixture-connector" {
		t.Fatalf("expected webhook path /webhooks/fixture-connector, got %q", receivedPath)
	}
	if string(receivedBody) != string(body) {
		t.Fatalf("unexpected forwarded body: %s", string(receivedBody))
	}

	events := bus.Events().List(10)
	if len(events) != 1 {
		t.Fatalf("expected 1 published event, got %d", len(events))
	}
	if events[0].SourceType != "connector" || events[0].SourceName != "fixture-connector" || events[0].EventType != "local_check" {
		t.Fatalf("unexpected event: %+v", events[0])
	}
}

func TestIntakeWebhookRequiresConnector(t *testing.T) {
	h := &handler{}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/events/intake/webhook", bytes.NewReader([]byte(`{"kind":"local-intake-check"}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.intakeWebhook(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if resp["error"] != "connector required" {
		t.Fatalf("unexpected error response: %#v", resp)
	}
}

func TestRelayWebhookDeliverRoutesToLocalPathAndPreservesHeaders(t *testing.T) {
	previousBaseURL := intakeBaseURL
	defer func() { intakeBaseURL = previousBaseURL }()

	var receivedPath string
	var receivedHeader string
	var receivedBody []byte
	intake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.RequestURI()
		receivedHeader = r.Header.Get("X-Slack-Signature")
		receivedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer intake.Close()
	intakeBaseURL = intake.URL

	h := &handler{}
	req := httptest.NewRequest(http.MethodPost, "/api/internal/relay/webhooks/deliver", bytes.NewReader([]byte(`{"type":"block_actions"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Slack-Signature", "v0=abc123")
	req.Header.Set("X-Relay-Webhook-Local-Path", "/webhooks/slack-alpha")
	req.Header.Set("X-Relay-Webhook-Query", "?trigger_id=123")
	req.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()

	h.relayWebhookDeliver(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if receivedPath != "/webhooks/slack-alpha?trigger_id=123" {
		t.Fatalf("received path = %q", receivedPath)
	}
	if receivedHeader != "v0=abc123" {
		t.Fatalf("received signature = %q", receivedHeader)
	}
	if string(receivedBody) != `{"type":"block_actions"}` {
		t.Fatalf("unexpected forwarded body: %s", string(receivedBody))
	}
}

func TestRelayWebhookDeliverRequiresWebhookPath(t *testing.T) {
	h := &handler{}
	req := httptest.NewRequest(http.MethodPost, "/api/internal/relay/webhooks/deliver", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("X-Relay-Webhook-Local-Path", "/api/v1/agents")
	w := httptest.NewRecorder()

	h.relayWebhookDeliver(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
}
