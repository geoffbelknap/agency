package events

import (
	"testing"

	"github.com/geoffbelknap/agency/internal/config"
)

func TestBuildNotificationSubscriptions_Basic(t *testing.T) {
	configs := []config.NotificationConfig{
		{
			Name:   "ops-alerts",
			Type:   "ntfy",
			URL:    "https://ntfy.example.com/agency",
			Events: []string{"agent_started", "agent_stopped"},
		},
	}

	subs := BuildNotificationSubscriptions(configs)
	if len(subs) != 2 {
		t.Fatalf("expected 2 subscriptions, got %d", len(subs))
	}

	// First sub
	if subs[0].EventType != "agent_started" {
		t.Errorf("expected event_type agent_started, got %s", subs[0].EventType)
	}
	if subs[0].Destination.Type != DestNtfy {
		t.Errorf("expected destination type ntfy, got %s", subs[0].Destination.Type)
	}
	if subs[0].SourceType != "platform" {
		t.Errorf("expected source_type platform, got %s", subs[0].SourceType)
	}
	if subs[0].Origin != OriginNotification {
		t.Errorf("expected origin notification, got %s", subs[0].Origin)
	}
	if subs[0].OriginRef != "ops-alerts" {
		t.Errorf("expected origin_ref ops-alerts, got %s", subs[0].OriginRef)
	}
	if !subs[0].Active {
		t.Error("expected subscription to be active")
	}

	// Second sub
	if subs[1].EventType != "agent_stopped" {
		t.Errorf("expected event_type agent_stopped, got %s", subs[1].EventType)
	}
}

func TestBuildNotificationSubscriptions_Webhook(t *testing.T) {
	configs := []config.NotificationConfig{
		{
			Name:    "pagerduty",
			Type:    "webhook",
			URL:     "https://hooks.example.com/events",
			Events:  []string{"agent_error"},
			Headers: map[string]string{"Authorization": "Bearer tok"},
		},
	}

	subs := BuildNotificationSubscriptions(configs)
	if len(subs) != 1 {
		t.Fatalf("expected 1 subscription, got %d", len(subs))
	}

	if subs[0].Destination.Type != DestWebhook {
		t.Errorf("expected destination type webhook, got %s", subs[0].Destination.Type)
	}
	if subs[0].Destination.Target != "https://hooks.example.com/events" {
		t.Errorf("unexpected target: %s", subs[0].Destination.Target)
	}
}

func TestBuildNotificationSubscriptions_Multiple(t *testing.T) {
	configs := []config.NotificationConfig{
		{
			Name:   "ntfy-all",
			Type:   "ntfy",
			URL:    "https://ntfy.sh/agency",
			Events: []string{"agent_started", "agent_stopped", "agent_error"},
		},
		{
			Name:   "webhook-errors",
			Type:   "webhook",
			URL:    "https://hooks.example.com/errors",
			Events: []string{"agent_error"},
		},
	}

	subs := BuildNotificationSubscriptions(configs)
	// 3 from first config + 1 from second = 4
	if len(subs) != 4 {
		t.Fatalf("expected 4 subscriptions, got %d", len(subs))
	}
}

func TestBuildNotificationSubscriptions_Empty(t *testing.T) {
	subs := BuildNotificationSubscriptions(nil)
	if len(subs) != 0 {
		t.Fatalf("expected 0 subscriptions for nil configs, got %d", len(subs))
	}

	subs = BuildNotificationSubscriptions([]config.NotificationConfig{})
	if len(subs) != 0 {
		t.Fatalf("expected 0 subscriptions for empty configs, got %d", len(subs))
	}
}
