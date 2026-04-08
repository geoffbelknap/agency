package events

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"log/slog"

	"github.com/geoffbelknap/agency/internal/events"
)

func publishTestHandler(t *testing.T) *handler {
	t.Helper()
	logger := slog.Default()
	bus := events.NewBus(logger, nil)
	return &handler{
		deps: Deps{
			EventBus: bus,
		},
	}
}

func TestPublishEvent(t *testing.T) {
	h := publishTestHandler(t)

	body := `{"source_type":"platform","source_name":"intake","event_type":"item_ingested","data":{"item_id":"abc123"}}`
	req := httptest.NewRequest("POST", "/api/v1/events/publish", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.publishEvent(w, req)

	if w.Code != 202 {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}

	var result map[string]string
	json.Unmarshal(w.Body.Bytes(), &result)
	if result["status"] != "published" {
		t.Fatalf("expected status=published, got %s", result["status"])
	}
	if result["event_id"] == "" {
		t.Fatal("expected event_id in response")
	}
}

func TestPublishEvent_InvalidJSON(t *testing.T) {
	h := publishTestHandler(t)

	req := httptest.NewRequest("POST", "/api/v1/events/publish", bytes.NewBufferString(`{bad json`))
	w := httptest.NewRecorder()
	h.publishEvent(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}

	var result map[string]string
	json.Unmarshal(w.Body.Bytes(), &result)
	if result["error"] != "invalid JSON" {
		t.Fatalf("expected 'invalid JSON' error, got %s", result["error"])
	}
}

func TestPublishEvent_MissingFields(t *testing.T) {
	h := publishTestHandler(t)

	// Missing source_name and event_type
	body := `{"source_type":"platform"}`
	req := httptest.NewRequest("POST", "/api/v1/events/publish", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.publishEvent(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}

	var result map[string]string
	json.Unmarshal(w.Body.Bytes(), &result)
	if result["error"] == "" {
		t.Fatal("expected error message for missing fields")
	}
}

func TestPublishEvent_NilBus(t *testing.T) {
	h := &handler{
		deps: Deps{
			EventBus: nil,
		},
	}

	body := `{"source_type":"platform","source_name":"intake","event_type":"item_ingested"}`
	req := httptest.NewRequest("POST", "/api/v1/events/publish", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.publishEvent(w, req)

	if w.Code != 503 {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}
