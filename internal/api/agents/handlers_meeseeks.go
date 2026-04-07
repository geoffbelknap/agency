package agents

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/geoffbelknap/agency/internal/models"
	"github.com/geoffbelknap/agency/internal/orchestrate"
)

// spawnMeeseeks handles POST /api/v1/meeseeks
func (h *handler) spawnMeeseeks(w http.ResponseWriter, r *http.Request) {
	var req models.MeeseeksSpawnRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	if err := req.Validate(); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}

	// Identify parent agent from enforcer-injected header only.
	// X-Agency-Agent is set by the enforcer proxy (outside agent boundary)
	// and cannot be forged by the agent. Query params are untrusted and
	// MUST NOT be used for parent identity (ASK tenet 1: constraints are
	// external and inviolable; tenet 3: mediation is complete).
	parent := r.Header.Get("X-Agency-Agent")
	if parent == "" {
		writeJSON(w, 400, map[string]string{"error": "parent agent required via X-Agency-Agent header (set by enforcer)"})
		return
	}

	// Reject requests where a query param attempts to override the
	// enforcer-provided identity — this is a spoofing attempt.
	if qp := r.URL.Query().Get("parent"); qp != "" && qp != parent {
		h.deps.Audit.Write(parent, "meeseeks_spawn_spoofing_attempt", map[string]interface{}{
			"enforcer_identity": parent,
			"claimed_parent":    qp,
			"remote_addr":       r.RemoteAddr,
			"build_id":          h.deps.Config.BuildID,
		})
		writeJSON(w, 403, map[string]string{"error": "parent identity mismatch: query param does not match enforcer identity"})
		return
	}

	// Verify parent is running
	detail, err := h.deps.AgentManager.Show(r.Context(), parent)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": fmt.Sprintf("parent agent %s not found: %s", parent, err)})
		return
	}
	if detail.Status != "running" {
		writeJSON(w, 400, map[string]string{"error": fmt.Sprintf("parent agent %s is not running (status: %s)", parent, detail.Status)})
		return
	}

	// Load parent's mission config
	parentMission := detail.Mission
	if parentMission == "" {
		writeJSON(w, 403, map[string]string{"error": "parent agent has no active mission"})
		return
	}

	mission, err := h.deps.MissionManager.Get(parentMission)
	if err != nil {
		writeJSON(w, 403, map[string]string{"error": "parent mission not found: " + err.Error()})
		return
	}

	if !mission.Meeseeks {
		writeJSON(w, 403, map[string]string{"error": "mission does not have meeseeks enabled"})
		return
	}

	if mission.Status != "active" {
		writeJSON(w, 403, map[string]string{"error": fmt.Sprintf("mission %s is not active (status: %s)", mission.Name, mission.Status)})
		return
	}

	// Build config from mission
	cfg := orchestrate.MeeseeksConfig{
		Enabled: mission.Meeseeks,
		Limit:   mission.MeeseeksLimit,
		Model:   mission.MeeseeksModel,
		Budget:  mission.MeeseksBudget,
	}

	// Spawn via manager
	mks, err := h.deps.MeeseeksManager.Spawn(&req, parent, mission.ID, detail.GrantedCaps, cfg)
	if err != nil {
		// Rate limit exceeded returns 429
		if isRateLimit(err) {
			writeJSON(w, 429, map[string]string{"error": err.Error()})
		} else {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
		}
		return
	}

	h.deps.Audit.Write(parent, "meeseeks_spawned", map[string]interface{}{
		"meeseeks_id": mks.ID,
		"task":        mks.Task,
		"model":       mks.Model,
		"budget":      mks.Budget,
		"parent":      parent,
		"build_id":    h.deps.Config.BuildID,
	})

	writeJSON(w, 201, map[string]interface{}{
		"meeseeks_id": mks.ID,
		"status":      string(mks.Status),
	})
}

// listMeeseeks handles GET /api/v1/meeseeks
func (h *handler) listMeeseeks(w http.ResponseWriter, r *http.Request) {
	parent := r.URL.Query().Get("parent")
	list := h.deps.MeeseeksManager.List(parent)
	writeJSON(w, 200, list)
}

// showMeeseeks handles GET /api/v1/meeseeks/{id}
func (h *handler) showMeeseeks(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	mks, err := h.deps.MeeseeksManager.Get(id)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, mks)
}

