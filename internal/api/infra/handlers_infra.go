package infra

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/geoffbelknap/agency/internal/budget"
	"github.com/geoffbelknap/agency/internal/events"
	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
	"github.com/geoffbelknap/agency/internal/infratier"
	"github.com/geoffbelknap/agency/internal/models"
)

// ── Infrastructure Status ────────────────────────────────────────────────────

func (h *handler) infraStatus(w http.ResponseWriter, r *http.Request) {
	backend := h.configuredBackend()
	if !runtimehost.IsContainerBackend(backend) || h.deps.Runtime == nil {
		limits := models.DefaultPlatformBudgetConfig()
		store := budget.NewStore(filepath.Join(h.deps.Config.Home, "budget"))
		infraState, _ := store.Load("_infrastructure")
		writeJSON(w, 200, map[string]interface{}{
			"version":                 h.deps.Config.Version,
			"build_id":                h.deps.Config.BuildID,
			"gateway_url":             "http://" + h.deps.Config.GatewayAddr,
			"web_url":                 "http://127.0.0.1:8280",
			"components":              []interface{}{},
			"infra_llm_daily_used":    infraState.DailyUsed,
			"infra_llm_daily_limit":   limits.InfraDaily,
			"backend":                 backend,
			"backend_endpoint":        runtimehost.ResolvedBackendEndpoint(backend, h.deps.Config.Hub.DeploymentBackendConfig),
			"backend_mode":            runtimehost.ResolvedBackendMode(backend, h.deps.Config.Hub.DeploymentBackendConfig),
			"infra_control_available": false,
			"container_backend":       "not_applicable",
			"host_runtime":            "not_applicable",
		})
		return
	}

	status, err := h.deps.Runtime.InfraStatus(r.Context())
	if err != nil {
		if h.deps.BackendHealth != nil {
			h.deps.BackendHealth.RecordError(err)
		}
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if h.deps.BackendHealth != nil {
		h.deps.BackendHealth.RecordSuccess()
	}
	limits := models.DefaultPlatformBudgetConfig()
	store := budget.NewStore(filepath.Join(h.deps.Config.Home, "budget"))
	infraState, _ := store.Load("_infrastructure")
	containerBackendState := func() string {
		if h.deps.BackendHealth != nil && !h.deps.BackendHealth.Available() {
			return "unavailable"
		}
		return "available"
	}()
	writeJSON(w, 200, map[string]interface{}{
		"version":                 h.deps.Config.Version,
		"build_id":                h.deps.Config.BuildID,
		"gateway_url":             "http://" + h.deps.Config.GatewayAddr,
		"web_url":                 "http://127.0.0.1:8280",
		"components":              status,
		"infra_llm_daily_used":    infraState.DailyUsed,
		"infra_llm_daily_limit":   limits.InfraDaily,
		"backend":                 backend,
		"backend_endpoint":        runtimehost.ResolvedBackendEndpoint(backend, h.deps.Config.Hub.DeploymentBackendConfig),
		"backend_mode":            runtimehost.ResolvedBackendMode(backend, h.deps.Config.Hub.DeploymentBackendConfig),
		"infra_control_available": true,
		"container_backend":       containerBackendState,
		"host_runtime": func() string {
			return containerBackendState
		}(),
	})
}

// ── Infrastructure Up ────────────────────────────────────────────────────────

func (h *handler) infraUp(w http.ResponseWriter, r *http.Request) {
	if !h.containerBackendRequired(w) {
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
			if h.deps.BackendHealth != nil {
				h.deps.BackendHealth.RecordError(err)
			}
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		if h.deps.BackendHealth != nil {
			h.deps.BackendHealth.RecordSuccess()
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
	var writeMu sync.Mutex
	onProgress := func(component, status string) {
		writeMu.Lock()
		defer writeMu.Unlock()
		enc.Encode(map[string]string{
			"type":      "progress",
			"component": component,
			"status":    status,
		})
		flusher.Flush()
	}

	if err := h.deps.Infra.EnsureRunningWithProgress(ctx, onProgress); err != nil {
		if h.deps.BackendHealth != nil {
			h.deps.BackendHealth.RecordError(err)
		}
		writeMu.Lock()
		enc.Encode(map[string]string{"type": "error", "error": err.Error()})
		flusher.Flush()
		writeMu.Unlock()
		return
	}
	if h.deps.BackendHealth != nil {
		h.deps.BackendHealth.RecordSuccess()
	}

	events.EmitInfraEvent(h.deps.EventBus, "infra_up", nil)
	writeMu.Lock()
	enc.Encode(map[string]string{"type": "done", "status": "running"})
	flusher.Flush()
	writeMu.Unlock()
}

// ── Infrastructure Down ──────────────────────────────────────────────────────

func (h *handler) infraDown(w http.ResponseWriter, r *http.Request) {
	if !h.containerBackendRequired(w) {
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
	var writeMu sync.Mutex

	onProgress := func(component, status string) {
		writeMu.Lock()
		defer writeMu.Unlock()
		enc.Encode(map[string]string{"type": "progress", "component": component, "status": status})
		flusher.Flush()
	}
	if err := h.deps.Infra.TeardownWithProgress(r.Context(), onProgress); err != nil {
		writeMu.Lock()
		enc.Encode(map[string]string{"type": "error", "error": err.Error()})
		flusher.Flush()
		writeMu.Unlock()
		return
	}
	writeMu.Lock()
	enc.Encode(map[string]string{"type": "done", "status": "stopped"})
	flusher.Flush()
	writeMu.Unlock()
}

// ── Infrastructure Rebuild ───────────────────────────────────────────────────

func (h *handler) infraRebuild(w http.ResponseWriter, r *http.Request) {
	if !h.containerBackendRequired(w) {
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
	var writeMu sync.Mutex

	onProgress := func(comp, status string) {
		writeMu.Lock()
		defer writeMu.Unlock()
		enc.Encode(map[string]string{"type": "progress", "component": comp, "status": status})
		flusher.Flush()
	}
	if err := h.deps.Infra.RestartComponentWithProgress(ctx, component, onProgress); err != nil {
		writeMu.Lock()
		enc.Encode(map[string]string{"type": "error", "error": err.Error()})
		flusher.Flush()
		writeMu.Unlock()
		return
	}
	writeMu.Lock()
	enc.Encode(map[string]string{"type": "done", "status": "restarted", "component": component})
	flusher.Flush()
	writeMu.Unlock()
}

// ── Infrastructure Reload ────────────────────────────────────────────────────

func (h *handler) infraReload(w http.ResponseWriter, r *http.Request) {
	if !h.containerBackendRequired(w) {
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
	components := infratier.ReloadComponents()
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

func (h *handler) infraLogs(w http.ResponseWriter, r *http.Request) {
	if !h.containerBackendRequired(w) {
		return
	}
	component := chi.URLParam(r, "component")
	tail := 200
	if raw := r.URL.Query().Get("tail"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tail must be a positive integer"})
			return
		}
		if parsed > 1000 {
			parsed = 1000
		}
		tail = parsed
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	out, err := h.deps.Runtime.InfraLogs(ctx, component, tail)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	if h.deps.Audit != nil {
		_ = h.deps.Audit.WriteSystem("infra_logs_read", map[string]interface{}{
			"component": component,
			"tail":      tail,
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"component": component,
		"tail":      tail,
		"logs":      out,
	})
}
