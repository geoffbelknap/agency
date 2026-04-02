package api

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// MCP formatting helpers — ported from internal/mcp/format.go for gateway-side
// use. These operate on native Go types instead of raw JSON bytes.
// ---------------------------------------------------------------------------

// fmtAgentList formats a list of agents grouped by status.
func fmtAgentList(agents []map[string]interface{}) string {
	if len(agents) == 0 {
		return "No agents found."
	}
	byStatus := map[string][]string{}
	for _, a := range agents {
		name := mapStr(a, "name")
		if name == "" {
			name = "?"
		}
		status := mapStr(a, "status")
		if status == "" {
			status = "unknown"
		}
		byStatus[status] = append(byStatus[status], name)
	}

	var parts []string
	for _, status := range []string{"running", "paused", "stopped"} {
		names, ok := byStatus[status]
		if ok {
			parts = append(parts, fmt.Sprintf("%s: %s", status, strings.Join(names, ", ")))
			delete(byStatus, status)
		}
	}
	remaining := make([]string, 0, len(byStatus))
	for s := range byStatus {
		remaining = append(remaining, s)
	}
	sort.Strings(remaining)
	for _, status := range remaining {
		names := byStatus[status]
		parts = append(parts, fmt.Sprintf("%s: %s", status, strings.Join(names, ", ")))
	}

	return fmt.Sprintf("%d agents. %s.", len(agents), strings.Join(parts, ". "))
}

// ---------------------------------------------------------------------------
// Log formatting — ported from internal/mcp/format.go
// ---------------------------------------------------------------------------

var mcpLogNoiseTypes = map[string]bool{
	"phase_start":         true,
	"phase_complete":      true,
	"constraints_loaded":  true,
	"constraints_applied": true,
	"network_created":     true,
	"container_created":   true,
	"container_started":   true,
	"volume_created":      true,
	"identity_generated":  true,
	"enforcer_started":    true,
	"gateway_started":     true,
}

