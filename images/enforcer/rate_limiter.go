package main

import (
	"sync"
	"time"
)

type ProviderState struct {
	Limit      int
	Remaining  int
	ResetAt    time.Time
	Window     []time.Time
	Discovered bool
	LastUsed   time.Time
}

type RateLimiter struct {
	mu            sync.Mutex
	providers     map[string]*ProviderState
	defaultRPM    int
	windowSeconds int
}

func NewRateLimiter(defaultRPM, windowSeconds int) *RateLimiter {
	return &RateLimiter{
		providers:     make(map[string]*ProviderState),
		defaultRPM:    defaultRPM,
		windowSeconds: windowSeconds,
	}
}

func (rl *RateLimiter) getOrCreate(provider string) *ProviderState {
	s, ok := rl.providers[provider]
	if !ok {
		s = &ProviderState{
			Limit:     rl.defaultRPM,
			Remaining: rl.defaultRPM,
		}
		rl.providers[provider] = s
	}
	return s
}

func (rl *RateLimiter) pruneWindow(s *ProviderState, now time.Time) {
	cutoff := now.Add(-time.Duration(rl.windowSeconds) * time.Second)
	i := 0
	for i < len(s.Window) && s.Window[i].Before(cutoff) {
		i++
	}
	if i > 0 {
		s.Window = s.Window[i:]
	}
}

func (rl *RateLimiter) Acquire(provider string) (bool, float64) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	// Evict stale providers (unused for >1 hour) to prevent map growth
	if len(rl.providers) > 10 {
		for name, ps := range rl.providers {
			if !ps.LastUsed.IsZero() && now.Sub(ps.LastUsed) > time.Hour {
				delete(rl.providers, name)
			}
		}
	}

	s := rl.getOrCreate(provider)
	s.LastUsed = now
	rl.pruneWindow(s, now)

	if !s.ResetAt.IsZero() && now.After(s.ResetAt) {
		s.Remaining = s.Limit
		s.ResetAt = time.Time{}
	}

	if s.Discovered && s.Remaining <= 0 {
		wait := 1.0
		if !s.ResetAt.IsZero() {
			wait = s.ResetAt.Sub(now).Seconds()
			if wait < 0.1 {
				wait = 0.1
			}
		}
		return false, wait
	}

	if len(s.Window) >= s.Limit {
		oldest := s.Window[0]
		wait := time.Duration(rl.windowSeconds)*time.Second - now.Sub(oldest)
		if wait < 100*time.Millisecond {
			wait = 100 * time.Millisecond
		}
		return false, wait.Seconds()
	}

	return true, 0
}

func (rl *RateLimiter) RecordRequest(provider string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	s := rl.getOrCreate(provider)
	s.LastUsed = now
	s.Window = append(s.Window, now)
	if s.Remaining > 0 {
		s.Remaining--
	}
}

func (rl *RateLimiter) Update(provider string, limitReqs, remainingReqs int, resetSecs float64) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	s := rl.getOrCreate(provider)
	s.Limit = limitReqs
	s.Remaining = remainingReqs
	s.Discovered = true
	if resetSecs > 0 {
		s.ResetAt = time.Now().Add(time.Duration(resetSecs * float64(time.Second)))
	}
}

func (rl *RateLimiter) Report429(provider string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	s := rl.getOrCreate(provider)
	s.Limit = s.Limit / 2
	if s.Limit < 1 {
		s.Limit = 1
	}
	s.Remaining = 0
}

func (rl *RateLimiter) GetState(provider string) ProviderState {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	s := rl.getOrCreate(provider)
	rl.pruneWindow(s, time.Now())
	return ProviderState{
		Limit:      s.Limit,
		Remaining:  s.Remaining,
		ResetAt:    s.ResetAt,
		Discovered: s.Discovered,
	}
}
