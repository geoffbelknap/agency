package comms

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

func (h *handler) listChannels(w http.ResponseWriter, r *http.Request) {
	// Merge open channels (team, system) with operator's DM channels.
	// The comms /channels endpoint only returns open channels by default.
	// DMs require a member filter.
	ctx := r.Context()
	includeArchived := r.URL.Query().Get("include_archived") == "true"
	includeUnavailable := r.URL.Query().Get("include_unavailable") == "true"
	openData, err := h.deps.Comms.CommsRequest(ctx, "GET", "/channels", nil)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	dmData, _ := h.deps.Comms.CommsRequest(ctx, "GET", "/channels?member=_operator", nil)

	// Merge: parse both, deduplicate by name
	var openChannels, dmChannels []map[string]interface{}
	json.Unmarshal(openData, &openChannels)   //nolint:errcheck
	json.Unmarshal(dmData, &dmChannels)        //nolint:errcheck
	knownAgents := h.knownAgentNames(ctx)

	seen := make(map[string]bool)
	var merged []map[string]interface{}
	for _, ch := range openChannels {
		if !includeArchived && channelState(ch) == "archived" {
			continue
		}
		name, _ := ch["name"].(string)
		seen[name] = true
		merged = append(merged, ch)
	}
	for _, ch := range dmChannels {
		if !includeArchived && channelState(ch) == "archived" {
			continue
		}
		name, _ := ch["name"].(string)
		orphaned := isOrphanDMChannel(name, knownAgents)
		if orphaned {
			ch["availability"] = "unavailable"
		}
		if orphaned && !includeArchived && !includeUnavailable {
			continue
		}
		if !seen[name] {
			merged = append(merged, ch)
		}
	}

	writeJSON(w, 200, merged)
}

func channelState(ch map[string]interface{}) string {
	state, _ := ch["state"].(string)
	return strings.ToLower(state)
}

func (h *handler) knownAgentNames(ctx context.Context) map[string]struct{} {
	known := make(map[string]struct{})
	if h.deps.AgentManager == nil {
		return known
	}
	agents, err := h.deps.AgentManager.List(ctx)
	if err != nil {
		return known
	}
	for _, agent := range agents {
		if agent.Name == "" {
			continue
		}
		known[agent.Name] = struct{}{}
	}
	return known
}

func isOrphanDMChannel(name string, knownAgents map[string]struct{}) bool {
	if !strings.HasPrefix(name, "dm-") {
		return false
	}
	agentName := strings.TrimPrefix(name, "dm-")
	if agentName == "" {
		return false
	}
	_, ok := knownAgents[agentName]
	return !ok
}

func (h *handler) createChannel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name  string `json:"name"`
		Topic string `json:"topic"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	if body.Name == "" {
		writeJSON(w, 400, map[string]string{"error": "name required"})
		return
	}

	commsBody := map[string]interface{}{
		"name":       body.Name,
		"type":       "team",
		"topic":      body.Topic,
		"created_by": "_operator",
		"members":    []string{"_operator"},
	}
	data, err := h.deps.Comms.CommsRequest(r.Context(), "POST", "/channels", commsBody)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(201)
	w.Write(data) //nolint:errcheck
}

func (h *handler) readMessages(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	limit := r.URL.Query().Get("limit")
	if limit == "" {
		limit = "50"
	}
	path := "/channels/" + name + "/messages?limit=" + limit + "&reader=_operator"
	data, err := h.deps.Comms.CommsRequest(r.Context(), "GET", path, nil)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data) //nolint:errcheck
}

func (h *handler) sendMessage(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var body map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	// Normalize operator author to the internal _operator identity used in comms
	if body["author"] == nil || body["author"] == "operator" {
		body["author"] = "_operator"
	}
	path := "/channels/" + name + "/messages"
	data, err := h.deps.Comms.CommsRequest(r.Context(), "POST", path, body)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data) //nolint:errcheck
}

func (h *handler) editMessage(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	id := chi.URLParam(r, "id")
	var body map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	if body["author"] == nil {
		body["author"] = "_operator"
	}
	path := "/channels/" + name + "/messages/" + id
	data, err := h.deps.Comms.CommsRequest(r.Context(), "PUT", path, body)
	if err != nil {
		status := 502
		if strings.Contains(err.Error(), "comms returned 404") {
			status = 404
		} else if strings.Contains(err.Error(), "comms returned 403") {
			status = 403
		}
		if data != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			w.Write(data) //nolint:errcheck
			return
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data) //nolint:errcheck
}

func (h *handler) deleteMessage(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	id := chi.URLParam(r, "id")
	body := map[string]interface{}{"author": "_operator"}
	// Try to read body for author override
	var reqBody map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err == nil {
		if reqBody["author"] != nil {
			body["author"] = reqBody["author"]
		}
	}
	path := "/channels/" + name + "/messages/" + id
	data, err := h.deps.Comms.CommsRequest(r.Context(), "DELETE", path, body)
	if err != nil {
		status := 502
		if strings.Contains(err.Error(), "comms returned 404") {
			status = 404
		} else if strings.Contains(err.Error(), "comms returned 403") {
			status = 403
		}
		if data != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			w.Write(data) //nolint:errcheck
			return
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data) //nolint:errcheck
}

func (h *handler) addReaction(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	id := chi.URLParam(r, "id")
	var body map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	path := "/channels/" + name + "/messages/" + id + "/reactions"
	data, err := h.deps.Comms.CommsRequest(r.Context(), "POST", path, body)
	if err != nil {
		status := 502
		if strings.Contains(err.Error(), "comms returned 404") {
			status = 404
		}
		if data != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			w.Write(data) //nolint:errcheck
			return
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data) //nolint:errcheck
}

func (h *handler) removeReaction(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	id := chi.URLParam(r, "id")
	emoji := chi.URLParam(r, "emoji")
	author := r.URL.Query().Get("author")
	if author == "" {
		author = "_operator"
	}
	path := "/channels/" + name + "/messages/" + id + "/reactions/" + emoji + "?author=" + author
	data, err := h.deps.Comms.CommsRequest(r.Context(), "DELETE", path, nil)
	if err != nil {
		status := 502
		if strings.Contains(err.Error(), "comms returned 404") {
			status = 404
		}
		if data != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			w.Write(data) //nolint:errcheck
			return
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data) //nolint:errcheck
}

func (h *handler) searchMessages(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	channel := r.URL.Query().Get("channel")
	if query == "" {
		writeJSON(w, 400, map[string]string{"error": "q parameter required"})
		return
	}

	path := "/search?q=" + query + "&reader=_operator"
	if channel != "" {
		path += "&channel=" + channel
	}
	data, err := h.deps.Comms.CommsRequest(r.Context(), "GET", path, nil)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data) //nolint:errcheck
}

func (h *handler) archiveChannel(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	data, err := h.deps.Comms.CommsRequest(r.Context(), "POST", "/channels/"+name+"/archive", nil)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data) //nolint:errcheck
}

func (h *handler) getUnreads(w http.ResponseWriter, r *http.Request) {
	data, err := h.deps.Comms.CommsRequest(r.Context(), "GET", "/unreads/_operator", nil)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data) //nolint:errcheck
}

func (h *handler) markRead(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	body := map[string]interface{}{"participant": "_operator"}
	data, err := h.deps.Comms.CommsRequest(r.Context(), "POST", "/channels/"+name+"/mark-read", body)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data) //nolint:errcheck
}