// fmtLogVerbose formats audit events in verbose mode with tail/types filtering.
func fmtLogVerbose(events []map[string]interface{}, args map[string]interface{}) string {
	// Filter by types if specified.
	typeFilter := mapStr(args, "types")
	if typeFilter != "" {
		allowed := map[string]bool{}
		for _, t := range strings.Split(typeFilter, ",") {
			allowed[strings.TrimSpace(t)] = true
		}
		var filtered []map[string]interface{}
		for _, e := range events {
			et := eventType(e)
			if allowed[et] {
				filtered = append(filtered, e)
			}
		}
		events = filtered
	}

	// Apply tail limit.
	tail := mapInt(args, "tail", 10)
	if tail > 0 && len(events) > tail {
		events = events[len(events)-tail:]
	}

	if len(events) == 0 {
		return "No events found."
	}

	lines := []string{fmt.Sprintf("Audit log (%d events):", len(events))}
	for _, event := range events {
		ts := mapStr(event, "ts")
		timeStr := ts
		if len(ts) >= 19 {
			timeStr = ts[11:19]
		}
		agentName := mapStr(event, "agent")
		if agentName == "" {
			agentName = "?"
		}
		evType := eventType(event)

		detail := ""
		switch evType {
		case "start_phase":
			phaseName := mapStr(event, "phase_name")
			phase := mapInt(event, "phase", 0)
			trigger := mapStr(event, "trigger")
			if phaseName != "" {
				detail = fmt.Sprintf(" - phase %d: %s", phase, phaseName)
				if trigger != "" {
					detail += fmt.Sprintf(" (%s)", trigger)
				}
			}
		case "start_failed", "restart_failed":
			if reason := mapStr(event, "error"); reason != "" {
				detail = fmt.Sprintf(" - %s", reason)
			}
		case "agent_halted":
			parts := []string{}
			if t := mapStr(event, "type"); t != "" {
				parts = append(parts, t)
			}
			if r := mapStr(event, "reason"); r != "" {
				parts = append(parts, r)
			}
			if i := mapStr(event, "initiator"); i != "" {
				parts = append(parts, "by "+i)
			}
			if len(parts) > 0 {
				detail = " - " + strings.Join(parts, " — ")
			}
		case "agent_started":
			detail = " - all phases complete"
		case "agent_restarted":
			detail = " - all phases complete (key rotated)"
		case "LLM_DIRECT", "LLM_DIRECT_STREAM":
			model := mapStr(event, "model")
			dur := mapInt(event, "duration_ms", 0)
			inTok := mapInt(event, "input_tokens", 0)
			outTok := mapInt(event, "output_tokens", 0)
			if model != "" {
				detail = fmt.Sprintf(" - %s %dms in:%d out:%d", model, dur, inTok, outTok)
			}
		case "CONFIG_RELOAD":
			detail = " - enforcer config reloaded"
		case "task_delivered":
			detail = fmt.Sprintf(` - "%s"`, mapStr(event, "content"))
		case "halt_initiated":
			detail = fmt.Sprintf(" - %s", mapStr(event, "reason"))
		case "agent_signal_finding":
			detail = fmt.Sprintf(" - [%s] %s", mapStr(event, "severity"), mapStr(event, "description"))
		case "agent_signal_task_accepted":
			detail = fmt.Sprintf(" - task_id=%s", mapStr(event, "task_id"))
		case "agent_signal_task_complete":
			result := mapStr(event, "result")
			if result == "" {
				result = mapStr(event, "summary")
			}
			if len(result) > 80 {
				result = result[:77] + "..."
			}
			detail = fmt.Sprintf(" - %s", result)
		case "agent_signal_progress_update":
			detail = fmt.Sprintf(" - %s", mapStr(event, "content"))
		case "agent_signal_error":
			category := mapStr(event, "category")
			stage := mapStr(event, "stage")
			status := mapInt(event, "status", 0)
			msg := mapStr(event, "message")
			if len(msg) > 60 {
				msg = msg[:57] + "..."
			}
			if category != "" {
				if status != 0 {
					detail = fmt.Sprintf(" - %s: %s (%d) %s", category, stage, status, msg)
				} else {
					detail = fmt.Sprintf(" - %s: %s %s", category, stage, msg)
				}
			}
		}

		lines = append(lines, fmt.Sprintf("  [%s] %s %s%s", timeStr, agentName, evType, detail))
	}

	return strings.Join(lines, "\n")
}

// eventType returns the event type from a log entry, checking "event" then
// "type" (the gateway writer uses "event"; some external sources use "type").
func eventType(e map[string]interface{}) string {
	if et := mapStr(e, "event"); et != "" {
		return et
	}
	if et := mapStr(e, "type"); et != "" {
		return et
	}
	return "unknown"
}

// fmtLogSummary formats audit events as a per-agent summary.
func fmtLogSummary(events []map[string]interface{}) string {
	// Filter out noise types.
	var meaningful []map[string]interface{}
	for _, e := range events {
		et := eventType(e)
		if !mcpLogNoiseTypes[et] {
			meaningful = append(meaningful, e)
		}
	}

	type agentInfo struct {
		lastEvent map[string]interface{}
		task      string
	}

	var agentOrder []string
	agents := map[string]*agentInfo{}

	for _, event := range meaningful {
		name := mapStr(event, "agent")
		if name == "" {
			name = "?"
		}
		if _, exists := agents[name]; !exists {
			agentOrder = append(agentOrder, name)
			agents[name] = &agentInfo{lastEvent: event}
		} else {
			agents[name].lastEvent = event
		}
		if eventType(event) == "task_delivered" {
			agents[name].task = mapStr(event, "content")
		}
	}

	if len(agents) == 0 {
		return "No events found."
	}

	lines := []string{fmt.Sprintf("Agent log summary (%d agents, %d total events):", len(agents), len(events))}
	for _, name := range agentOrder {
		info := agents[name]
		last := info.lastEvent
		ts := mapStr(last, "ts")
		timeStr := ts
		if len(ts) >= 19 {
			timeStr = ts[11:19]
		}
		lastType := eventType(last)
		taskStr := ""
		if info.task != "" {
			taskStr = fmt.Sprintf(` | task: "%s"`, info.task)
		}
		lines = append(lines, fmt.Sprintf("  %s: %s at %s%s", name, lastType, timeStr, taskStr))
	}

	return strings.Join(lines, "\n")
}

