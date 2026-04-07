package api

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/events"
	"github.com/geoffbelknap/agency/internal/models"
)

// detectNotificationType infers the notification type from the destination URL.
func detectNotificationType(url string) string {
	lower := strings.ToLower(url)
	if strings.Contains(lower, "ntfy.") || strings.Contains(lower, "ntfy.sh") {
		return "ntfy"
	}
	return "webhook"
}

var defaultNotificationEvents = []string{"operator_alert", "enforcer_exited", "mission_health_alert"}

// ── Events (6 tools) ────────────────────────────────────────────────────────

func registerEventTools(reg *MCPToolRegistry) {

	// agency_event_list
	reg.Register(
		"agency_event_list",
		"List recent events from the event bus. Filter by source_type, source_name, or event_type.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"source_type": map[string]interface{}{"type": "string", "description": "Filter by source type (connector, channel, schedule, webhook, platform)"},
				"source_name": map[string]interface{}{"type": "string", "description": "Filter by source name"},
				"event_type":  map[string]interface{}{"type": "string", "description": "Filter by event type"},
				"limit":       map[string]interface{}{"type": "integer", "description": "Max events to return (default 50)", "default": 50},
			},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			if d.eventBus == nil {
				return "Event bus not initialized.", true
			}

			sourceType := mapStr(args, "source_type")
			sourceName := mapStr(args, "source_name")
			eventType := mapStr(args, "event_type")
			limit := 50
			if l, ok := args["limit"].(float64); ok && l > 0 {
				limit = int(l)
			}

			var events interface{}
			if sourceType != "" || sourceName != "" || eventType != "" {
				events = d.eventBus.Events().ListFiltered(sourceType, sourceName, eventType, limit)
			} else {
				events = d.eventBus.Events().List(limit)
			}

			data, _ := json.Marshal(events)
			return string(data), false
		},
	)

	// agency_event_show
	reg.Register(
		"agency_event_show",
		"Show details of a specific event by ID.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id": map[string]interface{}{"type": "string", "description": "Event ID"},
			},
			"required": []string{"id"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			if d.eventBus == nil {
				return "Event bus not initialized.", true
			}

			id := mapStr(args, "id")
			if id == "" {
				return "Error: id is required", true
			}

			event := d.eventBus.Events().Get(id)
			if event == nil {
				return fmt.Sprintf("Event %q not found.", id), true
			}

			data, _ := json.Marshal(event)
			return string(data), false
		},
	)

	// agency_event_subscriptions
	reg.Register(
		"agency_event_subscriptions",
		"List all active event subscriptions (from missions, notifications, and system rules).",
		nil,
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			if d.eventBus == nil {
				return "Event bus not initialized.", true
			}

			subs := d.eventBus.Subscriptions().List()
			if len(subs) == 0 {
				return "No subscriptions.", false
			}

			var lines []string
			for _, s := range subs {
				active := "active"
				if !s.Active {
					active = "paused"
				}
				lines = append(lines, fmt.Sprintf("  %s: %s/%s -> %s/%s [%s] (%s:%s)",
					s.ID, s.SourceType, s.EventType,
					s.Destination.Type, s.Destination.Target,
					active, s.Origin, s.OriginRef,
				))
			}
			return fmt.Sprintf("Subscriptions (%d):\n%s", len(subs), strings.Join(lines, "\n")), false
		},
	)

	// agency_webhook_create
	reg.Register(
		"agency_webhook_create",
		"Register an inbound webhook endpoint. Returns the URL and secret.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name":       map[string]interface{}{"type": "string", "description": "Webhook name (lowercase alphanumeric with hyphens)"},
				"event_type": map[string]interface{}{"type": "string", "description": "Event type this webhook produces"},
			},
			"required": []string{"name", "event_type"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			if d.webhookMgr == nil {
				return "Webhook manager not initialized.", true
			}

			name := mapStr(args, "name")
			eventType := mapStr(args, "event_type")
			if name == "" || eventType == "" {
				return "Error: name and event_type are required", true
			}

			wh, err := d.webhookMgr.Create(name, eventType)
			if err != nil {
				return "Error: " + err.Error(), true
			}

			return fmt.Sprintf("Webhook created:\n  Name: %s\n  URL: %s\n  Secret: %s\n  Event type: %s",
				wh.Name, wh.URL, wh.Secret, wh.EventType), false
		},
	)

	// agency_webhook_list
	reg.Register(
		"agency_webhook_list",
		"List all registered inbound webhooks.",
		nil,
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			if d.webhookMgr == nil {
				return "Webhook manager not initialized.", true
			}

			webhooks, err := d.webhookMgr.List()
			if err != nil {
				return "Error: " + err.Error(), true
			}
			if len(webhooks) == 0 {
				return "No webhooks registered.", false
			}

			var lines []string
			for _, wh := range webhooks {
				lines = append(lines, fmt.Sprintf("  %s: %s (%s)", wh.Name, wh.EventType, wh.URL))
			}
			return fmt.Sprintf("Webhooks (%d):\n%s", len(webhooks), strings.Join(lines, "\n")), false
		},
	)

	// agency_webhook_delete
	reg.Register(
		"agency_webhook_delete",
		"Delete a registered inbound webhook.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{"type": "string", "description": "Webhook name"},
			},
			"required": []string{"name"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			if d.webhookMgr == nil {
				return "Webhook manager not initialized.", true
			}

			name := mapStr(args, "name")
			if name == "" {
				return "Error: name is required", true
			}

			if err := d.webhookMgr.Delete(name); err != nil {
				return "Error: " + err.Error(), true
			}

			return fmt.Sprintf("Webhook '%s' deleted.", name), false
		},
	)
}

