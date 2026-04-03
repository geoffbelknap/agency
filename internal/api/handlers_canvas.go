package api

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
)

// getCanvas handles GET /api/v1/missions/{name}/canvas
func (h *handler) getCanvas(w http.ResponseWriter, r *http.Request) {
	name := safeName(w, chi.URLParam(r, "name"))
	if name == "" {
		return
	}
	canvasPath := filepath.Join(h.cfg.Home, "missions", name+".canvas.json")

	data, err := os.ReadFile(canvasPath)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, 404, map[string]string{"error": "no canvas for this mission"})
			return
		}
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

// putCanvas handles PUT /api/v1/missions/{name}/canvas
func (h *handler) putCanvas(w http.ResponseWriter, r *http.Request) {
	name := safeName(w, chi.URLParam(r, "name"))
	if name == "" {
		return
	}

	// Verify mission exists
	missionPath := filepath.Join(h.cfg.Home, "missions", name+".yaml")
	if _, err := os.Stat(missionPath); os.IsNotExist(err) {
		writeJSON(w, 404, map[string]string{"error": "mission not found"})
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MB limit
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "failed to read body"})
		return
	}

	// Validate it's valid JSON
	var doc map[string]interface{}
	if json.Unmarshal(body, &doc) != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}

	canvasPath := filepath.Join(h.cfg.Home, "missions", name+".canvas.json")
	if err := os.WriteFile(canvasPath, body, 0644); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, 200, map[string]string{"status": "saved"})
}

// deleteCanvas handles DELETE /api/v1/missions/{name}/canvas
func (h *handler) deleteCanvas(w http.ResponseWriter, r *http.Request) {
	name := safeName(w, chi.URLParam(r, "name"))
	if name == "" {
		return
	}
	canvasPath := filepath.Join(h.cfg.Home, "missions", name+".canvas.json")
	os.Remove(canvasPath) // ignore errors — may not exist
	writeJSON(w, 200, map[string]string{"status": "deleted"})
}
