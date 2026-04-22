package agents

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/geoffbelknap/agency/internal/logs"
)

type pactRunProjection struct {
	TaskID      string                 `json:"task_id"`
	Agent       string                 `json:"agent"`
	Activation  interface{}            `json:"activation,omitempty"`
	Contract    map[string]interface{} `json:"contract,omitempty"`
	Evidence    map[string]interface{} `json:"evidence,omitempty"`
	Verdict     map[string]interface{} `json:"verdict,omitempty"`
	Outcome     string                 `json:"outcome,omitempty"`
	Artifact    map[string]interface{} `json:"artifact,omitempty"`
	AuditEvents []logs.Event           `json:"audit_events"`
	Sources     []string               `json:"sources"`
}

// getPactRun handles GET /api/v1/agents/{name}/pact/runs/{taskId}.
//
// The projection is assembled from existing durable surfaces: result artifact
// frontmatter and append-only audit events. It is a read-only convenience view,
// not a new authority or storage layer.
func (h *handler) getPactRun(w http.ResponseWriter, r *http.Request) {
	agentName := chi.URLParam(r, "name")
	taskID := chi.URLParam(r, "taskId")
	if invalidResultTaskID(taskID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid task ID"})
		return
	}

	projection := pactRunProjection{
		TaskID:      taskID,
		Agent:       agentName,
		AuditEvents: []logs.Event{},
		Sources:     []string{},
	}

	if data, err := h.readResultArtifact(r.Context(), agentName, taskID); err == nil {
		projection.Artifact = map[string]interface{}{
			"task_id": taskID,
			"url":     "/api/v1/agents/" + url.PathEscape(agentName) + "/results/" + url.PathEscape(taskID),
		}
		projection.Sources = appendSource(projection.Sources, "result_artifact")
		if metadata, found, err := parseResultFrontmatter(data); err == nil && found {
			projection.Activation = metadata["pact_activation"]
			if pact, ok := metadata["pact"].(map[string]interface{}); ok {
				applyPactMetadataToProjection(&projection, pact)
			}
		} else if err != nil {
			projection.Artifact["metadata_error"] = "invalid result metadata"
		}
	}

	if h.deps.Config != nil {
		reader := logs.NewReader(h.deps.Config.Home)
		if events, err := reader.ReadAgentLog(agentName, "", ""); err == nil {
			for _, event := range events {
				eventTaskID, _ := event["task_id"].(string)
				if strings.TrimSpace(eventTaskID) != taskID {
					continue
				}
				projection.AuditEvents = append(projection.AuditEvents, event)
				projection.Sources = appendSource(projection.Sources, "audit_log")
				if eventType, _ := event["type"].(string); eventType == "agent_signal_pact_verdict" {
					applyPactVerdictEventToProjection(&projection, event)
				}
			}
		}
	}

	if projection.Artifact == nil && len(projection.AuditEvents) == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "PACT run not found"})
		return
	}

	writeJSON(w, http.StatusOK, projection)
}

func applyPactMetadataToProjection(projection *pactRunProjection, pact map[string]interface{}) {
	projection.Contract = map[string]interface{}{
		"kind":                    pact["kind"],
		"required_evidence":       pact["required_evidence"],
		"answer_requirements":     pact["answer_requirements"],
		"allowed_terminal_states": pact["allowed_terminal_states"],
	}
	projection.Evidence = map[string]interface{}{
		"observed":           pact["observed"],
		"source_urls":        pact["source_urls"],
		"artifact_paths":     pact["artifact_paths"],
		"changed_files":      pact["changed_files"],
		"validation_results": pact["validation_results"],
		"tools":              pact["tools"],
	}
	projection.Verdict = map[string]interface{}{
		"verdict":          pact["verdict"],
		"missing_evidence": pact["missing_evidence"],
	}
	if verdict, _ := pact["verdict"].(string); verdict != "" {
		projection.Outcome = verdict
	}
}

func applyPactVerdictEventToProjection(projection *pactRunProjection, event logs.Event) {
	if projection.Contract == nil {
		projection.Contract = map[string]interface{}{}
	}
	for _, key := range []string{"kind", "required_evidence", "answer_requirements"} {
		if _, exists := projection.Contract[key]; !exists {
			projection.Contract[key] = event[key]
		}
	}
	if projection.Evidence == nil {
		projection.Evidence = map[string]interface{}{}
	}
	for _, key := range []string{"observed", "source_urls", "artifact_paths", "changed_files", "validation_results", "tools"} {
		if _, exists := projection.Evidence[key]; !exists {
			projection.Evidence[key] = event[key]
		}
	}
	if projection.Verdict == nil {
		projection.Verdict = map[string]interface{}{}
	}
	for _, key := range []string{"verdict", "missing_evidence"} {
		if _, exists := projection.Verdict[key]; !exists {
			projection.Verdict[key] = event[key]
		}
	}
	if projection.Outcome == "" {
		if verdict, _ := event["verdict"].(string); verdict != "" {
			projection.Outcome = verdict
		}
	}
}

func appendSource(sources []string, source string) []string {
	for _, item := range sources {
		if item == source {
			return sources
		}
	}
	return append(sources, source)
}
