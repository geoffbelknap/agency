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
