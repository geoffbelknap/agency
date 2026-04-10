package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/docker/go-connections/nat"
	"github.com/go-chi/chi/v5"
	"log/slog"

	apiadmin "github.com/geoffbelknap/agency/internal/api/admin"
	apiagents "github.com/geoffbelknap/agency/internal/api/agents"
	apicomms "github.com/geoffbelknap/agency/internal/api/comms"
	"github.com/geoffbelknap/agency/internal/api/creds"
	apievents "github.com/geoffbelknap/agency/internal/api/events"
	"github.com/geoffbelknap/agency/internal/api/graph"
	apihub "github.com/geoffbelknap/agency/internal/api/hub"
	apiinfra "github.com/geoffbelknap/agency/internal/api/infra"
	apimissions "github.com/geoffbelknap/agency/internal/api/missions"
	"github.com/geoffbelknap/agency/internal/api/platform"
	"github.com/geoffbelknap/agency/internal/audit"
	"github.com/geoffbelknap/agency/internal/config"
	agencyctx "github.com/geoffbelknap/agency/internal/context"
	"github.com/geoffbelknap/agency/internal/credstore"
	"github.com/geoffbelknap/agency/internal/docker"
	"github.com/geoffbelknap/agency/internal/events"
	"github.com/geoffbelknap/agency/internal/knowledge"
	"github.com/geoffbelknap/agency/internal/logs"
	"github.com/geoffbelknap/agency/internal/orchestrate"
	"github.com/geoffbelknap/agency/internal/profiles"

	"github.com/geoffbelknap/agency/internal/registry"
	"github.com/geoffbelknap/agency/internal/routing"
	"github.com/geoffbelknap/agency/internal/ws"
)

// RouteOptions holds optional dependencies for route registration.
type RouteOptions struct {
	Hub             *ws.Hub
	EventBus        *events.Bus
	Scheduler       *events.Scheduler
	WebhookMgr      *events.WebhookManager
	HealthMonitor   *orchestrate.MissionHealthMonitor
	NotifStore      *events.NotificationStore
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
func RegisterSocketRoutes(r chi.Router, cfg *config.Config, dc *docker.Client, logger *slog.Logger, startup *StartupResult, opts RouteOptions) {
	// Defense-in-depth: validate X-Agency-Caller on protected endpoints
	callerAllowlist := map[string][]string{
		"POST /api/v1/agents/{name}/signal": {"enforcer"},
		"POST /api/v1/infra/internal/llm":   {"enforcer", "knowledge"},
		"POST /api/v1/comms/channels/*":     {"comms", "intake"},
		"GET /api/v1/comms/channels/*":      {"comms", "intake", "enforcer"},
		"POST /api/v1/graph/ingest":         {"intake"},
		"POST /api/v1/events/publish":       {"intake", "knowledge"},
	}
	r.Use(CallerValidation(callerAllowlist))

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
		Comms:        dc,
		AgentManager: startup.AgentManager,
		Config:       cfg,
		Logger:       logger,
	})

	// Knowledge graph routes on the socket — used by intake for graph ingest.
	graph.RegisterRoutes(r, graph.Deps{
		Knowledge: startup.Knowledge,
		Logger:    logger,
	})

	// Event routes on the socket — used by intake for event publishing.
	if opts.EventBus != nil {
		apievents.RegisterRoutes(r, apievents.Deps{
			EventBus:   opts.EventBus,
			WebhookMgr: opts.WebhookMgr,
			Scheduler:  opts.Scheduler,
			NotifStore: opts.NotifStore,
		})
	}
}

