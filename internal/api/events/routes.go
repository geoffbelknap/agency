package events

import (
	"encoding/json"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/geoffbelknap/agency/internal/events"
	"github.com/geoffbelknap/agency/internal/logs"
)

// Deps holds the dependencies required by the events module.
type Deps struct {
	EventBus   *events.Bus
	WebhookMgr *events.WebhookManager
	Scheduler  *events.Scheduler
	NotifStore *events.NotificationStore
	Audit      *logs.Writer
}

type handler struct {
	deps      Deps
	webhookRL *webhookRateLimiter
}

// RegisterRoutes mounts all event, webhook, notification, and subscription
// routes onto r. Should only be called when EventBus is non-nil.
func RegisterRoutes(r chi.Router, d Deps) {
	h := &handler{deps: d}
	if d.WebhookMgr != nil {
		h.webhookRL = newWebhookRateLimiter()
	}

	// Events
	r.Get("/api/v1/events", h.listEvents)
	r.Get("/api/v1/events/{id}", h.showEvent)
	r.Get("/api/v1/subscriptions", h.listSubscriptions)

	// Webhooks
	r.Post("/api/v1/webhooks", h.createWebhook)
	r.Get("/api/v1/webhooks", h.listWebhooks)
	r.Get("/api/v1/webhooks/{name}", h.showWebhook)
	r.Delete("/api/v1/webhooks/{name}", h.deleteWebhook)
	r.Post("/api/v1/webhooks/{name}/rotate-secret", h.rotateWebhookSecret)

	// Inbound webhook receiver
	r.Post("/api/v1/events/webhook/{name}", h.receiveWebhook)

	// Intake proxy
	r.Get("/api/v1/intake/items", h.intakeItems)
	r.Get("/api/v1/intake/stats", h.intakeStats)
	r.Post("/api/v1/intake/webhook", h.intakeWebhook)

	// Notifications
	r.Get("/api/v1/notifications", h.listNotifications)
	r.Post("/api/v1/notifications", h.addNotification)
	r.Get("/api/v1/notifications/{name}", h.showNotification)
	r.Delete("/api/v1/notifications/{name}", h.deleteNotification)
	r.Post("/api/v1/notifications/{name}/test", h.testNotification)
}

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

// validResourceName matches lowercase alphanumeric names with hyphens, 1-64 chars.
var validResourceName = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$`)

// requireName validates a user-supplied resource name from a URL param or body.
// Returns the name and true if valid, or writes a 400 response and returns ("", false).
func requireName(w http.ResponseWriter, raw string) (string, bool) {
	if !validResourceName.MatchString(raw) {
		writeJSON(w, 400, map[string]string{"error": "invalid name"})
		return "", false
	}
	return raw, true
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
