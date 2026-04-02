package events

import (
	"fmt"
	"testing"

	"github.com/geoffbelknap/agency/internal/models"
)

func makeEvent(id, sourceType, sourceName, eventType string) *models.Event {
	return &models.Event{
		ID:         id,
		SourceType: sourceType,
		SourceName: sourceName,
		EventType:  eventType,
		Data:       map[string]interface{}{},
	}
}

func TestRingBufferAddAndList(t *testing.T) {
	rb := NewRingBuffer(5)
	for i := 0; i < 5; i++ {
		rb.Add(makeEvent(fmt.Sprintf("evt-%d", i), "connector", "jira", "issue_created"))
	}
	events := rb.List(0)
	if len(events) != 5 {
		t.Fatalf("expected 5 events, got %d", len(events))
	}
	// Newest first
	if events[0].ID != "evt-4" {
		t.Errorf("expected newest event evt-4, got %s", events[0].ID)
	}
	if events[4].ID != "evt-0" {
		t.Errorf("expected oldest event evt-0, got %s", events[4].ID)
	}
}

func TestRingBufferWrapAround(t *testing.T) {
	rb := NewRingBuffer(DefaultRingSize)
	for i := 0; i < 1500; i++ {
		rb.Add(makeEvent(fmt.Sprintf("evt-%d", i), "connector", "jira", "issue_created"))
	}
	events := rb.List(0)
	if len(events) != DefaultRingSize {
		t.Fatalf("expected %d events after wrap, got %d", DefaultRingSize, len(events))
	}
	// Newest should be evt-1499
	if events[0].ID != "evt-1499" {
		t.Errorf("expected newest evt-1499, got %s", events[0].ID)
	}
	// Oldest retained should be evt-500
	if events[DefaultRingSize-1].ID != "evt-500" {
		t.Errorf("expected oldest evt-500, got %s", events[DefaultRingSize-1].ID)
	}
	// evt-0 should be gone
	if rb.Get("evt-0") != nil {
		t.Error("evt-0 should have been overwritten")
	}
}

func TestRingBufferGet(t *testing.T) {
	rb := NewRingBuffer(10)
	rb.Add(makeEvent("evt-abc", "connector", "jira", "issue_created"))
	rb.Add(makeEvent("evt-def", "channel", "incidents", "message"))

	if e := rb.Get("evt-abc"); e == nil || e.ID != "evt-abc" {
		t.Error("expected to find evt-abc")
	}
	if e := rb.Get("evt-missing"); e != nil {
		t.Error("expected nil for missing event")
	}
}

func TestRingBufferListLimit(t *testing.T) {
	rb := NewRingBuffer(10)
	for i := 0; i < 10; i++ {
		rb.Add(makeEvent(fmt.Sprintf("evt-%d", i), "connector", "jira", "issue_created"))
	}
	events := rb.List(3)
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
	if events[0].ID != "evt-9" {
		t.Errorf("expected evt-9, got %s", events[0].ID)
	}
}

func TestRingBufferListFiltered(t *testing.T) {
	rb := NewRingBuffer(10)
	rb.Add(makeEvent("evt-1", "connector", "jira", "issue_created"))
	rb.Add(makeEvent("evt-2", "channel", "incidents", "message"))
	rb.Add(makeEvent("evt-3", "connector", "github", "pr_opened"))
	rb.Add(makeEvent("evt-4", "connector", "jira", "issue_updated"))

	// Filter by source_type only
	events := rb.ListFiltered("connector", "", "", 0)
	if len(events) != 3 {
		t.Errorf("expected 3 connector events, got %d", len(events))
	}

	// Filter by source_type + source_name
	events = rb.ListFiltered("connector", "jira", "", 0)
	if len(events) != 2 {
		t.Errorf("expected 2 jira events, got %d", len(events))
	}

	// Filter by source_type + event_type
	events = rb.ListFiltered("connector", "", "pr_opened", 0)
	if len(events) != 1 {
		t.Errorf("expected 1 pr_opened event, got %d", len(events))
	}

	// Filter with limit
	events = rb.ListFiltered("connector", "", "", 1)
	if len(events) != 1 {
		t.Errorf("expected 1 event with limit, got %d", len(events))
	}
}
