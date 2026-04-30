package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"

	apimissions "github.com/geoffbelknap/agency/internal/api/missions"
	"github.com/geoffbelknap/agency/internal/events"
	"github.com/geoffbelknap/agency/internal/models"
	"github.com/geoffbelknap/agency/internal/orchestrate"
	runtimecontract "github.com/geoffbelknap/agency/internal/runtime/contract"
)

func (h *handler) listAgents(w http.ResponseWriter, r *http.Request) {
	if h.deps.AgentManager == nil {
		writeJSON(w, 500, map[string]string{"error": "agent manager not initialized"})
		return
	}
	agents, err := h.deps.AgentManager.List(r.Context())
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, agents)
}

func (h *handler) showAgent(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	detail, err := h.deps.AgentManager.Show(r.Context(), name)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, detail)
}

func (h *handler) createAgent(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name   string `json:"name"`
		Preset string `json:"preset"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	if body.Name == "" {
		writeJSON(w, 400, map[string]string{"error": "name required"})
		return
	}
	if body.Preset == "" {
		body.Preset = "generalist"
	}
	if err := h.deps.AgentManager.Create(r.Context(), body.Name, body.Preset); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	h.deps.Audit.Write(body.Name, "agent_created", map[string]interface{}{"preset": body.Preset})
	writeJSON(w, 201, map[string]string{"status": "created", "name": body.Name})
}

func (h *handler) deleteAgent(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := h.deps.AgentManager.Delete(r.Context(), name); err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	// Remove agent from all channel memberships
	h.deps.Comms.CommsRequest(r.Context(), "POST", "/participants/"+name+"/leave-all", nil)
	// Retire the dedicated DM alias so future agents can reuse the name without
	// inheriting old direct-message history.
	h.deps.Comms.CommsRequest(r.Context(), "POST", "/channels/dm-"+name+"/retire", map[string]interface{}{"retired_by": "_platform"})
	h.deps.Audit.WriteSystem("agent_deleted", map[string]interface{}{"agent": name})
	writeJSON(w, 200, map[string]string{"status": "deleted", "name": name})
}

func (h *handler) relaySignal(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var body struct {
		SignalType string                 `json:"signal_type"`
		Data       map[string]interface{} `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	if body.SignalType == "" {
		writeJSON(w, 400, map[string]string{"error": "signal_type required"})
		return
	}

	eventType := "agent_signal_" + body.SignalType

	// Write to audit log (ASK tenet 2: every action leaves a trace)
	h.deps.Audit.Write(name, eventType, body.Data)

	// Broadcast via WebSocket for real-time delivery
	if h.deps.WSHub != nil {
		h.deps.WSHub.BroadcastAgentSignal(name, eventType, body.Data)
	}

	// Run success criteria evaluation on task_complete signals
	if body.SignalType == "task_complete" {
		go h.evaluateTaskCompletion(name, body.Data)

		// Emit a companion workflow_economics signal so WebSocket clients
		// can display per-task cost data in real time.
		econData := map[string]interface{}{
			"task_id": body.Data["task_id"],
			"steps":   body.Data["turns"],
		}
		if h.deps.WSHub != nil {
			h.deps.WSHub.BroadcastAgentSignal(name, "agent_signal_workflow_economics", econData)
		}
	}

	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func (h *handler) listResults(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if dir, ok := h.hostResultsDir(name); ok {
		entries, err := os.ReadDir(dir)
		if err != nil {
			writeJSON(w, 200, []interface{}{})
			return
		}
		var results []resultListItem
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			taskID := strings.TrimSuffix(entry.Name(), ".md")
			if taskID != "" {
				item := resultListItem{TaskID: taskID}
				if data, err := os.ReadFile(filepath.Join(dir, entry.Name())); err == nil {
					item = resultListItemFromData(taskID, data)
				}
				results = append(results, item)
			}
		}
		if results == nil {
			results = []resultListItem{}
		}
		writeJSON(w, 200, results)
		return
	}
	ref := runtimecontract.InstanceRef{RuntimeID: name, Role: runtimecontract.RoleWorkspace}
	out, err := h.deps.Runtime.Exec(r.Context(), ref, []string{
		"sh", "-c", "ls -1 /workspace/.results/*.md 2>/dev/null | while read f; do basename \"$f\" .md; done",
	})
	if err != nil {
		writeJSON(w, 200, []interface{}{})
		return
	}
	var results []resultListItem
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			item := resultListItem{TaskID: line}
			if data, err := h.readResultArtifact(r.Context(), name, line); err == nil {
				item = resultListItemFromData(line, data)
			}
			results = append(results, item)
		}
	}
	if results == nil {
		results = []resultListItem{}
	}
	writeJSON(w, 200, results)
}

