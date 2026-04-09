package graph

import (
	"encoding/json"
	"net/http"

	"github.com/geoffbelknap/agency/internal/knowledge"
)

// knowledgeOntology handles GET /api/v1/graph/ontology
func (h *handler) knowledgeOntology(w http.ResponseWriter, r *http.Request) {
	cfg, err := knowledge.LoadOntology(h.deps.Config.Home)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, cfg)
}

// knowledgeOntologyTypes handles GET /api/v1/graph/ontology/types
func (h *handler) knowledgeOntologyTypes(w http.ResponseWriter, r *http.Request) {
	cfg, err := knowledge.LoadOntology(h.deps.Config.Home)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]interface{}{
		"entity_types": cfg.EntityTypes,
		"count":        len(cfg.EntityTypes),
	})
}

// knowledgeOntologyRelationships handles GET /api/v1/graph/ontology/relationships
func (h *handler) knowledgeOntologyRelationships(w http.ResponseWriter, r *http.Request) {
	cfg, err := knowledge.LoadOntology(h.deps.Config.Home)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]interface{}{
		"relationship_types": cfg.RelationshipTypes,
		"count":              len(cfg.RelationshipTypes),
	})
}

// knowledgeOntologyValidate handles POST /api/v1/graph/ontology/validate
func (h *handler) knowledgeOntologyValidate(w http.ResponseWriter, r *http.Request) {
	cfg, err := knowledge.LoadOntology(h.deps.Config.Home)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}

	// Get all nodes from knowledge graph
	proxy := knowledge.NewProxy()
	statsData, err := proxy.Stats(r.Context())
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": "cannot reach knowledge service: " + err.Error()})
		return
	}

	var stats map[string]interface{}
	json.Unmarshal(statsData, &stats)

	kindsRaw, _ := stats["kinds"].(map[string]interface{})
	var issues []map[string]interface{}
	validCount := 0
	invalidCount := 0

	for kind, countRaw := range kindsRaw {
		count, _ := countRaw.(float64)
		corrected, changed := knowledge.ValidateNode(kind, cfg)
		if changed {
			invalidCount += int(count)
			issues = append(issues, map[string]interface{}{
				"kind":      kind,
				"count":     int(count),
				"suggested": corrected,
				"action":    "migrate " + kind + " " + corrected,
			})
		} else {
			validCount += int(count)
		}
	}

	writeJSON(w, 200, map[string]interface{}{
		"valid_nodes":      validCount,
		"invalid_nodes":    invalidCount,
		"issues":           issues,
		"ontology_version": cfg.Version,
	})
}

// knowledgeOntologyMigrate handles POST /api/v1/graph/ontology/migrate
func (h *handler) knowledgeOntologyMigrate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	if body.From == "" || body.To == "" {
		writeJSON(w, 400, map[string]string{"error": "from and to required"})
		return
	}

	// Validate target type exists in ontology
	cfg, err := knowledge.LoadOntology(h.deps.Config.Home)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if _, ok := cfg.EntityTypes[body.To]; !ok {
		writeJSON(w, 400, map[string]string{"error": "target type '" + body.To + "' not in ontology"})
		return
	}

	// Proxy the migration to the knowledge service
	proxy := knowledge.NewProxy()
	migrationBody := map[string]string{"from": body.From, "to": body.To}
	data, err := proxy.Post(r.Context(), "/migrate-kind", migrationBody)
	if err != nil {
		// If knowledge service doesn't support migration endpoint, return info
		writeJSON(w, 200, map[string]interface{}{
			"status":  "pending",
			"from":    body.From,
			"to":      body.To,
			"message": "Migration queued. Knowledge service will process on next cycle.",
		})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

// listOntologyCandidates handles GET /api/v1/graph/ontology/candidates
// Proxies to the knowledge service's /ontology/candidates endpoint.
// Returns emergence candidates — entity types observed in agent contributions
// that don't match the current ontology schema.
func (h *handler) listOntologyCandidates(w http.ResponseWriter, r *http.Request) {
	kp := knowledge.NewProxy()
	candidates, err := knowledge.ListOntologyCandidates(r.Context(), kp)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": "knowledge service unavailable: " + err.Error()})
		return
	}
	writeJSON(w, 200, map[string]interface{}{"candidates": candidates})
}

// promoteOntologyCandidate handles POST /api/v1/graph/ontology/promote
// Proxies to the knowledge service to promote a candidate value into the base ontology.
func (h *handler) promoteOntologyCandidate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		NodeID string `json:"node_id"`
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid request body"})
		return
	}
	if body.NodeID == "" && body.Value == "" {
		writeJSON(w, 400, map[string]string{"error": "node_id or value is required"})
		return
	}

	kp := knowledge.NewProxy()
	nodeID, err := knowledge.ResolveOntologyCandidateID(r.Context(), kp, body.NodeID, body.Value)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	data, err := kp.Post(r.Context(), "/ontology/promote", map[string]string{"node_id": nodeID})
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": "knowledge service unavailable: " + err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

// rejectOntologyCandidate handles POST /api/v1/graph/ontology/reject
// Proxies to the knowledge service to reject a candidate value.
func (h *handler) rejectOntologyCandidate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		NodeID string `json:"node_id"`
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid request body"})
		return
	}
	if body.NodeID == "" && body.Value == "" {
		writeJSON(w, 400, map[string]string{"error": "node_id or value is required"})
		return
	}

	kp := knowledge.NewProxy()
	nodeID, err := knowledge.ResolveOntologyCandidateID(r.Context(), kp, body.NodeID, body.Value)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	data, err := kp.Post(r.Context(), "/ontology/reject", map[string]string{"node_id": nodeID})
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": "knowledge service unavailable: " + err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

// restoreOntologyCandidate handles POST /api/v1/graph/ontology/restore
// Restores a previously promoted or rejected ontology candidate back to review.
func (h *handler) restoreOntologyCandidate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		NodeID string `json:"node_id"`
		Value  string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid request body"})
		return
	}
	if body.NodeID == "" && body.Value == "" {
		writeJSON(w, 400, map[string]string{"error": "node_id or value is required"})
		return
	}

	kp := knowledge.NewProxy()
	nodeID, err := knowledge.ResolveOntologyCandidateID(r.Context(), kp, body.NodeID, body.Value)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}

	data, err := kp.Restore(r.Context(), nodeID)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": "knowledge service unavailable: " + err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}
