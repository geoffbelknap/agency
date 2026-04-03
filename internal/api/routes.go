package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"

	"github.com/geoffbelknap/agency/internal/audit"
	agencyctx "github.com/geoffbelknap/agency/internal/context"
	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/credstore"
	"github.com/geoffbelknap/agency/internal/docker"
	"github.com/geoffbelknap/agency/internal/events"
	"github.com/geoffbelknap/agency/internal/hub"
	"github.com/geoffbelknap/agency/internal/knowledge"
	"github.com/geoffbelknap/agency/internal/logs"
	"github.com/geoffbelknap/agency/internal/orchestrate"
	"github.com/geoffbelknap/agency/internal/policy"
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
}

// RegisterSocketRoutes sets up the restricted API surface for the Unix socket.
// Only endpoints needed by infra containers are registered — no BearerAuth middleware.
// Each infra-facing endpoint has its own auth mechanism (X-Agency-Token / X-Agency-Caller)
// or is read-only health/status data.
func RegisterSocketRoutes(r chi.Router, cfg *config.Config, dc *docker.Client, logger *log.Logger, opts RouteOptions) {
	h := newHandler(cfg, dc, logger)
	if opts.Hub != nil {
		h.wsHub = opts.Hub
	}
	if opts.EventBus != nil {
		h.eventBus = opts.EventBus
	}

	r.Get("/api/v1/health", h.health)
	r.Post("/api/v1/agents/{name}/signal", h.relaySignal)
	r.Post("/api/v1/internal/llm", h.internalLLM)
	r.Get("/api/v1/internal/credentials/resolve", h.resolveCredential)
	r.Get("/api/v1/infra/status", h.infraStatus)
	r.Get("/api/v1/channels", h.listChannels)
	r.Get("/api/v1/channels/{name}/messages", h.readMessages)
	r.Post("/api/v1/channels/{name}/messages", h.sendMessage)
}

// RegisterRoutes sets up all REST API routes on the given router.
// The hub parameter is optional — if non-nil, the WebSocket endpoint is registered.
func RegisterRoutes(r chi.Router, cfg *config.Config, dc *docker.Client, logger *log.Logger, hub ...*ws.Hub) {
	opts := RouteOptions{}
	if len(hub) > 0 {
		opts.Hub = hub[0]
	}
	RegisterRoutesWithOptions(r, cfg, dc, logger, opts)
}

