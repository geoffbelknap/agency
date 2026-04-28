package agents

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"log/slog"

	"github.com/geoffbelknap/agency/internal/backendhealth"
	"github.com/geoffbelknap/agency/internal/config"
	agencyctx "github.com/geoffbelknap/agency/internal/context"
	"github.com/geoffbelknap/agency/internal/credstore"
	"github.com/geoffbelknap/agency/internal/events"
	"github.com/geoffbelknap/agency/internal/features"
	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
	"github.com/geoffbelknap/agency/internal/knowledge"
	"github.com/geoffbelknap/agency/internal/logs"
	"github.com/geoffbelknap/agency/internal/orchestrate"
	runtimecontract "github.com/geoffbelknap/agency/internal/runtime/contract"
	"github.com/geoffbelknap/agency/internal/ws"
)

// Ensure the host-scoped backend client satisfies RuntimeInstanceClient at compile time.
var _ RuntimeInstanceClient = (*runtimehost.Client)(nil)

// CommsClient is the interface for making requests to the comms service.
// Defined locally per Go convention: interfaces belong where they are consumed.
type CommsClient interface {
	CommsRequest(ctx context.Context, method, path string, body interface{}) ([]byte, error)
}

// SignalSender sends OS signals to named runtime instances.
// Defined locally per Go convention: interfaces belong where they are consumed.
type SignalSender interface {
	Signal(ctx context.Context, ref runtimecontract.InstanceRef, signal string) error
}

// RuntimeInstanceClient provides runtime exec and instance ID lookup used by agent handlers.
// Defined locally per Go convention: interfaces belong where they are consumed.
type RuntimeInstanceClient interface {
	Exec(ctx context.Context, ref runtimecontract.InstanceRef, cmd []string) (string, error)
	ShortID(ctx context.Context, ref runtimecontract.InstanceRef) string
}

// Deps holds the dependencies required by the agents module.
type Deps struct {
	AgentManager    *orchestrate.AgentManager
	HaltController  *orchestrate.HaltController
	CtxMgr          *agencyctx.Manager
	Audit           *logs.Writer
	EventBus        *events.Bus // may be nil
	MeeseeksManager *orchestrate.MeeseeksManager
	Knowledge       *knowledge.Proxy
	MissionManager  *orchestrate.MissionManager
	Claims          *orchestrate.MissionClaimRegistry // for coordinator failover
	HealthMonitor   *orchestrate.MissionHealthMonitor // may be nil
	Scheduler       *events.Scheduler                 // may be nil
	Config          *config.Config
	Logger          *slog.Logger
	CredStore       *credstore.Store
	BackendHealth   backendhealth.Availability // may be nil
	WSHub           *ws.Hub                    // may be nil
	Comms           CommsClient
	Signal          SignalSender
	Runtime         RuntimeInstanceClient
	// RuntimeHost is required for StartSequence backend orchestration.
	// It is used only by start/restart handlers.
	RuntimeHost *runtimehost.Client
}

type handler struct {
	deps Deps
}

// RegisterRoutes mounts all agent lifecycle, config, grant/revoke, budget,
// cache, economics, trajectory, meeseeks, context, and memory routes onto r.
func RegisterRoutes(r chi.Router, d Deps) {
	h := &handler{deps: d}

	// Agent CRUD and lifecycle
	r.Get("/api/v1/agents", h.listAgents)
	r.Post("/api/v1/agents", h.createAgent)
	r.Get("/api/v1/agents/{name}", h.showAgent)
	r.Delete("/api/v1/agents/{name}", h.deleteAgent)
	r.Post("/api/v1/agents/{name}/start", h.startAgent)
	r.Post("/api/v1/agents/{name}/restart", h.restartAgent)
	r.Post("/api/v1/agents/{name}/stop", h.haltAgent) // canonical stop endpoint
	r.Post("/api/v1/agents/{name}/halt", h.haltAgent) // alias: backward compat
	r.Post("/api/v1/agents/{name}/resume", h.resumeAgent)
	r.Post("/api/v1/agents/{name}/grant", h.grantAgent)
	r.Post("/api/v1/agents/{name}/revoke", h.revokeAgent)
	r.Get("/api/v1/agents/{name}/channels", h.agentChannels)
	r.Post("/api/v1/agents/{name}/dm", h.ensureAgentDM)
	r.Get("/api/v1/agents/{name}/results", h.listResults)
	r.Get("/api/v1/agents/{name}/results/{taskId}/metadata", h.getResultMetadata)
	r.Get("/api/v1/agents/{name}/results/{taskId}", h.getResult)
	r.Get("/api/v1/agents/{name}/pact/runs/{taskId}", h.getPactRun)
	r.Get("/api/v1/agents/{name}/pact/runs/{taskId}/audit-report", h.getPactAuditReport)
	r.Post("/api/v1/agents/{name}/pact/runs/{taskId}/audit-report/verify", h.verifyPactAuditReport)
	r.Get("/api/v1/agents/{name}/config", h.agentConfig)
	r.Put("/api/v1/agents/{name}/config", h.updateAgentConfig)
	r.Get("/api/v1/agents/{name}/runtime/manifest", h.showRuntimeManifest)
	r.Get("/api/v1/agents/{name}/runtime/status", h.showRuntimeStatus)
	r.Post("/api/v1/agents/{name}/runtime/validate", h.validateRuntime)
	r.Get("/api/v1/agents/{name}/procedures", h.listAgentProcedures)
	r.Get("/api/v1/agents/{name}/episodes", h.listAgentEpisodes)
	r.Get("/api/v1/agents/{name}/trajectory", h.getAgentTrajectory)
	r.Delete("/api/v1/agents/{name}/cache", h.clearAgentCache)

	// Agent signals
	r.Post("/api/v1/agents/{name}/signal", h.relaySignal)

	// Budget
	r.Get("/api/v1/agents/{name}/budget", h.getBudget)
	r.Get("/api/v1/agents/{name}/budget/remaining", h.getBudgetRemaining)

	// Economics
	r.Get("/api/v1/agents/{name}/economics", h.getAgentEconomics)
	r.Get("/api/v1/agents/economics/summary", h.getEconomicsSummary)

	// Context API (mid-session constraint push)
	ctxH := &contextHandler{mgr: d.CtxMgr}
	r.Route("/api/v1/agents/{name}/context", func(r chi.Router) {
		r.Get("/constraints", ctxH.getConstraints)
		r.Get("/exceptions", ctxH.getExceptions)
		r.Get("/policy", ctxH.getPolicy)
		r.Get("/changes", ctxH.getChanges)
		r.Post("/push", ctxH.push)
		r.Get("/status", ctxH.getStatus)
		r.Get("/ws", h.connectContextWS)
	})

	r.Get("/api/v1/agents/{name}/logs", h.agentLogs)

	if features.ExperimentalEnabled() {
		// Meeseeks
		r.Post("/api/v1/agents/meeseeks", h.spawnMeeseeks)
		r.Get("/api/v1/agents/meeseeks", h.listMeeseeks)
		r.Get("/api/v1/agents/meeseeks/{id}", h.showMeeseeks)
		r.Delete("/api/v1/agents/meeseeks/{id}", h.killMeeseeks)
		r.Delete("/api/v1/agents/meeseeks", h.killMeeseeksByParent) // kill all for a parent (?parent=<agent>)
		r.Post("/api/v1/agents/meeseeks/{id}/complete", h.completeMeeseeks)
	}
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
