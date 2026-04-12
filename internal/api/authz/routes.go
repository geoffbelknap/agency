package authz

import (
	"encoding/json"
	"net/http"

	"log/slog"

	authzcore "github.com/geoffbelknap/agency/internal/authz"
	"github.com/go-chi/chi/v5"
)

// Deps holds the dependencies required by the authz module.
type Deps struct {
	Resolver authzcore.Resolver
	Logger   *slog.Logger
}

type handler struct {
	deps Deps
}

// RegisterRoutes mounts V2 authz routes onto r.
func RegisterRoutes(r chi.Router, d Deps) {
	h := &handler{deps: d}
	r.Post("/api/v1/authz/resolve", h.resolve)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}
