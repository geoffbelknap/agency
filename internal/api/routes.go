package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"

	"github.com/go-chi/chi/v5"
	"log/slog"

	apiadmin "github.com/geoffbelknap/agency/internal/api/admin"
	apiagents "github.com/geoffbelknap/agency/internal/api/agents"
	apiauthz "github.com/geoffbelknap/agency/internal/api/authz"
	apicomms "github.com/geoffbelknap/agency/internal/api/comms"
	"github.com/geoffbelknap/agency/internal/api/creds"
	apievents "github.com/geoffbelknap/agency/internal/api/events"
	"github.com/geoffbelknap/agency/internal/api/graph"
	apihub "github.com/geoffbelknap/agency/internal/api/hub"
	apiinfra "github.com/geoffbelknap/agency/internal/api/infra"
	apiinstances "github.com/geoffbelknap/agency/internal/api/instances"
	apimissions "github.com/geoffbelknap/agency/internal/api/missions"
	apipackages "github.com/geoffbelknap/agency/internal/api/packages"
	"github.com/geoffbelknap/agency/internal/api/platform"
	"github.com/geoffbelknap/agency/internal/audit"
	authzcore "github.com/geoffbelknap/agency/internal/authz"
	"github.com/geoffbelknap/agency/internal/backendhealth"
	commsclient "github.com/geoffbelknap/agency/internal/comms"
	"github.com/geoffbelknap/agency/internal/config"
	agencyctx "github.com/geoffbelknap/agency/internal/context"
	"github.com/geoffbelknap/agency/internal/credstore"
	"github.com/geoffbelknap/agency/internal/events"
	"github.com/geoffbelknap/agency/internal/features"
	"github.com/geoffbelknap/agency/internal/hostadapter"
	hostruntimebackend "github.com/geoffbelknap/agency/internal/hostadapter/runtimebackend"
	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
	"github.com/geoffbelknap/agency/internal/knowledge"
	"github.com/geoffbelknap/agency/internal/logs"
	"github.com/geoffbelknap/agency/internal/orchestrate"
	"github.com/geoffbelknap/agency/internal/profiles"

	"github.com/geoffbelknap/agency/internal/registry"
	"github.com/geoffbelknap/agency/internal/routing"
	runtimecontract "github.com/geoffbelknap/agency/internal/runtime/contract"
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
	BackendHealth   backendhealth.Recorder
	Registry        *registry.Registry
	Optimizer       *routing.RoutingOptimizer
}

func signalSenderFor(dc *runtimehost.Client) SignalSender {
	if dc == nil {
		return noopSignalSender{}
	}
	return dc
}

type namedRuntimeSignalSender struct {
	dc *runtimehost.Client
}

func (s namedRuntimeSignalSender) SignalRuntimeName(ctx context.Context, name, signal string) error {
	if s.dc == nil || s.dc.RawClient() == nil {
		return fmt.Errorf("signal sender unavailable")
	}
	return s.dc.RawClient().ContainerKill(ctx, name, signal)
}

func namedSignalSenderFor(dc *runtimehost.Client) namedRuntimeSignalSender {
	return namedRuntimeSignalSender{dc: dc}
}

func commsClientFor(dc *runtimehost.Client) interface {
	CommsRequest(ctx context.Context, method, path string, body interface{}) ([]byte, error)
} {
	if dc == nil {
		return commsclient.NewHTTPClient("http://localhost:" + gatewayProxyPort())
	}
	return dc
}

func gatewayProxyPort() string {
	raw := os.Getenv("AGENCY_GATEWAY_PROXY_PORT")
	if p, err := strconv.Atoi(raw); err == nil && p > 0 && p < 65536 {
		return raw
	}
	return "8202"
}

func runtimeExecClientFor(dc *runtimehost.Client) interface {
	Exec(ctx context.Context, ref runtimecontract.InstanceRef, cmd []string) (string, error)
	ShortID(ctx context.Context, ref runtimecontract.InstanceRef) string
} {
	if dc == nil {
		return noopRuntimeExecClient{}
	}
	return dc
}

