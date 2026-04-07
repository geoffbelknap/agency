package missions

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/geoffbelknap/agency/internal/logs"
)

// listMissionProcedures handles GET /api/v1/missions/{name}/procedures
// Returns procedure entities stored in the knowledge graph for a specific mission.
func (h *handler) listMissionProcedures(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	m, err := h.deps.MissionManager.Get(name)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "mission not found: " + err.Error()})
		return
	}
	data, err := h.deps.Knowledge.QueryByMission(r.Context(), "entity_type:procedure", m.ID)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": "knowledge query failed: " + err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

// listMissionEpisodes handles GET /api/v1/missions/{name}/episodes
// Returns episode entities stored in the knowledge graph for a specific mission.
func (h *handler) listMissionEpisodes(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	m, err := h.deps.MissionManager.Get(name)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "mission not found: " + err.Error()})
		return
	}
	data, err := h.deps.Knowledge.QueryByMission(r.Context(), "entity_type:episode", m.ID)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": "knowledge query failed: " + err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

// listMissionEvaluations handles GET /api/v1/missions/{name}/evaluations
// Returns recent success criteria evaluation results for a mission.
// Evaluations are persisted in audit logs as "task_evaluation" events.
func (h *handler) listMissionEvaluations(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	m, err := h.deps.MissionManager.Get(name)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "mission not found: " + err.Error()})
		return
	}

	// Parse limit parameter (default 20, max 100)
	limitStr := r.URL.Query().Get("limit")
	limit := 20
	if limitStr != "" {
		if n, err := fmt.Sscanf(limitStr, "%d", &limit); n != 1 || err != nil {
			limit = 20
		}
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	// Read audit logs for all agents, filter for task_evaluation events matching this mission
	reader := logs.NewReader(h.deps.Config.Home)
	allEvents, err := reader.ReadAllLogs("", "")
	if err != nil {
		// No audit logs yet — return empty
		writeJSON(w, 200, map[string]interface{}{
			"mission":     name,
			"evaluations": []interface{}{},
			"summary":     map[string]interface{}{"total": 0, "passed": 0, "failed": 0, "pass_rate": 0.0},
		})
		return
	}

	type evalResult struct {
		TaskID         string      `json:"task_id"`
		Passed         bool        `json:"passed"`
		EvaluationMode string      `json:"evaluation_mode"`
		ModelUsed      string      `json:"model_used,omitempty"`
		CriteriaResults interface{} `json:"criteria_results,omitempty"`
		EvaluatedAt    string      `json:"evaluated_at,omitempty"`
	}

	var evals []evalResult
	for _, ev := range allEvents {
		evType, _ := ev["event"].(string)
		if evType == "" {
			evType, _ = ev["type"].(string)
		}
		if evType != "task_evaluation" {
			continue
		}
		missionID, _ := ev["mission_id"].(string)
		if missionID != m.ID {
			continue
		}

		er := evalResult{
			EvaluatedAt: timestampStr(ev),
		}
		if tid, ok := ev["task_id"].(string); ok {
			er.TaskID = tid
		}
		if p, ok := ev["passed"].(bool); ok {
			er.Passed = p
		}
		if mode, ok := ev["mode"].(string); ok {
			er.EvaluationMode = mode
		}
		if cr, ok := ev["criteria"]; ok {
			er.CriteriaResults = cr
		}
		evals = append(evals, er)
	}

	// Reverse to get most-recent-first
	for i, j := 0, len(evals)-1; i < j; i, j = i+1, j-1 {
		evals[i], evals[j] = evals[j], evals[i]
	}

	// Apply limit
	if len(evals) > limit {
		evals = evals[:limit]
	}

	// Compute summary stats over all evaluations (not just the limited set)
	total := 0
	passed := 0
	for _, ev := range allEvents {
		evType, _ := ev["event"].(string)
		if evType == "" {
			evType, _ = ev["type"].(string)
		}
		if evType != "task_evaluation" {
			continue
		}
		missionID, _ := ev["mission_id"].(string)
		if missionID != m.ID {
			continue
		}
		total++
		if p, ok := ev["passed"].(bool); ok && p {
			passed++
		}
	}

	passRate := 0.0
	if total > 0 {
		passRate = float64(passed) / float64(total)
	}

	if evals == nil {
		evals = []evalResult{}
	}

	writeJSON(w, 200, map[string]interface{}{
		"mission":     name,
		"evaluations": evals,
		"summary": map[string]interface{}{
			"total":     total,
			"passed":    passed,
			"failed":    total - passed,
			"pass_rate": passRate,
		},
	})
}

// timestampStr extracts the timestamp string from an audit event.
func timestampStr(ev logs.Event) string {
	for _, key := range []string{"timestamp", "ts"} {
		if v, ok := ev[key].(string); ok {
			return v
		}
	}
	return ""
}