// RegisterRoutesWithOptions sets up all REST API routes with full option support.
func RegisterRoutesWithOptions(r chi.Router, cfg *config.Config, dc *docker.Client, logger *log.Logger, opts RouteOptions) {
	h := newHandler(cfg, dc, logger)

	// Wire event framework components
	if opts.EventBus != nil {
		h.eventBus = opts.EventBus
	}
	if opts.Scheduler != nil {
		h.scheduler = opts.Scheduler
	}
	if opts.WebhookMgr != nil {
		h.webhookMgr = opts.WebhookMgr
		h.webhookRL = newWebhookRateLimiter()
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
	if opts.DockerStatus != nil {
		h.dockerStatus = opts.DockerStatus
	}

	// WebSocket endpoint (outside /api/v1 — at root /ws per spec)
	if opts.Hub != nil {
		h.wsHub = opts.Hub
		r.Get("/ws", h.handleWebSocket)

		// Wire task completion handler — triggers success criteria evaluation
		// when task_complete signals arrive via the comms WebSocket relay.
		opts.Hub.SetTaskCompleteHandler(func(agent string, data map[string]interface{}) {
			h.evaluateTaskCompletion(agent, data)
		})
	}

	// Web UI config endpoint — serves the auth token so the containerized web UI
	// can authenticate API and WebSocket requests. Excluded from BearerAuth
	// middleware (the web UI needs this to get the token in the first place).
	// Only reachable on localhost (gateway binds to 127.0.0.1 by default).
	r.Get("/__agency/config", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"token":   cfg.Token,
			"gateway": "",
		})
	})

	r.Route("/api/v1", func(r chi.Router) {
		// OpenAPI spec
		r.Get("/openapi.yaml", h.openapiSpec)

		// Health
		r.Get("/health", h.health)

		// Init
		r.Post("/init", h.initPlatform)

		// Agents
		r.Get("/agents", h.listAgents)
		r.Post("/agents", h.createAgent)
		r.Get("/agents/{name}", h.showAgent)
		r.Delete("/agents/{name}", h.deleteAgent)
		r.Post("/agents/{name}/start", h.startAgent)
		r.Post("/agents/{name}/restart", h.restartAgent)
		r.Post("/agents/{name}/stop", h.haltAgent)  // canonical stop endpoint
		r.Post("/agents/{name}/halt", h.haltAgent)  // alias: backward compat
		r.Post("/agents/{name}/resume", h.resumeAgent)
		r.Post("/agents/{name}/grant", h.grantAgent)
		r.Post("/agents/{name}/revoke", h.revokeAgent)
		r.Get("/agents/{name}/channels", h.agentChannels)
		r.Get("/agents/{name}/results", h.listResults)
		r.Get("/agents/{name}/results/{taskId}", h.getResult)
		r.Get("/agents/{name}/config", h.agentConfig)
		r.Put("/agents/{name}/config", h.updateAgentConfig)
		r.Get("/agents/{name}/procedures", h.listAgentProcedures)
		r.Get("/agents/{name}/episodes", h.listAgentEpisodes)
		r.Get("/agents/{name}/trajectory", h.getAgentTrajectory)

		// Agent signals — enforcer relays body-originated signals here for
		// WebSocket broadcast. Mediated path: body → enforcer → gateway → hub.
		r.Post("/agents/{name}/signal", h.relaySignal)

		// Budget (computed from enforcer audit logs — no separate budget store)
		r.Get("/agents/{name}/budget", h.getBudget)
		r.Get("/agents/{name}/budget/remaining", h.getBudgetRemaining)

		// Context API (mid-session constraint push)
		ctxH := &contextHandler{mgr: h.ctxMgr}
		r.Route("/agents/{name}/context", func(r chi.Router) {
			r.Get("/constraints", ctxH.getConstraints)
			r.Get("/exceptions", ctxH.getExceptions)
			r.Get("/policy", ctxH.getPolicy)
			r.Get("/changes", ctxH.getChanges)
			r.Post("/push", ctxH.push)
			r.Get("/status", ctxH.getStatus)
		})

		// Presets
		r.Get("/presets", h.listPresets)
		r.Post("/presets", h.createPreset)
		r.Get("/presets/{name}", h.getPreset)
		r.Put("/presets/{name}", h.updatePreset)
		r.Delete("/presets/{name}", h.deletePreset)

		// Deploy
		r.Post("/deploy", h.deployPack)
		r.Post("/teardown/{pack}", h.teardownPack)

		// Policy
		r.Get("/policy/{agent}", h.showPolicy)
		r.Post("/policy/{agent}/validate", h.validatePolicy)

		// Hub — specific paths before wildcard {nameOrID}
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

		// Capabilities
		r.Get("/capabilities", h.listCapabilities)
		r.Get("/capabilities/{name}", h.showCapability)
		r.Post("/capabilities/{name}/enable", h.enableCapability)
		r.Post("/capabilities/{name}/disable", h.disableCapability)
		r.Post("/capabilities", h.addCapability)
		r.Delete("/capabilities/{name}", h.deleteCapability)

		// Agent logs
		r.Get("/agents/{name}/logs", h.agentLogs)

		// Knowledge (proxy to knowledge container)
		r.Post("/knowledge/query", h.knowledgeQuery)
		r.Get("/knowledge/who-knows", h.knowledgeWhoKnows)
		r.Get("/knowledge/stats", h.knowledgeStats)
		r.Get("/knowledge/export", h.knowledgeExport)
		r.Get("/knowledge/changes", h.knowledgeChanges)
		r.Get("/knowledge/context", h.knowledgeContext)
		r.Get("/knowledge/neighbors", h.knowledgeNeighbors)
		r.Get("/knowledge/path", h.knowledgePath)
		r.Get("/knowledge/flags", h.knowledgeFlags)
		r.Post("/knowledge/restore", h.knowledgeRestore)
		r.Get("/knowledge/curation-log", h.knowledgeCurationLog)

		// Knowledge review (operator-only — org-structural contributions)
		r.Get("/knowledge/pending", h.handleKnowledgePending)
		r.Post("/knowledge/review/{id}", h.handleKnowledgeReview)

		// Knowledge ontology
		r.Get("/knowledge/ontology", h.knowledgeOntology)
		r.Get("/knowledge/ontology/types", h.knowledgeOntologyTypes)
		r.Get("/knowledge/ontology/relationships", h.knowledgeOntologyRelationships)
		r.Post("/knowledge/ontology/validate", h.knowledgeOntologyValidate)
		r.Post("/knowledge/ontology/migrate", h.knowledgeOntologyMigrate)

		// Ontology candidates (emergence)
		r.Get("/ontology/candidates", h.listOntologyCandidates)
		r.Post("/ontology/promote", h.promoteOntologyCandidate)
		r.Post("/ontology/reject", h.rejectOntologyCandidate)

		// Infrastructure
		r.Get("/infra/status", h.infraStatus)
		r.Post("/infra/up", h.infraUp)
		r.Post("/infra/down", h.infraDown)
		r.Post("/infra/rebuild/{component}", h.infraRebuild)
		r.Post("/infra/reload", h.infraReload)

		// Internal LLM (infrastructure components)
		r.Post("/internal/llm", h.internalLLM)

		// Internal credential resolve (egress proxy)
		r.Get("/internal/credentials/resolve", h.resolveCredential)

		// Channels (proxy to comms container)
		r.Get("/channels", h.listChannels)
		r.Post("/channels", h.createChannel)
		r.Get("/channels/{name}/messages", h.readMessages)
		r.Post("/channels/{name}/messages", h.sendMessage)
		r.Put("/channels/{name}/messages/{id}", h.editMessage)
		r.Delete("/channels/{name}/messages/{id}", h.deleteMessage)
		r.Post("/channels/{name}/messages/{id}/reactions", h.addReaction)
		r.Delete("/channels/{name}/messages/{id}/reactions/{emoji}", h.removeReaction)
		r.Get("/channels/search", h.searchMessages)
		r.Post("/channels/{name}/archive", h.archiveChannel)
		r.Get("/unreads", h.getUnreads)
		r.Post("/channels/{name}/mark-read", h.markRead)

		// Routing metrics
		r.Get("/routing/metrics", h.routingMetrics)
		r.Get("/routing/config", h.routingConfig)

		// Providers and setup wizard
		r.Get("/providers", h.listProviders)
		r.Get("/setup/config", h.setupConfig)

		// Credentials
		r.Post("/credentials", h.createOrUpdateCredential)
		r.Get("/credentials", h.listCredentials)
		r.Get("/credentials/{name}", h.showCredential)
		r.Delete("/credentials/{name}", h.deleteCredential)
		r.Post("/credentials/{name}/rotate", h.rotateCredential)
		r.Post("/credentials/{name}/test", h.testCredential)
		r.Post("/credentials/groups", h.createCredentialGroup)

		// Admin
		r.Get("/admin/doctor", h.adminDoctor)
		r.Post("/admin/destroy", h.adminDestroy)
		r.Post("/agents/{name}/rebuild", h.rebuildAgent)
		r.Post("/admin/trust", h.adminTrust)
		r.Get("/admin/audit", h.adminAudit)
		r.Get("/admin/egress", h.adminEgress)
		r.Post("/admin/knowledge", h.adminKnowledge)
		r.Post("/admin/department", h.adminDepartment)

		// Teams
		r.Get("/teams", h.listTeams)
		r.Post("/teams", h.createTeam)
		r.Get("/teams/{name}", h.showTeam)
		r.Get("/teams/{name}/activity", h.teamActivity)

		// Intake
		r.Get("/intake/items", h.intakeItems)
		r.Get("/intake/stats", h.intakeStats)
		r.Post("/intake/webhook", h.intakeWebhook)

		// MCP tools
		r.Get("/mcp/tools", mcpToolsHandler(h.mcpReg))
		r.Post("/mcp/call", mcpCallHandler(h.mcpReg, h))

		// Missions
		r.Route("/missions", func(r chi.Router) {
			r.Post("/", h.createMission)
			r.Get("/", h.listMissions)
			r.Get("/health", h.missionHealth) // all missions health
			r.Get("/{name}", h.showMission)
			r.Put("/{name}", h.updateMission)
			r.Delete("/{name}", h.deleteMission)
			r.Post("/{name}/assign", h.assignMission)
			r.Post("/{name}/pause", h.pauseMission)
			r.Post("/{name}/resume", h.resumeMission)
			r.Post("/{name}/complete", h.completeMission)
			r.Get("/{name}/health", h.missionHealth)
			r.Get("/{name}/history", h.missionHistory)
			r.Post("/{name}/knowledge", h.missionKnowledge)
			r.Post("/{name}/claim", h.claimMissionEvent)
			r.Delete("/{name}/claim", h.releaseMissionClaim)
			r.Get("/{name}/procedures", h.listMissionProcedures)
			r.Get("/{name}/episodes", h.listMissionEpisodes)
			r.Get("/{name}/evaluations", h.listMissionEvaluations)
			r.Get("/{name}/canvas", h.getCanvas)
			r.Put("/{name}/canvas", h.putCanvas)
			r.Delete("/{name}/canvas", h.deleteCanvas)
		})

		// Meeseeks
		r.Post("/meeseeks", h.spawnMeeseeks)
		r.Get("/meeseeks", h.listMeeseeks)
		r.Get("/meeseeks/{id}", h.showMeeseeks)
		r.Delete("/meeseeks/{id}", h.killMeeseeks)
		r.Delete("/meeseeks", h.killMeeseeksByParent)          // kill all for a parent (?parent=<agent>)
		r.Post("/meeseeks/{id}/complete", h.completeMeeseeks)  // called by body runtime on task completion

		// Events
		r.Get("/events", h.listEvents)
		r.Get("/events/{id}", h.showEvent)
		r.Get("/subscriptions", h.listSubscriptions)

		// Webhooks
		r.Post("/webhooks", h.createWebhook)
		r.Get("/webhooks", h.listWebhooks)
		r.Get("/webhooks/{name}", h.showWebhook)
		r.Delete("/webhooks/{name}", h.deleteWebhook)
		r.Post("/webhooks/{name}/rotate-secret", h.rotateWebhookSecret)

		// Inbound webhook receiver
		r.Post("/events/webhook/{name}", h.receiveWebhook)

		// Notifications
		r.Get("/notifications", h.listNotifications)
		r.Post("/notifications", h.addNotification)
		r.Get("/notifications/{name}", h.showNotification)
		r.Delete("/notifications/{name}", h.deleteNotification)
		r.Post("/notifications/{name}/test", h.testNotification)

		// Audit summarization
		if opts.AuditSummarizer != nil {
			summarizer := opts.AuditSummarizer
			r.Post("/audit/summarize", func(w http.ResponseWriter, r *http.Request) {
				metrics, err := summarizer.Summarize()
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]interface{}{
					"metrics": metrics,
					"count":   len(metrics),
				})
			})
		}
	})
}

