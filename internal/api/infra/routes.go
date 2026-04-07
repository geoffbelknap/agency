package infra

import (
	"encoding/json"
	"net/http"

	"log/slog"
	"github.com/go-chi/chi/v5"

	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/credstore"
	"github.com/geoffbelknap/agency/internal/docker"
	"github.com/geoffbelknap/agency/internal/events"
	"github.com/geoffbelknap/agency/internal/logs"
	"github.com/geoffbelknap/agency/internal/orchestrate"
)

// Deps holds the dependencies required by the infra module.
type Deps struct {
	Infra        *orchestrate.Infra
	DC           *docker.Client
	DockerStatus *docker.Status // may be nil
	CredStore    *credstore.Store
	EventBus     *events.Bus // may be nil
	Config       *config.Config
	Logger       *slog.Logger
	Audit        *logs.Writer
}

type handler struct {
	deps Deps
}

// RegisterRoutes mounts all infrastructure, internal LLM, routing, provider, and
// setup routes onto r.
func RegisterRoutes(r chi.Router, d Deps) {
	h := &handler{deps: d}

	r.Route("/api/v1/infra", func(r chi.Router) {
		r.Get("/status", h.infraStatus)
		r.Post("/up", h.infraUp)
		r.Post("/down", h.infraDown)
		r.Post("/rebuild/{component}", h.infraRebuild)
		r.Post("/reload", h.infraReload)
	})

	r.Post("/api/v1/internal/llm", h.internalLLM)

	r.Get("/api/v1/routing/metrics", h.routingMetrics)
	r.Get("/api/v1/routing/config", h.routingConfig)

	// Routing optimizer
	r.Get("/api/v1/routing/suggestions", h.routingSuggestions)
	r.Post("/api/v1/routing/suggestions/{id}/approve", h.routingSuggestionApprove)
	r.Post("/api/v1/routing/suggestions/{id}/reject", h.routingSuggestionReject)
	r.Get("/api/v1/routing/stats", h.routingStats)

	r.Get("/api/v1/providers", h.listProviders)
	r.Get("/api/v1/setup/config", h.setupConfig)
}

// dockerRequired returns true if Docker is available. If not, writes a 503
// response with a human-readable error and returns false.
func (h *handler) dockerRequired(w http.ResponseWriter) bool {
	if h.deps.DockerStatus != nil && !h.deps.DockerStatus.Available() {
		writeJSON(w, 503, map[string]string{
			"error": "Docker is not available. Container operations are unavailable.",
		})
		return false
	}
	return true
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
