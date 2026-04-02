package api

import (
	"net/http"
	"os"
	"path/filepath"
)

func (h *handler) openapiSpec(w http.ResponseWriter, r *http.Request) {
	// Serve from disk so the spec is always current — no stale embeds.
	// Look in source dir first (dev), then ~/.agency/ (deployed).
	paths := []string{
		filepath.Join(h.cfg.SourceDir, "agency-gateway", "internal", "api", "openapi.yaml"),
		filepath.Join(h.cfg.Home, "openapi.yaml"),
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err == nil {
			w.Header().Set("Content-Type", "application/yaml")
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.WriteHeader(200)
			w.Write(data)
			return
		}
	}
	http.Error(w, "OpenAPI spec not found", 404)
}
