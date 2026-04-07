package events

import (
	"fmt"
	"strings"

	"log/slog"
	"github.com/geoffbelknap/agency/internal/models"
)

// DeliveryFunc is called for each matching subscription. Implementations
// handle agent delivery, outbound webhook POSTs, and ntfy formatting.
type DeliveryFunc func(sub *Subscription, event *models.Event) error

// AuditFunc writes audit log entries for event lifecycle.
type AuditFunc func(eventType string, data map[string]interface{})

// Bus receives events, evaluates subscriptions, and dispatches to delivery
// handlers. It owns the ring buffer and subscription table.
type Bus struct {
	ring    *RingBuffer
	table   *SubscriptionTable
	deliver map[string]DeliveryFunc // keyed by destination type
	audit   AuditFunc
	logger  *slog.Logger
}

// NewBus creates an event bus with a default-sized ring buffer and empty
// subscription table.
func NewBus(logger *slog.Logger, audit AuditFunc) *Bus {
	return &Bus{
		ring:    NewRingBuffer(DefaultRingSize),
		table:   NewSubscriptionTable(),
		deliver: make(map[string]DeliveryFunc),
		audit:   audit,
		logger:  logger,
	}
}

// RegisterDelivery registers a delivery handler for a destination type.
func (b *Bus) RegisterDelivery(destType string, fn DeliveryFunc) {
	b.deliver[destType] = fn
}

// Publish adds an event to the ring buffer, evaluates subscriptions,
// and dispatches to matching delivery handlers.
func (b *Bus) Publish(e *models.Event) {
	// Step 1: Validate
	if err := e.Validate(); err != nil {
		b.logger.Warn("event validation failed", "error", err, "event_id", e.ID)
		return
	}

	// Step 2: Add to ring buffer
	b.ring.Add(e)

	// Step 3: Audit received
	b.auditEvent("event_received", map[string]interface{}{
		"event_id":    e.ID,
		"source_type": e.SourceType,
		"source_name": e.SourceName,
		"event_type":  e.EventType,
	})

	delivered := 0

	// Step 4: Handle @mention and DM routing for channel events
	if e.SourceType == models.EventSourceChannel && e.EventType == "message" {
		delivered += b.handleMentions(e)
		delivered += b.handleDM(e)
	}

	// Step 5: Evaluate subscription table
	matches := b.table.Match(e)

	// Step 6: Deliver to each match
	for _, sub := range matches {
		if err := b.deliverEvent(sub, e); err != nil {
			b.logger.Error("event delivery failed",
				"event_id", e.ID,
				"subscription_id", sub.ID,
				"error", err,
			)
			b.auditEvent("event_delivery_failed", map[string]interface{}{
				"event_id":        e.ID,
				"subscription_id": sub.ID,
				"error":           err.Error(),
			})
		} else {
			delivered++
			b.auditEvent("event_delivered", map[string]interface{}{
				"event_id":        e.ID,
				"subscription_id": sub.ID,
				"destination":     sub.Destination.Target,
			})
		}
	}

	// Step 7: If zero matches, audit unmatched
	if delivered == 0 && len(matches) == 0 {
		b.auditEvent("event_unmatched", map[string]interface{}{
			"event_id":    e.ID,
			"source_type": e.SourceType,
			"source_name": e.SourceName,
			"event_type":  e.EventType,
		})
	}
}

// handleMentions delivers channel events to @mentioned agents.
func (b *Bus) handleMentions(e *models.Event) int {
	delivered := 0
	mentionsRaw, ok := e.Metadata["mentions"]
	if !ok {
		return 0
	}
	mentions, ok := mentionsRaw.([]string)
	if !ok {
		return 0
	}
	for _, agentName := range mentions {
		name := strings.TrimPrefix(agentName, "@")
		sub := &Subscription{
			ID:         fmt.Sprintf("sys-mention-%s-%s", name, e.ID),
			SourceType: models.EventSourceChannel,
			EventType:  "message",
			Origin:     OriginSystem,
			OriginRef:  "mention",
			Active:     true,
			Destination: Destination{
				Type:   DestAgent,
				Target: name,
			},
		}
		if err := b.deliverEvent(sub, e); err != nil {
			b.logger.Error("mention delivery failed",
				"event_id", e.ID,
				"agent", name,
				"error", err,
			)
			b.auditEvent("event_delivery_failed", map[string]interface{}{
				"event_id": e.ID,
				"agent":    name,
				"reason":   "mention",
				"error":    err.Error(),
			})
		} else {
			delivered++
			b.auditEvent("event_delivered", map[string]interface{}{
				"event_id":    e.ID,
				"agent":       name,
				"reason":      "mention",
				"destination": name,
			})
		}
	}
	return delivered
}

// handleDM delivers channel events for direct message channels to the DM target.
func (b *Bus) handleDM(e *models.Event) int {
	channelType, _ := e.Metadata["channel_type"].(string)
	if channelType != "direct" {
		return 0
	}
	target, _ := e.Metadata["dm_target"].(string)
	if target == "" {
		return 0
	}
	sub := &Subscription{
		ID:         fmt.Sprintf("sys-dm-%s-%s", target, e.ID),
		SourceType: models.EventSourceChannel,
		EventType:  "message",
		Origin:     OriginSystem,
		OriginRef:  "dm",
		Active:     true,
		Destination: Destination{
			Type:   DestAgent,
			Target: target,
		},
	}
	if err := b.deliverEvent(sub, e); err != nil {
		b.logger.Error("DM delivery failed",
			"event_id", e.ID,
			"target", target,
			"error", err,
		)
		b.auditEvent("event_delivery_failed", map[string]interface{}{
			"event_id": e.ID,
			"target":   target,
			"reason":   "dm",
			"error":    err.Error(),
		})
		return 0
	}
	b.auditEvent("event_delivered", map[string]interface{}{
		"event_id":    e.ID,
		"target":      target,
		"reason":      "dm",
		"destination": target,
	})
	return 1
}

// deliverEvent calls the registered delivery function for the subscription's
// destination type.
func (b *Bus) deliverEvent(sub *Subscription, e *models.Event) error {
	fn, ok := b.deliver[sub.Destination.Type]
	if !ok {
		return fmt.Errorf("no delivery handler for destination type: %s", sub.Destination.Type)
	}
	return fn(sub, e)
}

// auditEvent calls the audit function if one is registered.
func (b *Bus) auditEvent(eventType string, data map[string]interface{}) {
	if b.audit != nil {
		b.audit(eventType, data)
	}
}

// Subscriptions returns the subscription table for external management.
func (b *Bus) Subscriptions() *SubscriptionTable {
	return b.table
}

// Events returns the ring buffer for observability queries.
func (b *Bus) Events() *RingBuffer {
	return b.ring
}