type handler struct {
	cfg        *config.Config
	dc         *docker.Client
	log        *log.Logger
	infra      *orchestrate.Infra
	agents     *orchestrate.AgentManager
	halt       *orchestrate.HaltController
	audit      *logs.Writer
	wsHub      *ws.Hub
	ctxMgr     *agencyctx.Manager
	mcpReg     *MCPToolRegistry
	knowledge  *knowledge.Proxy
	missions   *orchestrate.MissionManager
	meeseeks   *orchestrate.MeeseeksManager
	eventBus   *events.Bus
	webhookMgr *events.WebhookManager
	webhookRL  *webhookRateLimiter
	scheduler  *events.Scheduler
	claims        *orchestrate.MissionClaimRegistry
	healthMonitor *orchestrate.MissionHealthMonitor
	notifStore    *events.NotificationStore
	credStore     *credstore.Store
	dockerStatus  *docker.Status
}

func newHandler(cfg *config.Config, dc *docker.Client, logger *log.Logger) *handler {
	infra, _ := orchestrate.NewInfra(cfg.Home, cfg.Version, dc, logger, cfg.HMACKey)
	if infra != nil {
		infra.SourceDir = cfg.SourceDir
		infra.BuildID = cfg.BuildID
		infra.GatewayAddr = cfg.GatewayAddr
		infra.GatewayToken = cfg.Token
		infra.EgressToken = cfg.EgressToken
	}
	agents, _ := orchestrate.NewAgentManager(cfg.Home, dc, logger)
	halt, _ := orchestrate.NewHaltController(cfg.Home, cfg.Version, dc, logger)
	audit := logs.NewWriter(cfg.Home)
	ctxMgr := agencyctx.NewManager(audit)

	// Wire halt function so constraint timeout triggers agent halt.
	// ASK tenet 6: unacknowledged constraint changes are treated as potential compromise.
	if halt != nil {
		ctxMgr.SetHaltFunc(func(agent, changeID, reason string) error {
			return halt.HaltForUnackedConstraint(context.Background(), agent, changeID, reason)
		})
	}

	// Initialize credential store (non-fatal — endpoints return 503 if nil).
	var cs *credstore.Store
	storePath := filepath.Join(cfg.Home, "credentials", "store.enc")
	keyPath := filepath.Join(cfg.Home, "credentials", ".key")
	if fb, err := credstore.NewFileBackend(storePath, keyPath); err != nil {
		logger.Warn("credential store init failed", "err", err)
	} else if fb != nil {
		cs = credstore.NewStore(fb, cfg.Home)
	}

	h := &handler{cfg: cfg, dc: dc, log: logger, infra: infra, agents: agents, halt: halt, audit: audit, ctxMgr: ctxMgr, mcpReg: NewMCPToolRegistry(), knowledge: knowledge.NewProxy(), missions: orchestrate.NewMissionManager(cfg.Home), meeseeks: orchestrate.NewMeeseeksManager(), claims: orchestrate.NewMissionClaimRegistry(), credStore: cs}
	registerMCPTools(h.mcpReg)

	// Migrate flat-file hub installations to the instance-directory model on startup.
	hubMgr := hub.NewManager(cfg.Home)
	if migrated, err := hubMgr.Registry.MigrateIfNeeded(); err != nil {
		logger.Warn("hub migration failed", "err", err)
	} else if migrated > 0 {
		logger.Info("migrated hub instances from flat files", "count", migrated)
	}

	return h
}

