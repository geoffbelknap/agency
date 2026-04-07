package graph

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/geoffbelknap/agency/internal/knowledge"
)

// knowledgeQuery handles POST /api/v1/knowledge/query
func (h *handler) knowledgeQuery(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Query string `json:"query"`
		Text  string `json:"text"`
		Q     string `json:"q"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	q := body.Query
	if q == "" {
		q = body.Text
	}
	if q == "" {
		q = body.Q
	}
	if q == "" {
		writeJSON(w, 400, map[string]string{"error": "query required (use 'query', 'text', or 'q' field)"})
		return
	}
	data, err := h.deps.Knowledge.Query(r.Context(), q)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

// knowledgeWhoKnows handles GET /api/v1/knowledge/who-knows
func (h *handler) knowledgeWhoKnows(w http.ResponseWriter, r *http.Request) {
	topic := r.URL.Query().Get("topic")
	if topic == "" {
		writeJSON(w, 400, map[string]string{"error": "topic parameter required"})
		return
	}
	proxy := knowledge.NewProxy()
	data, err := proxy.WhoKnows(r.Context(), topic)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

// knowledgeStats handles GET /api/v1/knowledge/stats
func (h *handler) knowledgeStats(w http.ResponseWriter, r *http.Request) {
	proxy := knowledge.NewProxy()
	data, err := proxy.Stats(r.Context())
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

// knowledgeExport handles GET /api/v1/knowledge/export
func (h *handler) knowledgeExport(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "json"
	}
	proxy := knowledge.NewProxy()
	data, err := proxy.Export(r.Context(), format)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

// knowledgeImport handles POST /api/v1/knowledge/import
func (h *handler) knowledgeImport(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "read body: " + err.Error()})
		return
	}
	proxy := knowledge.NewProxy()
	result, err := proxy.Import(r.Context(), body)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(result)
}

// knowledgeChanges handles GET /api/v1/knowledge/changes
func (h *handler) knowledgeChanges(w http.ResponseWriter, r *http.Request) {
	since := r.URL.Query().Get("since")
	proxy := knowledge.NewProxy()
	data, err := proxy.Changes(r.Context(), since)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

// knowledgeContext handles GET /api/v1/knowledge/context
func (h *handler) knowledgeContext(w http.ResponseWriter, r *http.Request) {
	subject := r.URL.Query().Get("subject")
	if subject == "" {
		writeJSON(w, 400, map[string]string{"error": "subject parameter required"})
		return
	}
	proxy := knowledge.NewProxy()
	data, err := proxy.Context(r.Context(), subject)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

// knowledgeNeighbors handles GET /api/v1/knowledge/neighbors
func (h *handler) knowledgeNeighbors(w http.ResponseWriter, r *http.Request) {
	nodeID := r.URL.Query().Get("node_id")
	if nodeID == "" {
		writeJSON(w, 400, map[string]string{"error": "node_id parameter required"})
		return
	}
	proxy := knowledge.NewProxy()
	data, err := proxy.Neighbors(r.Context(), nodeID)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

// knowledgePath handles GET /api/v1/knowledge/path
func (h *handler) knowledgePath(w http.ResponseWriter, r *http.Request) {
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	if from == "" || to == "" {
		writeJSON(w, 400, map[string]string{"error": "from and to parameters required"})
		return
	}
	proxy := knowledge.NewProxy()
	data, err := proxy.Path(r.Context(), from, to)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

// knowledgeFlags handles GET /api/v1/knowledge/flags
func (h *handler) knowledgeFlags(w http.ResponseWriter, r *http.Request) {
	proxy := knowledge.NewProxy()
	data, err := proxy.Flags(r.Context())
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

// knowledgeRestore handles POST /api/v1/knowledge/restore
func (h *handler) knowledgeRestore(w http.ResponseWriter, r *http.Request) {
	var body struct {
		NodeID string `json:"node_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	if body.NodeID == "" {
		writeJSON(w, 400, map[string]string{"error": "node_id required"})
		return
	}
	proxy := knowledge.NewProxy()
	data, err := proxy.Restore(r.Context(), body.NodeID)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

// knowledgeCurationLog handles GET /api/v1/knowledge/curation-log
func (h *handler) knowledgeCurationLog(w http.ResponseWriter, r *http.Request) {
	proxy := knowledge.NewProxy()
	data, err := proxy.CurationLog(r.Context())
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}
