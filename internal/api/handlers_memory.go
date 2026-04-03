package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/geoffbelknap/agency/internal/evaluation"
	"github.com/geoffbelknap/agency/internal/logs"
	"github.com/geoffbelknap/agency/internal/models"
	"github.com/go-chi/chi/v5"
)

// listAgentProcedures handles GET /api/v1/agents/{name}/procedures
// Returns procedure entities stored in the knowledge graph for a specific agent.
func (h *handler) listAgentProcedures(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	data, err := h.knowledge.QueryByMission(r.Context(), "entity_type:procedure agent:"+name, "")
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": "knowledge query failed: " + err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

// listMissionProcedures handles GET /api/v1/missions/{name}/procedures
// Returns procedure entities stored in the knowledge graph for a specific mission.
func (h *handler) listMissionProcedures(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	m, err := h.missions.Get(name)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "mission not found: " + err.Error()})
		return
	}
	data, err := h.knowledge.QueryByMission(r.Context(), "entity_type:procedure", m.ID)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": "knowledge query failed: " + err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

// listAgentEpisodes handles GET /api/v1/agents/{name}/episodes
// Returns episode entities stored in the knowledge graph for a specific agent.
func (h *handler) listAgentEpisodes(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	data, err := h.knowledge.QueryByMission(r.Context(), "entity_type:episode agent:"+name, "")
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
	m, err := h.missions.Get(name)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "mission not found: " + err.Error()})
		return
	}
	data, err := h.knowledge.QueryByMission(r.Context(), "entity_type:episode", m.ID)
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
	m, err := h.missions.Get(name)
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
	reader := logs.NewReader(h.cfg.Home)
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

// callEvaluationLLM makes a one-shot LLM call via the gateway's internal LLM endpoint
// to evaluate a task summary against success criteria. Uses the same model resolution,
// credential loading, and budget tracking as other infrastructure LLM calls.
func (h *handler) callEvaluationLLM(taskSummary string, criteria []evaluation.CriterionItem, model string) (evaluation.EvaluationResult, error) {
	if model == "" || model == "default" {
		model = "claude-haiku" // cheap model for evaluation
	}

	prompt := evaluation.BuildEvaluationPrompt(taskSummary, criteria)

	reqBody, _ := json.Marshal(map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"max_tokens": 2000,
	})

	// Call the gateway's own internal LLM endpoint
	addr := h.cfg.GatewayAddr
	if addr == "" {
		addr = "127.0.0.1:8200"
	}
	url := fmt.Sprintf("http://%s/api/v1/internal/llm", addr)
	req, err := http.NewRequest("POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return evaluation.EvaluationResult{}, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agency-Caller", "platform-evaluation")
	req.Header.Set("X-Agency-Token", h.cfg.Token)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return evaluation.EvaluationResult{}, fmt.Errorf("internal LLM call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return evaluation.EvaluationResult{}, fmt.Errorf("internal LLM returned %d: %s", resp.StatusCode, string(body))
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return evaluation.EvaluationResult{}, fmt.Errorf("failed to read response: %w", err)
	}

	// Parse OpenAI-format response to get content
	var llmResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &llmResp); err != nil || len(llmResp.Choices) == 0 {
		return evaluation.EvaluationResult{}, fmt.Errorf("malformed LLM response")
	}

	content := llmResp.Choices[0].Message.Content
	result, err := evaluation.ParseEvaluationResponse(content, criteria)
	if err != nil {
		return evaluation.EvaluationResult{}, fmt.Errorf("failed to parse evaluation: %w", err)
	}
	result.ModelUsed = model
	return result, nil
}

// evaluateTaskCompletion runs success criteria evaluation after a task_complete signal.
// Runs asynchronously (called via goroutine from relaySignal).
func (h *handler) evaluateTaskCompletion(agentName string, signalData map[string]interface{}) {
	// Find the agent's active mission
	missions, err := h.missions.List()
	if err != nil {
		return
	}

	var activeMission *models.Mission
	for _, m := range missions {
		if m.AssignedTo == agentName && m.Status == "active" {
			activeMission = m
			break
		}
	}
	if activeMission == nil || activeMission.SuccessCriteria == nil {
		return
	}
	eval := activeMission.SuccessCriteria.Evaluation
	if eval == nil || !eval.Enabled {
		return
	}

	taskSummary, _ := signalData["result"].(string)
	if taskSummary == "" {
		return
	}
	taskID, _ := signalData["task_id"].(string)

	// Convert checklist to evaluation items
	var criteria []evaluation.CriterionItem
	for _, c := range activeMission.SuccessCriteria.Checklist {
		criteria = append(criteria, evaluation.CriterionItem{
			ID:          c.ID,
			Description: c.Description,
			Required:    c.Required,
		})
	}

	mode := eval.Mode
	if mode == "" {
		mode = "checklist_only"
	}
	threshold := eval.ChecklistThreshold
	if threshold == 0 {
		threshold = 0.3
	}

	var result evaluation.EvaluationResult
	switch mode {
	case "checklist_only":
		result = evaluation.EvaluateChecklist(taskSummary, criteria, threshold)
	case "llm":
		llmResult, err := h.callEvaluationLLM(taskSummary, criteria, eval.Model)
		if err != nil {
			log.Printf("LLM evaluation failed for %s, falling back to checklist: %v", activeMission.Name, err)
			result = evaluation.EvaluateChecklist(taskSummary, criteria, threshold)
			result.EvaluationMode = "checklist_only_fallback"
		} else {
			result = llmResult
		}
	default:
		return
	}

	// Log evaluation result
	h.audit.Write(agentName, "task_evaluation", map[string]interface{}{
		"mission_id": activeMission.ID,
		"task_id":    taskID,
		"passed":     result.Passed,
		"mode":       result.EvaluationMode,
		"criteria":   result.CriteriaResults,
	})

	// Handle on_failure action when evaluation fails
	if !result.Passed {
		onFailure := eval.OnFailure
		if onFailure == "" {
			onFailure = "flag"
		}
		log.Printf("Task evaluation failed for %s/%s (on_failure=%s)", strings.ReplaceAll(agentName, "\n", "\\n"), strings.ReplaceAll(taskID, "\n", "\\n"), onFailure)

		if h.eventBus != nil {
			h.eventBus.Publish(&models.Event{
				SourceType: "platform",
				SourceName: "gateway",
				EventType:  "task_evaluation_failed",
				Timestamp:  time.Now(),
				Data: map[string]interface{}{
					"agent":      agentName,
					"mission_id": activeMission.ID,
					"task_id":    taskID,
					"mode":       result.EvaluationMode,
					"on_failure": onFailure,
					"criteria":   result.CriteriaResults,
				},
			})
		}
	}
}
