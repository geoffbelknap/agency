package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/charmbracelet/log"
	"github.com/go-chi/chi/v5"

	"github.com/geoffbelknap/agency/internal/audit"
	agencyctx "github.com/geoffbelknap/agency/internal/context"
	apiadmin "github.com/geoffbelknap/agency/internal/api/admin"
	apiagents "github.com/geoffbelknap/agency/internal/api/agents"
	"github.com/geoffbelknap/agency/internal/api/creds"
	apicomms "github.com/geoffbelknap/agency/internal/api/comms"
	apievents "github.com/geoffbelknap/agency/internal/api/events"
	"github.com/geoffbelknap/agency/internal/api/graph"
	apihub "github.com/geoffbelknap/agency/internal/api/hub"
	apiinfra "github.com/geoffbelknap/agency/internal/api/infra"
	apimissions "github.com/geoffbelknap/agency/internal/api/missions"
	"github.com/geoffbelknap/agency/internal/api/platform"
	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/credstore"
	"github.com/geoffbelknap/agency/internal/docker"
	"github.com/geoffbelknap/agency/internal/profiles"
	"github.com/geoffbelknap/agency/internal/events"
	"github.com/geoffbelknap/agency/internal/knowledge"
	"github.com/geoffbelknap/agency/internal/logs"
	"github.com/geoffbelknap/agency/internal/orchestrate"

	"github.com/geoffbelknap/agency/internal/registry"
	"github.com/geoffbelknap/agency/internal/routing"
	"github.com/geoffbelknap/agency/internal/ws"
)

// RouteOptions holds optional dependencies for route registration.
type RouteOptions struct {
	Hub           *ws.Hub
	EventBus      *events.Bus
	Scheduler     *events.Scheduler
	WebhookMgr    *events.WebhookManager
	HealthMonitor *orchestrate.MissionHealthMonitor
	NotifStore    *events.NotificationStore
	StopSuppress    *orchestrate.StopSuppression
	AuditSummarizer *audit.AuditSummarizer
	DockerStatus    *docker.Status
	Registry        *registry.Registry
	Optimizer       *routing.RoutingOptimizer
}

// RegisterSocketRoutes sets up the restricted API surface for the Unix socket.
// Only endpoints needed by infra containers are registered — no BearerAuth middleware.
// Each infra-facing endpoint has its own auth mechanism (X-Agency-Token / X-Agency-Caller)
// or is read-only health/status data.
func RegisterSocketRoutes(r chi.Router, cfg *config.Config, dc *docker.Client, logger *log.Logger, startup *StartupResult, opts RouteOptions) {
	r.Get("/api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"status": "ok", "version": cfg.Version, "build_id": cfg.BuildID})
	})

	// Agent signal relay for the socket — used by infra containers to relay
	// body-originated signals to the WebSocket hub. Register via agents module.
	apiagents.RegisterRoutes(r, apiagents.Deps{
		AgentManager:    startup.AgentManager,
		HaltController:  startup.HaltController,
		CtxMgr:          startup.CtxMgr,
		Audit:           startup.Audit,
		EventBus:        opts.EventBus,
		MeeseeksManager: startup.MeeseeksManager,
		Knowledge:       startup.Knowledge,
		MissionManager:  startup.MissionManager,
		Config:          cfg,
		Logger:          logger,
		CredStore:       startup.CredStore,
		DockerStatus:    opts.DockerStatus,
		WSHub:           opts.Hub,
		Comms:           dc,
		Signal:          &DockerSignalSender{RawClient: dc.RawClient()},
		DC:              dc,
		RawDocker:       dc,
	})

	// Infra routes on the socket (subset: status + internal LLM only)
	apiinfra.RegisterRoutes(r, apiinfra.Deps{
		Infra:        startup.Infra,
		DC:           dc,
		DockerStatus: opts.DockerStatus,
		CredStore:    startup.CredStore,
		Config:       cfg,
		Logger:       logger,
		Audit:        startup.Audit,
	})

	apicomms.RegisterRoutes(r, apicomms.Deps{
		Comms:  dc,
		Config: cfg,
		Logger: logger,
	})
}

