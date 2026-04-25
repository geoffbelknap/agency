package platform

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"time"

	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/credstore"
	"github.com/geoffbelknap/agency/internal/openapispec"
	"github.com/geoffbelknap/agency/internal/principal"
)

func (h *handler) openapiSpec(w http.ResponseWriter, r *http.Request) {
	h.serveOpenAPISpec(w, "full")
}

func (h *handler) openapiCoreSpec(w http.ResponseWriter, r *http.Request) {
	h.serveOpenAPISpec(w, "core")
}

func (h *handler) serveOpenAPISpec(w http.ResponseWriter, view string) {
	// Serve from disk so the spec is always current — no stale embeds.
	// Look in source dir first (dev), then ~/.agency/ (deployed).
	paths := []string{
		filepath.Join(h.deps.Config.SourceDir, "agency-gateway", "internal", "api", "openapi.yaml"),
		filepath.Join(h.deps.Config.SourceDir, "internal", "api", "openapi.yaml"),
		filepath.Join(h.deps.Config.Home, "openapi.yaml"),
	}
	data, err := openapispec.Load(paths)
	if err != nil {
		http.Error(w, "OpenAPI spec not found", 404)
		return
	}
	if view == "core" {
		data, err = openapispec.FilterByTier(data, "core")
		if err != nil {
			http.Error(w, "OpenAPI core view unavailable", http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/yaml")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(200)
	w.Write(data) //nolint:errcheck
}

func (h *handler) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]string{
		"status":   "ok",
		"version":  h.deps.Config.Version,
		"build_id": h.deps.Config.BuildID,
	})
}

func (h *handler) initPlatform(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Operator     string            `json:"operator"`
		Force        bool              `json:"force"`
		Provider     string            `json:"provider"`
		APIKey       string            `json:"api_key"`
		ProviderKeys map[string]string `json:"provider_keys"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err.Error() != "EOF" {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}

	opts := config.InitOptions{
		Operator:     body.Operator,
		Force:        body.Force,
		Provider:     body.Provider,
		APIKey:       body.APIKey,
		ProviderKeys: body.ProviderKeys,
	}
	pendingKeys, err := config.RunInit(opts)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}

	// Store any new API keys in the credential store
	for _, key := range pendingKeys {
		if h.deps.CredStore != nil {
			now := time.Now().UTC().Format(time.RFC3339)
			h.deps.CredStore.Put(credstore.Entry{ //nolint:errcheck
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

	writeJSON(w, 200, map[string]string{"status": "initialized", "home": h.deps.Config.Home})
}

func (h *handler) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	if h.deps.WSHub == nil {
		writeJSON(w, 500, map[string]string{"error": "websocket hub not initialized"})
		return
	}
	// BearerAuth middleware has already validated the token and (best-effort)
	// resolved the principal into the request context. Pass it to the hub
	// so the Client carries its identity for scope filtering and audit.
	h.deps.WSHub.HandleWebSocket(w, r, principal.Get(r))
}
