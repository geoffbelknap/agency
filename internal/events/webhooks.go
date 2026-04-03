package events

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/geoffbelknap/agency/internal/models"
	"gopkg.in/yaml.v3"
)

// WebhookManager handles CRUD for inbound webhook registrations.
// Webhooks are stored as YAML files in ~/.agency/webhooks/.
type WebhookManager struct {
	Home string // ~/.agency
}

// NewWebhookManager creates a webhook manager.
func NewWebhookManager(home string) *WebhookManager {
	return &WebhookManager{Home: home}
}

// webhooksDir returns the path to ~/.agency/webhooks/.
func (wm *WebhookManager) webhooksDir() string {
	return filepath.Join(wm.Home, "webhooks")
}

// Create registers a new inbound webhook, generating a secret.
func (wm *WebhookManager) Create(name, eventType string) (*models.Webhook, error) {
	wh := &models.Webhook{
		Name:      name,
		EventType: eventType,
		Secret:    models.GenerateSecret(),
		CreatedAt: time.Now().UTC(),
		URL:       fmt.Sprintf("/api/v1/events/webhook/%s", name),
	}

	if err := wh.Validate(); err != nil {
		return nil, err
	}

	dir := wm.webhooksDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create webhooks dir: %w", err)
	}

	name = filepath.Base(name)
	// Check for existing
	path := filepath.Join(dir, name+".yaml")
	if _, err := os.Stat(path); err == nil {
		return nil, fmt.Errorf("webhook %q already exists", name)
	}

	data, err := yaml.Marshal(wh)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return nil, err
	}

	return wh, nil
}

// Get retrieves a webhook by name.
func (wm *WebhookManager) Get(name string) (*models.Webhook, error) {
	name = filepath.Base(name)
	path := filepath.Join(wm.webhooksDir(), name+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("webhook %q not found", name)
	}
	var wh models.Webhook
	if err := yaml.Unmarshal(data, &wh); err != nil {
		return nil, err
	}
	return &wh, nil
}

// List returns all registered webhooks.
func (wm *WebhookManager) List() ([]*models.Webhook, error) {
	dir := wm.webhooksDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil // no webhooks dir = empty list
	}

	var webhooks []*models.Webhook
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var wh models.Webhook
		if yaml.Unmarshal(data, &wh) == nil {
			webhooks = append(webhooks, &wh)
		}
	}

	return webhooks, nil
}

// Delete removes a webhook by name.
func (wm *WebhookManager) Delete(name string) error {
	name = filepath.Base(name)
	path := filepath.Join(wm.webhooksDir(), name+".yaml")
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("webhook %q not found", name)
	}
	return os.Remove(path)
}

// RotateSecret generates a new secret for the webhook.
func (wm *WebhookManager) RotateSecret(name string) (*models.Webhook, error) {
	wh, err := wm.Get(name)
	if err != nil {
		return nil, err
	}

	wh.Secret = models.GenerateSecret()

	data, err := yaml.Marshal(wh)
	if err != nil {
		return nil, err
	}

	path := filepath.Join(wm.webhooksDir(), filepath.Base(name)+".yaml")
	if err := os.WriteFile(path, data, 0600); err != nil {
		return nil, err
	}

	return wh, nil
}
