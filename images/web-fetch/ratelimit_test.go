package main

import (
	"testing"
	"time"
)

func TestRateLimiter_PerDomain(t *testing.T) {
	rl := NewRateLimiter(100, 2)
	if !rl.Allow("example.com") {
		t.Error("first request should be allowed")
	}
	if !rl.Allow("example.com") {
		t.Error("second request should be allowed")
	}
	if rl.Allow("example.com") {
		t.Error("third request should be rate limited")
	}
	if !rl.Allow("other.com") {
		t.Error("different domain should be allowed")
	}
}

func TestRateLimiter_Global(t *testing.T) {
	rl := NewRateLimiter(2, 100)
	rl.Allow("a.com")
	rl.Allow("b.com")
	if rl.Allow("c.com") {
		t.Error("global limit should be hit")
	}
}

func TestRateLimiter_WindowReset(t *testing.T) {
	rl := NewRateLimiter(100, 1)
	rl.window = 50 * time.Millisecond
	rl.Allow("example.com")
	if rl.Allow("example.com") {
		t.Error("should be limited within window")
	}
	time.Sleep(60 * time.Millisecond)
	if !rl.Allow("example.com") {
		t.Error("should be allowed after window reset")
	}
}
