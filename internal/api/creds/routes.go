package creds

import (
	"encoding/json"
	"net/http"

	"log/slog"
	"github.com/go-chi/chi/v5"

	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/credstore"
	"github.com/geoffbelknap/agency/internal/logs"
)

// Deps holds the dependencies required by the creds module.
type Deps struct {
	CredStore *credstore.Store
	Audit     *logs.Writer
	Config    *config.Config
	Logger    *slog.Logger
}

type handler struct {
	deps Deps
}

// RegisterRoutes mounts all credential routes onto r.
func RegisterRoutes(r chi.Router, d Deps) {
	h := &handler{deps: d}

	r.Route("/api/v1/credentials", func(r chi.Router) {
		r.Post("/", h.createOrUpdateCredential)
		r.Get("/", h.listCredentials)
		r.Get("/{name}", h.showCredential)
		r.Delete("/{name}", h.deleteCredential)
		r.Post("/{name}/rotate", h.rotateCredential)
		r.Post("/{name}/test", h.testCredential)
		r.Post("/groups", h.createCredentialGroup)
	})

	r.Get("/api/v1/internal/credentials/resolve", h.resolveCredential)
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
