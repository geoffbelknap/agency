package hub

import (
	"context"
	"encoding/json"
	"net/http"

	"log/slog"
	"github.com/go-chi/chi/v5"

	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/credstore"
	"github.com/geoffbelknap/agency/internal/docker"
	"github.com/geoffbelknap/agency/internal/logs"
	"github.com/geoffbelknap/agency/internal/orchestrate"
)

// SignalSender sends OS signals to named containers.
// Defined locally per Go convention: interfaces belong where they are consumed.
type SignalSender interface {
	SignalContainer(ctx context.Context, containerName, signal string) error
}

// Deps holds the dependencies required by the hub module.
type Deps struct {
	CredStore *credstore.Store
	Audit     *logs.Writer
	Config    *config.Config
	Logger    *slog.Logger
	Signal    SignalSender
	// DC is required by deployPack/teardownPack which use orchestrate.NewDeployer.
	DC *docker.Client
	// Agents is required by deployPack to set CredStore on the deployer.
	Agents *orchestrate.AgentManager
}

type handler struct {
	deps Deps
}

// RegisterRoutes mounts all hub, connector, preset, deploy, and teardown routes onto r.
func RegisterRoutes(r chi.Router, d Deps) {
	h := &handler{deps: d}

	// Hub — specific static paths before wildcard {nameOrID}
	r.Post("/api/v1/hub/update", h.hubUpdate)
	r.Get("/api/v1/hub/outdated", h.hubOutdated)
	r.Post("/api/v1/hub/upgrade", h.hubUpgrade)
	r.Get("/api/v1/hub/search", h.hubSearch)
	r.Post("/api/v1/hub/install", h.hubInstall)
	r.Get("/api/v1/hub/installed", h.hubInstalled)
	r.Get("/api/v1/hub/instances", h.hubInstances)
	r.Get("/api/v1/hub/doctor", h.hubDoctor)
	r.Get("/api/v1/intake/poll-health", h.intakePollHealth)
	r.Post("/api/v1/intake/poll/{connector}", h.intakePollTrigger)
	// Wildcard routes after static paths
	r.Get("/api/v1/hub/{nameOrID}", h.hubShow)
	r.Get("/api/v1/hub/{nameOrID}/check", h.hubCheck)
	r.Post("/api/v1/hub/{nameOrID}/activate", h.hubActivate)
	r.Post("/api/v1/hub/{nameOrID}/deactivate", h.hubDeactivate)
	r.Put("/api/v1/hub/{nameOrID}/config", h.hubConfigure)
	r.Delete("/api/v1/hub/{nameOrID}", h.hubRemove)
	// Legacy info route
	r.Get("/api/v1/hub/{name}/info", h.hubInfo)

	// Connector setup — requirements check + credential provisioning
	r.Get("/api/v1/connectors/{name}/requirements", h.connectorRequirements)
	r.Post("/api/v1/connectors/{name}/configure", h.connectorConfigure)

	// Egress domain provenance
	r.Get("/api/v1/egress/domains", h.egressDomains)
	r.Get("/api/v1/egress/domains/{domain}/provenance", h.egressDomainProvenance)

	// Deploy / teardown
	r.Post("/api/v1/deploy", h.deployPack)
	r.Post("/api/v1/teardown/{pack}", h.teardownPack)

	// Presets
	r.Get("/api/v1/presets", h.listPresets)
	r.Post("/api/v1/presets", h.createPreset)
	r.Get("/api/v1/presets/{name}", h.getPreset)
	r.Put("/api/v1/presets/{name}", h.updatePreset)
	r.Delete("/api/v1/presets/{name}", h.deletePreset)
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