// RegisterCredentialSocketRoutes registers the credential-only socket router.
// This socket is mounted exclusively by the egress container for credential
// resolution. It is NOT bridged to TCP — credentials never traverse a Docker network.
func RegisterCredentialSocketRoutes(r chi.Router, cfg *config.Config, dc *docker.Client, logger *log.Logger, startup *StartupResult, opts RouteOptions) {
	if startup.CredStore != nil {
		creds.RegisterRoutes(r, creds.Deps{
			CredStore: startup.CredStore,
			Audit:     startup.Audit,
			Config:    cfg,
			Logger:    logger,
		})
	}
}

// RegisterAll sets up all REST API routes with full option support.
// This is the canonical registration entry point for the full HTTP API surface.
func RegisterAll(r chi.Router, cfg *config.Config, dc *docker.Client, logger *log.Logger, startup *StartupResult, opts RouteOptions) {
	h := &handler{
		cfg: cfg, dc: dc, log: logger,
		infra: startup.Infra, agents: startup.AgentManager,
		halt: startup.HaltController, audit: startup.Audit,
		ctxMgr: startup.CtxMgr, mcpReg: startup.MCPReg,
		knowledge: startup.Knowledge, missions: startup.MissionManager,
		meeseeks: startup.MeeseeksManager, claims: startup.Claims,
		credStore: startup.CredStore, profileStore: startup.ProfileStore,
	}

	// Wire event framework components
	if opts.EventBus != nil {
		h.eventBus = opts.EventBus
	}
	if opts.WebhookMgr != nil {
		h.webhookMgr = opts.WebhookMgr
	}
	if opts.HealthMonitor != nil {
		h.healthMonitor = opts.HealthMonitor
	}
	if opts.NotifStore != nil {
		h.notifStore = opts.NotifStore
	}
	if opts.StopSuppress != nil && h.agents != nil {
		h.agents.StopSuppress = opts.StopSuppress
	}

	if opts.Optimizer != nil && h.infra != nil {
		h.infra.Optimizer = opts.Optimizer
	}

	// Permission enforcement middleware — runs after BearerAuth has resolved
	// the principal into the request context. If no registry is available,
	// the middleware is not applied (backward compatible).
	// ASK Tenet 7: least privilege — route-level permission checks.
	if opts.Registry != nil {
		r.Use(PermissionMiddleware(opts.Registry))
	}

	// Platform routes (extracted module) — openapi, health, init, websocket,
	// audit summarization, and the /__agency/config config endpoint.
	platform.RegisterRoutes(r, platform.Deps{
		WSHub:           opts.Hub,
		AuditSummarizer: opts.AuditSummarizer,
		CredStore:       startup.CredStore,
		Config:          cfg,
		Logger:          logger,
	})

	// Agents module — lifecycle, config, grants, budget, cache, economics,
	// trajectory, meeseeks, context, and memory routes.
	apiagents.RegisterRoutes(r, apiagents.Deps{
		AgentManager:    startup.AgentManager,
		HaltController:  startup.HaltController,
		CtxMgr:          startup.CtxMgr,
		Audit:           startup.Audit,
		EventBus:        opts.EventBus,
		MeeseeksManager: startup.MeeseeksManager,
		Knowledge:       startup.Knowledge,
		MissionManager:  startup.MissionManager,
		Claims:          startup.Claims,
		HealthMonitor:   opts.HealthMonitor,
		Scheduler:       opts.Scheduler,
		Config:          cfg,
		Logger:          logger,
		CredStore:       startup.CredStore,
		DockerStatus:    opts.DockerStatus,
		WSHub:           opts.Hub,
		Comms:           dc,
		Signal:          &DockerSignalSender{RawClient: dc.RawClient()},
		DC:              dc,
		RawDocker:       dc,
	})

	r.Route("/api/v1", func(r chi.Router) {
		// Agent logs (still in api package — depends on logs.Reader)
		r.Get("/agents/{name}/logs", h.agentLogs)

		// New routes added after modularization — these remain on the
		// monolithic handler temporarily until moved to their modules.
		r.Post("/knowledge/ingest", h.knowledgeIngest)
		r.Post("/knowledge/insight", h.knowledgeSaveInsight)
		r.Get("/knowledge/principals", h.knowledgePrincipalsList)
		r.Post("/knowledge/principals", h.knowledgePrincipalsRegister)
		r.Get("/knowledge/principals/{uuid}", h.knowledgePrincipalsResolve)
		r.Post("/knowledge/quarantine", h.knowledgeQuarantine)
		r.Post("/knowledge/quarantine/release", h.knowledgeQuarantineRelease)
		r.Get("/knowledge/quarantine", h.knowledgeQuarantineList)
		r.Get("/knowledge/classification", h.knowledgeClassification)
		r.Get("/knowledge/communities", h.knowledgeCommunities)
		r.Get("/knowledge/communities/{id}", h.knowledgeCommunity)
		r.Get("/knowledge/hubs", h.knowledgeHubs)

		// Principal registry
		r.Get("/registry", h.registrySnapshot)
		r.Get("/registry/resolve", h.registryResolve)
		r.Get("/registry/list", h.registryList)
		r.Post("/registry", h.registryRegister)
		r.Get("/registry/{uuid}/effective", h.registryEffective)
		r.Put("/registry/{uuid}", h.registryUpdate)
		r.Delete("/registry/{uuid}", h.registryDelete)

		// Routing optimizer
		r.Get("/routing/suggestions", h.routingSuggestions)
		r.Post("/routing/suggestions/{id}/approve", h.routingSuggestionApprove)
		r.Post("/routing/suggestions/{id}/reject", h.routingSuggestionReject)
		r.Get("/routing/stats", h.routingStats)

		// Intake
		r.Get("/intake/items", h.intakeItems)
		r.Get("/intake/stats", h.intakeStats)
		r.Post("/intake/webhook", h.intakeWebhook)

		// MCP tools
		r.Get("/mcp/tools", mcpToolsHandler(h.mcpReg))
		r.Post("/mcp/call", mcpCallHandler(h.mcpReg, h))

		// Missions routes are registered by the missions module below.
	})

	// Events, webhook, notification, and subscription routes (extracted module)
	// Only registered when the event bus is wired in.
	if opts.EventBus != nil {
		apievents.RegisterRoutes(r, apievents.Deps{
			EventBus:   opts.EventBus,
			WebhookMgr: opts.WebhookMgr,
			Scheduler:  opts.Scheduler,
			NotifStore: opts.NotifStore,
			Audit:      startup.Audit,
		})
	}

	// Missions and canvas routes (extracted module)
	apimissions.RegisterRoutes(r, apimissions.Deps{
		MissionManager: startup.MissionManager,
		Claims:         startup.Claims,
		HealthMonitor:  opts.HealthMonitor,
		Scheduler:      opts.Scheduler,
		EventBus:       opts.EventBus,
		Knowledge:      startup.Knowledge,
		CredStore:      startup.CredStore,
		Audit:          startup.Audit,
		Config:         cfg,
		Logger:         logger,
		Comms:          dc,
		Signal:         &DockerSignalSender{RawClient: dc.RawClient()},
	})

	// Knowledge graph and ontology routes (extracted module)
	graph.RegisterRoutes(r, graph.Deps{
		Knowledge: startup.Knowledge,
		Config:    cfg,
		Logger:    logger,
		Audit:     startup.Audit,
	})

	// Hub, connector, preset, deploy, and teardown routes (extracted module)
	apihub.RegisterRoutes(r, apihub.Deps{
		CredStore: startup.CredStore,
		Audit:     startup.Audit,
		Config:    cfg,
		Logger:    logger,
		Signal:    &DockerSignalSender{RawClient: dc.RawClient()},
		DC:        dc,
	})

	// Credential routes (extracted module) — only if CredStore is initialized
	if startup.CredStore != nil {
		creds.RegisterRoutes(r, creds.Deps{
			CredStore: startup.CredStore,
			Audit:     startup.Audit,
			Config:    cfg,
			Logger:    logger,
		})
	}

	// Comms routes (extracted module) — channel/messaging proxy to comms container
	apicomms.RegisterRoutes(r, apicomms.Deps{
		Comms:  dc,
		Config: cfg,
		Logger: logger,
	})

	// Infra, internal LLM, routing, providers, and setup routes (extracted module)
	apiinfra.RegisterRoutes(r, apiinfra.Deps{
		Infra:        startup.Infra,
		DC:           dc,
		DockerStatus: opts.DockerStatus,
		CredStore:    startup.CredStore,
		EventBus:     opts.EventBus,
		Config:       cfg,
		Logger:       logger,
		Audit:        startup.Audit,
	})

	// Admin, teams, capabilities, profiles, and policy routes (extracted module)
	apiadmin.RegisterRoutes(r, apiadmin.Deps{
		AgentManager: startup.AgentManager,
		Infra:        startup.Infra,
		Knowledge:    startup.Knowledge,
		Audit:        startup.Audit,
		ProfileStore: startup.ProfileStore,
		CredStore:    startup.CredStore,
		Config:       cfg,
		Logger:       logger,
		DC:           dc,
		Signal:       &DockerSignalSender{RawClient: dc.RawClient()},
		EventBus:     opts.EventBus,
	})
}

