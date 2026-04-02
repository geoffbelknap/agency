package ws

import (
	"testing"

	"github.com/charmbracelet/log"
)

func TestSignalPromotionEndToEnd(t *testing.T) {
	h := NewHub(log.Default())

	var published []struct {
		agent      string
		signalType string
		severity   string
	}

	h.SetAgentSignalPublisher(func(agent, signalType string, data map[string]interface{}) {
		sev, _ := data["severity"].(string)
		published = append(published, struct {
			agent      string
			signalType string
			severity   string
		}{agent, signalType, sev})
	})

	// Simulate what comms relay does for a promotable signal
	promotableData := map[string]interface{}{
		"category": "budget_exhausted",
		"severity": "critical",
		"message":  "scout daily budget exhausted",
	}

	// The comms relay calls BroadcastAgentSignal then checks isPromotableSignal
	h.BroadcastAgentSignal("scout", "agent_signal_error", promotableData)
	signalType := "error" // extracted from "agent_signal_error"
	if isPromotableSignal(signalType) {
		h.PublishAgentSignal("scout", signalType, promotableData)
	}

	if len(published) != 1 {
		t.Fatalf("expected 1 published signal, got %d", len(published))
	}
	if published[0].agent != "scout" {
		t.Errorf("expected agent=scout, got %q", published[0].agent)
	}
	if published[0].severity != "critical" {
		t.Errorf("expected severity=critical, got %q", published[0].severity)
	}

	// Non-promotable signal should NOT be published
	if isPromotableSignal("processing") {
		t.Error("processing should not be promotable")
	}
	if isPromotableSignal("task_complete") {
		t.Error("task_complete should not be promotable")
	}
}
