package hub

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/charmbracelet/log"
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
	Logger    *log.Logger
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

	r.Route("/api/v1", func(r chi.Router) {
		// Hub — specific static paths before wildcard {nameOrID}
		r.Post("/hub/update", h.hubUpdate)
		r.Get("/hub/outdated", h.hubOutdated)
		r.Post("/hub/upgrade", h.hubUpgrade)
		r.Get("/hub/search", h.hubSearch)
		r.Post("/hub/install", h.hubInstall)
		r.Get("/hub/installed", h.hubInstalled)
		r.Get("/hub/instances", h.hubInstances)
		r.Get("/hub/doctor", h.hubDoctor)
		r.Get("/intake/poll-health", h.intakePollHealth)
		r.Post("/intake/poll/{connector}", h.intakePollTrigger)
		// Wildcard routes after static paths
		r.Get("/hub/{nameOrID}", h.hubShow)
		r.Get("/hub/{nameOrID}/check", h.hubCheck)
		r.Post("/hub/{nameOrID}/activate", h.hubActivate)
		r.Post("/hub/{nameOrID}/deactivate", h.hubDeactivate)
		r.Put("/hub/{nameOrID}/config", h.hubConfigure)
		r.Delete("/hub/{nameOrID}", h.hubRemove)
		// Legacy info route
		r.Get("/hub/{name}/info", h.hubInfo)

		// Connector setup — requirements check + credential provisioning
		r.Get("/connectors/{name}/requirements", h.connectorRequirements)
		r.Post("/connectors/{name}/configure", h.connectorConfigure)

		// Egress domain provenance
		r.Get("/egress/domains", h.egressDomains)
		r.Get("/egress/domains/{domain}/provenance", h.egressDomainProvenance)

		// Deploy / teardown
		r.Post("/deploy", h.deployPack)
		r.Post("/teardown/{pack}", h.teardownPack)

		// Presets
		r.Get("/presets", h.listPresets)
		r.Post("/presets", h.createPreset)
		r.Get("/presets/{name}", h.getPreset)
		r.Put("/presets/{name}", h.updatePreset)
		r.Delete("/presets/{name}", h.deletePreset)
	})
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