// RegisterSocketRoutes sets up the restricted API surface for the Unix socket.
// Only endpoints needed by infra containers are registered — no BearerAuth middleware.
// Each infra-facing endpoint has its own auth mechanism (X-Agency-Token / X-Agency-Caller)
// or is read-only health/status data.
func RegisterSocketRoutes(r chi.Router, cfg *config.Config, dc *runtimehost.Client, logger *slog.Logger, startup *StartupResult, opts RouteOptions) {
	// Defense-in-depth: validate X-Agency-Caller on protected endpoints
	callerAllowlist := map[string][]string{
		"POST /api/v1/agents/{name}/signal":         {"enforcer"},
		"POST /api/v1/infra/internal/llm":           {"enforcer", "knowledge"},
		"POST /api/v1/comms/channels/*":             {"comms", "intake"},
		"GET /api/v1/comms/channels/*":              {"comms", "intake", "enforcer"},
		"POST /api/v1/graph/ingest":                 {"intake"},
		"POST /api/v1/events/publish":               {"intake", "knowledge"},
		"POST /api/internal/relay/webhooks/deliver": {"relay"},
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
		BackendHealth:   opts.BackendHealth,
		WSHub:           opts.Hub,
		Comms:           commsClientFor(dc),
		Signal:          signalSenderFor(dc),
		Runtime:         runtimeExecClientFor(dc),
		RuntimeHost:     dc,
	})

	// Infra routes on the socket (subset: status + internal LLM only)
	apiinfra.RegisterRoutes(r, apiinfra.Deps{
		Infra:         startup.Infra,
		AgentManager:  startup.AgentManager,
		Runtime:       startup.InfraRuntime,
		BackendHealth: opts.BackendHealth,
		CredStore:     startup.CredStore,
		Config:        cfg,
		Logger:        logger,
		Audit:         startup.Audit,
	})

	apicomms.RegisterRoutes(r, apicomms.Deps{
		Comms:        commsClientFor(dc),
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
		apievents.RegisterInternalRoutes(r, apievents.Deps{
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
func RegisterCredentialSocketRoutes(r chi.Router, cfg *config.Config, dc *runtimehost.Client, logger *slog.Logger, startup *StartupResult, opts RouteOptions) {
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
func RegisterAll(r chi.Router, cfg *config.Config, dc *runtimehost.Client, logger *slog.Logger, startup *StartupResult, opts RouteOptions) {
	experimental := features.ExperimentalEnabled()
	var host hostadapter.Adapter
	if dc != nil {
		host = hostadapter.NewAdapter(hostruntimebackend.NormalizeRuntimeBackend(cfg.Hub.DeploymentBackend), dc, logger)
	}
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
		BackendHealth:   opts.BackendHealth,
		WSHub:           opts.Hub,
		Comms:           commsClientFor(dc),
		Signal:          signalSenderFor(dc),
		Runtime:         runtimeExecClientFor(dc),
		RuntimeHost:     dc,
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

	if experimental {
		// Missions and canvas routes (extracted module)
		apimissions.RegisterRoutes(r, apimissions.Deps{
			MissionManager: startup.MissionManager,
			Runtime:        startup.Runtime,
			Claims:         startup.Claims,
			HealthMonitor:  opts.HealthMonitor,
			Scheduler:      opts.Scheduler,
			EventBus:       opts.EventBus,
			Knowledge:      startup.Knowledge,
			CredStore:      startup.CredStore,
			Audit:          startup.Audit,
			Config:         cfg,
			Logger:         logger,
			Comms:          commsClientFor(dc),
			Signal:         signalSenderFor(dc),
		})
	}

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
		Signal:    namedSignalSenderFor(dc),
		Host:      host,
		Runtime:   dc,
	})

	if experimental {
		// V2 package routes over the local hub package registry.
		apipackages.RegisterRoutes(r, apipackages.Deps{
			Config:   cfg,
			Registry: startup.HubRegistry,
			Logger:   logger,
		})

		apiinstances.RegisterRoutes(r, apiinstances.Deps{
			Config:   cfg,
			Store:    startup.InstanceStore,
			Registry: startup.HubRegistry,
			Logger:   logger,
			Signal:   namedSignalSenderFor(dc),
			EventBus: opts.EventBus,
		})
		apiauthz.RegisterRoutes(r, apiauthz.Deps{
			Resolver: derefResolver(startup.AuthzResolver),
			Logger:   logger,
		})
	}

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
		Comms:        commsClientFor(dc),
		AgentManager: startup.AgentManager,
		Config:       cfg,
		Logger:       logger,
	})

	// Infra, internal LLM, routing, providers, and setup routes (extracted module)
	apiinfra.RegisterRoutes(r, apiinfra.Deps{
		Infra:         startup.Infra,
		AgentManager:  startup.AgentManager,
		Runtime:       startup.InfraRuntime,
		BackendHealth: opts.BackendHealth,
		CredStore:     startup.CredStore,
		EventBus:      opts.EventBus,
		Config:        cfg,
		Logger:        logger,
		Audit:         startup.Audit,
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
		Runtime:      dc,
		Host:         host,
		Signal:       signalSenderFor(dc),
		EventBus:     opts.EventBus,
	})
}

// mcpDeps holds the dependencies consumed exclusively by MCP tool handlers.
// All REST route handlers have been extracted into their own subpackage modules
// (agents, admin, hub, infra, events, missions, platform, graph, creds, comms).
// Moving MCP registration into the individual modules is a follow-up task.
type mcpDeps struct {
	cfg           *config.Config
	dc            *runtimehost.Client
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

// registerEnforcerWSClient no longer dials the enforcer from the host. The
// enforcer connects back into the gateway via the authenticated context/ws
// route once its constraint server is ready. Used by MCP start/restart tools.
func (d *mcpDeps) registerEnforcerWSClient(agentName string) {
	if d.log != nil {
		d.log.Info("waiting for enforcer ws connection", "agent", agentName)
	}
}

// unregisterEnforcerWSClient closes and removes the WebSocket client for an agent.
func (d *mcpDeps) unregisterEnforcerWSClient(agentName string) {
	d.ctxMgr.UnregisterWSClient(agentName)
}

// runtimeInstanceID returns the backend instance identifier for a component.
func (d *mcpDeps) runtimeInstanceID(ctx context.Context, agentName, component string) string {
	if d == nil || d.dc == nil {
		return agentName + ":" + component
	}
	ref := runtimecontract.InstanceRef{RuntimeID: agentName, Role: runtimecontract.ComponentRole(component)}
	return d.dc.ShortID(ctx, ref)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func derefResolver(r *authzcore.Resolver) authzcore.Resolver {
	if r == nil {
		return authzcore.Resolver{}
	}
	return *r
}
