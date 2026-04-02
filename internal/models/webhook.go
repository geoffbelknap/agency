package models

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"time"
)

// Webhook represents a registered inbound webhook endpoint.
type Webhook struct {
	Name      string    `json:"name" yaml:"name"`
	Secret    string    `json:"secret" yaml:"secret"`
	EventType string    `json:"event_type" yaml:"event_type"`
	CreatedAt time.Time `json:"created_at" yaml:"created_at"`
	URL       string    `json:"url" yaml:"url"` // computed: /api/v1/events/webhook/{name}
}

var webhookNameRe = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}$`)

// Validate checks that the webhook has a valid name and event type.
func (w *Webhook) Validate() error {
	if !webhookNameRe.MatchString(w.Name) {
		return fmt.Errorf("webhook name must be lowercase alphanumeric with hyphens, 2-63 chars")
	}
	if w.EventType == "" {
		return fmt.Errorf("event_type is required")
	}
	return nil
}

// GenerateSecret creates a 32-byte hex-encoded secret.
func GenerateSecret() string {
	b := make([]byte, 32)
	rand.Read(b) //nolint:errcheck
	return hex.EncodeToString(b)
}
