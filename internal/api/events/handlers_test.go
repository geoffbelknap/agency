package events

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"log/slog"
	"github.com/go-chi/chi/v5"

	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/events"
)

func notifTestHandler(t *testing.T) (*handler, *events.NotificationStore, *events.Bus) {
	t.Helper()
	home := t.TempDir()
	store := events.NewNotificationStore(home)
	logger := slog.Default()
	bus := events.NewBus(logger, nil)
	h := &handler{
		deps: Deps{
			NotifStore: store,
			EventBus:   bus,
		},
		webhookRL: newWebhookRateLimiter(),
	}
	return h, store, bus
}

func TestListNotifications_Empty(t *testing.T) {
	h, _, _ := notifTestHandler(t)

	req := httptest.NewRequest("GET", "/api/v1/notifications", nil)
	w := httptest.NewRecorder()
	h.listNotifications(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var result []config.NotificationConfig
	json.Unmarshal(w.Body.Bytes(), &result)
	if len(result) != 0 {
		t.Fatalf("expected empty, got %d", len(result))
	}
}

func TestAddNotification(t *testing.T) {
	h, store, bus := notifTestHandler(t)

	body := `{"name":"test-ntfy","type":"ntfy","url":"https://ntfy.sh/test","events":["operator_alert"]}`
	req := httptest.NewRequest("POST", "/api/v1/notifications", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.addNotification(w, req)

	if w.Code != 201 {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	list := store.List()
	if len(list) != 1 || list[0].Name != "test-ntfy" {
		t.Fatalf("store: expected [test-ntfy], got %v", list)
	}

	subs := bus.Subscriptions().List()
	found := false
	for _, s := range subs {
		if s.Origin == events.OriginNotification && s.OriginRef == "test-ntfy" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected notification subscription in event bus")
	}
}

func TestAddNotification_Duplicate(t *testing.T) {
	h, _, _ := notifTestHandler(t)

	body := `{"name":"dup","url":"https://ntfy.sh/dup"}`
	req := httptest.NewRequest("POST", "/api/v1/notifications", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.addNotification(w, req)

	req2 := httptest.NewRequest("POST", "/api/v1/notifications", bytes.NewBufferString(body))
	w2 := httptest.NewRecorder()
	h.addNotification(w2, req2)

	if w2.Code != 409 {
		t.Fatalf("expected 409, got %d", w2.Code)
	}
}

func TestAddNotification_DefaultEvents(t *testing.T) {
	h, store, _ := notifTestHandler(t)

	body := `{"name":"defaults","url":"https://ntfy.sh/defaults"}`
	req := httptest.NewRequest("POST", "/api/v1/notifications", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.addNotification(w, req)

	if w.Code != 201 {
		t.Fatalf("expected 201, got %d", w.Code)
	}
	list := store.List()
	if len(list[0].Events) != 3 {
		t.Fatalf("expected 3 default events, got %d", len(list[0].Events))
	}
}

func TestAddNotification_AutoDetectType(t *testing.T) {
	h, store, _ := notifTestHandler(t)

	body := `{"name":"auto-ntfy","url":"https://ntfy.sh/topic"}`
	req := httptest.NewRequest("POST", "/api/v1/notifications", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.addNotification(w, req)

	list := store.List()
	if list[0].Type != "ntfy" {
		t.Fatalf("expected ntfy, got %s", list[0].Type)
	}

	body2 := `{"name":"auto-wh","url":"https://hooks.example.com/ops"}`
	req2 := httptest.NewRequest("POST", "/api/v1/notifications", bytes.NewBufferString(body2))
	w2 := httptest.NewRecorder()
	h.addNotification(w2, req2)

	list2 := store.List()
	if list2[1].Type != "webhook" {
		t.Fatalf("expected webhook, got %s", list2[1].Type)
	}
}

func TestAddNotification_RejectsPrivateIP(t *testing.T) {
	h, _, _ := notifTestHandler(t)
	body := `{"name":"evil","type":"webhook","url":"https://169.254.169.254/latest/meta-data/","events":["operator_alert"]}`
	req := httptest.NewRequest("POST", "/api/v1/notifications", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.addNotification(w, req)
	if w.Code != 400 {
		t.Fatalf("expected 400 for private IP, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAddNotification_RejectsHTTP(t *testing.T) {
	h, _, _ := notifTestHandler(t)
	body := `{"name":"evil","type":"webhook","url":"http://external.com/hook","events":["operator_alert"]}`
	req := httptest.NewRequest("POST", "/api/v1/notifications", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.addNotification(w, req)
	if w.Code != 400 {
		t.Fatalf("expected 400 for http to non-localhost, got %d: %s", w.Code, w.Body.String())
	}
}

func TestShowNotification(t *testing.T) {
	h, _, _ := notifTestHandler(t)

	body := `{"name":"show-me","url":"https://ntfy.sh/show"}`
	req := httptest.NewRequest("POST", "/api/v1/notifications", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.addNotification(w, req)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "show-me")
	req2 := httptest.NewRequest("GET", "/api/v1/notifications/show-me", nil)
	req2 = req2.WithContext(context.WithValue(req2.Context(), chi.RouteCtxKey, rctx))
	w2 := httptest.NewRecorder()
	h.showNotification(w2, req2)

	if w2.Code != 200 {
		t.Fatalf("expected 200, got %d", w2.Code)
	}
}

func TestDeleteNotification(t *testing.T) {
	h, store, bus := notifTestHandler(t)

	body := `{"name":"del-me","url":"https://ntfy.sh/del","events":["operator_alert"]}`
	req := httptest.NewRequest("POST", "/api/v1/notifications", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.addNotification(w, req)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "del-me")
	req2 := httptest.NewRequest("DELETE", "/api/v1/notifications/del-me", nil)
	req2 = req2.WithContext(context.WithValue(req2.Context(), chi.RouteCtxKey, rctx))
	w2 := httptest.NewRecorder()
	h.deleteNotification(w2, req2)

	if w2.Code != 200 {
		t.Fatalf("expected 200, got %d", w2.Code)
	}
	if len(store.List()) != 0 {
		t.Fatal("expected empty store after delete")
	}

	for _, s := range bus.Subscriptions().List() {
		if s.Origin == events.OriginNotification && s.OriginRef == "del-me" {
			t.Fatal("subscription should have been removed")
		}
	}
}

func TestTestNotification(t *testing.T) {
	h, _, _ := notifTestHandler(t)

	body := `{"name":"test-dest","url":"https://ntfy.sh/test","events":["operator_alert"]}`
	req := httptest.NewRequest("POST", "/api/v1/notifications", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.addNotification(w, req)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "test-dest")
	req2 := httptest.NewRequest("POST", "/api/v1/notifications/test-dest/test", nil)
	req2 = req2.WithContext(context.WithValue(req2.Context(), chi.RouteCtxKey, rctx))
	w2 := httptest.NewRecorder()
	h.testNotification(w2, req2)

	if w2.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	var result map[string]string
	json.Unmarshal(w2.Body.Bytes(), &result)
	if result["event_id"] == "" {
		t.Fatal("expected event_id in response")
	}
}

func TestTestNotification_NotFound(t *testing.T) {
	h, _, _ := notifTestHandler(t)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "nonexistent")
	req := httptest.NewRequest("POST", "/api/v1/notifications/nonexistent/test", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	h.testNotification(w, req)

	if w.Code != 404 {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}
