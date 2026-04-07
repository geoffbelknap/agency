package agents

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	agencyctx "github.com/geoffbelknap/agency/internal/context"
)

type contextHandler struct {
	mgr *agencyctx.Manager
}

// getConstraints returns the current (acked) constraints for the agent.
func (h *contextHandler) getConstraints(w http.ResponseWriter, r *http.Request) {
	agent := chi.URLParam(r, "name")
	constraints := h.mgr.CurrentConstraints(agent)
	if constraints == nil {
		constraints = map[string]interface{}{}
	}
	writeJSON(w, http.StatusOK, constraints)
}

// getPolicy returns agent name and current constraints. Full policy engine
// integration is out of scope for this handler; see /api/v1/policy/{agent}.
func (h *contextHandler) getPolicy(w http.ResponseWriter, r *http.Request) {
	agent := chi.URLParam(r, "name")
	constraints := h.mgr.CurrentConstraints(agent)
	if constraints == nil {
		constraints = map[string]interface{}{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"agent":       agent,
		"constraints": constraints,
	})
}

// getExceptions returns an empty list. Exception tracking is out of scope.
func (h *contextHandler) getExceptions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, []interface{}{})
}

// getChanges returns the full constraint change history for the agent.
func (h *contextHandler) getChanges(w http.ResponseWriter, r *http.Request) {
	agent := chi.URLParam(r, "name")
	changes := h.mgr.Changes(agent)
	if changes == nil {
		changes = []agencyctx.ConstraintChange{}
	}
	writeJSON(w, http.StatusOK, changes)
}

// push handles POST /api/v1/agents/{name}/context/push.
// Validates the request body, calls Manager.Push, and returns 202 on success.
func (h *contextHandler) push(w http.ResponseWriter, r *http.Request) {
	agent := chi.URLParam(r, "name")

	var body struct {
		Constraints      map[string]interface{} `json:"constraints"`
		SeverityOverride string                 `json:"severity_override"`
		Reason           string                 `json:"reason"`
		Initiator        string                 `json:"initiator"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if body.Constraints == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "constraints required"})
		return
	}
	if body.Reason == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "reason required"})
		return
	}
	if body.Initiator == "" {
		body.Initiator = "operator"
	}

	change, err := h.mgr.Push(agent, body.Constraints, body.SeverityOverride, body.Reason, body.Initiator)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	// Trigger async delivery to the agent's enforcer via WebSocket.
	// This is fire-and-forget from the HTTP handler's perspective — the two-stage
	// timeout logic in DeliverAsync handles alerting and auto-halt.
	h.mgr.DeliverAsync(change)

	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"change_id":           change.ChangeID,
		"version":             change.Version,
		"severity":            change.Severity.String(),
		"status":              string(change.Status),
		"ack_timeout_seconds": int(change.Severity.AckTimeout().Seconds()),
	})
}

// getStatus returns the latest constraint change status for the agent.
func (h *contextHandler) getStatus(w http.ResponseWriter, r *http.Request) {
	agent := chi.URLParam(r, "name")
	change := h.mgr.GetStatus(agent)
	if change == nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "no_changes"})
		return
	}
	writeJSON(w, http.StatusOK, change)
}
