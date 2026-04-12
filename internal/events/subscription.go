package events

import (
	"path/filepath"
	"strings"
	"sync"

	"github.com/geoffbelknap/agency/internal/models"
	"github.com/google/uuid"
)

// DestinationType enumerates subscriber destination types.
const (
	DestAgent   = "agent"
	DestWebhook = "webhook"
	DestChannel = "channel"
	DestRuntime = "runtime"
)

// SubscriptionOrigin tracks why a subscription exists.
const (
	OriginMission      = "mission"
	OriginNotification = "notification"
	OriginInstance     = "instance"
	OriginSystem       = "system" // hard-coded rules (@mention, DM)
)

// Destination describes where a matched event should be delivered.
type Destination struct {
	Type   string `json:"type"`   // agent, webhook, channel
	Target string `json:"target"` // agent name, URL, or channel name
}

// Subscription defines a rule for matching events to destinations.
type Subscription struct {
	ID          string      `json:"id"`
	SourceType  string      `json:"source_type"`           // required
	SourceName  string      `json:"source_name,omitempty"` // optional — omit for any
	EventType   string      `json:"event_type,omitempty"`  // optional — omit for any
	Match       string      `json:"match,omitempty"`       // glob pattern, channel source only
	Destination Destination `json:"destination"`
	Origin      string      `json:"origin"`               // mission, notification, system
	OriginRef   string      `json:"origin_ref,omitempty"` // mission name, notification name
	Active      bool        `json:"active"`
}

// Matches evaluates whether an event matches this subscription.
// Inactive subscriptions never match.
func (s *Subscription) Matches(e *models.Event) bool {
	if !s.Active {
		return false
	}
	if s.SourceType != e.SourceType {
		return false
	}
	if s.SourceName != "" && s.SourceName != e.SourceName {
		return false
	}
	if s.EventType != "" && s.EventType != e.EventType {
		return false
	}
	if s.Match != "" && e.SourceType == models.EventSourceChannel {
		content, _ := e.Data["content"].(string)
		matched, err := filepath.Match(s.Match, content)
		if err != nil || !matched {
			return false
		}
	}
	return true
}

// SubscriptionTable is a thread-safe collection of subscriptions.
type SubscriptionTable struct {
	mu   sync.RWMutex
	subs []*Subscription
}

// NewSubscriptionTable creates a new subscription table with system rules.
func NewSubscriptionTable() *SubscriptionTable {
	st := &SubscriptionTable{}
	st.addSystemRules()
	return st
}

// addSystemRules adds hard-coded system subscriptions for @mention and DM routing.
// These appear in the list for observability but are handled with custom logic in the bus.
func (st *SubscriptionTable) addSystemRules() {
	st.subs = append(st.subs, &Subscription{
		ID:         "sys-mention",
		SourceType: models.EventSourceChannel,
		EventType:  "message",
		Origin:     OriginSystem,
		OriginRef:  "mention",
		Active:     true,
		Destination: Destination{
			Type:   DestAgent,
			Target: "@mentioned", // placeholder — bus extracts actual target
		},
	})
	st.subs = append(st.subs, &Subscription{
		ID:         "sys-dm",
		SourceType: models.EventSourceChannel,
		EventType:  "message",
		Origin:     OriginSystem,
		OriginRef:  "dm",
		Active:     true,
		Destination: Destination{
			Type:   DestAgent,
			Target: "@dm-target", // placeholder — bus extracts actual target
		},
	})
}

// Add adds a subscription to the table. If the subscription has no ID, one is generated.
func (st *SubscriptionTable) Add(sub *Subscription) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if sub.ID == "" {
		sub.ID = "sub-" + strings.ReplaceAll(uuid.New().String()[:8], "-", "")
	}
	st.subs = append(st.subs, sub)
}

// RemoveByOrigin removes all subscriptions with the given origin and origin ref.
func (st *SubscriptionTable) RemoveByOrigin(origin, originRef string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	filtered := make([]*Subscription, 0, len(st.subs))
	for _, s := range st.subs {
		if s.Origin == origin && s.OriginRef == originRef {
			continue
		}
		filtered = append(filtered, s)
	}
	st.subs = filtered
}

// DeactivateByOrigin deactivates all subscriptions with the given origin and origin ref.
func (st *SubscriptionTable) DeactivateByOrigin(origin, originRef string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, s := range st.subs {
		if s.Origin == origin && s.OriginRef == originRef {
			s.Active = false
		}
	}
}

// ActivateByOrigin activates all subscriptions with the given origin and origin ref.
func (st *SubscriptionTable) ActivateByOrigin(origin, originRef string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, s := range st.subs {
		if s.Origin == origin && s.OriginRef == originRef {
			s.Active = true
		}
	}
}

// Match returns all active subscriptions that match the given event.
// System subscriptions (origin=system) are excluded — they are handled
// with custom logic in the bus.
func (st *SubscriptionTable) Match(e *models.Event) []*Subscription {
	st.mu.RLock()
	defer st.mu.RUnlock()
	var matches []*Subscription
	for _, s := range st.subs {
		if s.Origin == OriginSystem {
			continue // handled by bus custom logic
		}
		if s.Matches(e) {
			matches = append(matches, s)
		}
	}
	return matches
}

// List returns all subscriptions (including system rules) for observability.
func (st *SubscriptionTable) List() []*Subscription {
	st.mu.RLock()
	defer st.mu.RUnlock()
	result := make([]*Subscription, len(st.subs))
	copy(result, st.subs)
	return result
}