// ---------------------------------------------------------------------------
// Safe map accessors for MCP args/event maps
// ---------------------------------------------------------------------------

func mapStr(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Sprintf("%v", v)
	}
	return s
}

func mapBool(m map[string]interface{}, key string) bool {
	if m == nil {
		return false
	}
	v, ok := m[key]
	if !ok {
		return false
	}
	b, ok := v.(bool)
	return ok && b
}

func mapInt(m map[string]interface{}, key string, def int) int {
	if m == nil {
		return def
	}
	v, ok := m[key]
	if !ok {
		return def
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case string:
		if i, err := strconv.Atoi(n); err == nil {
			return i
		}
	}
	return def
}

func mapSlice(m map[string]interface{}, key string) []interface{} {
	if m == nil {
		return nil
	}
	v, ok := m[key]
	if !ok {
		return nil
	}
	s, ok := v.([]interface{})
	if !ok {
		return nil
	}
	return s
}

// fmtChannelList formats a JSON array of channels.
func fmtChannelList(data []byte) string {
	var channels []map[string]interface{}
	if err := json.Unmarshal(data, &channels); err != nil {
		return string(data)
	}
	if len(channels) == 0 {
		return "No channels found."
	}
	lines := []string{"Channels:"}
	for _, ch := range channels {
		name := mapStr(ch, "name")
		topic := mapStr(ch, "topic")
		chType := mapStr(ch, "type")
		archived := mapBool(ch, "archived")
		if archived {
			archivedAt := mapStr(ch, "archived_at")
			ts := archivedAt
			if len(archivedAt) > 16 {
				ts = archivedAt[:16]
			}
			lines = append(lines, fmt.Sprintf("  [ARCHIVED] #%s (archived %s)", name, ts))
		} else {
			if topic != "" {
				lines = append(lines, fmt.Sprintf("  #%s (%s) - %s", name, chType, topic))
			} else {
				lines = append(lines, fmt.Sprintf("  #%s (%s)", name, chType))
			}
		}
	}
	return strings.Join(lines, "\n")
}

// fmtMessages formats a JSON array of messages for channel read.
func fmtMessages(channel string, data []byte) string {
	var messages []map[string]interface{}
	if err := json.Unmarshal(data, &messages); err != nil {
		return string(data)
	}
	if len(messages) == 0 {
		return fmt.Sprintf("No messages in #%s.", channel)
	}
	lines := []string{fmt.Sprintf("#%s (%d messages):", channel, len(messages))}
	for _, msg := range messages {
		author := mapStr(msg, "author")
		if author == "" {
			author = "?"
		}
		content := mapStr(msg, "content")
		ts := mapStr(msg, "ts")
		if ts == "" {
			ts = mapStr(msg, "timestamp")
		}
		timeStr := ts
		if len(ts) >= 19 {
			timeStr = ts[11:19]
		}
		lines = append(lines, fmt.Sprintf("  [%s] %s: %s", timeStr, author, content))
	}
	return strings.Join(lines, "\n")
}

// fmtSearchResults formats a JSON array of search results.
func fmtSearchResults(query string, data []byte) string {
	var results []map[string]interface{}
	if err := json.Unmarshal(data, &results); err != nil {
		return string(data)
	}
	if len(results) == 0 {
		return fmt.Sprintf("No results for '%s'.", query)
	}
	lines := []string{fmt.Sprintf("Search results for '%s' (%d matches):", query, len(results))}
	for _, msg := range results {
		author := mapStr(msg, "author")
		if author == "" {
			author = "?"
		}
		content := mapStr(msg, "content")
		ch := mapStr(msg, "channel")
		if ch == "" {
			ch = "?"
		}
		lines = append(lines, fmt.Sprintf("  #%s %s: %s", ch, author, content))
	}
	return strings.Join(lines, "\n")
}
