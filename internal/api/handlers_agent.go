package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"

	"github.com/geoffbelknap/agency/internal/hub"
	"github.com/geoffbelknap/agency/internal/logs"
	"github.com/geoffbelknap/agency/internal/models"
)

// ── Channels (extended) ─────────────────────────────────────────────────────

func (h *handler) createChannel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name  string `json:"name"`
		Topic string `json:"topic"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	if body.Name == "" {
		writeJSON(w, 400, map[string]string{"error": "name required"})
		return
	}

	commsBody := map[string]interface{}{
		"name":       body.Name,
		"type":       "team",
		"topic":      body.Topic,
		"created_by": "_operator",
		"members":    []string{"_operator"},
	}
	data, err := h.dc.CommsRequest(r.Context(), "POST", "/channels", commsBody)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(201)
	w.Write(data)
}

func (h *handler) searchMessages(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	channel := r.URL.Query().Get("channel")
	if query == "" {
		writeJSON(w, 400, map[string]string{"error": "q parameter required"})
		return
	}

	path := "/search?q=" + query + "&reader=_operator"
	if channel != "" {
		path += "&channel=" + channel
	}
	data, err := h.dc.CommsRequest(r.Context(), "GET", path, nil)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

func (h *handler) archiveChannel(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	data, err := h.dc.CommsRequest(r.Context(), "POST", "/channels/"+name+"/archive", nil)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

func (h *handler) getUnreads(w http.ResponseWriter, r *http.Request) {
	data, err := h.dc.CommsRequest(r.Context(), "GET", "/unreads/_operator", nil)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

func (h *handler) markRead(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	body := map[string]interface{}{"participant": "_operator"}
	data, err := h.dc.CommsRequest(r.Context(), "POST", "/channels/"+name+"/mark-read", body)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

// ── Teams ───────────────────────────────────────────────────────────────────

func (h *handler) listTeams(w http.ResponseWriter, r *http.Request) {
	teamsDir := filepath.Join(h.cfg.Home, "teams")
	entries, err := os.ReadDir(teamsDir)
	if err != nil {
		writeJSON(w, 200, []interface{}{})
		return
	}

	var teams []map[string]interface{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		team := map[string]interface{}{"name": e.Name()}
		teamPath := filepath.Join(teamsDir, e.Name(), "team.yaml")
		if data, err := os.ReadFile(teamPath); err == nil {
			var t map[string]interface{}
			if yaml.Unmarshal(data, &t) == nil {
				team = t
				if team["name"] == nil {
					team["name"] = e.Name()
				}
			}
		}
		// Count members
		if members, ok := team["members"].([]interface{}); ok {
			team["member_count"] = len(members)
		} else {
			team["member_count"] = 0
		}
		teams = append(teams, team)
	}
	writeJSON(w, 200, teams)
}

func (h *handler) createTeam(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name   string   `json:"name"`
		Agents []string `json:"agents"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	body.Name = filepath.Base(body.Name)
	if body.Name == "" || body.Name == "." || body.Name == ".." {
		writeJSON(w, 400, map[string]string{"error": "invalid name"})
		return
	}

	teamDir := filepath.Join(h.cfg.Home, "teams", body.Name)
	if err := os.MkdirAll(teamDir, 0755); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}

	team := map[string]interface{}{
		"name":    body.Name,
		"members": body.Agents,
	}
	data, _ := yaml.Marshal(team)
	if err := os.WriteFile(filepath.Join(teamDir, "team.yaml"), data, 0644); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}

	h.log.Info("team created", "name", body.Name, "members", body.Agents)
	writeJSON(w, 201, map[string]string{"status": "created", "name": body.Name})
}

func (h *handler) showTeam(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(chi.URLParam(r, "name"))
	if name == "" || name == "." || name == ".." {
		writeJSON(w, 400, map[string]string{"error": "invalid name"})
		return
	}
	teamPath := filepath.Join(h.cfg.Home, "teams", name, "team.yaml")
	data, err := os.ReadFile(teamPath)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "team not found: " + name})
		return
	}
	var team map[string]interface{}
	if err := yaml.Unmarshal(data, &team); err != nil {
		writeJSON(w, 500, map[string]string{"error": "invalid team config"})
		return
	}
	writeJSON(w, 200, team)
}

func (h *handler) teamActivity(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(chi.URLParam(r, "name"))
	if name == "" || name == "." || name == ".." {
		writeJSON(w, 400, map[string]string{"error": "invalid name"})
		return
	}

	// Read team config to find members
	teamPath := filepath.Join(h.cfg.Home, "teams", name, "team.yaml")
	data, err := os.ReadFile(teamPath)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "team not found: " + name})
		return
	}
	var team map[string]interface{}
	yaml.Unmarshal(data, &team)

	// Aggregate audit logs from all team members
	members, _ := team["members"].([]interface{})
	var activity []map[string]interface{}
	reader := logs.NewReader(h.cfg.Home)
	for _, m := range members {
		memberName, ok := m.(string)
		if !ok {
			continue
		}
		events, err := reader.ReadAgentLog(memberName, "", "")
		if err != nil {
			continue
		}
		// Take last 20 events per member
		start := 0
		if len(events) > 20 {
			start = len(events) - 20
		}
		for _, e := range events[start:] {
			e["agent"] = memberName
			activity = append(activity, e)
		}
	}

	writeJSON(w, 200, activity)
}

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
