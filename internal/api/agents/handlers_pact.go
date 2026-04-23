package agents

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/geoffbelknap/agency/internal/logs"
)

type pactRunProjection struct {
	TaskID      string                    `json:"task_id"`
	Agent       string                    `json:"agent"`
	Activation  *pactActivationProjection `json:"activation,omitempty"`
	Contract    *pactContractProjection   `json:"contract,omitempty"`
	Evidence    *pactEvidenceProjection   `json:"evidence,omitempty"`
	Verdict     *pactVerdictProjection    `json:"verdict,omitempty"`
	Outcome     string                    `json:"outcome,omitempty"`
	Artifact    *pactArtifactProjection   `json:"artifact,omitempty"`
	AuditEvents []logs.Event              `json:"audit_events"`
	Sources     []string                  `json:"sources"`
}

type pactActivationProjection struct {
	Content       string `json:"content,omitempty"`
	MatchType     string `json:"match_type,omitempty"`
	Source        string `json:"source,omitempty"`
	Channel       string `json:"channel,omitempty"`
	Author        string `json:"author,omitempty"`
	MissionActive *bool  `json:"mission_active,omitempty"`
}

type pactContractProjection struct {
	Kind                  interface{} `json:"kind,omitempty"`
	RequiredEvidence      interface{} `json:"required_evidence,omitempty"`
	AnswerRequirements    interface{} `json:"answer_requirements,omitempty"`
	AllowedTerminalStates interface{} `json:"allowed_terminal_states,omitempty"`
}

type pactEvidenceProjection struct {
	Observed          interface{} `json:"observed,omitempty"`
	SourceURLs        interface{} `json:"source_urls,omitempty"`
	ArtifactPaths     interface{} `json:"artifact_paths,omitempty"`
	ChangedFiles      interface{} `json:"changed_files,omitempty"`
	ValidationResults interface{} `json:"validation_results,omitempty"`
	EvidenceEntries   interface{} `json:"evidence_entries,omitempty"`
	Tools             interface{} `json:"tools,omitempty"`
}

type pactVerdictProjection struct {
	Verdict         interface{} `json:"verdict,omitempty"`
	MissingEvidence interface{} `json:"missing_evidence,omitempty"`
	Reasons         []string    `json:"reasons"`
	StopReason      string      `json:"stop_reason,omitempty"`
}

type pactArtifactProjection struct {
	TaskID        string `json:"task_id"`
	URL           string `json:"url"`
	MetadataError string `json:"metadata_error,omitempty"`
}

type pactAuditReport struct {
	ReportID        string                   `json:"report_id"`
	GeneratedAt     string                   `json:"generated_at"`
	Agent           string                   `json:"agent"`
	TaskID          string                   `json:"task_id"`
	Run             pactRunProjection        `json:"run"`
	EvidenceEntries []interface{}            `json:"evidence_entries"`
	ArtifactRefs    []pactArtifactProjection `json:"artifact_refs"`
	AuditEvents     []logs.Event             `json:"audit_events"`
	Integrity       pactReportIntegrity      `json:"integrity"`
}

type pactReportIntegrity struct {
	Algorithm string `json:"algorithm"`
	Hash      string `json:"hash"`
	Scope     string `json:"scope"`
}

type pactAuditReportVerification struct {
	Valid        bool   `json:"valid"`
	Agent        string `json:"agent"`
	TaskID       string `json:"task_id"`
	Algorithm    string `json:"algorithm"`
	ExpectedHash string `json:"expected_hash,omitempty"`
	ActualHash   string `json:"actual_hash"`
	ReportID     string `json:"report_id"`
	CheckedAt    string `json:"checked_at"`
	Reason       string `json:"reason,omitempty"`
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

	projection, found := h.buildPactRunProjection(r.Context(), agentName, taskID)
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "PACT run not found"})
		return
	}

	writeJSON(w, http.StatusOK, projection)
}

func (h *handler) getPactAuditReport(w http.ResponseWriter, r *http.Request) {
	agentName := chi.URLParam(r, "name")
	taskID := chi.URLParam(r, "taskId")
	if invalidResultTaskID(taskID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid task ID"})
		return
	}

	projection, found := h.buildPactRunProjection(r.Context(), agentName, taskID)
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "PACT run not found"})
		return
	}

	report, err := pactAuditReportFromRun(agentName, taskID, projection, time.Now().UTC())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to build PACT audit report"})
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (h *handler) verifyPactAuditReport(w http.ResponseWriter, r *http.Request) {
	agentName := chi.URLParam(r, "name")
	taskID := chi.URLParam(r, "taskId")
	if invalidResultTaskID(taskID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid task ID"})
		return
	}

	projection, found := h.buildPactRunProjection(r.Context(), agentName, taskID)
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "PACT run not found"})
		return
	}

	report, err := pactAuditReportFromRun(agentName, taskID, projection, time.Now().UTC())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to build PACT audit report"})
		return
	}
	expectedHash, reason, err := expectedPactReportHash(r, agentName, taskID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if expectedHash == "" {
		expectedHash = report.Integrity.Hash
	}
	valid := expectedHash == report.Integrity.Hash && reason == ""
	verification := pactAuditReportVerification{
		Valid:        valid,
		Agent:        agentName,
		TaskID:       taskID,
		Algorithm:    report.Integrity.Algorithm,
		ExpectedHash: expectedHash,
		ActualHash:   report.Integrity.Hash,
		ReportID:     report.ReportID,
		CheckedAt:    time.Now().UTC().Format(time.RFC3339),
	}
	if !valid {
		if reason != "" {
			verification.Reason = reason
		} else {
			verification.Reason = "hash_mismatch"
		}
	}
	writeJSON(w, http.StatusOK, verification)
}