func (h *handler) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]string{"status": "ok", "version": h.cfg.Version, "build_id": h.cfg.BuildID})
}

func (h *handler) initPlatform(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Operator        string `json:"operator"`
		Force           bool   `json:"force"`
		AnthropicAPIKey string `json:"anthropic_api_key"`
		OpenAIAPIKey    string `json:"openai_api_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err.Error() != "EOF" {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}

	opts := config.InitOptions{
		Operator:        body.Operator,
		Force:           body.Force,
		AnthropicAPIKey: body.AnthropicAPIKey,
		OpenAIAPIKey:    body.OpenAIAPIKey,
	}
	pendingKeys, err := config.RunInit(opts)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}

	// Store any new API keys in the credential store
	for _, key := range pendingKeys {
		if h.credStore != nil {
			now := time.Now().UTC().Format(time.RFC3339)
			h.credStore.Put(credstore.Entry{ //nolint:errcheck
				Name:  key.EnvVar,
				Value: key.Key,
				Metadata: credstore.Metadata{
					Kind:      "provider",
					Scope:     "platform",
					Protocol:  "api-key",
					Source:    "setup",
					CreatedAt: now,
					RotatedAt: now,
				},
			})
		}
	}

	writeJSON(w, 200, map[string]string{"status": "initialized", "home": h.cfg.Home})
}

func (h *handler) listAgents(w http.ResponseWriter, r *http.Request) {
	if h.agents == nil {
		writeJSON(w, 500, map[string]string{"error": "agent manager not initialized"})
		return
	}
	agents, err := h.agents.List(r.Context())
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, agents)
}

func (h *handler) showAgent(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	detail, err := h.agents.Show(r.Context(), name)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, detail)
}

func (h *handler) createAgent(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name   string `json:"name"`
		Preset string `json:"preset"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	if body.Name == "" {
		writeJSON(w, 400, map[string]string{"error": "name required"})
		return
	}
	if body.Preset == "" {
		body.Preset = "generalist"
	}
	if err := h.agents.Create(r.Context(), body.Name, body.Preset); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	h.audit.Write(body.Name, "agent_created", map[string]interface{}{"preset": body.Preset})
	writeJSON(w, 201, map[string]string{"status": "created", "name": body.Name})
}

func (h *handler) deleteAgent(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := h.agents.Delete(r.Context(), name); err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	// Remove agent from all channel memberships
	h.dc.CommsRequest(r.Context(), "POST", "/participants/"+name+"/leave-all", nil)
	h.audit.WriteSystem("agent_deleted", map[string]interface{}{"agent": name})
	writeJSON(w, 200, map[string]string{"status": "deleted", "name": name})
}

