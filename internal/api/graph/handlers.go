package graph

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

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

// knowledgeIngest handles POST /api/v1/knowledge/ingest
func (h *handler) knowledgeIngest(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Content     string          `json:"content"`
		Filename    string          `json:"filename"`
		ContentType string          `json:"content_type"`
		Scope       json.RawMessage `json:"scope,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	if body.Content == "" && body.Filename == "" {
		writeJSON(w, 400, map[string]string{"error": "content or filename required"})
		return
	}
	data, err := h.deps.Knowledge.Ingest(r.Context(), body.Content, body.Filename, body.ContentType, body.Scope)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

// knowledgeSaveInsight handles POST /api/v1/knowledge/insight
func (h *handler) knowledgeSaveInsight(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Insight     string   `json:"insight"`
		SourceNodes []string `json:"source_nodes"`
		Confidence  string   `json:"confidence"`
		Tags        []string `json:"tags,omitempty"`
		AgentName   string   `json:"agent_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	if body.Insight == "" {
		writeJSON(w, 400, map[string]string{"error": "insight required"})
		return
	}
	data, err := h.deps.Knowledge.SaveInsight(r.Context(), body.Insight, body.SourceNodes, body.Confidence, body.Tags, body.AgentName)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

// knowledgePrincipalsList handles GET /api/v1/knowledge/principals
func (h *handler) knowledgePrincipalsList(w http.ResponseWriter, r *http.Request) {
	principalType := r.URL.Query().Get("type")
	data, err := h.deps.Knowledge.Principals(r.Context(), principalType)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": "knowledge service unavailable: " + err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

// knowledgePrincipalsRegister handles POST /api/v1/knowledge/principals
func (h *handler) knowledgePrincipalsRegister(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	if body.Type == "" {
		writeJSON(w, 400, map[string]string{"error": "type required"})
		return
	}
	if body.Name == "" {
		writeJSON(w, 400, map[string]string{"error": "name required"})
		return
	}
	data, err := h.deps.Knowledge.RegisterPrincipal(r.Context(), body.Type, body.Name)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": "knowledge service unavailable: " + err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(201)
	w.Write(data)
}

// knowledgePrincipalsResolve handles GET /api/v1/knowledge/principals/{uuid}
func (h *handler) knowledgePrincipalsResolve(w http.ResponseWriter, r *http.Request) {
	uuid := chi.URLParam(r, "uuid")
	if uuid == "" {
		writeJSON(w, 400, map[string]string{"error": "uuid required"})
		return
	}
	data, err := h.deps.Knowledge.ResolvePrincipal(r.Context(), uuid)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": "knowledge service unavailable: " + err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

// knowledgeQuarantine handles POST /api/v1/knowledge/quarantine
func (h *handler) knowledgeQuarantine(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Agent string `json:"agent"`
		Since string `json:"since"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	if body.Agent == "" {
		writeJSON(w, 400, map[string]string{"error": "agent required"})
		return
	}
	data, err := h.deps.Knowledge.Quarantine(r.Context(), body.Agent, body.Since)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

// knowledgeQuarantineRelease handles POST /api/v1/knowledge/quarantine/release
func (h *handler) knowledgeQuarantineRelease(w http.ResponseWriter, r *http.Request) {
	var body struct {
		NodeID string `json:"node_id"`
		Agent  string `json:"agent"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	if body.NodeID == "" && body.Agent == "" {
		writeJSON(w, 400, map[string]string{"error": "node_id or agent required"})
		return
	}
	data, err := h.deps.Knowledge.QuarantineRelease(r.Context(), body.NodeID, body.Agent)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

// knowledgeQuarantineList handles GET /api/v1/knowledge/quarantine
func (h *handler) knowledgeQuarantineList(w http.ResponseWriter, r *http.Request) {
	agent := r.URL.Query().Get("agent")
	data, err := h.deps.Knowledge.QuarantineList(r.Context(), agent)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

// knowledgeClassification handles GET /api/v1/knowledge/classification
func (h *handler) knowledgeClassification(w http.ResponseWriter, r *http.Request) {
	data, err := h.deps.Knowledge.Classification(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

// knowledgeCommunities handles GET /api/v1/knowledge/communities
func (h *handler) knowledgeCommunities(w http.ResponseWriter, r *http.Request) {
	data, err := h.deps.Knowledge.Communities(r.Context())
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

// knowledgeCommunity handles GET /api/v1/knowledge/communities/{id}
func (h *handler) knowledgeCommunity(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSON(w, 400, map[string]string{"error": "missing community id"})
		return
	}
	data, err := h.deps.Knowledge.Community(r.Context(), id)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

// knowledgeHubs handles GET /api/v1/knowledge/hubs
func (h *handler) knowledgeHubs(w http.ResponseWriter, r *http.Request) {
	limit := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	data, err := h.deps.Knowledge.Hubs(r.Context(), limit)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}
