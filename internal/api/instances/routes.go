package instances

import (
	"encoding/json"
	"net/http"
	"path/filepath"

	"log/slog"

	"github.com/geoffbelknap/agency/internal/config"
	instancepkg "github.com/geoffbelknap/agency/internal/instances"
	"github.com/go-chi/chi/v5"
)

// Deps holds the dependencies required by the instances module.
type Deps struct {
	Store  *instancepkg.Store
	Config *config.Config
	Logger *slog.Logger
}

type handler struct {
	deps Deps
}

// RegisterRoutes mounts V2 instance routes onto r.
func RegisterRoutes(r chi.Router, d Deps) {
	h := &handler{deps: d}

	r.Get("/api/v1/instances", h.listInstances)
	r.Post("/api/v1/instances", h.createInstance)
	r.Get("/api/v1/instances/{id}", h.showInstance)
	r.Post("/api/v1/instances/{id}/validate", h.validateInstance)
	r.Post("/api/v1/instances/{id}/claim", h.claimInstance)
	r.Post("/api/v1/instances/{id}/release", h.releaseInstance)
	r.Get("/api/v1/instances/{id}/runtime/manifest", h.showRuntimeManifest)
	r.Post("/api/v1/instances/{id}/runtime/manifest", h.compileRuntimeManifest)
	r.Post("/api/v1/instances/{id}/runtime/reconcile", h.reconcileRuntime)
	r.Get("/api/v1/instances/{id}/runtime/nodes/{nodeID}", h.runtimeNodeStatus)
	r.Post("/api/v1/instances/{id}/runtime/nodes/{nodeID}/start", h.startRuntimeNode)
	r.Post("/api/v1/instances/{id}/runtime/nodes/{nodeID}/stop", h.stopRuntimeNode)
}

func (h *handler) store() *instancepkg.Store {
	if h.deps.Store != nil {
		return h.deps.Store
	}
	if h.deps.Config == nil {
		return nil
	}
	return instancepkg.NewStore(filepath.Join(h.deps.Config.Home, "instances"))
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}
