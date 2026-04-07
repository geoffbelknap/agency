package agents

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/charmbracelet/log"
	"github.com/go-chi/chi/v5"

	agencyctx "github.com/geoffbelknap/agency/internal/context"
	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/credstore"
	"github.com/geoffbelknap/agency/internal/docker"
	"github.com/geoffbelknap/agency/internal/events"
	"github.com/geoffbelknap/agency/internal/knowledge"
	"github.com/geoffbelknap/agency/internal/logs"
	"github.com/geoffbelknap/agency/internal/orchestrate"
	"github.com/geoffbelknap/agency/internal/ws"
)

// Ensure *docker.Client satisfies DockerClient at compile time.
var _ DockerClient = (*docker.Client)(nil)

// CommsClient is the interface for making requests to the comms service.
// Defined locally per Go convention: interfaces belong where they are consumed.
type CommsClient interface {
	CommsRequest(ctx context.Context, method, path string, body interface{}) ([]byte, error)
}

// SignalSender sends OS signals to named containers.
// Defined locally per Go convention: interfaces belong where they are consumed.
type SignalSender interface {
	SignalContainer(ctx context.Context, containerName, signal string) error
}

// DockerClient provides container exec and ID lookup used by agent handlers.
// Defined locally per Go convention: interfaces belong where they are consumed.
type DockerClient interface {
	ExecInContainer(ctx context.Context, containerName string, cmd []string) (string, error)
	ContainerShortID(ctx context.Context, name string) string
}

// Deps holds the dependencies required by the agents module.
type Deps struct {
	AgentManager    *orchestrate.AgentManager
	HaltController  *orchestrate.HaltController
	CtxMgr          *agencyctx.Manager
	Audit           *logs.Writer
	EventBus        *events.Bus                       // may be nil
	MeeseeksManager *orchestrate.MeeseeksManager
	Knowledge       *knowledge.Proxy
	MissionManager  *orchestrate.MissionManager
	Claims          *orchestrate.MissionClaimRegistry // for coordinator failover
	HealthMonitor   *orchestrate.MissionHealthMonitor // may be nil
	Scheduler       *events.Scheduler                 // may be nil
	Config          *config.Config
	Logger          *log.Logger
	CredStore       *credstore.Store
	DockerStatus    *docker.Status   // may be nil
	WSHub           *ws.Hub          // may be nil
	Comms           CommsClient
	Signal          SignalSender
	DC              DockerClient
	// RawDocker is required for StartSequence (container orchestration).
	// It is used only by start/restart handlers.
	RawDocker       *docker.Client
}

type handler struct {
	deps Deps
}

// RegisterRoutes mounts all agent lifecycle, config, grant/revoke, budget,
// cache, economics, trajectory, meeseeks, context, and memory routes onto r.
func RegisterRoutes(r chi.Router, d Deps) {
	h := &handler{deps: d}

	r.Route("/api/v1", func(r chi.Router) {
		// Agent CRUD and lifecycle
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

		// Agent signals
		r.Post("/agents/{name}/signal", h.relaySignal)

		// Budget
		r.Get("/agents/{name}/budget", h.getBudget)
		r.Get("/agents/{name}/budget/remaining", h.getBudgetRemaining)

		// Economics
		r.Get("/agents/{name}/economics", h.getAgentEconomics)
		r.Get("/economics/summary", h.getEconomicsSummary)

		// Context API (mid-session constraint push)
		ctxH := &contextHandler{mgr: d.CtxMgr}
		r.Route("/agents/{name}/context", func(r chi.Router) {
			r.Get("/constraints", ctxH.getConstraints)
			r.Get("/exceptions", ctxH.getExceptions)
			r.Get("/policy", ctxH.getPolicy)
			r.Get("/changes", ctxH.getChanges)
			r.Post("/push", ctxH.push)
			r.Get("/status", ctxH.getStatus)
		})

		// Meeseeks
		r.Post("/meeseeks", h.spawnMeeseeks)
		r.Get("/meeseeks", h.listMeeseeks)
		r.Get("/meeseeks/{id}", h.showMeeseeks)
		r.Delete("/meeseeks/{id}", h.killMeeseeks)
		r.Delete("/meeseeks", h.killMeeseeksByParent) // kill all for a parent (?parent=<agent>)
		r.Post("/meeseeks/{id}/complete", h.completeMeeseeks)
	})
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
