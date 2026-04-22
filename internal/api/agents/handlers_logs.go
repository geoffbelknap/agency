package agents

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

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
	h.annotateLogResultArtifacts(name, events)
	writeJSON(w, 200, events)
}

func (h *handler) annotateLogResultArtifacts(agentName string, events []logs.Event) {
	if len(events) == 0 {
		return
	}
	taskIDs := h.resultTaskIDs(agentName)
	if len(taskIDs) == 0 {
		return
	}
	for _, event := range events {
		taskID, ok := event["task_id"].(string)
		if !ok || strings.TrimSpace(taskID) == "" {
			continue
		}
		if _, exists := taskIDs[taskID]; !exists {
			continue
		}
		event["has_result"] = true
		event["result"] = map[string]interface{}{
			"task_id": taskID,
			"url":     "/api/v1/agents/" + url.PathEscape(agentName) + "/results/" + url.PathEscape(taskID),
		}
	}
}

func (h *handler) resultTaskIDs(agentName string) map[string]struct{} {
	ids := map[string]struct{}{}
	if dir, ok := h.hostResultsDir(agentName); ok {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return ids
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			taskID := strings.TrimSuffix(entry.Name(), ".md")
			if taskID != "" {
				ids[taskID] = struct{}{}
			}
		}
		return ids
	}
	if h.deps.DC == nil {
		return ids
	}
	containerName := "agency-" + agentName + "-workspace"
	out, err := h.deps.DC.ExecInContainer(context.Background(), containerName, []string{
		"sh", "-c", "ls -1 /workspace/.results/*.md 2>/dev/null | while read f; do basename \"$f\" .md; done",
	})
	if err != nil {
		return ids
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		taskID := strings.TrimSpace(filepath.Base(line))
		taskID = strings.TrimSuffix(taskID, ".md")
		if taskID != "" {
			ids[taskID] = struct{}{}
		}
	}
	return ids
}
