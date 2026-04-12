package packages

import (
	"encoding/json"
	"net/http"

	"log/slog"

	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/hub"
	"github.com/go-chi/chi/v5"
)

// Deps holds the dependencies required by the packages module.
type Deps struct {
	Registry *hub.Registry
	Config   *config.Config
	Logger   *slog.Logger
}

type handler struct {
	deps Deps
}

// RegisterRoutes mounts V2 package routes onto r.
func RegisterRoutes(r chi.Router, d Deps) {
	h := &handler{deps: d}

	r.Get("/api/v1/packages", h.listPackages)
	r.Get("/api/v1/packages/{kind}/{name}", h.showPackage)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}
