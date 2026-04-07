package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/go-chi/chi/v5"

	"github.com/geoffbelknap/agency/internal/audit"
	agencyctx "github.com/geoffbelknap/agency/internal/context"
	apiadmin "github.com/geoffbelknap/agency/internal/api/admin"
	"github.com/geoffbelknap/agency/internal/api/creds"
	apicomms "github.com/geoffbelknap/agency/internal/api/comms"
	apievents "github.com/geoffbelknap/agency/internal/api/events"
	"github.com/geoffbelknap/agency/internal/api/graph"
	apihub "github.com/geoffbelknap/agency/internal/api/hub"
	apiinfra "github.com/geoffbelknap/agency/internal/api/infra"
	"github.com/geoffbelknap/agency/internal/api/platform"
	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/credstore"
	"github.com/geoffbelknap/agency/internal/docker"
	"github.com/geoffbelknap/agency/internal/profiles"
	"github.com/geoffbelknap/agency/internal/events"
	"github.com/geoffbelknap/agency/internal/knowledge"
	"github.com/geoffbelknap/agency/internal/logs"
	"github.com/geoffbelknap/agency/internal/orchestrate"
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
func RegisterSocketRoutes(r chi.Router, cfg *config.Config, dc *docker.Client, logger *log.Logger, startup *StartupResult, opts RouteOptions) {
	h := &handler{
		cfg: cfg, dc: dc, log: logger,
		infra: startup.Infra, agents: startup.AgentManager,
		halt: startup.HaltController, audit: startup.Audit,
		ctxMgr: startup.CtxMgr, mcpReg: startup.MCPReg,
		knowledge: startup.Knowledge, missions: startup.MissionManager,
		meeseeks: startup.MeeseeksManager, claims: startup.Claims,
		credStore: startup.CredStore, profileStore: startup.ProfileStore,
	}
	if opts.Hub != nil {
		h.wsHub = opts.Hub
	}
	if opts.EventBus != nil {
		h.eventBus = opts.EventBus
	}

	r.Get("/api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"status": "ok", "version": cfg.Version, "build_id": cfg.BuildID})
	})
	r.Post("/api/v1/agents/{name}/signal", h.relaySignal)

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

// RegisterRoutes sets up all REST API routes on the given router.
// The hub parameter is optional — if non-nil, the WebSocket endpoint is registered.
func RegisterRoutes(r chi.Router, cfg *config.Config, dc *docker.Client, logger *log.Logger, startup *StartupResult, hub ...*ws.Hub) {
	opts := RouteOptions{}
	if len(hub) > 0 {
		opts.Hub = hub[0]
	}
	RegisterRoutesWithOptions(r, cfg, dc, logger, startup, opts)
}

// RegisterRoutesWithOptions sets up all REST API routes with full option support.
func RegisterRoutesWithOptions(r chi.Router, cfg *config.Config, dc *docker.Client, logger *log.Logger, startup *StartupResult, opts RouteOptions) {
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
	if opts.Scheduler != nil {
		h.scheduler = opts.Scheduler
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
	if opts.DockerStatus != nil {
		h.dockerStatus = opts.DockerStatus
	}

	// Wire task completion handler before platform routes so it is set up
	// regardless of whether the hub is registered here or in platform.
	if opts.Hub != nil {
		h.wsHub = opts.Hub
		// Wire task completion handler — triggers success criteria evaluation
		// when task_complete signals arrive via the comms WebSocket relay.
		opts.Hub.SetTaskCompleteHandler(func(agent string, data map[string]interface{}) {
			h.evaluateTaskCompletion(agent, data)
		})
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

	r.Route("/api/v1", func(r chi.Router) {
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
		r.Delete("/agents/{name}/cache", h.clearAgentCache)

		// Agent signals — enforcer relays body-originated signals here for
		// WebSocket broadcast. Mediated path: body → enforcer → gateway → hub.
		r.Post("/agents/{name}/signal", h.relaySignal)

		// Budget (computed from enforcer audit logs — no separate budget store)
		r.Get("/agents/{name}/budget", h.getBudget)
		r.Get("/agents/{name}/budget/remaining", h.getBudgetRemaining)

		// Economics (cost + performance analytics)
		r.Get("/agents/{name}/economics", h.getAgentEconomics)
		r.Get("/economics/summary", h.getEconomicsSummary)

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

		// Presets, deploy, hub, connector: handled by hub module (registered below)

		// Agent logs
		r.Get("/agents/{name}/logs", h.agentLogs)

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
	scheduler  *events.Scheduler
	claims        *orchestrate.MissionClaimRegistry
	healthMonitor *orchestrate.MissionHealthMonitor
	notifStore    *events.NotificationStore
	credStore     *credstore.Store
	profileStore  *profiles.Store
	dockerStatus  *docker.Status
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

		// Emit a companion workflow_economics signal so WebSocket clients
		// can display per-task cost data in real time.
		// TODO: enrich with aggregated loop_cost_usd and context_expansion_rate
		// from the economics rollup store once /economics endpoints land.
		econData := map[string]interface{}{
			"task_id": body.Data["task_id"],
			"steps":   body.Data["turns"],
		}
		if h.wsHub != nil {
			h.wsHub.BroadcastAgentSignal(name, "agent_signal_workflow_economics", econData)
		}
	}

	writeJSON(w, 200, map[string]string{"status": "ok"})
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
