package ws

import (
	"encoding/json"
	"testing"
	"time"

	"log/slog"
)

func TestHub_BroadcastAgentSignal(t *testing.T) {
	h := NewHub(slog.Default())

	ch := make(chan []byte, 10)
	client := &Client{
		hub:          h,
		send:         ch,
		subscription: &Subscription{Agents: []string{"mybot"}},
		log:          slog.Default(),
	}
	h.register <- client
	time.Sleep(50 * time.Millisecond) // let registration process

	h.BroadcastAgentSignal("mybot", "agent_signal_error", map[string]interface{}{
		"category": "llm.call_failed",
		"stage":    "provider_auth",
	})

	select {
	case msg := <-ch:
		var event map[string]interface{}
		if err := json.Unmarshal(msg, &event); err != nil {
			t.Fatalf("failed to unmarshal event: %v", err)
		}
		if event["type"] != "agent_signal_error" {
			t.Errorf("type = %v, want agent_signal_error", event["type"])
		}
		if event["agent"] != "mybot" {
			t.Errorf("agent = %v, want mybot", event["agent"])
		}
		data, ok := event["data"].(map[string]interface{})
		if !ok {
			t.Fatal("data field missing or wrong type")
		}
		if data["category"] != "llm.call_failed" {
			t.Errorf("data.category = %v, want llm.call_failed", data["category"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for broadcast")
	}
}

func TestHub_SetAgentSignalPublisher(t *testing.T) {
	h := NewHub(slog.Default())

	var captured struct {
		agent      string
		signalType string
		data       map[string]interface{}
	}

	h.SetAgentSignalPublisher(func(agent, signalType string, data map[string]interface{}) {
		captured.agent = agent
		captured.signalType = signalType
		captured.data = data
	})

	testData := map[string]interface{}{"category": "budget", "severity": "critical"}
	h.PublishAgentSignal("scout", "error", testData)

	if captured.agent != "scout" {
		t.Errorf("expected agent=scout, got %q", captured.agent)
	}
	if captured.signalType != "error" {
		t.Errorf("expected signalType=error, got %q", captured.signalType)
	}
	if captured.data["severity"] != "critical" {
		t.Errorf("expected severity=critical, got %v", captured.data["severity"])
	}
}

func TestSubscription_MatchesAgentSignal_OnlyForSubscribedAgent(t *testing.T) {
	sub := &Subscription{Agents: []string{"mybot"}}

	// Should match agent_signal_error for subscribed agent
	match := sub.matches(&Event{Type: "agent_signal_error", Agent: "mybot"})
	if !match {
		t.Error("expected agent_signal_error for mybot to match")
	}

	// Should NOT match agent_signal_error for other agent
	noMatch := sub.matches(&Event{Type: "agent_signal_error", Agent: "otherbot"})
	if noMatch {
		t.Error("expected agent_signal_error for otherbot to NOT match")
	}

	// Should match agent_signal_progress_update too
	match2 := sub.matches(&Event{Type: "agent_signal_progress_update", Agent: "mybot"})
	if !match2 {
		t.Error("expected agent_signal_progress_update for mybot to match")
	}

	// Empty agent subscription should match all
	subAll := &Subscription{}
	matchAll := subAll.matches(&Event{Type: "agent_signal_error", Agent: "anybot"})
	if !matchAll {
		t.Error("expected empty subscription to match all agent signals")
	}
}