type resultListItem struct {
	TaskID        string                 `json:"task_id"`
	HasMetadata   bool                   `json:"has_metadata,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
	Pact          interface{}            `json:"pact,omitempty"`
	MetadataError string                 `json:"metadata_error,omitempty"`
}

func resultListItemFromData(taskID string, data []byte) resultListItem {
	item := resultListItem{TaskID: taskID}
	metadata, found, err := parseResultFrontmatter(data)
	if err != nil {
		item.HasMetadata = true
		item.MetadataError = "invalid result metadata"
		return item
	}
	if !found {
		return item
	}
	item.HasMetadata = true
	item.Metadata = metadata
	item.Pact = metadata["pact"]
	return item
}

func (h *handler) getResult(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	taskID := chi.URLParam(r, "taskId")
	if invalidResultTaskID(taskID) {
		writeJSON(w, 400, map[string]string{"error": "invalid task ID"})
		return
	}
	data, err := h.readResultArtifact(r.Context(), name, taskID)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "result not found"})
		return
	}
	if r.URL.Query().Get("download") == "true" {
		w.Header().Set("Content-Disposition", "attachment; filename=\""+taskID+".md\"")
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.WriteHeader(200)
	w.Write(data)
}

func (h *handler) getResultMetadata(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	taskID := chi.URLParam(r, "taskId")
	if invalidResultTaskID(taskID) {
		writeJSON(w, 400, map[string]string{"error": "invalid task ID"})
		return
	}
	data, err := h.readResultArtifact(r.Context(), name, taskID)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "result not found"})
		return
	}
	metadata, found, err := parseResultFrontmatter(data)
	if err != nil {
		writeJSON(w, 422, map[string]string{"error": "invalid result metadata"})
		return
	}
	if !found {
		metadata = map[string]interface{}{}
	}
	writeJSON(w, 200, map[string]interface{}{
		"task_id":      taskID,
		"metadata":     metadata,
		"pact":         metadata["pact"],
		"has_metadata": found,
	})
}

func (h *handler) readResultArtifact(ctx context.Context, name, taskID string) ([]byte, error) {
	if dir, ok := h.hostResultsDir(name); ok {
		return os.ReadFile(filepath.Join(dir, taskID+".md"))
	}
	if h.deps.Runtime == nil {
		return nil, fmt.Errorf("runtime client not initialized")
	}
	ref := runtimecontract.InstanceRef{RuntimeID: name, Role: runtimecontract.RoleWorkspace}
	data, err := h.deps.Runtime.Exec(ctx, ref, []string{
		"cat", "/workspace/.results/" + taskID + ".md",
	})
	if err != nil {
		return nil, err
	}
	return []byte(data), nil
}

func invalidResultTaskID(taskID string) bool {
	return strings.TrimSpace(taskID) == "" || strings.Contains(taskID, "/") || strings.Contains(taskID, "..")
}

func parseResultFrontmatter(data []byte) (map[string]interface{}, bool, error) {
	const marker = "---\n"
	if !strings.HasPrefix(string(data), marker) {
		return nil, false, nil
	}
	rest := string(data[len(marker):])
	end := strings.Index(rest, "\n---\n")
	if end < 0 && strings.HasSuffix(rest, "\n---") {
		end = len(rest) - len("\n---")
	}
	if end < 0 {
		return nil, true, fmt.Errorf("frontmatter terminator not found")
	}
	var metadata map[string]interface{}
	if err := yaml.Unmarshal([]byte(rest[:end]), &metadata); err != nil {
		return nil, true, err
	}
	if metadata == nil {
		metadata = map[string]interface{}{}
	}
	return metadata, true, nil
}

func (h *handler) hostResultsDir(agentName string) (string, bool) {
	if h.deps.AgentManager == nil || h.deps.AgentManager.Runtime == nil {
		return "", false
	}
	manifest, err := h.deps.AgentManager.Runtime.Manifest(agentName)
	if err != nil {
		return "", false
	}
	workspacePath := strings.TrimSpace(manifest.Spec.Storage.WorkspacePath)
	if workspacePath == "" || workspacePath == "/workspace" || !filepath.IsAbs(workspacePath) {
		return "", false
	}
	return filepath.Join(workspacePath, ".results"), true
}

func (h *handler) agentChannels(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	data, err := h.deps.Comms.CommsRequest(r.Context(), "GET", "/channels?member="+name, nil)
	if err != nil {
		// Return empty array if comms is unavailable
		writeJSON(w, 200, []interface{}{})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

func (h *handler) ensureAgentDM(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if strings.TrimSpace(name) == "" {
		writeJSON(w, 400, map[string]string{"error": "name required"})
		return
	}

	channelName, err := h.ensureDirectChannel(r.Context(), name)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{
		"status":  "ready",
		"channel": channelName,
	})
}

func (h *handler) ensureDirectChannel(ctx context.Context, agentName string) (string, error) {
	dmChannel := models.DMChannelName(agentName)
	dmBody := map[string]interface{}{
		"name":       dmChannel,
		"topic":      "DM channel for " + agentName,
		"type":       models.ChannelTypeDirect,
		"visibility": "private",
		"members":    []string{agentName, "_operator"},
	}
	if _, err := h.deps.Comms.CommsRequest(ctx, "POST", "/channels", dmBody); err != nil {
		if !strings.Contains(err.Error(), "409") {
			return "", fmt.Errorf("create DM channel %s: %w", dmChannel, err)
		}
		if !h.directChannelActive(ctx, dmChannel) {
			_, _ = h.deps.Comms.CommsRequest(ctx, "POST", "/channels/"+dmChannel+"/retire", map[string]interface{}{"retired_by": "_platform"})
			if _, retryErr := h.deps.Comms.CommsRequest(ctx, "POST", "/channels", dmBody); retryErr != nil {
				if !strings.Contains(retryErr.Error(), "409") {
					return "", fmt.Errorf("create DM channel %s: %w", dmChannel, retryErr)
				}
			}
		}
	}

	dmGrant := map[string]interface{}{"agent": agentName}
	if _, err := h.deps.Comms.CommsRequest(ctx, "POST", "/channels/"+dmChannel+"/grant-access", dmGrant); err != nil {
		return "", fmt.Errorf("grant agent access to %s: %w", dmChannel, err)
	}
	opGrant := map[string]interface{}{"agent": "_operator"}
	if _, err := h.deps.Comms.CommsRequest(ctx, "POST", "/channels/"+dmChannel+"/grant-access", opGrant); err != nil {
		return "", fmt.Errorf("grant operator access to %s: %w", dmChannel, err)
	}
	return dmChannel, nil
}

func (h *handler) directChannelActive(ctx context.Context, channelName string) bool {
	data, err := h.deps.Comms.CommsRequest(ctx, "GET", "/channels?member=_operator&state=all", nil)
	if err != nil {
		return false
	}
	var channels []map[string]interface{}
	if err := json.Unmarshal(data, &channels); err != nil {
		return false
	}
	for _, ch := range channels {
		if ch["name"] == channelName && ch["state"] == models.ChannelStateActive {
			return true
		}
	}
	return false
}

// runtimeInstanceID returns a backend-specific runtime identifier for audit
// events. For Docker this is the short container ID; for non-Docker backends
// it falls back to the agent/component identifier so lifecycle events remain
// attributable without assuming container semantics.
func (h *handler) runtimeInstanceID(ctx context.Context, agentName, component string) string {
	if h.deps.Runtime == nil {
		return agentName + ":" + component
	}
	ref := runtimecontract.InstanceRef{RuntimeID: agentName, Role: runtimecontract.ComponentRole(component)}
	if shortID := h.deps.Runtime.ShortID(ctx, ref); shortID != "" {
		return shortID
	}
	return agentName + ":" + component
}

func (h *handler) startAgent(w http.ResponseWriter, r *http.Request) {
	if !h.runtimeLifecycleAvailable(w) {
		return
	}
	name := chi.URLParam(r, "name")

	// Ensure agent exists and load detail for lifecycle_id wiring
	detail, err := h.deps.AgentManager.Show(r.Context(), name)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}

	// Wire lifecycle_id into audit writer so all subsequent events carry it.
	h.deps.Audit.SetLifecycleID(name, detail.LifecycleID)

	ss := &orchestrate.StartSequence{
		AgentName:   name,
		Home:        h.deps.Config.Home,
		Version:     h.deps.Config.Version,
		SourceDir:   h.deps.Config.SourceDir,
		BuildID:     h.deps.Config.BuildID,
		BackendName: h.deps.Config.Hub.DeploymentBackend,
		Backend:     h.deps.RuntimeHost,
		Comms:       h.deps.Comms,
		Log:         h.deps.Logger,
		CredStore:   h.deps.CredStore,
		Runtime:     h.deps.AgentManager.Runtime,
	}

	// Stream progress as NDJSON if client requests it
	streaming := r.Header.Get("Accept") == "application/x-ndjson"
	var flusher http.Flusher
	if streaming {
		flusher, _ = w.(http.Flusher)
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(200)
	}

	startedAt := time.Now()
	lastProgressAt := startedAt
	result, err := ss.Run(r.Context(), func(phase int, phaseName, desc string) {
		now := time.Now()
		elapsedMs := now.Sub(startedAt).Milliseconds()
		phaseElapsedMs := now.Sub(lastProgressAt).Milliseconds()
		lastProgressAt = now
		h.deps.Logger.Info("start phase", "agent", name, "phase", phase, "name", phaseName)
		h.deps.Audit.Write(name, "start_phase", map[string]interface{}{
			"phase":            phase,
			"phase_name":       phaseName,
			"elapsed_ms":       elapsedMs,
			"phase_elapsed_ms": phaseElapsedMs,
			"instance_id":      h.runtimeInstanceID(r.Context(), name, "enforcer"),
			"build_id":         h.deps.Config.BuildID,
		})
		if streaming && flusher != nil {
			event := map[string]interface{}{
				"type":             "phase",
				"phase":            phase,
				"name":             phaseName,
				"description":      desc,
				"elapsed_ms":       elapsedMs,
				"phase_elapsed_ms": phaseElapsedMs,
			}
			data, _ := json.Marshal(event)
			w.Write(data)
			w.Write([]byte("\n"))
			flusher.Flush()
		}
	})
	if err != nil {
		elapsedMs := time.Since(startedAt).Milliseconds()
		h.deps.Audit.Write(name, "start_failed", map[string]interface{}{"error": err.Error(), "elapsed_ms": elapsedMs, "build_id": h.deps.Config.BuildID})
		if streaming && flusher != nil {
			event := map[string]interface{}{"type": "error", "error": err.Error(), "elapsed_ms": elapsedMs}
			data, _ := json.Marshal(event)
			w.Write(data)
			w.Write([]byte("\n"))
			flusher.Flush()
			return
		}
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}

	// Wire WebSocket client to enforcer for constraint delivery.
	h.registerEnforcerWSClient(name)

	h.deps.Audit.Write(name, "agent_started", map[string]interface{}{
		"instance_id": h.runtimeInstanceID(r.Context(), name, "workspace"),
		"elapsed_ms":  time.Since(startedAt).Milliseconds(),
		"build_id":    h.deps.Config.BuildID,
	})
	events.EmitAgentEvent(h.deps.EventBus, "agent_started", name, nil)
	if streaming && flusher != nil {
		event := map[string]interface{}{"type": "complete", "agent": result.Agent, "model": result.Model, "phases": result.Phases, "elapsed_ms": time.Since(startedAt).Milliseconds()}
		data, _ := json.Marshal(event)
		w.Write(data)
		w.Write([]byte("\n"))
		flusher.Flush()
		return
	}
	writeJSON(w, 200, result)
}

// registerEnforcerWSClient no longer dials the enforcer from the host. The
// enforcer connects back into the gateway via the authenticated context/ws
// route once its constraint server is ready.
func (h *handler) registerEnforcerWSClient(agentName string) {
	if h.deps.Logger != nil {
		h.deps.Logger.Info("waiting for enforcer ws connection", "agent", agentName)
	}
}

// unregisterEnforcerWSClient closes and removes the WebSocket client for an agent.
func (h *handler) unregisterEnforcerWSClient(agentName string) {
	h.deps.CtxMgr.UnregisterWSClient(agentName)
}

func (h *handler) restartAgent(w http.ResponseWriter, r *http.Request) {
	if !h.runtimeLifecycleAvailable(w) {
		return
	}
	name := chi.URLParam(r, "name")

	// Ensure agent exists and load detail for lifecycle_id wiring
	detail, err := h.deps.AgentManager.Show(r.Context(), name)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}

	// Wire lifecycle_id into audit writer so all subsequent events carry it.
	h.deps.Audit.SetLifecycleID(name, detail.LifecycleID)

	// Stop existing runtime and close old WS client
	h.unregisterEnforcerWSClient(name)
	h.deps.AgentManager.StopAgentRuntime(r.Context(), name)

	// Start with key rotation — generates a fresh scoped key instead of
	// reusing the old one (ASK tenet 4: least privilege)
	ss := &orchestrate.StartSequence{
		AgentName:   name,
		Home:        h.deps.Config.Home,
		Version:     h.deps.Config.Version,
		SourceDir:   h.deps.Config.SourceDir,
		BuildID:     h.deps.Config.BuildID,
		BackendName: h.deps.Config.Hub.DeploymentBackend,
		Backend:     h.deps.RuntimeHost,
		Comms:       h.deps.Comms,
		Log:         h.deps.Logger,
		KeyRotation: true,
		CredStore:   h.deps.CredStore,
		Runtime:     h.deps.AgentManager.Runtime,
	}

	result, err := ss.Run(r.Context(), func(phase int, phaseName, desc string) {
		h.deps.Logger.Info("restart phase", "agent", name, "phase", phase, "name", phaseName)
		h.deps.Audit.Write(name, "start_phase", map[string]interface{}{
			"phase":       phase,
			"phase_name":  phaseName,
			"trigger":     "restart",
			"instance_id": h.runtimeInstanceID(r.Context(), name, "enforcer"),
			"build_id":    h.deps.Config.BuildID,
		})
	})
	if err != nil {
		h.deps.Audit.Write(name, "restart_failed", map[string]interface{}{"error": err.Error(), "build_id": h.deps.Config.BuildID})
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}

	// Re-wire WebSocket client to enforcer after restart.
	h.registerEnforcerWSClient(name)

	h.deps.Audit.Write(name, "agent_restarted", map[string]interface{}{
		"instance_id": h.runtimeInstanceID(r.Context(), name, "workspace"),
		"build_id":    h.deps.Config.BuildID,
	})
	writeJSON(w, 200, result)
}

func (h *handler) haltAgent(w http.ResponseWriter, r *http.Request) {
	if !h.runtimeLifecycleAvailable(w) {
		return
	}
	name := chi.URLParam(r, "name")
	var body struct {
		Type      string `json:"type"`
		Reason    string `json:"reason"`
		Initiator string `json:"initiator"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	if body.Type == "" {
		body.Type = "supervised"
	}
	if body.Type == "emergency" && body.Reason == "" {
		writeJSON(w, 400, map[string]string{"error": "emergency halt requires a reason (ASK Tenet 2)"})
		return
	}
	h.unregisterEnforcerWSClient(name)
	record, err := h.deps.HaltController.Halt(r.Context(), name, body.Type, body.Reason, body.Initiator)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	h.deps.Audit.Write(name, "agent_halted", map[string]interface{}{
		"type":        body.Type,
		"reason":      body.Reason,
		"initiator":   body.Initiator,
		"instance_id": h.runtimeInstanceID(r.Context(), name, "workspace"),
		"build_id":    h.deps.Config.BuildID,
	})
	events.EmitAgentEvent(h.deps.EventBus, "agent_halted", name, map[string]interface{}{
		"type": body.Type, "reason": body.Reason,
	})

	// Orphan detection: mark any running Meeseeks for this parent as orphaned.
	// ASK tenet 13: principal and agent lifecycles are independent — halting
	// a parent does not auto-terminate its Meeseeks, but they must be flagged.
	if h.deps.MeeseeksManager != nil {
		orphanedIDs := h.deps.MeeseeksManager.MarkOrphaned(name)
		for _, mid := range orphanedIDs {
			h.deps.Audit.Write(name, "meeseeks_orphaned", map[string]interface{}{
				"meeseeks_id": mid,
				"parent":      name,
				"reason":      "parent agent halted",
				"build_id":    h.deps.Config.BuildID,
			})
		}
		if len(orphanedIDs) > 0 {
			h.deps.Logger.Warn("Orphaned Meeseeks after parent halt",
				"parent", name,
				"count", len(orphanedIDs),
				"ids", orphanedIDs,
			)
			// Alert operator via comms (best-effort; comms may not be running)
			msg := fmt.Sprintf("[operator] Parent agent %q halted with %d orphaned Meeseeks: %v", name, len(orphanedIDs), orphanedIDs)
			h.deps.Comms.CommsRequest(r.Context(), "POST", "/channels/operator/messages", map[string]interface{}{
				"author":  "_system",
				"content": msg,
			})
		}
	}

	// Coverage failover: if halted agent is a coordinator for an active team
	// mission, failover to the coverage agent (ASK tenet 14 — authority is never orphaned).
	apimissions.CheckCoordinatorFailover(r.Context(), name, apimissions.Deps{
		MissionManager: h.deps.MissionManager,
		Claims:         h.deps.Claims,
		HealthMonitor:  h.deps.HealthMonitor,
		Scheduler:      h.deps.Scheduler,
		EventBus:       h.deps.EventBus,
		Knowledge:      h.deps.Knowledge,
		CredStore:      h.deps.CredStore,
		Audit:          h.deps.Audit,
		Config:         h.deps.Config,
		Logger:         h.deps.Logger,
		Comms:          h.deps.Comms,
		Signal:         h.deps.Signal,
	})

	writeJSON(w, 200, record)
}

