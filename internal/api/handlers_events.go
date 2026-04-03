package api

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/events"
	"github.com/geoffbelknap/agency/internal/models"
)

// ── Event observability ──────────────────────────────────────────────────────

// listEvents handles GET /api/v1/events
func (h *handler) listEvents(w http.ResponseWriter, r *http.Request) {
	if h.eventBus == nil {
		writeJSON(w, 503, map[string]string{"error": "event bus not initialized"})
		return
	}

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}

	sourceType := r.URL.Query().Get("source_type")
	sourceName := r.URL.Query().Get("source_name")
	eventType := r.URL.Query().Get("event_type")

	var events []*models.Event
	if sourceType != "" || sourceName != "" || eventType != "" {
		events = h.eventBus.Events().ListFiltered(sourceType, sourceName, eventType, limit)
	} else {
		events = h.eventBus.Events().List(limit)
	}

	if events == nil {
		events = make([]*models.Event, 0)
	}
	writeJSON(w, 200, events)
}

// showEvent handles GET /api/v1/events/{id}
func (h *handler) showEvent(w http.ResponseWriter, r *http.Request) {
	if h.eventBus == nil {
		writeJSON(w, 503, map[string]string{"error": "event bus not initialized"})
		return
	}

	id := chi.URLParam(r, "id")
	event := h.eventBus.Events().Get(id)
	if event == nil {
		writeJSON(w, 404, map[string]string{"error": "event not found"})
		return
	}
	writeJSON(w, 200, event)
}

// listSubscriptions handles GET /api/v1/subscriptions
func (h *handler) listSubscriptions(w http.ResponseWriter, r *http.Request) {
	if h.eventBus == nil {
		writeJSON(w, 503, map[string]string{"error": "event bus not initialized"})
		return
	}

	subs := h.eventBus.Subscriptions().List()
	if subs == nil {
		subs = make([]*events.Subscription, 0)
	}
	writeJSON(w, 200, subs)
}

// ── Webhook CRUD ─────────────────────────────────────────────────────────────

