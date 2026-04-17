package missions

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"

	"github.com/geoffbelknap/agency/internal/events"
	"github.com/geoffbelknap/agency/internal/models"
	"github.com/geoffbelknap/agency/internal/orchestrate"
)

// signalMissionReload sends SIGHUP to the agent's enforcer so the body runtime
// re-reads the updated mission.yaml at the next task boundary.
// ASK tenet compliance: signal and audit write must not be separated — callers
// are responsible for writing the audit entry before or after calling this.
func (h *handler) signalMissionReload(agentName string) {
	go func() {
		ctx := context.Background()
		if h.deps.Runtime != nil {
			if err := h.deps.Runtime.ReloadEnforcer(ctx, agentName); err != nil {
				h.deps.Logger.Warn("failed to reload runtime enforcer for mission update", "agent", agentName, "err", err)
			}
			return
		}
		enforcerName := fmt.Sprintf("agency-%s-enforcer", agentName)
		if err := h.deps.Signal.SignalContainer(ctx, enforcerName, "SIGHUP"); err != nil {
			h.deps.Logger.Warn("failed to signal enforcer for mission reload", "agent", agentName, "err", err)
		}
	}()
}

// createMission handles POST /api/v1/missions
// Accepts a YAML body with mission fields, persists a new mission, and returns 201.
func (h *handler) createMission(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "read body: " + err.Error()})
		return
	}

	var m models.Mission
	if err := yaml.Unmarshal(data, &m); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid YAML: " + err.Error()})
		return
	}

	if err := h.deps.MissionManager.Create(&m); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}

	h.deps.Audit.Write("_system", "mission_created", map[string]interface{}{
		"mission_id":   m.ID,
		"mission_name": m.Name,
		"build_id":     h.deps.Config.BuildID,
	})
	writeJSON(w, 201, m)
}

// listMissions handles GET /api/v1/missions
func (h *handler) listMissions(w http.ResponseWriter, r *http.Request) {
	missions, err := h.deps.MissionManager.List()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, missions)
}

// showMission handles GET /api/v1/missions/{name}
func (h *handler) showMission(w http.ResponseWriter, r *http.Request) {
	name, ok := requireName(w, chi.URLParam(r, "name"))
	if !ok {
		return
	}
	m, err := h.deps.MissionManager.Get(name)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}

	canvasPath := filepath.Join(h.deps.Config.Home, "missions", name+".canvas.json")
	_, canvasErr := os.Stat(canvasPath)
	hasCanvas := canvasErr == nil

	type missionResponse struct {
		*models.Mission
		HasCanvas bool `json:"has_canvas"`
	}
	writeJSON(w, 200, missionResponse{Mission: m, HasCanvas: hasCanvas})
}

// updateMission handles PUT /api/v1/missions/{name}
// Accepts a YAML body with updated mission fields.
func (h *handler) updateMission(w http.ResponseWriter, r *http.Request) {
	name, ok := requireName(w, chi.URLParam(r, "name"))
	if !ok {
		return
	}

	existing, err := h.deps.MissionManager.Get(name)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	oldVersion := existing.Version

	data, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "read body: " + err.Error()})
		return
	}

	var updated models.Mission
	if err := yaml.Unmarshal(data, &updated); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid YAML: " + err.Error()})
		return
	}

	if err := h.deps.MissionManager.Update(name, &updated); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}

	h.deps.Audit.Write("_system", "mission_updated", map[string]interface{}{
		"mission_id":   updated.ID,
		"mission_name": name,
		"old_version":  oldVersion,
		"new_version":  updated.Version,
		"build_id":     h.deps.Config.BuildID,
	})

	// Signal enforcer so body runtime reloads mission on next task boundary.
	if updated.AssignedTo != "" {
		h.signalMissionReload(updated.AssignedTo)
	}
	writeJSON(w, 200, updated)
}

