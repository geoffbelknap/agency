package models

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// EventSourceType enumerates allowed source types.
const (
	EventSourceConnector = "connector"
	EventSourceChannel   = "channel"
	EventSourceSchedule  = "schedule"
	EventSourceWebhook   = "webhook"
	EventSourcePlatform  = "platform"
)

// Event is the universal event envelope routed through the event bus.
type Event struct {
	ID         string                 `json:"id"`
	SourceType string                 `json:"source_type"`
	SourceName string                 `json:"source_name"`
	EventType  string                 `json:"event_type"`
	Timestamp  time.Time              `json:"timestamp"`
	Data       map[string]interface{} `json:"data"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

var validSourceTypes = map[string]bool{
	EventSourceConnector: true,
	EventSourceChannel:   true,
	EventSourceSchedule:  true,
	EventSourceWebhook:   true,
	EventSourcePlatform:  true,
}

func (e *Event) Validate() error {
	if e.ID == "" {
		return fmt.Errorf("event id is required")
	}
	if !validSourceTypes[e.SourceType] {
		return fmt.Errorf("invalid source_type: %s", e.SourceType)
	}
	if e.SourceName == "" {
		return fmt.Errorf("source_name is required")
	}
	if e.EventType == "" {
		return fmt.Errorf("event_type is required")
	}
	return nil
}

// NewEvent creates an event with a generated ID and current timestamp.
func NewEvent(sourceType, sourceName, eventType string, data map[string]interface{}) *Event {
	return &Event{
		ID:         "evt-" + strings.ReplaceAll(uuid.New().String()[:8], "-", ""),
		SourceType: sourceType,
		SourceName: sourceName,
		EventType:  eventType,
		Timestamp:  time.Now().UTC(),
		Data:       data,
	}
}

// NewChannelEvent creates a channel-source event, reusing the comms message ID.
func NewChannelEvent(channelName, commsID string, data map[string]interface{}, metadata map[string]interface{}) *Event {
	return &Event{
		ID:         "evt-msg-" + commsID,
		SourceType: EventSourceChannel,
		SourceName: channelName,
		EventType:  "message",
		Timestamp:  time.Now().UTC(),
		Data:       data,
		Metadata:   metadata,
	}
}
