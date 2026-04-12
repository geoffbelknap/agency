package events

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/geoffbelknap/agency/internal/models"
)

// AgentDelivery formats event data as a task message and sends via comms.
type AgentDelivery struct {
	CommsURL string // e.g., http://agency-comms:8300
	Client   *http.Client
}

// NewAgentDelivery creates a new agent delivery handler.
func NewAgentDelivery(commsURL string) *AgentDelivery {
	return &AgentDelivery{
		CommsURL: strings.TrimRight(commsURL, "/"),
		Client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Deliver formats the event as a mission-context task and POSTs to comms.
func (ad *AgentDelivery) Deliver(sub *Subscription, event *models.Event) error {
	message := formatAgentMessage(sub, event)

	agentName := sub.Destination.Target
	metadata := map[string]interface{}{"event_id": event.ID}
	for k, v := range event.Metadata {
		metadata[k] = v
	}

	body, err := json.Marshal(map[string]interface{}{
		"task_content": message,
		"work_item_id": event.ID,
		"priority":     "normal",
		"source":       fmt.Sprintf("%s:%s", event.SourceType, event.SourceName),
		"metadata":     metadata,
	})
	if err != nil {
		return fmt.Errorf("marshal task delivery: %w", err)
	}

	url := fmt.Sprintf("%s/tasks/%s", ad.CommsURL, agentName)
	var lastErr error
	for attempt := 1; attempt <= 60; attempt++ {
		resp, err := ad.Client.Post(url, "application/json", bytes.NewReader(body))
		if err != nil {
			lastErr = fmt.Errorf("post task to comms: %w", err)
		} else {
			respBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode < 400 {
				return nil
			}
			lastErr = fmt.Errorf("comms returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
			if resp.StatusCode != http.StatusNotFound {
				break
			}
		}

		time.Sleep(500 * time.Millisecond)
	}

	return lastErr
}

// formatAgentMessage builds the task message delivered to agents.
func formatAgentMessage(sub *Subscription, event *models.Event) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("[Mission trigger: %s %s / %s]\n\n", event.SourceType, event.SourceName, event.EventType))
	b.WriteString(fmt.Sprintf("New event from %s:\n", event.SourceName))

	// Format data fields sorted by key for deterministic output.
	if len(event.Data) > 0 {
		keys := make([]string, 0, len(event.Data))
		for k := range event.Data {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			b.WriteString(fmt.Sprintf("  %s: %v\n", k, event.Data[k]))
		}
	}

	b.WriteString("\nProcess this according to your mission instructions.")

	return b.String()
}