// missionHealth handles GET /api/v1/missions/health and GET /api/v1/missions/{name}/health
func (h *handler) missionHealth(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" || name == "health" {
		// GET /missions/health — all missions
		missions, err := h.deps.MissionManager.List()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		checker := &orchestrate.MissionHealthChecker{Home: h.deps.Config.Home, CredStore: h.deps.CredStore}
		var results []orchestrate.MissionHealthResponse
		for _, m := range missions {
			if m.Status == "active" || m.Status == "paused" {
				results = append(results, checker.CheckHealth(m))
			}
		}
		if results == nil {
			results = []orchestrate.MissionHealthResponse{}
		}
		writeJSON(w, 200, map[string]interface{}{"missions": results})
		return
	}
	name, ok := requireName(w, name)
	if !ok {
		return
	}

	m, err := h.deps.MissionManager.Get(name)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}

	checker := &orchestrate.MissionHealthChecker{Home: h.deps.Config.Home, CredStore: h.deps.CredStore}
	resp := checker.CheckHealth(m)
	writeJSON(w, 200, resp)
}

// deleteMission handles DELETE /api/v1/missions/{name}
func (h *handler) deleteMission(w http.ResponseWriter, r *http.Request) {
	name, ok := requireName(w, chi.URLParam(r, "name"))
	if !ok {
		return
	}

	m, err := h.deps.MissionManager.Get(name)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}

	if err := h.deps.MissionManager.Delete(name); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}

	os.Remove(filepath.Join(h.deps.Config.Home, "missions", name+".canvas.json"))

	h.deps.Audit.Write("_system", "mission_deleted", map[string]interface{}{
		"mission_id":   m.ID,
		"mission_name": name,
		"build_id":     h.deps.Config.BuildID,
	})
	writeJSON(w, 200, map[string]string{"status": "deleted", "name": name})
}