// killMeeseeks handles DELETE /api/v1/meeseeks/{id}
func (h *handler) killMeeseeks(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Check for kill-all-by-parent via query param
	if id == "" {
		parent := r.URL.Query().Get("parent")
		if parent == "" {
			writeJSON(w, 400, map[string]string{"error": "id or parent query param required"})
			return
		}
		list := h.deps.MeeseeksManager.List(parent)
		var killed []string
		for _, mks := range list {
			_ = h.deps.MeeseeksManager.UpdateStatus(mks.ID, models.MeeseeksStatusTerminated)
			h.deps.MeeseeksManager.Remove(mks.ID)
			killed = append(killed, mks.ID)
		}
		h.deps.Audit.Write("_system", "meeseeks_killed_all", map[string]interface{}{
			"parent":   parent,
			"killed":   killed,
			"build_id": h.deps.Config.BuildID,
		})
		writeJSON(w, 200, map[string]interface{}{"status": "terminated", "killed": killed})
		return
	}

	mks, err := h.deps.MeeseeksManager.Get(id)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}

	_ = h.deps.MeeseeksManager.UpdateStatus(id, models.MeeseeksStatusTerminated)
	h.deps.MeeseeksManager.Remove(id)

	h.deps.Audit.Write(mks.ParentAgent, "meeseeks_terminated", map[string]interface{}{
		"meeseeks_id": id,
		"task":        mks.Task,
		"budget_used": mks.BudgetUsed,
		"build_id":    h.deps.Config.BuildID,
	})

	writeJSON(w, 200, map[string]string{"status": "terminated", "meeseeks_id": id})
}

// completeMeeseeks handles POST /api/v1/meeseeks/{id}/complete
// Called by the body runtime when a Meeseeks signals task completion.
func (h *handler) completeMeeseeks(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	mks, err := h.deps.MeeseeksManager.Get(id)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}

	_ = h.deps.MeeseeksManager.UpdateStatus(id, models.MeeseeksStatusCompleted)

	h.deps.Audit.Write(mks.ParentAgent, "meeseeks_completed", map[string]interface{}{
		"meeseeks_id": id,
		"task":        mks.Task,
		"budget_used": mks.BudgetUsed,
		"build_id":    h.deps.Config.BuildID,
	})

	writeJSON(w, 200, map[string]string{"status": "completed", "meeseeks_id": id})
}

// killMeeseeksByParent handles DELETE /api/v1/meeseeks?parent=<agent>
func (h *handler) killMeeseeksByParent(w http.ResponseWriter, r *http.Request) {
	parent := r.URL.Query().Get("parent")
	if parent == "" {
		writeJSON(w, 400, map[string]string{"error": "parent query param required"})
		return
	}
	list := h.deps.MeeseeksManager.List(parent)
	var killed []string
	for _, mks := range list {
		_ = h.deps.MeeseeksManager.UpdateStatus(mks.ID, models.MeeseeksStatusTerminated)
		h.deps.MeeseeksManager.Remove(mks.ID)
		killed = append(killed, mks.ID)
		h.deps.Audit.Write(parent, "meeseeks_terminated", map[string]interface{}{
			"meeseeks_id": mks.ID,
			"task":        mks.Task,
			"budget_used": mks.BudgetUsed,
			"reason":      "kill-by-parent",
			"build_id":    h.deps.Config.BuildID,
		})
	}
	writeJSON(w, 200, map[string]interface{}{"status": "terminated", "killed": killed})
}

// handleMeeseeksDistress handles budget distress signals from a Meeseeks.
func (h *handler) handleMeeseeksDistress(meeseeksID string, level string, budgetUsed, budgetTotal float64, task, parent string) {
	switch level {
	case "warning":
		// 50% budget — post to task channel
		h.deps.Logger.Warn("Meeseeks budget warning",
			"id", meeseeksID,
			"budget_used", fmt.Sprintf("$%.2f", budgetUsed),
			"budget_total", fmt.Sprintf("$%.2f", budgetTotal),
		)
	case "distress":
		// 80% budget — escalate to operator
		h.deps.Logger.Error("Meeseeks distress",
			"id", meeseeksID,
			"budget_used", fmt.Sprintf("$%.2f", budgetUsed),
			"budget_total", fmt.Sprintf("$%.2f", budgetTotal),
			"task", task,
			"parent", parent,
		)
		_ = h.deps.MeeseeksManager.UpdateStatus(meeseeksID, models.MeeseeksStatusDistressed)
		h.deps.Audit.Write(parent, "meeseeks_distressed", map[string]interface{}{
			"meeseeks_id":  meeseeksID,
			"budget_used":  budgetUsed,
			"budget_total": budgetTotal,
			"task":         task,
		})
	case "exhausted":
		// 100% budget — auto-kill
		h.deps.Logger.Error("Meeseeks budget exhausted",
			"id", meeseeksID,
			"budget_total", fmt.Sprintf("$%.2f", budgetTotal),
			"task", task,
			"parent", parent,
		)
		_ = h.deps.MeeseeksManager.UpdateStatus(meeseeksID, models.MeeseeksStatusTerminated)
		h.deps.MeeseeksManager.Remove(meeseeksID)
		h.deps.Audit.Write(parent, "meeseeks_budget_exhausted", map[string]interface{}{
			"meeseeks_id": meeseeksID,
			"budget":      budgetTotal,
			"task":        task,
		})
	}
}

// isRateLimit checks if an error message indicates rate limiting.
func isRateLimit(err error) bool {
	return err != nil && len(err.Error()) > 10 && err.Error()[:10] == "spawn rate"
}