// ── Notifications (4 tools) ────────────────────────────────────────────────

func registerNotificationTools(reg *MCPToolRegistry) {

	// agency_notification_list
	reg.Register(
		"agency_notification_list",
		"List configured notification destinations for operator alerts.",
		nil,
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			if d.notifStore == nil {
				return "Notification store not initialized.", true
			}
			configs := d.notifStore.List()
			if len(configs) == 0 {
				return "No notification destinations configured.", false
			}
			data, _ := json.Marshal(configs)
			return string(data), false
		},
	)

	// agency_notification_add
	reg.Register(
		"agency_notification_add",
		"Add a notification destination for operator alerts. Type is auto-detected from URL if omitted. Default events: operator_alert, enforcer_exited, mission_health_alert.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name":   map[string]interface{}{"type": "string", "description": "Unique name for this destination"},
				"url":    map[string]interface{}{"type": "string", "description": "Notification URL (ntfy or webhook)"},
				"type":   map[string]interface{}{"type": "string", "description": "ntfy or webhook (auto-detected if omitted)"},
				"events": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Event types to subscribe to"},
			},
			"required": []string{"name", "url"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			if d.notifStore == nil {
				return "Notification store not initialized.", true
			}

			name := mapStr(args, "name")
			url := mapStr(args, "url")
			nType := mapStr(args, "type")
			if name == "" || url == "" {
				return "Error: name and url are required", true
			}

			if nType == "" {
				nType = detectNotificationType(url)
			}

			evts := defaultNotificationEvents
			if rawEvts, ok := args["events"].([]interface{}); ok && len(rawEvts) > 0 {
				evts = make([]string, len(rawEvts))
				for i, e := range rawEvts {
					evts[i], _ = e.(string)
				}
			}

			nc := config.NotificationConfig{
				Name:   name,
				Type:   nType,
				URL:    url,
				Events: evts,
			}

			if err := d.notifStore.Add(nc); err != nil {
				return "Error: " + err.Error(), true
			}

			// Hot-reload subscriptions
			if d.eventBus != nil {
				subs := events.BuildNotificationSubscriptions([]config.NotificationConfig{nc})
				for _, sub := range subs {
					d.eventBus.Subscriptions().Add(sub)
				}
			}

			return fmt.Sprintf("Added notification destination %q (%s) for events: %s", name, nType, strings.Join(evts, ", ")), false
		},
	)

	// agency_notification_remove
	reg.Register(
		"agency_notification_remove",
		"Remove a notification destination.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{"type": "string", "description": "Name of the destination to remove"},
			},
			"required": []string{"name"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			if d.notifStore == nil {
				return "Notification store not initialized.", true
			}

			name := mapStr(args, "name")
			if name == "" {
				return "Error: name is required", true
			}

			if err := d.notifStore.Remove(name); err != nil {
				return "Error: " + err.Error(), true
			}

			if d.eventBus != nil {
				d.eventBus.Subscriptions().RemoveByOrigin(events.OriginNotification, name)
			}

			return fmt.Sprintf("Removed notification destination %q", name), false
		},
	)

	// agency_notification_test
	reg.Register(
		"agency_notification_test",
		"Send a test notification to verify delivery is working.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{"type": "string", "description": "Name of the destination to test"},
			},
			"required": []string{"name"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			if d.notifStore == nil {
				return "Notification store not initialized.", true
			}

			name := mapStr(args, "name")
			if name == "" {
				return "Error: name is required", true
			}

			if _, err := d.notifStore.Get(name); err != nil {
				return "Error: " + err.Error(), true
			}

			if d.eventBus == nil {
				return "Event bus not initialized.", true
			}

			event := models.NewEvent(models.EventSourcePlatform, "gateway", "operator_alert", map[string]interface{}{
				"category": "test",
				"severity": "info",
				"message":  "Test notification from agency",
			})
			d.eventBus.Publish(event)

			return fmt.Sprintf("Test notification sent (event: %s)", event.ID), false
		},
	)
}
