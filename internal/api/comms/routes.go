package comms

import (
	"encoding/json"
	"net/http"

	"github.com/charmbracelet/log"
	"github.com/go-chi/chi/v5"

	"github.com/geoffbelknap/agency/internal/config"
	commsClient "github.com/geoffbelknap/agency/internal/comms"
)

// Deps holds the dependencies required by the comms module.
type Deps struct {
	Comms  commsClient.Client
	Config *config.Config
	Logger *log.Logger
}

type handler struct {
	deps Deps
}

// RegisterRoutes mounts all channel/messaging routes onto r.
func RegisterRoutes(r chi.Router, d Deps) {
	h := &handler{deps: d}

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/channels", h.listChannels)
		r.Post("/channels", h.createChannel)
		r.Get("/channels/search", h.searchMessages)
		r.Get("/channels/{name}/messages", h.readMessages)
		r.Post("/channels/{name}/messages", h.sendMessage)
		r.Put("/channels/{name}/messages/{id}", h.editMessage)
		r.Delete("/channels/{name}/messages/{id}", h.deleteMessage)
		r.Post("/channels/{name}/messages/{id}/reactions", h.addReaction)
		r.Delete("/channels/{name}/messages/{id}/reactions/{emoji}", h.removeReaction)
		r.Post("/channels/{name}/archive", h.archiveChannel)
		r.Get("/unreads", h.getUnreads)
		r.Post("/channels/{name}/mark-read", h.markRead)
	})
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
