package graph

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/geoffbelknap/agency/internal/knowledge"
)

// handleKnowledgePending handles GET /api/v1/graph/pending
// Proxies to the knowledge service's /pending endpoint.
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

// handleMemoryProposals handles GET /api/v1/graph/memory/proposals
// Returns durable-memory proposals awaiting operator review.
func (h *handler) handleMemoryProposals(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "needs_review"
	}
	if status != "pending_review" && status != "needs_review" && status != "approved" && status != "rejected" {
		writeJSON(w, 400, map[string]string{"error": "invalid proposal status"})
		return
	}
	limit := 100
	if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": "limit must be an integer"})
			return
		}
		limit = parsed
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 250 {
		limit = 250
	}

	proxy := knowledge.NewProxy()
	data, err := proxy.MemoryProposals(r.Context(), status, limit)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": "knowledge service unavailable: " + err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

// handleMemoryProposalReview handles POST /api/v1/graph/memory/proposals/{id}/review
// ASK tenet 5: durable memory review is operator-owned.
func (h *handler) handleMemoryProposalReview(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSON(w, 400, map[string]string{"error": "memory proposal ID required"})
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
	data, err := proxy.ReviewMemoryProposal(r.Context(), id, body.Action, body.Reason)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": "knowledge service unavailable: " + err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

// handleKnowledgeReview handles POST /api/v1/graph/review/{id}
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
