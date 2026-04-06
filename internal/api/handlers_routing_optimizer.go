package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// routingSuggestions returns routing optimization suggestions.
//
//	GET /api/v1/routing/suggestions?status=pending
//
// Query params:
//
//	status — filter by suggestion status: pending, approved, rejected (optional)
func (h *handler) routingSuggestions(w http.ResponseWriter, r *http.Request) {
	if h.infra == nil || h.infra.Optimizer == nil {
		writeJSON(w, 503, map[string]string{"error": "routing optimizer not available"})
		return
	}

	suggestions := h.infra.Optimizer.Suggestions()

	if status := r.URL.Query().Get("status"); status != "" {
		var filtered []interface{}
		for _, s := range suggestions {
			if s.Status == status {
				filtered = append(filtered, s)
			}
		}
		if filtered == nil {
			filtered = []interface{}{}
		}
		writeJSON(w, 200, filtered)
		return
	}

	writeJSON(w, 200, suggestions)
}

// routingSuggestionApprove approves a routing suggestion.
//
//	POST /api/v1/routing/suggestions/{id}/approve
func (h *handler) routingSuggestionApprove(w http.ResponseWriter, r *http.Request) {
	if h.infra == nil || h.infra.Optimizer == nil {
		writeJSON(w, 503, map[string]string{"error": "routing optimizer not available"})
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSON(w, 400, map[string]string{"error": "missing suggestion id"})
		return
	}

	suggestion, err := h.infra.Optimizer.Approve(id)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, 200, suggestion)
}

// routingSuggestionReject rejects a routing suggestion.
//
//	POST /api/v1/routing/suggestions/{id}/reject
func (h *handler) routingSuggestionReject(w http.ResponseWriter, r *http.Request) {
	if h.infra == nil || h.infra.Optimizer == nil {
		writeJSON(w, 503, map[string]string{"error": "routing optimizer not available"})
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSON(w, 400, map[string]string{"error": "missing suggestion id"})
		return
	}

	if err := h.infra.Optimizer.Reject(id); err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, 200, map[string]string{"status": "rejected", "id": id})
}

// routingStats returns per-model per-task-type statistics from the optimizer.
//
//	GET /api/v1/routing/stats?task_type=tool_use
//
// Query params:
//
//	task_type — filter to a single task type (optional)
func (h *handler) routingStats(w http.ResponseWriter, r *http.Request) {
	if h.infra == nil || h.infra.Optimizer == nil {
		writeJSON(w, 503, map[string]string{"error": "routing optimizer not available"})
		return
	}

	stats := h.infra.Optimizer.Stats()

	if taskType := r.URL.Query().Get("task_type"); taskType != "" {
		var filtered []interface{}
		for _, s := range stats {
			if s.TaskType == taskType {
				filtered = append(filtered, s)
			}
		}
		if filtered == nil {
			filtered = []interface{}{}
		}
		writeJSON(w, 200, filtered)
		return
	}

	writeJSON(w, 200, stats)
}
