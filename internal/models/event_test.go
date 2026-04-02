package models

import "testing"

func TestEventValidation(t *testing.T) {
	tests := []struct {
		name    string
		event   Event
		wantErr bool
	}{
		{"valid", Event{ID: "evt-abc", SourceType: "connector", SourceName: "jira", EventType: "issue_created"}, false},
		{"no id", Event{SourceType: "connector", SourceName: "jira", EventType: "issue_created"}, true},
		{"bad source_type", Event{ID: "evt-abc", SourceType: "invalid", SourceName: "jira", EventType: "issue_created"}, true},
		{"no source_name", Event{ID: "evt-abc", SourceType: "connector", EventType: "issue_created"}, true},
		{"no event_type", Event{ID: "evt-abc", SourceType: "connector", SourceName: "jira"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.event.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewEvent(t *testing.T) {
	e := NewEvent("connector", "jira-ops", "issue_created", map[string]interface{}{"key": "INC-1"})
	if e.ID == "" || e.SourceType != "connector" || e.EventType != "issue_created" {
		t.Errorf("unexpected event: %+v", e)
	}
	if err := e.Validate(); err != nil {
		t.Errorf("new event should be valid: %v", err)
	}
}

func TestNewChannelEvent(t *testing.T) {
	e := NewChannelEvent("incidents", "msg123", map[string]interface{}{"content": "test"}, nil)
	if e.ID != "evt-msg-msg123" {
		t.Errorf("expected evt-msg-msg123, got %s", e.ID)
	}
}