// handler is the monolithic handler struct that exists solely to support MCP tool
// registration. All REST route handlers have been extracted into their own modules
// (agents, admin, hub, infra, events, missions, platform, graph, creds, comms).
//
// The remaining fields are consumed by the MCP tool registration files
// (mcp_register.go, mcp_admin.go, mcp_credentials.go, mcp_events.go,
// mcp_meeseeks.go, mcp_missions.go, mcp_profiles.go) and by the small number
// of routes still registered inline in RegisterAll (agentLogs, intakeItems,
// intakeStats, intakeWebhook). Moving MCP registration into the individual
// modules is a follow-up task.
type handler struct {
	cfg        *config.Config
	dc         *docker.Client
	log        *log.Logger
	infra      *orchestrate.Infra
	agents     *orchestrate.AgentManager
	halt       *orchestrate.HaltController
	audit      *logs.Writer
	ctxMgr     *agencyctx.Manager
	mcpReg     *MCPToolRegistry
	knowledge  *knowledge.Proxy
	missions   *orchestrate.MissionManager
	meeseeks   *orchestrate.MeeseeksManager
	eventBus   *events.Bus
	webhookMgr *events.WebhookManager
	claims        *orchestrate.MissionClaimRegistry
	healthMonitor *orchestrate.MissionHealthMonitor
	notifStore    *events.NotificationStore
	credStore     *credstore.Store
	profileStore  *profiles.Store
}