// assignMission handles POST /api/v1/missions/{name}/assign
// Accepts JSON body: {"target": "...", "type": "agent|team"}
// Returns 422 with a structured pre-flight result when pre-flight checks fail.
func (h *handler) assignMission(w http.ResponseWriter, r *http.Request) {
	name, ok := requireName(w, chi.URLParam(r, "name"))
	if !ok {
		return
	}

	var body struct {
		Target string `json:"target"`
		Type   string `json:"type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	if body.Target == "" {
		writeJSON(w, 400, map[string]string{"error": "target required"})
		return
	}
	if body.Type == "" {
		body.Type = "agent"
	}

	// Run pre-flight separately so we can return a structured response on failure.
	mission, err := h.deps.MissionManager.Get(name)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}

	pf := h.deps.MissionManager.PreFlight(mission, body.Target, body.Type)
	if !pf.OK {
		writeJSON(w, 422, map[string]interface{}{
			"error":     "pre-flight failed: " + strings.Join(pf.Failures, "; "),
			"preflight": pf,
		})
		return
	}

	// Team assignment: load team config and use AssignToTeam for coordinator
	// routing and ASK tenet 11 validation.
	if body.Type == "team" {
		teamCfg, err := h.deps.MissionManager.LoadTeamConfig(body.Target)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": "load team config: " + err.Error()})
			return
		}
		if err := h.deps.MissionManager.AssignToTeam(name, body.Target, teamCfg); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
	} else {
		if err := h.deps.MissionManager.Assign(name, body.Target, body.Type); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
	}

	h.deps.Audit.Write(body.Target, "mission_assigned", map[string]interface{}{
		"mission_name": name,
		"target":       body.Target,
		"target_type":  body.Type,
		"build_id":     h.deps.Config.BuildID,
	})

	// Wire event bus: add mission subscriptions
	m, _ := h.deps.MissionManager.Get(name)
	if m != nil && h.deps.EventBus != nil {
		events.OnMissionAssigned(h.deps.EventBus, m)
		// Register schedule triggers
		if h.deps.Scheduler != nil {
			for _, t := range m.Triggers {
				if t.Source == "schedule" && t.Cron != "" {
					h.deps.Scheduler.Register(t.Name, t.Cron, "") //nolint:errcheck
				}
			}
		}
	}
	events.EmitMissionEvent(h.deps.EventBus, "mission_assigned", name, map[string]interface{}{
		"target": body.Target, "target_type": body.Type,
	})

	// Check for reflection-without-evaluation warning
	var warnings []string
	if m != nil {
		if m.Reflection != nil && m.Reflection.Enabled {
			if m.SuccessCriteria == nil || m.SuccessCriteria.Evaluation == nil || !m.SuccessCriteria.Evaluation.Enabled {
				warnings = append(warnings, fmt.Sprintf(
					"Mission %q has reflection enabled but no platform-side evaluation. "+
						"Reflection is agent-internal self-review. For external quality verification, "+
						"also enable success_criteria.evaluation.", m.Name))
			}
		}
	}

	// Build response with optional warnings
	response := map[string]interface{}{
		"status":  "assigned",
		"mission": name,
		"target":  body.Target,
	}
	if len(warnings) > 0 {
		response["warnings"] = warnings
	}
	writeJSON(w, 200, response)
}

// pauseMission handles POST /api/v1/missions/{name}/pause
func (h *handler) pauseMission(w http.ResponseWriter, r *http.Request) {
	name, ok := requireName(w, chi.URLParam(r, "name"))
	if !ok {
		return
	}

	// Capture assigned agent before pause for audit.
	m, err := h.deps.MissionManager.Get(name)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	agentName := m.AssignedTo
	if agentName == "" {
		agentName = "_system"
	}

	var body struct {
		Reason string `json:"reason"`
	}
	// Best-effort decode — reason is optional.
	json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck

	if err := h.deps.MissionManager.Pause(name, body.Reason); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}

	h.deps.Audit.Write(agentName, "mission_paused", map[string]interface{}{
		"mission_name": name,
		"reason":       body.Reason,
		"build_id":     h.deps.Config.BuildID,
	})

	// Wire event bus: deactivate mission subscriptions
	if h.deps.EventBus != nil {
		events.OnMissionPaused(h.deps.EventBus, name)
	}
	if h.deps.Scheduler != nil {
		for _, t := range m.Triggers {
			if t.Source == "schedule" {
				h.deps.Scheduler.Deactivate(t.Name)
			}
		}
	}
	events.EmitMissionEvent(h.deps.EventBus, "mission_paused", name, nil)

	// Signal enforcer so body runtime picks up the paused status.
	if agentName != "_system" {
		h.signalMissionReload(agentName)
	}
	writeJSON(w, 200, map[string]string{"status": "paused", "mission": name})
}

// resumeMission handles POST /api/v1/missions/{name}/resume
func (h *handler) resumeMission(w http.ResponseWriter, r *http.Request) {
	name, ok := requireName(w, chi.URLParam(r, "name"))
	if !ok {
		return
	}

	m, err := h.deps.MissionManager.Get(name)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	agentName := m.AssignedTo
	if agentName == "" {
		agentName = "_system"
	}

	if err := h.deps.MissionManager.Resume(name); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}

	h.deps.Audit.Write(agentName, "mission_resumed", map[string]interface{}{
		"mission_name": name,
		"build_id":     h.deps.Config.BuildID,
	})

	// Wire event bus: reactivate mission subscriptions
	if h.deps.EventBus != nil {
		events.OnMissionResumed(h.deps.EventBus, name)
	}
	if h.deps.Scheduler != nil {
		for _, t := range m.Triggers {
			if t.Source == "schedule" {
				h.deps.Scheduler.Activate(t.Name)
			}
		}
	}
	events.EmitMissionEvent(h.deps.EventBus, "mission_resumed", name, nil)

	// Signal enforcer so body runtime picks up the resumed status.
	if agentName != "_system" {
		h.signalMissionReload(agentName)
	}
	writeJSON(w, 200, map[string]string{"status": "resumed", "mission": name})
}

// completeMission handles POST /api/v1/missions/{name}/complete
func (h *handler) completeMission(w http.ResponseWriter, r *http.Request) {
	name, ok := requireName(w, chi.URLParam(r, "name"))
	if !ok {
		return
	}

	m, err := h.deps.MissionManager.Get(name)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	agentName := m.AssignedTo
	if agentName == "" {
		agentName = "_system"
	}

	if err := h.deps.MissionManager.Complete(name); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}

	h.deps.Audit.Write(agentName, "mission_completed", map[string]interface{}{
		"mission_name": name,
		"build_id":     h.deps.Config.BuildID,
	})

	// Wire event bus: remove mission subscriptions
	if h.deps.EventBus != nil {
		events.OnMissionCompleted(h.deps.EventBus, name)
	}
	if h.deps.Scheduler != nil {
		for _, t := range m.Triggers {
			if t.Source == "schedule" {
				h.deps.Scheduler.Remove(t.Name)
			}
		}
	}
	events.EmitMissionEvent(h.deps.EventBus, "mission_completed", name, nil)

	writeJSON(w, 200, map[string]string{"status": "completed", "mission": name})
}

// missionHistory handles GET /api/v1/missions/{name}/history
func (h *handler) missionHistory(w http.ResponseWriter, r *http.Request) {
	name, ok := requireName(w, chi.URLParam(r, "name"))
	if !ok {
		return
	}
	entries, err := h.deps.MissionManager.History(name)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, entries)
}

// missionKnowledge handles POST /api/v1/missions/{name}/knowledge
// Queries knowledge graph scoped to the mission ID. ASK tenet 24: knowledge
// access is bounded by authorization scope.
func (h *handler) missionKnowledge(w http.ResponseWriter, r *http.Request) {
	name, ok := requireName(w, chi.URLParam(r, "name"))
	if !ok {
		return
	}
	mission, err := h.deps.MissionManager.Get(name)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "mission not found"})
		return
	}

	var req struct {
		Query string `json:"query"`
	}
	json.NewDecoder(r.Body).Decode(&req) //nolint:errcheck

	// ASK tenet 24: verify requesting agent is assigned to this mission.
	agentName := r.Header.Get("X-Agent-Name")
	if mission.AssignedType == "agent" && mission.AssignedTo != agentName && agentName != "" {
		writeJSON(w, 403, map[string]string{"error": "not authorized for this mission's knowledge"})
		return
	}

	result, err := h.deps.Knowledge.QueryByMission(r.Context(), req.Query, mission.ID)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(result) //nolint:errcheck
}

// claimMissionEvent handles POST /api/v1/missions/{name}/claim
// Used by no-coordinator team missions for event deconfliction.
func (h *handler) claimMissionEvent(w http.ResponseWriter, r *http.Request) {
	name, ok := requireName(w, chi.URLParam(r, "name"))
	if !ok {
		return
	}
	mission, err := h.deps.MissionManager.Get(name)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "mission not found"})
		return
	}

	var req struct {
		EventKey  string `json:"event_key"`
		AgentName string `json:"agent_name"`
	}
	json.NewDecoder(r.Body).Decode(&req) //nolint:errcheck

	if req.EventKey == "" {
		writeJSON(w, 400, map[string]string{"error": "event_key required"})
		return
	}

	claimed, holder := h.deps.Claims.Claim(mission.ID, req.EventKey, req.AgentName)
	writeJSON(w, 200, map[string]interface{}{
		"claimed": claimed,
		"holder":  holder,
	})
}

// releaseMissionClaim handles DELETE /api/v1/missions/{name}/claim
func (h *handler) releaseMissionClaim(w http.ResponseWriter, r *http.Request) {
	name, ok := requireName(w, chi.URLParam(r, "name"))
	if !ok {
		return
	}
	mission, err := h.deps.MissionManager.Get(name)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "mission not found"})
		return
	}

	var req struct {
		EventKey string `json:"event_key"`
	}
	json.NewDecoder(r.Body).Decode(&req) //nolint:errcheck

	h.deps.Claims.Release(mission.ID, req.EventKey)
	writeJSON(w, 200, map[string]string{"status": "released"})
}

// CheckCoordinatorFailover checks if a halted agent is a coordinator for any
// active team mission. If so, fails over to the designated coverage agent.
// ASK tenet 14: authority is never orphaned.
// This is exported so the agent handler in the parent package can call it on halt.
func CheckCoordinatorFailover(ctx context.Context, agentName string, d Deps) {
	missions, err := d.MissionManager.List()
	if err != nil {
		if d.Logger != nil {
			d.Logger.Warn("coordinator failover: list missions failed", "agent", agentName, "err", err)
		}
		return
	}
	foundCandidate := false
	for _, m := range missions {
		if m.Status != "active" || m.AssignedType != "team" {
			continue
		}
		foundCandidate = true
		teamCfg, err := d.MissionManager.LoadTeamConfig(m.AssignedTo)
		if err != nil {
			if d.Logger != nil {
				d.Logger.Warn("coordinator failover: load team config failed", "agent", agentName, "mission", m.Name, "team", m.AssignedTo, "err", err)
			}
			continue
		}
		if d.Logger != nil {
			d.Logger.Info("coordinator failover inspecting mission", "halted_agent", agentName, "mission", m.Name, "team", m.AssignedTo, "coordinator", teamCfg.Coordinator, "coverage", teamCfg.Coverage)
		}
		if teamCfg.Coordinator != agentName {
			continue
		}
		if d.Logger != nil {
			d.Logger.Info("coordinator failover candidate", "agent", agentName, "mission", m.Name, "team", m.AssignedTo, "coverage", teamCfg.Coverage)
		}

		// Coordinator is down — failover.
		coverage := teamCfg.Coverage
		if coverage == "" {
			// No coverage designated — alert operator only.
			msg := fmt.Sprintf("[operator] Coordinator %q for mission %q is down — no coverage agent designated. Team members continue in-progress work.", agentName, m.Name)
			d.Comms.CommsRequest(ctx, "POST", "/channels/operator/messages", map[string]interface{}{ //nolint:errcheck
				"author":  "_system",
				"content": msg,
			})
			d.Audit.Write(agentName, "mission_coordinator_down", map[string]interface{}{
				"mission_name": m.Name,
				"team":         m.AssignedTo,
				"coverage":     "none",
			})
			if d.Logger != nil {
				d.Logger.Warn("coordinator failover: no coverage designated", "agent", agentName, "mission", m.Name, "team", m.AssignedTo)
			}
			continue
		}

		// Copy mission to coverage agent.
		if err := d.MissionManager.AssignCoverageAgent(m, coverage); err != nil {
			if d.Logger != nil {
				d.Logger.Error("coverage failover failed", "mission", m.Name, "coverage", coverage, "err", err)
			}
			continue
		}
		if d.Logger != nil {
			d.Logger.Info("coordinator failover assigned coverage", "mission", m.Name, "from", agentName, "to", coverage)
		}

		// Update event bus subscriptions: route triggers to coverage agent.
		if d.EventBus != nil {
			events.OnMissionCompleted(d.EventBus, m.Name) // remove old subs
			// Re-register with coverage as target.
			coverageMission := *m
			coverageMission.AssignedTo = coverage
			events.OnMissionAssigned(d.EventBus, &coverageMission)
		}

		// Signal coverage agent to reload — audit write is co-located (ASK tenet compliance).
		d.Audit.Write(agentName, "mission_coordinator_failover", map[string]interface{}{
			"mission_name": m.Name,
			"team":         m.AssignedTo,
			"from":         agentName,
			"to":           coverage,
		})
		go func(coverageName string) {
			sigCtx := context.Background()
			if d.Runtime != nil {
				if err := d.Runtime.ReloadEnforcer(sigCtx, coverageName); err != nil {
					d.Logger.Warn("failed to reload coverage runtime enforcer", "agent", coverageName, "err", err)
				}
				return
			}
			enforcerName := fmt.Sprintf("agency-%s-enforcer", coverageName)
			if err := d.Signal.SignalContainer(sigCtx, enforcerName, "SIGHUP"); err != nil {
				d.Logger.Warn("failed to signal coverage enforcer", "agent", coverageName, "err", err)
			}
		}(coverage)

		// Alert operator.
		msg := fmt.Sprintf("[operator] Coordinator %q for mission %q is down — %q has assumed coordination (ASK tenet 14)", agentName, m.Name, coverage)
		d.Comms.CommsRequest(ctx, "POST", "/channels/operator/messages", map[string]interface{}{ //nolint:errcheck
			"author":  "_system",
			"content": msg,
		})
	}
	if !foundCandidate && d.Logger != nil {
		d.Logger.Info("coordinator failover found no active team missions", "halted_agent", agentName)
	}
}
