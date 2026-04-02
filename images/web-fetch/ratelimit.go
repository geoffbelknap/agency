package main

import (
	"sync"
	"time"
)

type RateLimiter struct {
	mu            sync.Mutex
	globalLimit   int
	domainLimit   int
	window        time.Duration
	globalCount   int
	domainCounts  map[string]int
	windowStarted time.Time
}

func NewRateLimiter(globalRPM, domainRPM int) *RateLimiter {
	return &RateLimiter{
		globalLimit:   globalRPM,
		domainLimit:   domainRPM,
		window:        time.Minute,
		domainCounts:  make(map[string]int),
		windowStarted: time.Now(),
	}
}

func (rl *RateLimiter) Allow(domain string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	if now.Sub(rl.windowStarted) >= rl.window {
		rl.globalCount = 0
		rl.domainCounts = make(map[string]int)
		rl.windowStarted = now
	}
	if rl.globalLimit > 0 && rl.globalCount >= rl.globalLimit {
		return false
	}
	if rl.domainLimit > 0 && rl.domainCounts[domain] >= rl.domainLimit {
		return false
	}
	rl.globalCount++
	rl.domainCounts[domain]++
	return true
}