// registerEnforcerWSClient creates a WebSocket client to the agent's enforcer
// and registers it with the ContextManager for constraint delivery.
// Used by MCP start/restart tools which run through the monolithic handler.
func (h *handler) registerEnforcerWSClient(agentName string) {
	enforcerWSURL := fmt.Sprintf("ws://agency-%s-enforcer:8081/ws", agentName)
	wsClient := agencyctx.NewWSClient(agentName, enforcerWSURL, h.log)
	wsClient.SetCallbacks(
		func(agent string) { h.ctxMgr.HandleEnforcerDisconnect(agent) },
		func(agent string) { h.ctxMgr.HandleEnforcerReconnect(agent) },
	)
	go wsClient.ConnectWithReconnect()
	h.ctxMgr.RegisterWSClient(agentName, wsClient)
	h.log.Info("enforcer ws client registered", "agent", agentName, "url", enforcerWSURL)
}

// unregisterEnforcerWSClient closes and removes the WebSocket client for an agent.
func (h *handler) unregisterEnforcerWSClient(agentName string) {
	h.ctxMgr.UnregisterWSClient(agentName)
}

// containerInstanceID returns the short Docker container ID for a component.
func (h *handler) containerInstanceID(ctx context.Context, agentName, component string) string {
	containerName := fmt.Sprintf("agency-%s-%s", agentName, component)
	return h.dc.ContainerShortID(ctx, containerName)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
