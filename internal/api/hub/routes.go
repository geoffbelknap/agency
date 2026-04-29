package hub

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"log/slog"

	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/credstore"
	"github.com/geoffbelknap/agency/internal/hostadapter"
	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
	"github.com/geoffbelknap/agency/internal/logs"
	"github.com/geoffbelknap/agency/internal/orchestrate"
)

type SignalSender interface {
	SignalRuntimeName(ctx context.Context, name, signal string) error
}

// Deps holds the dependencies required by the hub module.
type Deps struct {
	CredStore *credstore.Store
	Audit     *logs.Writer
	Config    *config.Config
	Logger    *slog.Logger
	Signal    SignalSender
	Host      hostadapter.Adapter
	Runtime   *runtimehost.Client
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
	r.Get("/api/v1/hub/deployments", h.deploymentList)
	r.Post("/api/v1/hub/deployments", h.deploymentCreate)
	r.Post("/api/v1/hub/deployments/import", h.deploymentImport)
	r.Get("/api/v1/hub/deployments/schema/{pack}", h.deploymentSchema)
	r.Get("/api/v1/hub/deployments/{nameOrID}", h.deploymentShow)
	r.Put("/api/v1/hub/deployments/{nameOrID}/config", h.deploymentConfigure)
	r.Post("/api/v1/hub/deployments/{nameOrID}/validate", h.deploymentValidate)
	r.Post("/api/v1/hub/deployments/{nameOrID}/apply", h.deploymentApply)
	r.Get("/api/v1/hub/deployments/{nameOrID}/export", h.deploymentExport)
	r.Post("/api/v1/hub/deployments/{nameOrID}/claim", h.deploymentClaim)
	r.Post("/api/v1/hub/deployments/{nameOrID}/release", h.deploymentRelease)
	r.Delete("/api/v1/hub/deployments/{nameOrID}", h.deploymentDestroy)
	r.Get("/api/v1/hub/doctor", h.hubDoctor)
	r.Get("/api/v1/hub/intake/poll-health", h.intakePollHealth)
	r.Post("/api/v1/hub/intake/poll/{connector}", h.intakePollTrigger)
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
	r.Get("/api/v1/hub/connectors/{name}/requirements", h.connectorRequirements)
	r.Post("/api/v1/hub/connectors/{name}/configure", h.connectorConfigure)

	// Egress domain provenance
	r.Get("/api/v1/hub/egress/domains", h.egressDomains)
	r.Get("/api/v1/hub/egress/domains/{domain}/provenance", h.egressDomainProvenance)

	// Deploy / teardown
	r.Post("/api/v1/hub/deploy", h.deployPack)
	r.Post("/api/v1/hub/teardown/{pack}", h.teardownPack)

	// Presets
	r.Get("/api/v1/hub/presets", h.listPresets)
	r.Post("/api/v1/hub/presets", h.createPreset)
	r.Get("/api/v1/hub/presets/{name}", h.getPreset)
	r.Put("/api/v1/hub/presets/{name}", h.updatePreset)
	r.Delete("/api/v1/hub/presets/{name}", h.deletePreset)
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
