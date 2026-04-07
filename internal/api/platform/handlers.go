package platform

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/credstore"
)

func (h *handler) openapiSpec(w http.ResponseWriter, r *http.Request) {
	// Serve from disk so the spec is always current — no stale embeds.
	// Look in source dir first (dev), then ~/.agency/ (deployed).
	paths := []string{
		filepath.Join(h.deps.Config.SourceDir, "agency-gateway", "internal", "api", "openapi.yaml"),
		filepath.Join(h.deps.Config.Home, "openapi.yaml"),
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err == nil {
			w.Header().Set("Content-Type", "application/yaml")
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.WriteHeader(200)
			w.Write(data) //nolint:errcheck
			return
		}
	}
	http.Error(w, "OpenAPI spec not found", 404)
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
	h.deps.WSHub.HandleWebSocket(w, r)
}
