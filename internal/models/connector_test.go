// agency-gateway/internal/models/connector_test.go
package models

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestConnectorSource_Validate_Poll tests poll source validation.
func TestConnectorSource_Validate_Poll(t *testing.T) {
	url := "https://api.example.com/items"
	interval := "5m"

	t.Run("valid", func(t *testing.T) {
		cs := &ConnectorSource{
			Type:     "poll",
			URL:      &url,
			Interval: &interval,
		}
		if err := cs.Validate(); err != nil {
			t.Errorf("expected valid, got: %v", err)
		}
	})

	t.Run("missing_url", func(t *testing.T) {
		cs := &ConnectorSource{
			Type:     "poll",
			Interval: &interval,
		}
		err := cs.Validate()
		if err == nil {
			t.Fatal("expected error for missing url, got nil")
		}
		if !strings.Contains(err.Error(), "url") {
			t.Errorf("expected url error, got: %v", err)
		}
	})

	t.Run("missing_interval", func(t *testing.T) {
		cs := &ConnectorSource{
			Type: "poll",
			URL:  &url,
		}
		err := cs.Validate()
		if err == nil {
			t.Fatal("expected error for missing interval, got nil")
		}
		if !strings.Contains(err.Error(), "interval") {
			t.Errorf("expected interval error, got: %v", err)
		}
	})

	t.Run("bad_interval_format", func(t *testing.T) {
		badInterval := "5min"
		cs := &ConnectorSource{
			Type:     "poll",
			URL:      &url,
			Interval: &badInterval,
		}
		err := cs.Validate()
		if err == nil {
			t.Fatal("expected error for bad interval format, got nil")
		}
		if !strings.Contains(err.Error(), "interval") {
			t.Errorf("expected interval format error, got: %v", err)
		}
	})
}

// TestConnectorSource_Validate_Schedule tests schedule source validation.
func TestConnectorSource_Validate_Schedule(t *testing.T) {
	cron := "0 9 * * 1-5"

	t.Run("valid", func(t *testing.T) {
		cs := &ConnectorSource{
			Type: "schedule",
			Cron: &cron,
		}
		if err := cs.Validate(); err != nil {
			t.Errorf("expected valid, got: %v", err)
		}
	})

	t.Run("missing_cron", func(t *testing.T) {
		cs := &ConnectorSource{
			Type: "schedule",
		}
		err := cs.Validate()
		if err == nil {
			t.Fatal("expected error for missing cron, got nil")
		}
		if !strings.Contains(err.Error(), "cron") {
			t.Errorf("expected cron error, got: %v", err)
		}
	})
}

// TestConnectorSource_Validate_ChannelWatch tests channel-watch source validation.
func TestConnectorSource_Validate_ChannelWatch(t *testing.T) {
	channel := "general"
	pattern := `\bhelp\b`

	t.Run("valid", func(t *testing.T) {
		cs := &ConnectorSource{
			Type:    "channel-watch",
			Channel: &channel,
			Pattern: &pattern,
		}
		if err := cs.Validate(); err != nil {
			t.Errorf("expected valid, got: %v", err)
		}
	})

	t.Run("missing_channel", func(t *testing.T) {
		cs := &ConnectorSource{
			Type:    "channel-watch",
			Pattern: &pattern,
		}
		err := cs.Validate()
		if err == nil {
			t.Fatal("expected error for missing channel, got nil")
		}
		if !strings.Contains(err.Error(), "channel") {
			t.Errorf("expected channel error, got: %v", err)
		}
	})

	t.Run("missing_pattern", func(t *testing.T) {
		cs := &ConnectorSource{
			Type:    "channel-watch",
			Channel: &channel,
		}
		err := cs.Validate()
		if err == nil {
			t.Fatal("expected error for missing pattern, got nil")
		}
		if !strings.Contains(err.Error(), "pattern") {
			t.Errorf("expected pattern error, got: %v", err)
		}
	})
}

// TestConnectorSource_Validate_Webhook tests webhook source validation.
func TestConnectorSource_Validate_Webhook(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		cs := &ConnectorSource{
			Type:   "webhook",
			Method: "GET",
		}
		if err := cs.Validate(); err != nil {
			t.Errorf("expected valid webhook, got: %v", err)
		}
	})

	t.Run("with_poll_fields", func(t *testing.T) {
		url := "https://example.com"
		cs := &ConnectorSource{
			Type:   "webhook",
			Method: "GET",
			URL:    &url,
		}
		err := cs.Validate()
		if err == nil {
			t.Fatal("expected error for webhook with poll fields, got nil")
		}
		if !strings.Contains(err.Error(), "webhook source does not accept") {
			t.Errorf("expected poll fields error, got: %v", err)
		}
	})
}

