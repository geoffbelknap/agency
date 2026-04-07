package infra

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/geoffbelknap/agency/internal/budget"
	"github.com/geoffbelknap/agency/internal/events"
	"github.com/geoffbelknap/agency/internal/models"
)

// ── Infrastructure Status ────────────────────────────────────────────────────

func (h *handler) infraStatus(w http.ResponseWriter, r *http.Request) {
	status, err := h.deps.DC.InfraStatus(r.Context())
	if err != nil {
		if h.deps.DockerStatus != nil {
			h.deps.DockerStatus.RecordError(err)
		}
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if h.deps.DockerStatus != nil {
		h.deps.DockerStatus.RecordSuccess()
	}
	limits := models.DefaultPlatformBudgetConfig()
	store := budget.NewStore(filepath.Join(h.deps.Config.Home, "budget"))
	infraState, _ := store.Load("_infrastructure")
	writeJSON(w, 200, map[string]interface{}{
		"version":               h.deps.Config.Version,
		"build_id":              h.deps.Config.BuildID,
		"gateway_url":           "http://" + h.deps.Config.GatewayAddr,
		"web_url":               "https://127.0.0.1:8280",
		"components":            status,
		"infra_llm_daily_used":  infraState.DailyUsed,
		"infra_llm_daily_limit": limits.InfraDaily,
		"docker": func() string {
			if h.deps.DockerStatus != nil && !h.deps.DockerStatus.Available() {
				return "unavailable"
			}
			return "available"
		}(),
	})
}

// ── Infrastructure Up ────────────────────────────────────────────────────────

func (h *handler) infraUp(w http.ResponseWriter, r *http.Request) {
	if !h.dockerRequired(w) {
		return
	}
	if h.deps.Infra == nil {
		writeJSON(w, 500, map[string]string{"error": "infrastructure manager not initialized"})
		return
	}

	// If the client accepts NDJSON, stream progress events.
	stream := r.Header.Get("Accept") == "application/x-ndjson"

	// Use background context: infra startup must complete even if the client
	// disconnects (otherwise we leave infrastructure half-running).
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if !stream {
		if err := h.deps.Infra.EnsureRunning(ctx); err != nil {
			if h.deps.DockerStatus != nil {
				h.deps.DockerStatus.RecordError(err)
			}
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		if h.deps.DockerStatus != nil {
			h.deps.DockerStatus.RecordSuccess()
		}
		events.EmitInfraEvent(h.deps.EventBus, "infra_up", nil)
		writeJSON(w, 200, map[string]string{"status": "running"})
		return
	}

	// Streaming mode: send progress events as NDJSON lines.
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, 500, map[string]string{"error": "streaming not supported"})
		return
	}

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(200)

	enc := json.NewEncoder(w)
	onProgress := func(component, status string) {
		enc.Encode(map[string]string{
			"type":      "progress",
			"component": component,
			"status":    status,
		})
		flusher.Flush()
	}

	if err := h.deps.Infra.EnsureRunningWithProgress(ctx, onProgress); err != nil {
		if h.deps.DockerStatus != nil {
			h.deps.DockerStatus.RecordError(err)
		}
		enc.Encode(map[string]string{"type": "error", "error": err.Error()})
		flusher.Flush()
		return
	}
	if h.deps.DockerStatus != nil {
		h.deps.DockerStatus.RecordSuccess()
	}

	events.EmitInfraEvent(h.deps.EventBus, "infra_up", nil)
	enc.Encode(map[string]string{"type": "done", "status": "running"})
	flusher.Flush()
}

// ── Infrastructure Down ──────────────────────────────────────────────────────

func (h *handler) infraDown(w http.ResponseWriter, r *http.Request) {
	if !h.dockerRequired(w) {
		return
	}
	if h.deps.Infra == nil {
		writeJSON(w, 500, map[string]string{"error": "infrastructure manager not initialized"})
		return
	}

	stream := r.Header.Get("Accept") == "application/x-ndjson"
	if !stream {
		if err := h.deps.Infra.Teardown(r.Context()); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		events.EmitInfraEvent(h.deps.EventBus, "infra_down", nil)
		writeJSON(w, 200, map[string]string{"status": "stopped"})
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, 500, map[string]string{"error": "streaming not supported"})
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(200)
	enc := json.NewEncoder(w)

	onProgress := func(component, status string) {
		enc.Encode(map[string]string{"type": "progress", "component": component, "status": status})
		flusher.Flush()
	}
	if err := h.deps.Infra.TeardownWithProgress(r.Context(), onProgress); err != nil {
		enc.Encode(map[string]string{"type": "error", "error": err.Error()})
		flusher.Flush()
		return
	}
	enc.Encode(map[string]string{"type": "done", "status": "stopped"})
	flusher.Flush()
}

// ── Infrastructure Rebuild ───────────────────────────────────────────────────

func (h *handler) infraRebuild(w http.ResponseWriter, r *http.Request) {
	if !h.dockerRequired(w) {
		return
	}
	component := chi.URLParam(r, "component")
	if h.deps.Infra == nil {
		writeJSON(w, 500, map[string]string{"error": "infrastructure manager not initialized"})
		return
	}

	stream := r.Header.Get("Accept") == "application/x-ndjson"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if !stream {
		if err := h.deps.Infra.RestartComponent(ctx, component); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"status": "restarted", "component": component})
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, 500, map[string]string{"error": "streaming not supported"})
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(200)
	enc := json.NewEncoder(w)

	onProgress := func(comp, status string) {
		enc.Encode(map[string]string{"type": "progress", "component": comp, "status": status})
		flusher.Flush()
	}
	if err := h.deps.Infra.RestartComponentWithProgress(ctx, component, onProgress); err != nil {
		enc.Encode(map[string]string{"type": "error", "error": err.Error()})
		flusher.Flush()
		return
	}
	enc.Encode(map[string]string{"type": "done", "status": "restarted", "component": component})
	flusher.Flush()
}

// ── Infrastructure Reload ────────────────────────────────────────────────────

func (h *handler) infraReload(w http.ResponseWriter, r *http.Request) {
	if !h.dockerRequired(w) {
		return
	}
	if h.deps.Infra == nil {
		writeJSON(w, 500, map[string]string{"error": "infrastructure manager not initialized"})
		return
	}
	// Regenerate credential-swaps.yaml before reloading — uses credential
	// store as source of truth, falling back to legacy file-based generation.
	h.regenerateSwapConfig()

	// Reload restarts all infra components to pick up config changes
	components := []string{"egress", "comms", "knowledge", "intake"}
	var reloaded []string
	for _, comp := range components {
		if err := h.deps.Infra.RestartComponent(r.Context(), comp); err != nil {
			h.deps.Logger.Warn("reload skip", "component", comp, "err", err)
			continue
		}
		reloaded = append(reloaded, comp)
	}
	writeJSON(w, 200, map[string]interface{}{"status": "reloaded", "components": reloaded})
}
