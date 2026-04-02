package events

import (
	"bytes"
	"encoding/json"
	"fmt"
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

	// DM channel name must match the channel created during agent start
	// (phase7Session in start.go creates "dm-{agent}").
	agentName := sub.Destination.Target
	channel := "dm-" + agentName

	body, err := json.Marshal(map[string]interface{}{
		"content": message,
		"author":  "_gateway",
		"metadata": map[string]interface{}{
			"event_id": event.ID,
		},
	})
	if err != nil {
		return fmt.Errorf("marshal comms message: %w", err)
	}

	url := fmt.Sprintf("%s/channels/%s/messages", ad.CommsURL, channel)
	resp, err := ad.Client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("post to comms: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("comms returned status %d", resp.StatusCode)
	}

	return nil
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
