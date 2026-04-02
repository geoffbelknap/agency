package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/geoffbelknap/agency/internal/knowledge"
)

// handleKnowledgePending proxies GET /pending to the knowledge service.
// Returns org-structural contributions awaiting operator review.
// ASK tenet 5: governance is operator-owned — only operators can review contributions.
func (h *handler) handleKnowledgePending(w http.ResponseWriter, r *http.Request) {
	proxy := knowledge.NewProxy()
	data, err := proxy.Pending(r.Context())
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": "knowledge service unavailable: " + err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

// handleKnowledgeReview proxies POST /review/{id} to the knowledge service.
// Approves or rejects a pending org-structural contribution.
// ASK tenet 5: governance is operator-owned — agents cannot modify their own rules.
func (h *handler) handleKnowledgeReview(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSON(w, 400, map[string]string{"error": "contribution ID required"})
		return
	}

	var body struct {
		Action string `json:"action"`
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid request body"})
		return
	}

	if body.Action != "approve" && body.Action != "reject" {
		writeJSON(w, 400, map[string]string{"error": "action must be 'approve' or 'reject'"})
		return
	}

	proxy := knowledge.NewProxy()
	data, err := proxy.Review(r.Context(), id, body.Action, body.Reason)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": "knowledge service unavailable: " + err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}