func (h *handler) resumeAgent(w http.ResponseWriter, r *http.Request) {
	if !h.runtimeLifecycleAvailable(w) {
		return
	}
	name := chi.URLParam(r, "name")
	var body struct {
		Initiator string `json:"initiator"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	if err := h.deps.HaltController.Resume(r.Context(), name, body.Initiator); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	h.registerEnforcerWSClient(name)
	h.deps.Audit.Write(name, "agent_resumed", map[string]interface{}{"initiator": body.Initiator})
	events.EmitAgentEvent(h.deps.EventBus, "agent_resumed", name, nil)
	writeJSON(w, 200, map[string]string{"status": "resumed", "agent": name})
}

// runtimeLifecycleAvailable returns true if the configured runtime backend is
// available. If not, it writes a 503 response and returns false.
func (h *handler) runtimeLifecycleAvailable(w http.ResponseWriter) bool {
	if h.deps.AgentManager != nil && h.deps.AgentManager.Runtime != nil {
		if err := h.deps.AgentManager.Runtime.RuntimeAvailable(context.Background()); err != nil {
			writeJSON(w, 503, map[string]string{"error": err.Error()})
			return false
		}
		return true
	}
	if h.deps.BackendHealth != nil && !h.deps.BackendHealth.Available() {
		writeJSON(w, 503, map[string]string{
			"error": "Runtime backend is not available. Lifecycle operations are unavailable.",
		})
		return false
	}
	return true
}
