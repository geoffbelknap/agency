package comms

import (
	"context"
	"encoding/json"
	"net/http"

	"log/slog"
	"github.com/go-chi/chi/v5"

	"github.com/geoffbelknap/agency/internal/config"
	commsClient "github.com/geoffbelknap/agency/internal/comms"
	"github.com/geoffbelknap/agency/internal/orchestrate"
)

type agentLister interface {
	List(ctx context.Context) ([]orchestrate.AgentDetail, error)
}

// Deps holds the dependencies required by the comms module.
type Deps struct {
	Comms        commsClient.Client
	AgentManager agentLister
	Config       *config.Config
	Logger       *slog.Logger
}

type handler struct {
	deps Deps
}

// RegisterRoutes mounts all channel/messaging routes onto r.
func RegisterRoutes(r chi.Router, d Deps) {
	h := &handler{deps: d}

	r.Get("/api/v1/comms/channels", h.listChannels)
	r.Post("/api/v1/comms/channels", h.createChannel)
	r.Get("/api/v1/comms/channels/search", h.searchMessages)
	r.Get("/api/v1/comms/channels/{name}/messages", h.readMessages)
	r.Post("/api/v1/comms/channels/{name}/messages", h.sendMessage)
	r.Put("/api/v1/comms/channels/{name}/messages/{id}", h.editMessage)
	r.Delete("/api/v1/comms/channels/{name}/messages/{id}", h.deleteMessage)
	r.Post("/api/v1/comms/channels/{name}/messages/{id}/reactions", h.addReaction)
	r.Delete("/api/v1/comms/channels/{name}/messages/{id}/reactions/{emoji}", h.removeReaction)
	r.Post("/api/v1/comms/channels/{name}/archive", h.archiveChannel)
	r.Get("/api/v1/comms/unreads", h.getUnreads)
	r.Post("/api/v1/comms/channels/{name}/mark-read", h.markRead)
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
