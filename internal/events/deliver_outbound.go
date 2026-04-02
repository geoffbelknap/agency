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

// DestNtfy is the destination type for ntfy push notifications.
const DestNtfy = "ntfy"

// OutboundDelivery POSTs event envelopes to external webhook URLs.
type OutboundDelivery struct {
	Client  *http.Client
	Headers map[string]map[string]string // URL -> custom headers
}

// NewOutboundDelivery creates an outbound delivery handler with a 10s timeout.
func NewOutboundDelivery() *OutboundDelivery {
	return &OutboundDelivery{
		Client: &http.Client{
			Timeout: 10 * time.Second,
		},
		Headers: make(map[string]map[string]string),
	}
}

// Deliver POSTs the full event envelope as JSON to the destination URL.
// Retries once on failure.
func (od *OutboundDelivery) Deliver(sub *Subscription, event *models.Event) error {
	url := sub.Destination.Target

	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	// First attempt
	if err := od.post(url, body); err != nil {
		// One retry
		if retryErr := od.post(url, body); retryErr != nil {
			return fmt.Errorf("webhook delivery failed after retry: %w", retryErr)
		}
	}

	return nil
}

// post sends a single POST request with custom headers if configured.
func (od *OutboundDelivery) post(url string, body []byte) error {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	// Apply custom headers for this URL
	if headers, ok := od.Headers[url]; ok {
		for k, v := range headers {
			req.Header.Set(k, v)
		}
	}

	resp, err := od.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	return nil
}

// NtfyPayload formats an event for ntfy's POST API.
type NtfyPayload struct {
	Topic    string `json:"topic"`
	Title    string `json:"title"`
	Message  string `json:"message"`
	Priority int    `json:"priority,omitempty"`
}

// NtfyDelivery sends events to ntfy push notification services.
type NtfyDelivery struct {
	Client *http.Client
}

// NewNtfyDelivery creates a new ntfy delivery handler.
func NewNtfyDelivery() *NtfyDelivery {
	return &NtfyDelivery{
		Client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Deliver formats and sends the event to the ntfy server.
func (nd *NtfyDelivery) Deliver(sub *Subscription, event *models.Event) error {
	payload := formatNtfy(sub.Destination.Target, event)

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal ntfy payload: %w", err)
	}

	// Extract base URL (everything before the topic path).
	// ntfy's JSON API requires POSTing to the base URL with the topic in the body.
	url := sub.Destination.Target
	if i := strings.LastIndex(strings.TrimRight(url, "/"), "/"); i > len("https://") {
		url = url[:i]
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create ntfy request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := nd.Client.Do(req)
	if err != nil {
		return fmt.Errorf("post to ntfy: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("ntfy returned status %d", resp.StatusCode)
	}

	return nil
}

// formatNtfy creates a NtfyPayload from an event.
func formatNtfy(url string, event *models.Event) *NtfyPayload {
	// Derive topic from the URL path (last segment)
	topic := "agency"
	parts := strings.Split(strings.TrimRight(url, "/"), "/")
	if len(parts) > 0 {
		last := parts[len(parts)-1]
		if last != "" {
			topic = last
		}
	}

	// Title from event_type
	title := fmt.Sprintf("Agency: %s", event.EventType)

	// Message from data summary
	message := summarizeData(event)

	// Priority from metadata
	priority := 3
	if event.Metadata != nil {
		if p, ok := event.Metadata["priority"]; ok {
			switch v := p.(type) {
			case string:
				switch v {
				case "P1":
					priority = 5
				case "P2":
					priority = 4
				case "P3":
					priority = 3
				}
			}
		}
	}

	// Severity field (from operator_alert signals) overrides P1/P2/P3
	if sev, ok := event.Data["severity"]; ok {
		switch sev {
		case "critical":
			priority = 5
		case "warning":
			priority = 3
		case "info":
			priority = 2
		}
	}

	return &NtfyPayload{
		Topic:    topic,
		Title:    title,
		Message:  message,
		Priority: priority,
	}
}

// summarizeData creates a text summary of event data fields.
func summarizeData(event *models.Event) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Source: %s/%s\n", event.SourceType, event.SourceName))

	if len(event.Data) > 0 {
		keys := make([]string, 0, len(event.Data))
		for k := range event.Data {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			b.WriteString(fmt.Sprintf("%s: %v\n", k, event.Data[k]))
		}
	}

	return strings.TrimRight(b.String(), "\n")
}