// RegisterCredentialSocketRoutes registers the credential-only socket router.
// This socket is mounted exclusively by the egress container for credential
// resolution. It is NOT bridged to TCP — credentials never traverse a Docker network.
func RegisterCredentialSocketRoutes(r chi.Router, cfg *config.Config, dc *docker.Client, logger *slog.Logger, startup *StartupResult, opts RouteOptions) {
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
func RegisterAll(r chi.Router, cfg *config.Config, dc *docker.Client, logger *slog.Logger, startup *StartupResult, opts RouteOptions) {
	d := &mcpDeps{
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
		d.eventBus = opts.EventBus
	}
	if opts.WebhookMgr != nil {
		d.webhookMgr = opts.WebhookMgr
	}
	if opts.HealthMonitor != nil {
		d.healthMonitor = opts.HealthMonitor
	}
	if opts.NotifStore != nil {
		d.notifStore = opts.NotifStore
	}
	if opts.StopSuppress != nil && d.agents != nil {
		d.agents.StopSuppress = opts.StopSuppress
	}
	if opts.StopSuppress != nil && startup.HaltController != nil {
		startup.HaltController.StopSuppress = opts.StopSuppress
	}

	if opts.Optimizer != nil && d.infra != nil {
		d.infra.Optimizer = opts.Optimizer
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

	// MCP tools
	r.Get("/api/v1/mcp/tools", mcpToolsHandler(d.mcpReg))
	r.Post("/api/v1/mcp/call", mcpCallHandler(d.mcpReg, d))

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
		Comms:        dc,
		AgentManager: startup.AgentManager,
		Config:       cfg,
		Logger:       logger,
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

// mcpDeps holds the dependencies consumed exclusively by MCP tool handlers.
// All REST route handlers have been extracted into their own subpackage modules
// (agents, admin, hub, infra, events, missions, platform, graph, creds, comms).
// Moving MCP registration into the individual modules is a follow-up task.
type mcpDeps struct {
	cfg           *config.Config
	dc            *docker.Client
	log           *slog.Logger
	infra         *orchestrate.Infra
	agents        *orchestrate.AgentManager
	halt          *orchestrate.HaltController
	audit         *logs.Writer
	ctxMgr        *agencyctx.Manager
	mcpReg        *MCPToolRegistry
	knowledge     *knowledge.Proxy
	missions      *orchestrate.MissionManager
	meeseeks      *orchestrate.MeeseeksManager
	eventBus      *events.Bus
	webhookMgr    *events.WebhookManager
	claims        *orchestrate.MissionClaimRegistry
	healthMonitor *orchestrate.MissionHealthMonitor
	notifStore    *events.NotificationStore
	credStore     *credstore.Store
	profileStore  *profiles.Store
}

// registerEnforcerWSClient creates a WebSocket client to the agent's enforcer
// and registers it with the ContextManager for constraint delivery.
// Used by MCP start/restart tools.
func (d *mcpDeps) registerEnforcerWSClient(agentName string) {
	enforcerWSURL := d.enforcerWSURL(context.Background(), agentName)
	wsClient := agencyctx.NewWSClient(agentName, enforcerWSURL, d.log)
	wsClient.SetCallbacks(
		func(agent string) { d.ctxMgr.HandleEnforcerDisconnect(agent) },
		func(agent string) { d.ctxMgr.HandleEnforcerReconnect(agent) },
	)
	go wsClient.ConnectWithReconnect()
	d.ctxMgr.RegisterWSClient(agentName, wsClient)
	d.log.Info("enforcer ws client registered", "agent", agentName, "url", enforcerWSURL)
}

func (d *mcpDeps) enforcerWSURL(ctx context.Context, agentName string) string {
	defaultURL := fmt.Sprintf("ws://agency-%s-enforcer:8081/ws", agentName)
	if d.dc == nil {
		return defaultURL
	}
	inspect, err := d.dc.RawClient().ContainerInspect(ctx, fmt.Sprintf("agency-%s-enforcer", agentName))
	if err != nil || inspect.NetworkSettings == nil {
		return defaultURL
	}
	bindings := inspect.NetworkSettings.Ports[nat.Port("8081/tcp")]
	if len(bindings) == 0 || bindings[0].HostPort == "" {
		return defaultURL
	}
	hostIP := bindings[0].HostIP
	if hostIP == "" || hostIP == "0.0.0.0" {
		hostIP = "127.0.0.1"
	}
	return fmt.Sprintf("ws://%s:%s/ws", hostIP, bindings[0].HostPort)
}

// unregisterEnforcerWSClient closes and removes the WebSocket client for an agent.
func (d *mcpDeps) unregisterEnforcerWSClient(agentName string) {
	d.ctxMgr.UnregisterWSClient(agentName)
}

// containerInstanceID returns the short Docker container ID for a component.
func (d *mcpDeps) containerInstanceID(ctx context.Context, agentName, component string) string {
	containerName := fmt.Sprintf("agency-%s-%s", agentName, component)
	return d.dc.ContainerShortID(ctx, containerName)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
