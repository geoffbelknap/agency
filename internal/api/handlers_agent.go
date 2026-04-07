package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/geoffbelknap/agency/internal/hub"
	"github.com/geoffbelknap/agency/internal/models"
)

// ── Connectors ──────────────────────────────────────────────────────────────
// These handlers delegate to the hub instance registry. The old flat-file
// connector routes (GET /connectors, POST /connectors/{name}/activate, etc.)
// are removed; callers should use the /hub/ routes instead.

func (h *handler) listConnectors(w http.ResponseWriter, r *http.Request) {
	mgr := hub.NewManager(h.cfg.Home)
	instances := mgr.Registry.List("connector")
	var result []map[string]interface{}
	for _, inst := range instances {
		result = append(result, map[string]interface{}{
			"name":   inst.Name,
			"id":     inst.ID,
			"source": inst.Source,
			"state":  inst.State,
		})
	}
	if result == nil {
		result = []map[string]interface{}{}
	}
	writeJSON(w, 200, result)
}

func (h *handler) activateConnector(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	mgr := hub.NewManager(h.cfg.Home)
	inst := mgr.Registry.Resolve(name)
	if inst == nil {
		writeJSON(w, 404, map[string]string{"error": "connector not found: " + name})
		return
	}
	if err := mgr.Registry.SetState(name, "active"); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	// Signal intake to pick up the change
	h.dc.CommsRequest(r.Context(), "POST", "/connectors/"+inst.Name+"/activate", nil)
	h.log.Info("connector activated", "name", inst.Name, "id", inst.ID)
	writeJSON(w, 200, map[string]string{"status": "activated", "connector": inst.Name, "id": inst.ID})
}

func (h *handler) deactivateConnector(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	mgr := hub.NewManager(h.cfg.Home)
	inst := mgr.Registry.Resolve(name)
	if inst == nil {
		writeJSON(w, 404, map[string]string{"error": "connector not found: " + name})
		return
	}
	if err := mgr.Registry.SetState(name, "inactive"); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	h.dc.CommsRequest(r.Context(), "POST", "/connectors/"+inst.Name+"/deactivate", nil)
	h.log.Info("connector deactivated", "name", inst.Name, "id", inst.ID)
	writeJSON(w, 200, map[string]string{"status": "deactivated", "connector": inst.Name, "id": inst.ID})
}

func (h *handler) connectorStatus(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	mgr := hub.NewManager(h.cfg.Home)
	inst := mgr.Registry.Resolve(name)
	if inst == nil {
		writeJSON(w, 404, map[string]string{"error": "connector not found: " + name})
		return
	}

	status := map[string]interface{}{
		"name":   inst.Name,
		"id":     inst.ID,
		"source": inst.Source,
		"state":  inst.State,
	}

	// Try to get live status from intake
	liveData, err := h.dc.CommsRequest(r.Context(), "GET", "/connectors/"+inst.Name+"/status", nil)
	if err == nil {
		var liveStatus map[string]interface{}
		if json.Unmarshal(liveData, &liveStatus) == nil {
			for k, v := range liveStatus {
				status[k] = v
			}
		}
	}

	writeJSON(w, 200, status)
}

// ── Intake ──────────────────────────────────────────────────────────────────

func (h *handler) intakeItems(w http.ResponseWriter, r *http.Request) {
	connector := r.URL.Query().Get("connector")
	path := "/items"
	if connector != "" {
		path += "?connector=" + connector
	}

	out, err := serviceGet(r.Context(), "8205", path)
	if err != nil {
		writeJSON(w, 200, []interface{}{})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(out)
}

func (h *handler) intakeStats(w http.ResponseWriter, r *http.Request) {
	out, err := serviceGet(r.Context(), "8205", "/stats")
	if err != nil {
		writeJSON(w, 200, map[string]interface{}{"total": 0, "pending": 0, "completed": 0})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(out)
}

func (h *handler) intakeWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "read body: " + err.Error()})
		return
	}

	webhookURL := "http://localhost:8205/webhook"
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	req.Header.Set("Content-Type", r.Header.Get("Content-Type"))

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": "intake unreachable: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	out, _ := io.ReadAll(resp.Body)

	// Publish connector event to event bus (Task 13: connector source integration)
	if resp.StatusCode < 400 && h.eventBus != nil {
		var payload map[string]interface{}
		if json.Unmarshal(body, &payload) == nil {
			connectorName, _ := payload["connector"].(string)
			if connectorName == "" {
				connectorName = "unknown"
			}
			eventType, _ := payload["event_type"].(string)
			if eventType == "" {
				eventType = "webhook_received"
			}
			event := models.NewEvent(models.EventSourceConnector, connectorName, eventType, payload)
			h.eventBus.Publish(event)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(out)
}

// serviceGet makes a GET request to an infra service via its localhost port.
func serviceGet(ctx context.Context, port, path string) ([]byte, error) {
	url := "http://localhost:" + port + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return out, fmt.Errorf("service (port %s) returned %d", port, resp.StatusCode)
	}
	return out, nil
}

// loadPresetScopes and rebuildServicesManifest have been unified into
// generateAgentManifest in manifest.go.