// TestConnectorRoute_Validate tests route validation.
func TestConnectorRoute_Validate(t *testing.T) {
	t.Run("no_target_or_relay", func(t *testing.T) {
		cr := &ConnectorRoute{
			Match: map[string]interface{}{"type": "message"},
		}
		err := cr.Validate()
		if err == nil {
			t.Fatal("expected error for no target/relay, got nil")
		}
		if !strings.Contains(err.Error(), "either") {
			t.Errorf("expected 'either' in error, got: %v", err)
		}
	})

	t.Run("both_target_and_relay", func(t *testing.T) {
		cr := &ConnectorRoute{
			Match:  map[string]interface{}{"type": "message"},
			Target: map[string]string{"agent": "atlas"},
			Relay:  &ConnectorRelayTarget{URL: "https://relay.example.com", Body: "{}"},
		}
		err := cr.Validate()
		if err == nil {
			t.Fatal("expected error for both target and relay, got nil")
		}
		if !strings.Contains(err.Error(), "both") {
			t.Errorf("expected 'both' in error, got: %v", err)
		}
	})

	t.Run("target_only", func(t *testing.T) {
		cr := &ConnectorRoute{
			Match:  map[string]interface{}{"type": "message"},
			Target: map[string]string{"agent": "atlas"},
		}
		if err := cr.Validate(); err != nil {
			t.Errorf("expected valid route with target, got: %v", err)
		}
	})

	t.Run("relay_without_target", func(t *testing.T) {
		cr := &ConnectorRoute{
			Match: map[string]interface{}{"type": "message"},
			Relay: &ConnectorRelayTarget{URL: "https://relay.example.com", Body: "{}"},
		}
		if err := cr.Validate(); err != nil {
			t.Errorf("expected valid route with relay only, got: %v", err)
		}
	})
}

// TestConnectorConfig_Fixtures tests fixture-based loading via LoadAndValidate.
func TestConnectorConfig_Fixtures(t *testing.T) {
	tests := []struct {
		file    string
		wantErr string
	}{
		{"valid_webhook.yaml", ""},
		{"valid_poll.yaml", ""},
		{"invalid_poll_no_url.yaml", "url"},
	}

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			fixturePath := filepath.Join("testdata", "models", "connector", tt.file)
			data, err := os.ReadFile(fixturePath)
			if err != nil {
				t.Fatalf("fixture not found: %s", fixturePath)
			}

			dir := t.TempDir()
			connectorFile := filepath.Join(dir, "connector.yaml")
			if err := os.WriteFile(connectorFile, data, 0644); err != nil {
				t.Fatalf("failed to write temp file: %v", err)
			}

			err = LoadAndValidate(connectorFile)

			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("expected valid, got error: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.wantErr)
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got: %v", tt.wantErr, err)
				}
			}
		})
	}
}

func TestConnectorMCPToolValidateConsentDirective(t *testing.T) {
	tool := &ConnectorMCPTool{
		Name: "drive_add_whitelist_entry",
		Parameters: map[string]interface{}{
			"drive_id":      map[string]interface{}{"type": "string"},
			"consent_token": map[string]interface{}{"type": "string"},
		},
		WhitelistCheck: "drive_id",
		RequiresConsentToken: &ConsentRequirement{
			OperationKind:    "add_managed_doc",
			TokenInputField:  "consent_token",
			TargetInputField: "drive_id",
		},
	}
	if err := tool.Validate(); err != nil {
		t.Fatalf("expected valid tool, got %v", err)
	}
}

func TestConnectorMCPToolValidateConsentDirectiveRejectsUnknownField(t *testing.T) {
	tool := &ConnectorMCPTool{
		Name: "drive_add_whitelist_entry",
		Parameters: map[string]interface{}{
			"drive_id": map[string]interface{}{"type": "string"},
		},
		RequiresConsentToken: &ConsentRequirement{
			OperationKind:    "add_managed_doc",
			TokenInputField:  "consent_token",
			TargetInputField: "drive_id",
		},
	}
	err := tool.Validate()
	if err == nil || !strings.Contains(err.Error(), "token_input_field") {
		t.Fatalf("expected token_input_field validation error, got %v", err)
	}
}
