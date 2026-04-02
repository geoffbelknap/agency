package main

import (
	"testing"
	"time"
)

func TestRateLimiterGrantsWithinLimit(t *testing.T) {
	rl := NewRateLimiter(10, 60)
	granted, wait := rl.Acquire("anthropic")
	if !granted || wait > 0 {
		t.Fatalf("expected granted with no wait, got granted=%v wait=%v", granted, wait)
	}
}

func TestRateLimiterDeniesAtLimit(t *testing.T) {
	rl := NewRateLimiter(2, 60)
	rl.Acquire("anthropic")
	rl.RecordRequest("anthropic")
	rl.Acquire("anthropic")
	rl.RecordRequest("anthropic")

	granted, wait := rl.Acquire("anthropic")
	if granted {
		t.Fatal("expected denied at limit")
	}
	if wait <= 0 {
		t.Fatal("expected positive wait time")
	}
}

func TestRateLimiterUpdateFromHeaders(t *testing.T) {
	rl := NewRateLimiter(10, 60)
	rl.Update("anthropic", 100, 50, 30.0)

	state := rl.GetState("anthropic")
	if state.Limit != 100 {
		t.Fatalf("expected limit 100, got %d", state.Limit)
	}
	if state.Remaining != 50 {
		t.Fatalf("expected remaining 50, got %d", state.Remaining)
	}
	if !state.Discovered {
		t.Fatal("expected discovered=true after update")
	}
}

func TestRateLimiterReport429(t *testing.T) {
	rl := NewRateLimiter(100, 60)
	rl.Update("anthropic", 100, 50, 30.0)
	rl.Report429("anthropic")

	state := rl.GetState("anthropic")
	if state.Limit != 50 {
		t.Fatalf("expected limit halved to 50, got %d", state.Limit)
	}
	if state.Remaining != 0 {
		t.Fatalf("expected remaining 0 after 429, got %d", state.Remaining)
	}
}

func TestRateLimiterIndependentProviders(t *testing.T) {
	rl := NewRateLimiter(1, 60)
	rl.Acquire("anthropic")
	rl.RecordRequest("anthropic")

	granted, _ := rl.Acquire("openai")
	if !granted {
		t.Fatal("expected openai granted (independent from anthropic)")
	}
}

func TestRateLimiterWindowExpiry(t *testing.T) {
	rl := NewRateLimiter(1, 1) // 1 rpm, 1s window
	rl.Acquire("anthropic")
	rl.RecordRequest("anthropic")

	granted, _ := rl.Acquire("anthropic")
	if granted {
		t.Fatal("expected denied at limit")
	}

	time.Sleep(1100 * time.Millisecond)

	granted, _ = rl.Acquire("anthropic")
	if !granted {
		t.Fatal("expected granted after window expiry")
	}
}
