package instances

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"

	"log/slog"

	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/events"
	"github.com/geoffbelknap/agency/internal/hub"
	instancepkg "github.com/geoffbelknap/agency/internal/instances"
	runpkg "github.com/geoffbelknap/agency/internal/runtime"
	"github.com/go-chi/chi/v5"
)

// Deps holds the dependencies required by the instances module.
type Deps struct {
	Store          *instancepkg.Store
	Registry       *hub.Registry
	Config         *config.Config
	Logger         *slog.Logger
	RuntimeManager runtimeManager
	Signal         signalSender
	EventBus       *events.Bus
}

type handler struct {
	deps Deps
}

type runtimeManager interface {
	Status(store *runpkg.Store, manifest *runpkg.Manifest, nodeID string) (*runpkg.NodeStatus, error)
	StartAuthority(store *runpkg.Store, manifest *runpkg.Manifest, nodeID string) (*runpkg.NodeStatus, error)
	StopAuthority(store *runpkg.Store, manifest *runpkg.Manifest, nodeID string) (*runpkg.NodeStatus, error)
}

type signalSender interface {
	SignalContainer(ctx context.Context, containerName, sig string) error
}

// RegisterRoutes mounts V2 instance routes onto r.
func RegisterRoutes(r chi.Router, d Deps) {
	h := &handler{deps: d}

	r.Get("/api/v1/instances", h.listInstances)
	r.Post("/api/v1/instances", h.createInstance)
	r.Post("/api/v1/instances/from-package", h.createInstanceFromPackage)
	r.Get("/api/v1/instances/{id}", h.showInstance)
	r.Patch("/api/v1/instances/{id}", h.updateInstance)
	r.Post("/api/v1/instances/{id}/validate", h.validateInstance)
	r.Post("/api/v1/instances/{id}/apply", h.applyInstance)
	r.Post("/api/v1/instances/{id}/claim", h.claimInstance)
	r.Post("/api/v1/instances/{id}/release", h.releaseInstance)
	r.Get("/api/v1/instances/{id}/runtime/manifest", h.showRuntimeManifest)
	r.Post("/api/v1/instances/{id}/runtime/manifest", h.compileRuntimeManifest)
	r.Post("/api/v1/instances/{id}/runtime/reconcile", h.reconcileRuntime)
	r.Get("/api/v1/instances/{id}/runtime/nodes/{nodeID}", h.runtimeNodeStatus)
	r.Post("/api/v1/instances/{id}/runtime/nodes/{nodeID}/start", h.startRuntimeNode)
	r.Post("/api/v1/instances/{id}/runtime/nodes/{nodeID}/stop", h.stopRuntimeNode)
	r.Post("/api/v1/instances/{id}/runtime/nodes/{nodeID}/invoke", h.invokeRuntimeNode)
	r.Post("/api/v1/instances/{id}/runtime/nodes/{nodeID}/actions/{action}", h.invokeRuntimeAction)
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

func (h *handler) runtimeManager() runtimeManager {
	if h.deps.RuntimeManager != nil {
		return h.deps.RuntimeManager
	}
	return runpkg.Manager{}
}

func (h *handler) packageRegistry() *hub.Registry {
	if h.deps.Registry != nil {
		return h.deps.Registry
	}
	if h.deps.Config == nil {
		return nil
	}
	return hub.NewManager(h.deps.Config.Home).Registry
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}
