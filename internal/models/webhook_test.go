package models

import "testing"

func TestWebhookValidation(t *testing.T) {
	tests := []struct {
		name    string
		webhook Webhook
		wantErr bool
	}{
		{"valid", Webhook{Name: "deploy-hook", EventType: "deployment_complete"}, false},
		{"no event type", Webhook{Name: "deploy-hook"}, true},
		{"invalid name uppercase", Webhook{Name: "Deploy", EventType: "x"}, true},
		{"invalid name too short", Webhook{Name: "x", EventType: "x"}, true},
		{"valid hyphenated", Webhook{Name: "my-webhook-123", EventType: "issue_created"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.webhook.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGenerateSecret(t *testing.T) {
	s1 := GenerateSecret()
	s2 := GenerateSecret()

	if len(s1) != 64 {
		t.Errorf("expected 64 hex chars, got %d", len(s1))
	}
	if s1 == s2 {
		t.Error("expected different secrets on subsequent calls")
	}
}
