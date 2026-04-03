package logs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Event represents a single audit log event.
type Event map[string]interface{}

// Reader reads JSONL audit logs from the agency audit directory.
type Reader struct {
	Home string
}

// NewReader creates a new log reader rooted at the agency home directory.
func NewReader(home string) *Reader {
	return &Reader{Home: home}
}

// ReadAgentLog reads all JSONL files for a specific agent, filtering by time range.
func (r *Reader) ReadAgentLog(name, since, until string) ([]Event, error) {
	name = filepath.Base(name)
	auditDir := filepath.Join(r.Home, "audit", name)
	if _, err := os.Stat(auditDir); err != nil {
		return nil, err
	}
	events, err := r.readDir(auditDir, since, until)
	if err != nil {
		return nil, err
	}
	if events == nil {
		return []Event{}, nil
	}
	return events, nil
}

// ReadAllLogs aggregates logs across all agents, filtering by time range.
func (r *Reader) ReadAllLogs(since, until string) ([]Event, error) {
	auditDir := filepath.Join(r.Home, "audit")
	entries, err := os.ReadDir(auditDir)
	if err != nil {
		return nil, err
	}

	var allEvents []Event
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		agentDir := filepath.Join(auditDir, e.Name())
		events, err := r.readDir(agentDir, since, until)
		if err != nil {
			continue
		}
		// Tag each event with the agent name if not already set
		for i := range events {
			if _, ok := events[i]["agent"]; !ok {
				events[i]["agent"] = e.Name()
			}
		}
		allEvents = append(allEvents, events...)
	}

	sortEvents(allEvents)
	return allEvents, nil
}

// readDir reads all JSONL files in a directory (and enforcer subdirectory),
// filtering by time range.
func (r *Reader) readDir(dir, since, until string) ([]Event, error) {
	var events []Event

	// Read top-level JSONL files
	topEvents := readJSONLDir(dir)
	events = append(events, topEvents...)

	// Check enforcer subdirectory
	enforcerDir := filepath.Join(dir, "enforcer")
	if enforcerEvents := readJSONLDir(enforcerDir); len(enforcerEvents) > 0 {
		for i := range enforcerEvents {
			enforcerEvents[i]["source"] = "enforcer"
		}
		events = append(events, enforcerEvents...)
	}

	// Filter by time range
	events = filterByTime(events, since, until)

	sortEvents(events)
	return events, nil
}

// readJSONLDir reads all .jsonl files in a directory and returns parsed events.
func readJSONLDir(dir string) []Event {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var events []Event
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var event Event
			if json.Unmarshal([]byte(line), &event) == nil {
				// Normalize timestamp field: gateway uses "timestamp",
				// enforcer uses "ts". Standardize on "ts" for display.
				if ts, ok := event["timestamp"]; ok {
					if _, hasTS := event["ts"]; !hasTS {
						event["ts"] = ts
					}
				}
				// Normalize event type: gateway uses "event",
				// enforcer uses "type". Keep both for compatibility.
				if et, ok := event["event"]; ok {
					if _, hasType := event["type"]; !hasType {
						event["type"] = et
					}
				}
				events = append(events, event)
			}
		}
	}
	return events
}

// filterByTime filters events by since/until timestamps (RFC3339 or prefix).
func filterByTime(events []Event, since, until string) []Event {
	if since == "" && until == "" {
		return events
	}

	var sinceT, untilT time.Time
	var hasSince, hasUntil bool

	if since != "" {
		if t, err := time.Parse(time.RFC3339, since); err == nil {
			sinceT = t
			hasSince = true
		}
	}
	if until != "" {
		if t, err := time.Parse(time.RFC3339, until); err == nil {
			untilT = t
			hasUntil = true
		}
	}

	if !hasSince && !hasUntil {
		return events
	}

	var filtered []Event
	for _, ev := range events {
		ts := eventTimestamp(ev)
		if ts.IsZero() {
			filtered = append(filtered, ev)
			continue
		}
		if hasSince && ts.Before(sinceT) {
			continue
		}
		if hasUntil && ts.After(untilT) {
			continue
		}
		filtered = append(filtered, ev)
	}
	return filtered
}

// eventTimestamp extracts the timestamp from an event.
func eventTimestamp(ev Event) time.Time {
	for _, key := range []string{"timestamp", "ts", "time"} {
		if v, ok := ev[key]; ok {
			switch val := v.(type) {
			case string:
				if t, err := time.Parse(time.RFC3339, val); err == nil {
					return t
				}
				if t, err := time.Parse(time.RFC3339Nano, val); err == nil {
					return t
				}
			case float64:
				return time.Unix(int64(val), 0)
			}
		}
	}
	return time.Time{}
}

// sortEvents sorts events by timestamp ascending.
func sortEvents(events []Event) {
	sort.Slice(events, func(i, j int) bool {
		ti, _ := events[i]["timestamp"].(string)
		tj, _ := events[j]["timestamp"].(string)
		if ti == "" {
			ti, _ = events[i]["ts"].(string)
		}
		if tj == "" {
			tj, _ = events[j]["ts"].(string)
		}
		return ti < tj
	})
}