func expectedPactReportHash(r *http.Request, agentName, taskID string) (string, string, error) {
	queryHash := strings.TrimSpace(r.URL.Query().Get("hash"))
	if r.Body == nil || r.ContentLength == 0 {
		return queryHash, "", nil
	}
	defer r.Body.Close()
	var submitted pactAuditReport
	if err := json.NewDecoder(r.Body).Decode(&submitted); err != nil {
		return "", "", err
	}
	if submitted.Agent != "" && submitted.Agent != agentName {
		return submitted.Integrity.Hash, "agent_mismatch", nil
	}
	if submitted.TaskID != "" && submitted.TaskID != taskID {
		return submitted.Integrity.Hash, "task_id_mismatch", nil
	}
	if submitted.Integrity.Algorithm != "" && submitted.Integrity.Algorithm != "sha256" {
		return submitted.Integrity.Hash, "unsupported_algorithm", nil
	}
	if submitted.Integrity.Hash != "" {
		return submitted.Integrity.Hash, "", nil
	}
	return queryHash, "", nil
}

func (h *handler) buildPactRunProjection(ctx context.Context, agentName, taskID string) (pactRunProjection, bool) {
	projection := pactRunProjection{
		TaskID:      taskID,
		Agent:       agentName,
		AuditEvents: []logs.Event{},
		Sources:     []string{},
	}

	if data, err := h.readResultArtifact(ctx, agentName, taskID); err == nil {
		projection.Artifact = &pactArtifactProjection{
			TaskID: taskID,
			URL:    "/api/v1/agents/" + url.PathEscape(agentName) + "/results/" + url.PathEscape(taskID),
		}
		projection.Sources = appendSource(projection.Sources, "result_artifact")
		if metadata, found, err := parseResultFrontmatter(data); err == nil && found {
			if activation, ok := metadata["pact_activation"].(map[string]interface{}); ok {
				projection.Activation = pactActivationFromMap(activation)
			}
			if pact, ok := metadata["pact"].(map[string]interface{}); ok {
				applyPactMetadataToProjection(&projection, pact)
			}
		} else if err != nil {
			projection.Artifact.MetadataError = "invalid result metadata"
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
		return projection, false
	}
	if projection.Verdict != nil && projection.Verdict.Reasons == nil {
		projection.Verdict.Reasons = []string{}
	}

	return projection, true
}

func pactAuditReportFromRun(agentName, taskID string, run pactRunProjection, generatedAt time.Time) (pactAuditReport, error) {
	report := pactAuditReport{
		GeneratedAt:     generatedAt.UTC().Format(time.RFC3339),
		Agent:           agentName,
		TaskID:          taskID,
		Run:             run,
		EvidenceEntries: pactEvidenceEntries(run),
		ArtifactRefs:    pactArtifactRefs(run),
		AuditEvents:     run.AuditEvents,
	}
	hash, err := pactReportHash(report)
	if err != nil {
		return pactAuditReport{}, err
	}
	report.ReportID = "pact-report-" + hash[:16]
	report.Integrity = pactReportIntegrity{
		Algorithm: "sha256",
		Hash:      hash,
		Scope:     "report_without_generated_at_or_integrity",
	}
	return report, nil
}

func pactEvidenceEntries(run pactRunProjection) []interface{} {
	if run.Evidence == nil || run.Evidence.EvidenceEntries == nil {
		return []interface{}{}
	}
	if entries, ok := run.Evidence.EvidenceEntries.([]interface{}); ok {
		return entries
	}
	return []interface{}{run.Evidence.EvidenceEntries}
}

func pactArtifactRefs(run pactRunProjection) []pactArtifactProjection {
	if run.Artifact == nil {
		return []pactArtifactProjection{}
	}
	return []pactArtifactProjection{*run.Artifact}
}

func pactReportHash(report pactAuditReport) (string, error) {
	stable := struct {
		Agent           string                   `json:"agent"`
		TaskID          string                   `json:"task_id"`
		Run             pactRunProjection        `json:"run"`
		EvidenceEntries []interface{}            `json:"evidence_entries"`
		ArtifactRefs    []pactArtifactProjection `json:"artifact_refs"`
		AuditEvents     []logs.Event             `json:"audit_events"`
	}{
		Agent:           report.Agent,
		TaskID:          report.TaskID,
		Run:             report.Run,
		EvidenceEntries: report.EvidenceEntries,
		ArtifactRefs:    report.ArtifactRefs,
		AuditEvents:     report.AuditEvents,
	}
	data, err := json.Marshal(stable)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func applyPactMetadataToProjection(projection *pactRunProjection, pact map[string]interface{}) {
	projection.Contract = &pactContractProjection{
		Kind:                  pact["kind"],
		RequiredEvidence:      pact["required_evidence"],
		AnswerRequirements:    pact["answer_requirements"],
		AllowedTerminalStates: pact["allowed_terminal_states"],
	}
	projection.Evidence = &pactEvidenceProjection{
		Observed:          pact["observed"],
		SourceURLs:        pact["source_urls"],
		ArtifactPaths:     pact["artifact_paths"],
		ChangedFiles:      pact["changed_files"],
		ValidationResults: pact["validation_results"],
		EvidenceEntries:   pact["evidence_entries"],
		Tools:             pact["tools"],
	}
	projection.Verdict = &pactVerdictProjection{
		Verdict:         pact["verdict"],
		MissingEvidence: pact["missing_evidence"],
		Reasons:         stringListValue(pact["reasons"]),
		StopReason:      stringValue(pact["stop_reason"]),
	}
	if verdict, _ := pact["verdict"].(string); verdict != "" {
		projection.Outcome = verdict
	}
}

func applyPactVerdictEventToProjection(projection *pactRunProjection, event logs.Event) {
	if projection.Contract == nil {
		projection.Contract = &pactContractProjection{}
	}
	if projection.Contract.Kind == nil {
		projection.Contract.Kind = event["kind"]
	}
	if projection.Contract.RequiredEvidence == nil {
		projection.Contract.RequiredEvidence = event["required_evidence"]
	}
	if projection.Contract.AnswerRequirements == nil {
		projection.Contract.AnswerRequirements = event["answer_requirements"]
	}
	if projection.Evidence == nil {
		projection.Evidence = &pactEvidenceProjection{}
	}
	if projection.Evidence.Observed == nil {
		projection.Evidence.Observed = event["observed"]
	}
	if projection.Evidence.SourceURLs == nil {
		projection.Evidence.SourceURLs = event["source_urls"]
	}
	if projection.Evidence.ArtifactPaths == nil {
		projection.Evidence.ArtifactPaths = event["artifact_paths"]
	}
	if projection.Evidence.ChangedFiles == nil {
		projection.Evidence.ChangedFiles = event["changed_files"]
	}
	if projection.Evidence.ValidationResults == nil {
		projection.Evidence.ValidationResults = event["validation_results"]
	}
	if projection.Evidence.EvidenceEntries == nil {
		projection.Evidence.EvidenceEntries = event["evidence_entries"]
	}
	if projection.Evidence.Tools == nil {
		projection.Evidence.Tools = event["tools"]
	}
	if projection.Verdict == nil {
		projection.Verdict = &pactVerdictProjection{}
	}
	if projection.Verdict.Verdict == nil {
		projection.Verdict.Verdict = event["verdict"]
	}
	if projection.Verdict.MissingEvidence == nil {
		projection.Verdict.MissingEvidence = event["missing_evidence"]
	}
	if len(projection.Verdict.Reasons) == 0 {
		projection.Verdict.Reasons = stringListValue(event["reasons"])
	}
	if projection.Verdict.StopReason == "" {
		projection.Verdict.StopReason = stringValue(event["stop_reason"])
	}
	if projection.Outcome == "" {
		if verdict, _ := event["verdict"].(string); verdict != "" {
			projection.Outcome = verdict
		}
	}
}

func pactActivationFromMap(metadata map[string]interface{}) *pactActivationProjection {
	activation := &pactActivationProjection{
		Content:   stringValue(metadata["content"]),
		MatchType: stringValue(metadata["match_type"]),
		Source:    stringValue(metadata["source"]),
		Channel:   stringValue(metadata["channel"]),
		Author:    stringValue(metadata["author"]),
	}
	if value, ok := metadata["mission_active"].(bool); ok {
		activation.MissionActive = &value
	}
	return activation
}

func stringValue(value interface{}) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

func stringListValue(value interface{}) []string {
	switch items := value.(type) {
	case []string:
		return append([]string{}, items...)
	case []interface{}:
		result := make([]string, 0, len(items))
		for _, item := range items {
			if value, ok := item.(string); ok {
				result = append(result, value)
			}
		}
		return result
	default:
		return nil
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