func (h *handler) relaySignal(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var body struct {
		SignalType string                 `json:"signal_type"`
		Data       map[string]interface{} `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	if body.SignalType == "" {
		writeJSON(w, 400, map[string]string{"error": "signal_type required"})
		return
	}

	eventType := "agent_signal_" + body.SignalType

	// Write to audit log (ASK tenet 2: every action leaves a trace)
	h.audit.Write(name, eventType, body.Data)

	// Broadcast via WebSocket for real-time delivery
	if h.wsHub != nil {
		h.wsHub.BroadcastAgentSignal(name, eventType, body.Data)
	}

	// Run success criteria evaluation on task_complete signals
	if body.SignalType == "task_complete" {
		go h.evaluateTaskCompletion(name, body.Data)
	}

	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func (h *handler) infraStatus(w http.ResponseWriter, r *http.Request) {
	status, err := h.dc.InfraStatus(r.Context())
	if err != nil {
		if h.dockerStatus != nil {
			h.dockerStatus.RecordError(err)
		}
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if h.dockerStatus != nil {
		h.dockerStatus.RecordSuccess()
	}
	limits := h.budgetConfig()
	store := h.budgetStore()
	infraState, _ := store.Load("_infrastructure")
	writeJSON(w, 200, map[string]interface{}{
		"version":              h.cfg.Version,
		"build_id":             h.cfg.BuildID,
		"gateway_url":          "http://" + h.cfg.GatewayAddr,
		"web_url":              "http://127.0.0.1:8280",
		"components":           status,
		"infra_llm_daily_used":  infraState.DailyUsed,
		"infra_llm_daily_limit": limits.InfraDaily,
		"docker": func() string {
			if h.dockerStatus != nil && !h.dockerStatus.Available() {
				return "unavailable"
			}
			return "available"
		}(),
	})
}

func (h *handler) listChannels(w http.ResponseWriter, r *http.Request) {
	// Merge open channels (team, system) with operator's DM channels.
	// The comms /channels endpoint only returns open channels by default.
	// DMs require a member filter.
	ctx := r.Context()
	openData, err := h.dc.CommsRequest(ctx, "GET", "/channels", nil)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	dmData, _ := h.dc.CommsRequest(ctx, "GET", "/channels?member=_operator", nil)

	// Merge: parse both, deduplicate by name
	var openChannels, dmChannels []map[string]interface{}
	json.Unmarshal(openData, &openChannels)
	json.Unmarshal(dmData, &dmChannels)

	seen := make(map[string]bool)
	var merged []map[string]interface{}
	for _, ch := range openChannels {
		name, _ := ch["name"].(string)
		seen[name] = true
		merged = append(merged, ch)
	}
	for _, ch := range dmChannels {
		name, _ := ch["name"].(string)
		if !seen[name] {
			merged = append(merged, ch)
		}
	}

	writeJSON(w, 200, merged)
}

func (h *handler) readMessages(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	limit := r.URL.Query().Get("limit")
	if limit == "" {
		limit = "50"
	}
	path := "/channels/" + name + "/messages?limit=" + limit + "&reader=_operator"
	data, err := h.dc.CommsRequest(r.Context(), "GET", path, nil)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

func (h *handler) sendMessage(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var body map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	// Normalize operator author to the internal _operator identity used in comms
	if body["author"] == nil || body["author"] == "operator" {
		body["author"] = "_operator"
	}
	path := "/channels/" + name + "/messages"
	data, err := h.dc.CommsRequest(r.Context(), "POST", path, body)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

func (h *handler) editMessage(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	id := chi.URLParam(r, "id")
	var body map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	if body["author"] == nil {
		body["author"] = "_operator"
	}
	path := "/channels/" + name + "/messages/" + id
	data, err := h.dc.CommsRequest(r.Context(), "PUT", path, body)
	if err != nil {
		status := 502
		if strings.Contains(err.Error(), "comms returned 404") {
			status = 404
		} else if strings.Contains(err.Error(), "comms returned 403") {
			status = 403
		}
		if data != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			w.Write(data)
			return
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

func (h *handler) deleteMessage(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	id := chi.URLParam(r, "id")
	body := map[string]interface{}{"author": "_operator"}
	// Try to read body for author override
	var reqBody map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err == nil {
		if reqBody["author"] != nil {
			body["author"] = reqBody["author"]
		}
	}
	path := "/channels/" + name + "/messages/" + id
	data, err := h.dc.CommsRequest(r.Context(), "DELETE", path, body)
	if err != nil {
		status := 502
		if strings.Contains(err.Error(), "comms returned 404") {
			status = 404
		} else if strings.Contains(err.Error(), "comms returned 403") {
			status = 403
		}
		if data != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			w.Write(data)
			return
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

func (h *handler) addReaction(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	id := chi.URLParam(r, "id")
	var body map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	path := "/channels/" + name + "/messages/" + id + "/reactions"
	data, err := h.dc.CommsRequest(r.Context(), "POST", path, body)
	if err != nil {
		status := 502
		if strings.Contains(err.Error(), "comms returned 404") {
			status = 404
		}
		if data != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			w.Write(data)
			return
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

func (h *handler) removeReaction(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	id := chi.URLParam(r, "id")
	emoji := chi.URLParam(r, "emoji")
	author := r.URL.Query().Get("author")
	if author == "" {
		author = "_operator"
	}
	path := "/channels/" + name + "/messages/" + id + "/reactions/" + emoji + "?author=" + author
	data, err := h.dc.CommsRequest(r.Context(), "DELETE", path, nil)
	if err != nil {
		status := 502
		if strings.Contains(err.Error(), "comms returned 404") {
			status = 404
		}
		if data != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			w.Write(data)
			return
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

func (h *handler) listResults(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	containerName := "agency-" + name + "-workspace"
	out, err := h.dc.ExecInContainer(r.Context(), containerName, []string{
		"sh", "-c", "ls -1 /workspace/.results/*.md 2>/dev/null | while read f; do basename \"$f\" .md; done",
	})
	if err != nil {
		writeJSON(w, 200, []interface{}{})
		return
	}
	var results []map[string]string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			results = append(results, map[string]string{"task_id": line})
		}
	}
	if results == nil {
		results = []map[string]string{}
	}
	writeJSON(w, 200, results)
}

func (h *handler) getResult(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	taskID := chi.URLParam(r, "taskId")
	if strings.Contains(taskID, "/") || strings.Contains(taskID, "..") {
		writeJSON(w, 400, map[string]string{"error": "invalid task ID"})
		return
	}
	containerName := "agency-" + name + "-workspace"
	data, err := h.dc.ExecInContainer(r.Context(), containerName, []string{
		"cat", "/workspace/.results/" + taskID + ".md",
	})
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "result not found"})
		return
	}
	if r.URL.Query().Get("download") == "true" {
		w.Header().Set("Content-Disposition", "attachment; filename=\""+taskID+".md\"")
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.WriteHeader(200)
	w.Write([]byte(data))
}

func (h *handler) agentChannels(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	data, err := h.dc.CommsRequest(r.Context(), "GET", "/channels?member="+name, nil)
	if err != nil {
		// Return empty array if comms is unavailable
		writeJSON(w, 200, []interface{}{})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

// containerInstanceID returns the short Docker container ID (first 12 hex chars)
// for the named component of the given agent.
func (h *handler) containerInstanceID(ctx context.Context, agentName, component string) string {
	containerName := fmt.Sprintf("agency-%s-%s", agentName, component)
	return h.dc.ContainerShortID(ctx, containerName)
}

func (h *handler) startAgent(w http.ResponseWriter, r *http.Request) {
	if !h.dockerRequired(w) {
		return
	}
	name := chi.URLParam(r, "name")

	// Ensure agent exists and load detail for lifecycle_id wiring
	detail, err := h.agents.Show(r.Context(), name)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}

	// Wire lifecycle_id into audit writer so all subsequent events carry it.
	h.audit.SetLifecycleID(name, detail.LifecycleID)

	ss := &orchestrate.StartSequence{
		AgentName:   name,
		Home:        h.cfg.Home,
		Version:     h.cfg.Version,
		SourceDir:   h.cfg.SourceDir,
		BuildID:     h.cfg.BuildID,
		Docker:      h.dc,
		Log:         h.log,
		CredStore:   h.credStore,
	}

	// Stream progress as NDJSON if client requests it
	streaming := r.Header.Get("Accept") == "application/x-ndjson"
	var flusher http.Flusher
	if streaming {
		flusher, _ = w.(http.Flusher)
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(200)
	}

	result, err := ss.Run(r.Context(), func(phase int, phaseName, desc string) {
		h.log.Info("start phase", "agent", name, "phase", phase, "name", phaseName)
		h.audit.Write(name, "start_phase", map[string]interface{}{
			"phase":       phase,
			"phase_name":  phaseName,
			"instance_id": h.containerInstanceID(r.Context(), name, "enforcer"),
			"build_id":    h.cfg.BuildID,
		})
		if streaming && flusher != nil {
			event := map[string]interface{}{"type": "phase", "phase": phase, "name": phaseName, "description": desc}
			data, _ := json.Marshal(event)
			w.Write(data)
			w.Write([]byte("\n"))
			flusher.Flush()
		}
	})
	if err != nil {
		h.audit.Write(name, "start_failed", map[string]interface{}{"error": err.Error(), "build_id": h.cfg.BuildID})
		if streaming && flusher != nil {
			event := map[string]interface{}{"type": "error", "error": err.Error()}
			data, _ := json.Marshal(event)
			w.Write(data)
			w.Write([]byte("\n"))
			flusher.Flush()
			return
		}
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}

	// Wire WebSocket client to enforcer for constraint delivery.
	// The enforcer is healthy at this point (phase 2 passed).
	h.registerEnforcerWSClient(name)

	h.audit.Write(name, "agent_started", map[string]interface{}{
		"instance_id": h.containerInstanceID(r.Context(), name, "workspace"),
		"build_id":    h.cfg.BuildID,
	})
	events.EmitAgentEvent(h.eventBus, "agent_started", name, nil)
	if streaming && flusher != nil {
		event := map[string]interface{}{"type": "complete", "agent": result.Agent, "model": result.Model, "phases": result.Phases}
		data, _ := json.Marshal(event)
		w.Write(data)
		w.Write([]byte("\n"))
		flusher.Flush()
		return
	}
	writeJSON(w, 200, result)
}

// registerEnforcerWSClient creates a WebSocket client to the agent's enforcer
// and registers it with the ContextManager for constraint delivery.
// Called after the start sequence completes successfully (enforcer is healthy).
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

func (h *handler) restartAgent(w http.ResponseWriter, r *http.Request) {
	if !h.dockerRequired(w) {
		return
	}
	name := chi.URLParam(r, "name")

	// Ensure agent exists and load detail for lifecycle_id wiring
	detail, err := h.agents.Show(r.Context(), name)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}

	// Wire lifecycle_id into audit writer so all subsequent events carry it.
	h.audit.SetLifecycleID(name, detail.LifecycleID)

	// Stop existing containers and close old WS client
	h.unregisterEnforcerWSClient(name)
	h.agents.StopContainers(r.Context(), name)

	// Start with key rotation — generates a fresh scoped key instead of
	// reusing the old one (ASK tenet 4: least privilege)
	ss := &orchestrate.StartSequence{
		AgentName:   name,
		Home:        h.cfg.Home,
		Version:     h.cfg.Version,
		SourceDir:   h.cfg.SourceDir,
		BuildID:     h.cfg.BuildID,
		Docker:      h.dc,
		Log:         h.log,
		KeyRotation: true,
		CredStore:   h.credStore,
	}

	result, err := ss.Run(r.Context(), func(phase int, phaseName, desc string) {
		h.log.Info("restart phase", "agent", name, "phase", phase, "name", phaseName)
		h.audit.Write(name, "start_phase", map[string]interface{}{
			"phase":       phase,
			"phase_name":  phaseName,
			"trigger":     "restart",
			"instance_id": h.containerInstanceID(r.Context(), name, "enforcer"),
			"build_id":    h.cfg.BuildID,
		})
	})
	if err != nil {
		h.audit.Write(name, "restart_failed", map[string]interface{}{"error": err.Error(), "build_id": h.cfg.BuildID})
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}

	// Re-wire WebSocket client to enforcer after restart.
	h.registerEnforcerWSClient(name)

	h.audit.Write(name, "agent_restarted", map[string]interface{}{
		"instance_id": h.containerInstanceID(r.Context(), name, "workspace"),
		"build_id":    h.cfg.BuildID,
	})
	writeJSON(w, 200, result)
}

func (h *handler) deployPack(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PackPath    string               `json:"pack_path"`
		Pack        *orchestrate.PackDef `json:"pack"`
		DryRun      bool                 `json:"dry_run"`
		Credentials map[string]string    `json:"credentials"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}

	var pack *orchestrate.PackDef
	var err error
	if body.PackPath != "" {
		pack, err = orchestrate.LoadPack(body.PackPath)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
	} else if body.Pack != nil {
		pack = body.Pack
	} else {
		writeJSON(w, 400, map[string]string{"error": "pack_path or pack required"})
		return
	}

	// Validate required credentials are present.
	if len(pack.Credentials) > 0 {
		var missing []string
		for _, cred := range pack.Credentials {
			if cred.Required {
				if _, ok := body.Credentials[cred.Name]; !ok {
					missing = append(missing, cred.Name)
				}
			}
		}
		if len(missing) > 0 {
			writeJSON(w, 400, map[string]interface{}{
				"status":  "credentials_required",
				"missing": missing,
			})
			return
		}
	}

	deployer := orchestrate.NewDeployer(h.cfg.Home, h.cfg.Version, h.dc, h.log)
	deployer.SourceDir = h.cfg.SourceDir
	deployer.BuildID = h.cfg.BuildID
	deployer.Credentials = body.Credentials
	deployer.CredStore = h.credStore

	if body.DryRun {
		result, err := deployer.DryRunDeploy(r.Context(), pack, func(s string) {
			h.log.Info("deploy dry-run", "status", s)
		})
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, result)
		return
	}

	result, err := deployer.Deploy(r.Context(), pack, func(s string) {
		h.log.Info("deploy", "status", s)
	})
	if err != nil {
		h.audit.WriteSystem("deploy_failed", map[string]interface{}{"pack": body.PackPath, "error": err.Error()})
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	h.audit.WriteSystem("pack_deployed", map[string]interface{}{"pack": body.PackPath})
	writeJSON(w, 200, result)
}

