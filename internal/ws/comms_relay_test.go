package ws

import "testing"

func TestIsPromotableSignal(t *testing.T) {
	promotable := []string{
		"error", "escalation", "self_halt",
	}
	for _, s := range promotable {
		if !isPromotableSignal(s) {
			t.Errorf("expected %q to be promotable", s)
		}
	}

	notPromotable := []string{
		"processing", "task_accepted", "progress_update",
		"task_complete", "finding", "ready",
	}
	for _, s := range notPromotable {
		if isPromotableSignal(s) {
			t.Errorf("expected %q to NOT be promotable", s)
		}
	}
}
