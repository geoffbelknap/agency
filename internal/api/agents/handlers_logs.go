package agents

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/geoffbelknap/agency/internal/logs"
)

func (h *handler) agentLogs(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	since := r.URL.Query().Get("since")
	until := r.URL.Query().Get("until")
	reader := logs.NewReader(h.deps.Config.Home)
	events, err := reader.ReadAgentLog(name, since, until)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "no audit logs for agent"})
		return
	}
	if len(events) > 500 {
		events = events[len(events)-500:]
	}
	writeJSON(w, 200, events)
}