// createWebhook handles POST /api/v1/webhooks
func (h *handler) createWebhook(w http.ResponseWriter, r *http.Request) {
	if h.webhookMgr == nil {
		writeJSON(w, 503, map[string]string{"error": "webhook manager not initialized"})
		return
	}

	var body struct {
		Name      string `json:"name"`
		EventType string `json:"event_type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	if _, ok := requireName(w, body.Name); !ok {
		return
	}
	if body.EventType == "" {
		writeJSON(w, 400, map[string]string{"error": "event_type required"})
		return
	}

	wh, err := h.webhookMgr.Create(body.Name, body.EventType)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}

	h.audit.Write("_system", "webhook_created", map[string]interface{}{
		"webhook_name": wh.Name,
		"event_type":   wh.EventType,
	})

	writeJSON(w, 201, wh)
}

// listWebhooks handles GET /api/v1/webhooks
func (h *handler) listWebhooks(w http.ResponseWriter, r *http.Request) {
	if h.webhookMgr == nil {
		writeJSON(w, 503, map[string]string{"error": "webhook manager not initialized"})
		return
	}

	webhooks, err := h.webhookMgr.List()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if webhooks == nil {
		webhooks = make([]*models.Webhook, 0)
	}

	// Redact secrets in list response
	type safeWebhook struct {
		Name      string `json:"name"`
		EventType string `json:"event_type"`
		CreatedAt string `json:"created_at"`
		URL       string `json:"url"`
	}
	var safe []safeWebhook
	for _, wh := range webhooks {
		safe = append(safe, safeWebhook{
			Name:      wh.Name,
			EventType: wh.EventType,
			CreatedAt: wh.CreatedAt.Format(time.RFC3339),
			URL:       wh.URL,
		})
	}
	writeJSON(w, 200, safe)
}

// showWebhook handles GET /api/v1/webhooks/{name}
func (h *handler) showWebhook(w http.ResponseWriter, r *http.Request) {
	if h.webhookMgr == nil {
		writeJSON(w, 503, map[string]string{"error": "webhook manager not initialized"})
		return
	}

	name, ok := requireName(w, chi.URLParam(r, "name"))
	if !ok {
		return
	}
	wh, err := h.webhookMgr.Get(name)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, wh)
}

// deleteWebhook handles DELETE /api/v1/webhooks/{name}
func (h *handler) deleteWebhook(w http.ResponseWriter, r *http.Request) {
	if h.webhookMgr == nil {
		writeJSON(w, 503, map[string]string{"error": "webhook manager not initialized"})
		return
	}

	name, ok := requireName(w, chi.URLParam(r, "name"))
	if !ok {
		return
	}
	if err := h.webhookMgr.Delete(name); err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}

	h.audit.Write("_system", "webhook_deleted", map[string]interface{}{
		"webhook_name": name,
	})

	writeJSON(w, 200, map[string]string{"status": "deleted", "name": name})
}

// rotateWebhookSecret handles POST /api/v1/webhooks/{name}/rotate-secret
func (h *handler) rotateWebhookSecret(w http.ResponseWriter, r *http.Request) {
	if h.webhookMgr == nil {
		writeJSON(w, 503, map[string]string{"error": "webhook manager not initialized"})
		return
	}

	name, ok := requireName(w, chi.URLParam(r, "name"))
	if !ok {
		return
	}
	wh, err := h.webhookMgr.RotateSecret(name)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}

	h.audit.Write("_system", "webhook_secret_rotated", map[string]interface{}{
		"webhook_name": name,
	})

	writeJSON(w, 200, wh)
}

// ── Inbound webhook receiver ─────────────────────────────────────────────────

// webhookRateLimiter provides simple per-name rate limiting.
type webhookRateLimiter struct {
	mu      sync.Mutex
	buckets map[string][]time.Time
}

func newWebhookRateLimiter() *webhookRateLimiter {
	return &webhookRateLimiter{
		buckets: make(map[string][]time.Time),
	}
}

// Allow returns true if the webhook name has not exceeded 60 req/min.
func (rl *webhookRateLimiter) Allow(name string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-1 * time.Minute)

	// Clean old entries
	times := rl.buckets[name]
	var fresh []time.Time
	for _, t := range times {
		if t.After(cutoff) {
			fresh = append(fresh, t)
		}
	}

	if len(fresh) >= 60 {
		rl.buckets[name] = fresh
		return false
	}

	fresh = append(fresh, now)
	rl.buckets[name] = fresh
	return true
}

// receiveWebhook handles POST /api/v1/events/webhook/{name}
func (h *handler) receiveWebhook(w http.ResponseWriter, r *http.Request) {
	if h.webhookMgr == nil || h.eventBus == nil {
		writeJSON(w, 503, map[string]string{"error": "webhook system not initialized"})
		return
	}

	name, ok := requireName(w, chi.URLParam(r, "name"))
	if !ok {
		return
	}

	// Look up registered webhook
	wh, err := h.webhookMgr.Get(name)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "webhook not found"})
		return
	}

	// Rate limit
	if !h.webhookRL.Allow(name) {
		writeJSON(w, 429, map[string]string{"error": "rate limit exceeded (60/min)"})
		return
	}

	// Validate secret (constant-time comparison)
	secret := r.Header.Get("X-Webhook-Secret")
	if subtle.ConstantTimeCompare([]byte(secret), []byte(wh.Secret)) != 1 {
		writeJSON(w, 401, map[string]string{"error": "unauthorized"})
		return
	}

	// Parse body
	var data map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		data = make(map[string]interface{})
	}

	// Create event and publish to bus
	event := models.NewEvent(models.EventSourceWebhook, name, wh.EventType, data)
	h.eventBus.Publish(event)

	writeJSON(w, 202, map[string]string{"event_id": event.ID, "status": "accepted"})
}

// ── Notification CRUD ───────────────────────────────────────────────────────

func detectNotificationType(url string) string {
	lower := strings.ToLower(url)
	if strings.Contains(lower, "ntfy.") || strings.Contains(lower, "ntfy.sh") {
		return "ntfy"
	}
	return "webhook"
}

var defaultNotificationEvents = []string{"operator_alert", "enforcer_exited", "mission_health_alert"}

// listNotifications handles GET /api/v1/notifications
func (h *handler) listNotifications(w http.ResponseWriter, r *http.Request) {
	if h.notifStore == nil {
		writeJSON(w, 503, map[string]string{"error": "notification store not initialized"})
		return
	}

	configs := h.notifStore.List()
	type safeConfig struct {
		Name   string   `json:"name"`
		Type   string   `json:"type"`
		URL    string   `json:"url"`
		Events []string `json:"events"`
	}
	safe := make([]safeConfig, len(configs))
	for i, nc := range configs {
		safe[i] = safeConfig{Name: nc.Name, Type: nc.Type, URL: nc.URL, Events: nc.Events}
	}
	writeJSON(w, 200, safe)
}

// showNotification handles GET /api/v1/notifications/{name}
func (h *handler) showNotification(w http.ResponseWriter, r *http.Request) {
	if h.notifStore == nil {
		writeJSON(w, 503, map[string]string{"error": "notification store not initialized"})
		return
	}

	name, ok := requireName(w, chi.URLParam(r, "name"))
	if !ok {
		return
	}
	nc, err := h.notifStore.Get(name)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}

	type safeConfig struct {
		Name   string   `json:"name"`
		Type   string   `json:"type"`
		URL    string   `json:"url"`
		Events []string `json:"events"`
	}
	writeJSON(w, 200, safeConfig{Name: nc.Name, Type: nc.Type, URL: nc.URL, Events: nc.Events})
}

// addNotification handles POST /api/v1/notifications
func (h *handler) addNotification(w http.ResponseWriter, r *http.Request) {
	if h.notifStore == nil {
		writeJSON(w, 503, map[string]string{"error": "notification store not initialized"})
		return
	}

	var body struct {
		Name    string            `json:"name"`
		Type    string            `json:"type"`
		URL     string            `json:"url"`
		Events  []string          `json:"events"`
		Headers map[string]string `json:"headers"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	if body.Name == "" || body.URL == "" {
		writeJSON(w, 400, map[string]string{"error": "name and url are required"})
		return
	}
	if err := validateOutboundURL(body.URL); err != nil {
		writeJSON(w, 400, map[string]string{"error": fmt.Sprintf("invalid notification URL: %s", err)})
		return
	}

	if body.Type == "" {
		body.Type = detectNotificationType(body.URL)
	}

	if len(body.Events) == 0 {
		body.Events = defaultNotificationEvents
	}

	nc := config.NotificationConfig{
		Name:    body.Name,
		Type:    body.Type,
		URL:     body.URL,
		Events:  body.Events,
		Headers: body.Headers,
	}

	if err := h.notifStore.Add(nc); err != nil {
		if strings.Contains(err.Error(), "already exists") {
			writeJSON(w, 409, map[string]string{"error": err.Error()})
		} else {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
		}
		return
	}

	// Hot-reload: add subscriptions to event bus
	if h.eventBus != nil {
		subs := events.BuildNotificationSubscriptions([]config.NotificationConfig{nc})
		for _, sub := range subs {
			h.eventBus.Subscriptions().Add(sub)
		}
	}

	writeJSON(w, 201, nc)
}

// deleteNotification handles DELETE /api/v1/notifications/{name}
func (h *handler) deleteNotification(w http.ResponseWriter, r *http.Request) {
	if h.notifStore == nil {
		writeJSON(w, 503, map[string]string{"error": "notification store not initialized"})
		return
	}

	name, ok := requireName(w, chi.URLParam(r, "name"))
	if !ok {
		return
	}
	if err := h.notifStore.Remove(name); err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}

	// Hot-reload: remove subscriptions from event bus
	if h.eventBus != nil {
		h.eventBus.Subscriptions().RemoveByOrigin(events.OriginNotification, name)
	}

	writeJSON(w, 200, map[string]string{"status": "deleted", "name": name})
}

// testNotification handles POST /api/v1/notifications/{name}/test
func (h *handler) testNotification(w http.ResponseWriter, r *http.Request) {
	if h.notifStore == nil {
		writeJSON(w, 503, map[string]string{"error": "notification store not initialized"})
		return
	}

	name, ok := requireName(w, chi.URLParam(r, "name"))
	if !ok {
		return
	}
	if _, err := h.notifStore.Get(name); err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}

	if h.eventBus == nil {
		writeJSON(w, 503, map[string]string{"error": "event bus not initialized"})
		return
	}

	event := models.NewEvent(models.EventSourcePlatform, "gateway", "operator_alert", map[string]interface{}{
		"category": "test",
		"severity": "info",
		"message":  "Test notification from agency",
	})
	h.eventBus.Publish(event)

	writeJSON(w, 200, map[string]string{"event_id": event.ID, "status": "sent"})
}

