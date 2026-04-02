package api

import (
	"encoding/json"
	"net/http"

	"github.com/geoffbelknap/agency/internal/knowledge"
)

// listOntologyCandidates handles GET /api/v1/ontology/candidates
// Proxies to the knowledge service's /ontology/candidates endpoint.
// Returns emergence candidates — entity types observed in agent contributions
// that don't match the current ontology schema.
func (h *handler) listOntologyCandidates(w http.ResponseWriter, r *http.Request) {
	kp := knowledge.NewProxy()
	data, err := kp.Get(r.Context(), "/ontology/candidates")
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": "knowledge service unavailable: " + err.Error()})
		return
	}

	// Wrap raw response in the spec envelope if it isn't already
	var parsed map[string]interface{}
	if json.Unmarshal(data, &parsed) == nil {
		if _, hasCandidates := parsed["candidates"]; hasCandidates {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(200)
			w.Write(data)
			return
		}
	}

	// If the knowledge service returned a bare array or different shape, wrap it
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

// promoteOntologyCandidate handles POST /api/v1/ontology/promote
// Proxies to the knowledge service to promote a candidate value into the base ontology.
func (h *handler) promoteOntologyCandidate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid request body"})
		return
	}
	if body.Value == "" {
		writeJSON(w, 400, map[string]string{"error": "value is required"})
		return
	}

	kp := knowledge.NewProxy()
	data, err := kp.Post(r.Context(), "/ontology/promote", map[string]string{"value": body.Value})
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": "knowledge service unavailable: " + err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

// rejectOntologyCandidate handles POST /api/v1/ontology/reject
// Proxies to the knowledge service to reject a candidate value.
func (h *handler) rejectOntologyCandidate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid request body"})
		return
	}
	if body.Value == "" {
		writeJSON(w, 400, map[string]string{"error": "value is required"})
		return
	}

	kp := knowledge.NewProxy()
	data, err := kp.Post(r.Context(), "/ontology/reject", map[string]string{"value": body.Value})
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": "knowledge service unavailable: " + err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}
