package events

import (
	"encoding/json"
	"net/http"

	"github.com/geoffbelknap/agency/internal/models"
)

// publishEvent handles POST /api/v1/events/publish
// Internal endpoint for infra services (intake, knowledge) to publish events
// to the event bus, replacing direct service-to-service comms calls.
func (h *handler) publishEvent(w http.ResponseWriter, r *http.Request) {
	if h.deps.EventBus == nil {
		writeJSON(w, 503, map[string]string{"error": "event bus not initialized"})
		return
	}

	var body struct {
		SourceType string                 `json:"source_type"`
		SourceName string                 `json:"source_name"`
		EventType  string                 `json:"event_type"`
		Data       map[string]interface{} `json:"data"`
		Metadata   map[string]interface{} `json:"metadata,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}

	if body.SourceType == "" || body.SourceName == "" || body.EventType == "" {
		writeJSON(w, 400, map[string]string{"error": "source_type, source_name, and event_type are required"})
		return
	}

	event := models.NewEvent(body.SourceType, body.SourceName, body.EventType, body.Data)
	if body.Metadata != nil {
		event.Metadata = body.Metadata
	}

	h.deps.EventBus.Publish(event)

	writeJSON(w, 202, map[string]string{"status": "published", "event_id": event.ID})
}
