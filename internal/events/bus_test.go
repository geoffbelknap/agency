package events

import (
	"errors"
	"sync"
	"testing"

	"github.com/geoffbelknap/agency/internal/models"
	"log/slog"
)

type auditEntry struct {
	eventType string
	data      map[string]interface{}
}

func newTestBus() (*Bus, *[]auditEntry, *[]string) {
	var auditLog []auditEntry
	var mu sync.Mutex
	audit := func(eventType string, data map[string]interface{}) {
		mu.Lock()
		defer mu.Unlock()
		auditLog = append(auditLog, auditEntry{eventType, data})
	}

	delivered := &[]string{}
	logger := slog.Default()

	bus := NewBus(logger, audit)
	bus.RegisterDelivery(DestAgent, func(sub *Subscription, event *models.Event) error {
		*delivered = append(*delivered, sub.Destination.Target)
		return nil
	})

	return bus, &auditLog, delivered
}

func hasAuditEntry(entries []auditEntry, eventType string) bool {
	for _, e := range entries {
		if e.eventType == eventType {
			return true
		}
	}
	return false
}

func TestBusPublishOneMatch(t *testing.T) {
	bus, auditLog, delivered := newTestBus()

	bus.Subscriptions().Add(&Subscription{
		ID:         "sub-1",
		SourceType: "connector",
		SourceName: "jira",
		EventType:  "issue_created",
		Active:     true,
		Origin:     OriginMission,
		OriginRef:  "test",
		Destination: Destination{
			Type:   DestAgent,
			Target: "responder",
		},
	})

	e := models.NewEvent("connector", "jira", "issue_created", map[string]interface{}{"key": "INC-1"})
	bus.Publish(e)

	if len(*delivered) != 1 || (*delivered)[0] != "responder" {
		t.Errorf("expected delivery to responder, got %v", *delivered)
	}
	if !hasAuditEntry(*auditLog, "event_received") {
		t.Error("expected event_received audit")
	}
	if !hasAuditEntry(*auditLog, "event_delivered") {
		t.Error("expected event_delivered audit")
	}
}

func TestBusPublishNoMatch(t *testing.T) {
	bus, auditLog, _ := newTestBus()

	e := models.NewEvent("webhook", "github", "push", map[string]interface{}{})
	bus.Publish(e)

	if !hasAuditEntry(*auditLog, "event_received") {
		t.Error("expected event_received audit")
	}
	if !hasAuditEntry(*auditLog, "event_unmatched") {
		t.Error("expected event_unmatched audit")
	}
}

func TestBusPublishMultipleMatches(t *testing.T) {
	bus, _, delivered := newTestBus()

	bus.Subscriptions().Add(&Subscription{
		ID:         "sub-1",
		SourceType: "connector",
		Active:     true,
		Origin:     OriginMission,
		OriginRef:  "m1",
		Destination: Destination{
			Type:   DestAgent,
			Target: "agent-a",
		},
	})
	bus.Subscriptions().Add(&Subscription{
		ID:         "sub-2",
		SourceType: "connector",
		Active:     true,
		Origin:     OriginMission,
		OriginRef:  "m2",
		Destination: Destination{
			Type:   DestAgent,
			Target: "agent-b",
		},
	})

	e := models.NewEvent("connector", "jira", "issue_created", map[string]interface{}{})
	bus.Publish(e)

	if len(*delivered) != 2 {
		t.Errorf("expected 2 deliveries, got %d", len(*delivered))
	}
}

func TestBusMentionRouting(t *testing.T) {
	bus, auditLog, delivered := newTestBus()

	e := models.NewChannelEvent("incidents", "msg-1",
		map[string]interface{}{"content": "@responder check this"},
		map[string]interface{}{"mentions": []string{"responder"}},
	)
	bus.Publish(e)

	if len(*delivered) != 1 || (*delivered)[0] != "responder" {
		t.Errorf("expected mention delivery to responder, got %v", *delivered)
	}
	if !hasAuditEntry(*auditLog, "event_delivered") {
		t.Error("expected event_delivered audit for mention")
	}
}

func TestBusDMRouting(t *testing.T) {
	bus, _, delivered := newTestBus()

	e := models.NewChannelEvent("dm-chan", "msg-2",
		map[string]interface{}{"content": "hello"},
		map[string]interface{}{
			"channel_type": "direct",
			"dm_target":    "agent-x",
			"author":       "_operator",
		},
	)
	bus.Publish(e)

	if len(*delivered) != 1 || (*delivered)[0] != "agent-x" {
		t.Errorf("expected DM delivery to agent-x, got %v", *delivered)
	}
}

func TestBusDMRoutingIgnoresGatewayAndTargetMessages(t *testing.T) {
	for _, author := range []string{"_gateway", "agent-x"} {
		bus, _, delivered := newTestBus()

		e := models.NewChannelEvent("dm-agent-x", "msg-"+author,
			map[string]interface{}{"content": "hello"},
			map[string]interface{}{
				"channel_type": "direct",
				"dm_target":    "agent-x",
				"author":       author,
			},
		)
		bus.Publish(e)

		if len(*delivered) != 0 {
			t.Fatalf("author %s should not be delivered back to agent-x, got %v", author, *delivered)
		}
	}
}

func TestBusDeliveryFailure(t *testing.T) {
	var auditLog []auditEntry
	audit := func(eventType string, data map[string]interface{}) {
		auditLog = append(auditLog, auditEntry{eventType, data})
	}
	logger := slog.Default()
	bus := NewBus(logger, audit)

	bus.RegisterDelivery(DestAgent, func(sub *Subscription, event *models.Event) error {
		return errors.New("delivery timeout")
	})

	bus.Subscriptions().Add(&Subscription{
		ID:         "sub-fail",
		SourceType: "connector",
		Active:     true,
		Origin:     OriginMission,
		OriginRef:  "test",
		Destination: Destination{
			Type:   DestAgent,
			Target: "agent-1",
		},
	})

	e := models.NewEvent("connector", "jira", "issue_created", map[string]interface{}{})
	bus.Publish(e)

	if !hasAuditEntry(auditLog, "event_delivery_failed") {
		t.Error("expected event_delivery_failed audit")
	}
}

func TestBusInvalidEvent(t *testing.T) {
	bus, auditLog, _ := newTestBus()

	e := &models.Event{ID: "", SourceType: "invalid"}
	bus.Publish(e)

	// Should not be audited as received
	if hasAuditEntry(*auditLog, "event_received") {
		t.Error("invalid event should not be audited as received")
	}
}

func TestBusRingBufferAndSubscriptions(t *testing.T) {
	bus, _, _ := newTestBus()

	if bus.Events() == nil {
		t.Error("expected non-nil ring buffer")
	}
	if bus.Subscriptions() == nil {
		t.Error("expected non-nil subscription table")
	}
}
