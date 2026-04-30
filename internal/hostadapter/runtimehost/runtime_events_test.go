package runtimehost

import "testing"

func TestNormalizeRuntimeEvent(t *testing.T) {
	tests := []struct {
		name      string
		eventName string
		action    string
		wantID    string
		wantComp  string
		wantAct   string
		wantOK    bool
	}{
		{
			name:      "enforcer stopped",
			eventName: "agency-henrybot900-enforcer",
			action:    "die",
			wantID:    "henrybot900",
			wantComp:  RuntimeComponentEnforcer,
			wantAct:   RuntimeActionStopped,
			wantOK:    true,
		},
		{
			name:      "workspace started",
			eventName: "/agency-bob-workspace",
			action:    "start",
			wantID:    "bob",
			wantComp:  RuntimeComponentWorkspace,
			wantAct:   RuntimeActionStarted,
			wantOK:    true,
		},
		{
			name:      "agent names can contain hyphens",
			eventName: "agency-my-agent-name-enforcer",
			action:    "die",
			wantID:    "my-agent-name",
			wantComp:  RuntimeComponentEnforcer,
			wantAct:   RuntimeActionStopped,
			wantOK:    true,
		},
		{
			name:      "wrong prefix",
			eventName: "other-henrybot900-enforcer",
			action:    "die",
			wantOK:    false,
		},
		{
			name:      "unknown component",
			eventName: "agency-henrybot900-router",
			action:    "die",
			wantOK:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := normalizeRuntimeEvent(ContainerEvent{Name: tt.eventName, Action: tt.action})
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if got.RuntimeID != tt.wantID || got.Component != tt.wantComp || got.Action != tt.wantAct {
				t.Fatalf("normalizeRuntimeEvent() = %#v, want id=%q component=%q action=%q", got, tt.wantID, tt.wantComp, tt.wantAct)
			}
		})
	}
}
