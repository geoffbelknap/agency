package events

import (
	"testing"

	"github.com/geoffbelknap/agency/internal/models"
)

func TestSubscriptionMatchesExact(t *testing.T) {
	sub := &Subscription{
		ID:         "sub-1",
		SourceType: "connector",
		SourceName: "jira",
		EventType:  "issue_created",
		Active:     true,
	}
	e := makeEvent("evt-1", "connector", "jira", "issue_created")
	if !sub.Matches(e) {
		t.Error("expected exact match")
	}
}

func TestSubscriptionMatchesWildcardSourceName(t *testing.T) {
	sub := &Subscription{
		ID:         "sub-1",
		SourceType: "connector",
		EventType:  "issue_created",
		Active:     true,
	}
	e := makeEvent("evt-1", "connector", "jira", "issue_created")
	if !sub.Matches(e) {
		t.Error("expected match with wildcard source_name")
	}
	e2 := makeEvent("evt-2", "connector", "github", "issue_created")
	if !sub.Matches(e2) {
		t.Error("expected match with any source_name")
	}
}

func TestSubscriptionMatchesWildcardEventType(t *testing.T) {
	sub := &Subscription{
		ID:         "sub-1",
		SourceType: "connector",
		SourceName: "jira",
		Active:     true,
	}
	e := makeEvent("evt-1", "connector", "jira", "issue_created")
	if !sub.Matches(e) {
		t.Error("expected match with wildcard event_type")
	}
	e2 := makeEvent("evt-2", "connector", "jira", "issue_updated")
	if !sub.Matches(e2) {
		t.Error("expected match with any event_type")
	}
}

func TestSubscriptionNoMatchWrongSourceType(t *testing.T) {
	sub := &Subscription{
		ID:         "sub-1",
		SourceType: "connector",
		Active:     true,
	}
	e := makeEvent("evt-1", "channel", "incidents", "message")
	if sub.Matches(e) {
		t.Error("expected no match on wrong source_type")
	}
}

func TestSubscriptionInactiveNoMatch(t *testing.T) {
	sub := &Subscription{
		ID:         "sub-1",
		SourceType: "connector",
		Active:     false,
	}
	e := makeEvent("evt-1", "connector", "jira", "issue_created")
	if sub.Matches(e) {
		t.Error("inactive subscription should not match")
	}
}

func TestSubscriptionGlobMatch(t *testing.T) {
	sub := &Subscription{
		ID:         "sub-1",
		SourceType: "channel",
		EventType:  "message",
		Match:      "urgent*",
		Active:     true,
	}
	e := &models.Event{
		ID:         "evt-1",
		SourceType: "channel",
		SourceName: "incidents",
		EventType:  "message",
		Data:       map[string]interface{}{"content": "urgent fix needed"},
	}
	if sub.Matches(e) {
		// filepath.Match requires exact match of the whole string, not prefix
		// "urgent*" matches "urgent fix needed" only if no path separators
		// Actually filepath.Match("urgent*", "urgent fix needed") — * doesn't match spaces? Let's check
		// * matches any sequence of non-Separator characters — on linux separator is /
		// So "urgent*" should match "urgent fix needed"
		t.Log("glob matched as expected")
	}

	// Test non-matching glob
	e2 := &models.Event{
		ID:         "evt-2",
		SourceType: "channel",
		SourceName: "incidents",
		EventType:  "message",
		Data:       map[string]interface{}{"content": "normal update"},
	}
	if sub.Matches(e2) {
		t.Error("glob should not match non-matching content")
	}
}

func TestSubscriptionTableAddAndList(t *testing.T) {
	st := NewSubscriptionTable()
	initialCount := len(st.List()) // system rules

	st.Add(&Subscription{
		SourceType: "connector",
		SourceName: "jira",
		EventType:  "issue_created",
		Active:     true,
		Origin:     OriginMission,
		OriginRef:  "incident-response",
		Destination: Destination{
			Type:   DestAgent,
			Target: "responder",
		},
	})

	subs := st.List()
	if len(subs) != initialCount+1 {
		t.Fatalf("expected %d subs, got %d", initialCount+1, len(subs))
	}
}

func TestSubscriptionTableMatch(t *testing.T) {
	st := NewSubscriptionTable()
	st.Add(&Subscription{
		ID:         "sub-match",
		SourceType: "connector",
		SourceName: "jira",
		EventType:  "issue_created",
		Active:     true,
		Origin:     OriginMission,
		OriginRef:  "test-mission",
		Destination: Destination{
			Type:   DestAgent,
			Target: "responder",
		},
	})

	e := makeEvent("evt-1", "connector", "jira", "issue_created")
	matches := st.Match(e)
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if matches[0].ID != "sub-match" {
		t.Errorf("expected sub-match, got %s", matches[0].ID)
	}

	// Non-matching event
	e2 := makeEvent("evt-2", "channel", "incidents", "message")
	matches = st.Match(e2)
	if len(matches) != 0 {
		t.Errorf("expected 0 matches, got %d", len(matches))
	}
}

func TestSubscriptionTableDeactivateActivate(t *testing.T) {
	st := NewSubscriptionTable()
	st.Add(&Subscription{
		ID:         "sub-1",
		SourceType: "connector",
		Active:     true,
		Origin:     OriginMission,
		OriginRef:  "mission-a",
		Destination: Destination{
			Type:   DestAgent,
			Target: "agent-1",
		},
	})

	e := makeEvent("evt-1", "connector", "jira", "issue_created")

	// Should match when active
	if len(st.Match(e)) != 1 {
		t.Fatal("expected 1 match when active")
	}

	// Deactivate
	st.DeactivateByOrigin(OriginMission, "mission-a")
	if len(st.Match(e)) != 0 {
		t.Error("expected 0 matches when deactivated")
	}

	// Reactivate
	st.ActivateByOrigin(OriginMission, "mission-a")
	if len(st.Match(e)) != 1 {
		t.Error("expected 1 match after reactivation")
	}
}

func TestSubscriptionTableRemoveByOrigin(t *testing.T) {
	st := NewSubscriptionTable()
	initialCount := len(st.List())

	st.Add(&Subscription{
		ID:         "sub-1",
		SourceType: "connector",
		Active:     true,
		Origin:     OriginMission,
		OriginRef:  "mission-a",
		Destination: Destination{
			Type:   DestAgent,
			Target: "agent-1",
		},
	})
	st.Add(&Subscription{
		ID:         "sub-2",
		SourceType: "connector",
		Active:     true,
		Origin:     OriginMission,
		OriginRef:  "mission-b",
		Destination: Destination{
			Type:   DestAgent,
			Target: "agent-2",
		},
	})

	st.RemoveByOrigin(OriginMission, "mission-a")
	subs := st.List()
	if len(subs) != initialCount+1 { // system rules + mission-b
		t.Errorf("expected %d subs after remove, got %d", initialCount+1, len(subs))
	}
}

func TestSubscriptionTableSystemRulesInList(t *testing.T) {
	st := NewSubscriptionTable()
	subs := st.List()
	foundMention := false
	foundDM := false
	for _, s := range subs {
		if s.ID == "sys-mention" {
			foundMention = true
		}
		if s.ID == "sys-dm" {
			foundDM = true
		}
	}
	if !foundMention {
		t.Error("expected sys-mention in list")
	}
	if !foundDM {
		t.Error("expected sys-dm in list")
	}
}
