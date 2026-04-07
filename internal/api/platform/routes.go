package platform

import (
	"encoding/json"
	"net/http"

	"github.com/charmbracelet/log"
	"github.com/go-chi/chi/v5"

	"github.com/geoffbelknap/agency/internal/audit"
	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/credstore"
	"github.com/geoffbelknap/agency/internal/ws"
)

// Deps holds the dependencies required by the platform module.
type Deps struct {
	WSHub           *ws.Hub
	AuditSummarizer *audit.AuditSummarizer
	CredStore       *credstore.Store
	Config          *config.Config
	Logger          *log.Logger
}

type handler struct {
	deps Deps
}

// RegisterRoutes mounts all platform-level routes onto r.
// These are: OpenAPI spec, health, init, websocket (conditional), audit
// summarization (conditional), and the /__agency/config config endpoint.
//
// The cfg argument is used directly for the /__agency/config inline handler
// which must be registered outside BearerAuth middleware.
func RegisterRoutes(r chi.Router, d Deps) {
	h := &handler{deps: d}

	// WebSocket endpoint — at root /ws per spec, outside /api/v1.
	if d.WSHub != nil {
		r.Get("/ws", h.handleWebSocket)
	}

	// Web UI config endpoint — excluded from BearerAuth so the web UI can
	// get the token before it can authenticate anything else.
	// Only reachable on localhost (gateway binds to 127.0.0.1 by default).
	r.Get("/__agency/config", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
			"token":   d.Config.Token,
			"gateway": "",
		})
	})

	// OpenAPI spec
	r.Get("/api/v1/openapi.yaml", h.openapiSpec)

	// Health
	r.Get("/api/v1/health", h.health)

	// Init
	r.Post("/api/v1/init", h.initPlatform)

	// Audit summarization (optional dependency)
	if d.AuditSummarizer != nil {
		summarizer := d.AuditSummarizer
		r.Post("/api/v1/audit/summarize", func(w http.ResponseWriter, r *http.Request) {
			metrics, err := summarizer.Summarize()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
				"metrics": metrics,
				"count":   len(metrics),
			})
		})
	}
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}
