package infra

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"log/slog"

	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/credstore"
	"github.com/geoffbelknap/agency/internal/events"
	"github.com/geoffbelknap/agency/internal/features"
	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
	"github.com/geoffbelknap/agency/internal/logs"
	"github.com/geoffbelknap/agency/internal/orchestrate"
)

// Deps holds the dependencies required by the infra module.
type Deps struct {
	Infra        *orchestrate.Infra
	DC           *runtimehost.Client
	DockerStatus *runtimehost.Status // may be nil
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
		r.Get("/capacity", h.infraCapacity)
		r.Get("/services/{component}/logs", h.infraLogs)
	})

	r.Post("/api/v1/infra/internal/llm", h.internalLLM)

	r.Get("/api/v1/infra/routing/metrics", h.routingMetrics)
	r.Get("/api/v1/infra/routing/config", h.routingConfig)

	if features.ExperimentalEnabled() {
		// Routing optimizer
		r.Get("/api/v1/infra/routing/suggestions", h.routingSuggestions)
		r.Post("/api/v1/infra/routing/suggestions/{id}/approve", h.routingSuggestionApprove)
		r.Post("/api/v1/infra/routing/suggestions/{id}/reject", h.routingSuggestionReject)
		r.Get("/api/v1/infra/routing/stats", h.routingStats)
	}

	r.Get("/api/v1/infra/providers", h.listProviders)
	r.Get("/api/v1/infra/provider-tools", h.providerTools)
	r.Post("/api/v1/infra/providers/{name}/install", h.installProvider)
	r.Get("/api/v1/infra/setup/config", h.setupConfig)
}

func (h *handler) configuredBackend() string {
	if h.deps.Config != nil && strings.TrimSpace(h.deps.Config.Hub.DeploymentBackend) != "" {
		return strings.TrimSpace(h.deps.Config.Hub.DeploymentBackend)
	}
	return runtimehost.BackendDocker
}

// containerBackendRequired returns true when container-backed infra control is available.
func (h *handler) containerBackendRequired(w http.ResponseWriter) bool {
	backend := h.configuredBackend()
	if !runtimehost.IsContainerBackend(backend) {
		writeJSON(w, 503, map[string]string{
			"error":   fmt.Sprintf("infrastructure container lifecycle is only available for container backends (current: %s)", backend),
			"backend": backend,
		})
		return false
	}
	if h.deps.DC == nil {
		writeJSON(w, 503, map[string]string{
			"error": fmt.Sprintf("%s infrastructure client is not initialized.", runtimehost.NormalizeContainerBackend(backend)),
		})
		return false
	}
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