func (h *handler) teardownPack(w http.ResponseWriter, r *http.Request) {
	packName := chi.URLParam(r, "pack")
	var body struct {
		Delete bool `json:"delete"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	deployer := orchestrate.NewDeployer(h.cfg.Home, h.cfg.Version, h.dc, h.log)
	deployer.CredStore = h.credStore
	if err := deployer.Teardown(r.Context(), packName, body.Delete); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	h.audit.WriteSystem("pack_teardown", map[string]interface{}{"pack": packName, "delete": body.Delete})
	writeJSON(w, 200, map[string]string{"status": "torn down", "pack": packName})
}

func (h *handler) showPolicy(w http.ResponseWriter, r *http.Request) {
	agent := chi.URLParam(r, "agent")
	eng := policy.NewEngine(h.cfg.Home)
	ep := eng.Show(agent)
	writeJSON(w, 200, ep)
}

func (h *handler) validatePolicy(w http.ResponseWriter, r *http.Request) {
	agent := chi.URLParam(r, "agent")
	eng := policy.NewEngine(h.cfg.Home)
	ep := eng.Validate(agent)

	// Additionally enforce hard floors on the agent's constraints.yaml
	// to prevent saving policies that violate immutable safety guarantees.
	constraintsPath := filepath.Join(h.cfg.Home, "agents", agent, "constraints.yaml")
	if data, err := os.ReadFile(constraintsPath); err == nil {
		var constraints map[string]interface{}
		if yaml.Unmarshal(data, &constraints) == nil {
			if err := policy.ValidatePolicy(constraints); err != nil {
				ep.Valid = false
				ep.Violations = append(ep.Violations, err.Error())
			}
		}
	}

	if !ep.Valid {
		writeJSON(w, 400, ep)
		return
	}
	writeJSON(w, 200, ep)
}

func (h *handler) haltAgent(w http.ResponseWriter, r *http.Request) {
	if !h.dockerRequired(w) {
		return
	}
	name := chi.URLParam(r, "name")
	var body struct {
		Type      string `json:"type"`
		Reason    string `json:"reason"`
		Initiator string `json:"initiator"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	if body.Type == "" {
		body.Type = "supervised"
	}
	if body.Type == "emergency" && body.Reason == "" {
		writeJSON(w, 400, map[string]string{"error": "emergency halt requires a reason (ASK Tenet 2)"})
		return
	}
	record, err := h.halt.Halt(r.Context(), name, body.Type, body.Reason, body.Initiator)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	h.audit.Write(name, "agent_halted", map[string]interface{}{
		"type":        body.Type,
		"reason":      body.Reason,
		"initiator":   body.Initiator,
		"instance_id": h.containerInstanceID(r.Context(), name, "workspace"),
		"build_id":    h.cfg.BuildID,
	})
	events.EmitAgentEvent(h.eventBus, "agent_halted", name, map[string]interface{}{
		"type": body.Type, "reason": body.Reason,
	})

	// Orphan detection: mark any running Meeseeks for this parent as orphaned.
	// ASK tenet 13: principal and agent lifecycles are independent — halting
	// a parent does not auto-terminate its Meeseeks, but they must be flagged.
	if h.meeseeks != nil {
		orphanedIDs := h.meeseeks.MarkOrphaned(name)
		for _, mid := range orphanedIDs {
			h.audit.Write(name, "meeseeks_orphaned", map[string]interface{}{
				"meeseeks_id": mid,
				"parent":      name,
				"reason":      "parent agent halted",
				"build_id":    h.cfg.BuildID,
			})
		}
		if len(orphanedIDs) > 0 {
			h.log.Warn("Orphaned Meeseeks after parent halt",
				"parent", name,
				"count", len(orphanedIDs),
				"ids", orphanedIDs,
			)
			// Alert operator via comms (best-effort; comms may not be running)
			msg := fmt.Sprintf("[operator] Parent agent %q halted with %d orphaned Meeseeks: %v", name, len(orphanedIDs), orphanedIDs)
			h.dc.CommsRequest(r.Context(), "POST", "/channels/operator/messages", map[string]interface{}{
				"author":  "_system",
				"content": msg,
			})
		}
	}

	// Coverage failover: if halted agent is a coordinator for an active team
	// mission, failover to the coverage agent (ASK tenet 14 — authority is never orphaned).
	h.checkCoordinatorFailover(r.Context(), name)

	writeJSON(w, 200, record)
}

func (h *handler) resumeAgent(w http.ResponseWriter, r *http.Request) {
	if !h.dockerRequired(w) {
		return
	}
	name := chi.URLParam(r, "name")
	var body struct {
		Initiator string `json:"initiator"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	if err := h.halt.Resume(r.Context(), name, body.Initiator); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	h.audit.Write(name, "agent_resumed", map[string]interface{}{"initiator": body.Initiator})
	events.EmitAgentEvent(h.eventBus, "agent_resumed", name, nil)
	writeJSON(w, 200, map[string]string{"status": "resumed", "agent": name})
}

func (h *handler) infraUp(w http.ResponseWriter, r *http.Request) {
	if !h.dockerRequired(w) {
		return
	}
	if h.infra == nil {
		writeJSON(w, 500, map[string]string{"error": "infrastructure manager not initialized"})
		return
	}

	// If the client accepts NDJSON, stream progress events.
	stream := r.Header.Get("Accept") == "application/x-ndjson"

	// Use background context: infra startup must complete even if the client
	// disconnects (otherwise we leave infrastructure half-running).
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if !stream {
		if err := h.infra.EnsureRunning(ctx); err != nil {
			if h.dockerStatus != nil {
				h.dockerStatus.RecordError(err)
			}
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		if h.dockerStatus != nil {
			h.dockerStatus.RecordSuccess()
		}
		events.EmitInfraEvent(h.eventBus, "infra_up", nil)
		writeJSON(w, 200, map[string]string{"status": "running"})
		return
	}

	// Streaming mode: send progress events as NDJSON lines.
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, 500, map[string]string{"error": "streaming not supported"})
		return
	}

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(200)

	enc := json.NewEncoder(w)
	onProgress := func(component, status string) {
		enc.Encode(map[string]string{
			"type":      "progress",
			"component": component,
			"status":    status,
		})
		flusher.Flush()
	}

	if err := h.infra.EnsureRunningWithProgress(ctx, onProgress); err != nil {
		if h.dockerStatus != nil {
			h.dockerStatus.RecordError(err)
		}
		enc.Encode(map[string]string{"type": "error", "error": err.Error()})
		flusher.Flush()
		return
	}
	if h.dockerStatus != nil {
		h.dockerStatus.RecordSuccess()
	}

	events.EmitInfraEvent(h.eventBus, "infra_up", nil)
	enc.Encode(map[string]string{"type": "done", "status": "running"})
	flusher.Flush()
}

func (h *handler) infraDown(w http.ResponseWriter, r *http.Request) {
	if !h.dockerRequired(w) {
		return
	}
	if h.infra == nil {
		writeJSON(w, 500, map[string]string{"error": "infrastructure manager not initialized"})
		return
	}

	stream := r.Header.Get("Accept") == "application/x-ndjson"
	if !stream {
		if err := h.infra.Teardown(r.Context()); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		events.EmitInfraEvent(h.eventBus, "infra_down", nil)
		writeJSON(w, 200, map[string]string{"status": "stopped"})
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, 500, map[string]string{"error": "streaming not supported"})
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(200)
	enc := json.NewEncoder(w)

	onProgress := func(component, status string) {
		enc.Encode(map[string]string{"type": "progress", "component": component, "status": status})
		flusher.Flush()
	}
	if err := h.infra.TeardownWithProgress(r.Context(), onProgress); err != nil {
		enc.Encode(map[string]string{"type": "error", "error": err.Error()})
		flusher.Flush()
		return
	}
	enc.Encode(map[string]string{"type": "done", "status": "stopped"})
	flusher.Flush()
}

func (h *handler) infraRebuild(w http.ResponseWriter, r *http.Request) {
	if !h.dockerRequired(w) {
		return
	}
	component := chi.URLParam(r, "component")
	if h.infra == nil {
		writeJSON(w, 500, map[string]string{"error": "infrastructure manager not initialized"})
		return
	}

	stream := r.Header.Get("Accept") == "application/x-ndjson"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if !stream {
		if err := h.infra.RestartComponent(ctx, component); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"status": "restarted", "component": component})
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, 500, map[string]string{"error": "streaming not supported"})
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(200)
	enc := json.NewEncoder(w)

	onProgress := func(comp, status string) {
		enc.Encode(map[string]string{"type": "progress", "component": comp, "status": status})
		flusher.Flush()
	}
	if err := h.infra.RestartComponentWithProgress(ctx, component, onProgress); err != nil {
		enc.Encode(map[string]string{"type": "error", "error": err.Error()})
		flusher.Flush()
		return
	}
	enc.Encode(map[string]string{"type": "done", "status": "restarted", "component": component})
	flusher.Flush()
}

func (h *handler) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	if h.wsHub == nil {
		writeJSON(w, 500, map[string]string{"error": "websocket hub not initialized"})
		return
	}
	h.wsHub.HandleWebSocket(w, r)
}

// dockerRequired returns true if Docker is available. If not, writes a 503
// response with a human-readable error and returns false.
func (h *handler) dockerRequired(w http.ResponseWriter) bool {
	if h.dockerStatus != nil && !h.dockerStatus.Available() {
		writeJSON(w, 503, map[string]string{
			"error": "Docker is not available. Container operations are unavailable.",
		})
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
