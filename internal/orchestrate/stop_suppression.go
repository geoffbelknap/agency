package orchestrate

import (
	"sync"
	"time"
)

// StopSuppression tracks agents undergoing intentional stop/restart so that
// container watchers can suppress spurious alerts. Entries auto-expire after
// a timeout to avoid permanent suppression if cleanup is missed.
type StopSuppression struct {
	mu      sync.Mutex
	agents  map[string]time.Time
	timeout time.Duration
}

// NewStopSuppression creates a suppression tracker. Entries expire after the
// given timeout (e.g., 30s covers the stop + restart window).
func NewStopSuppression(timeout time.Duration) *StopSuppression {
	return &StopSuppression{
		agents:  make(map[string]time.Time),
		timeout: timeout,
	}
}

// Suppress marks an agent as undergoing intentional shutdown.
func (s *StopSuppression) Suppress(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agents[name] = time.Now()
}

// Release removes the suppression for an agent.
func (s *StopSuppression) Release(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.agents, name)
}

// IsSuppressed returns true if the agent is in intentional shutdown and the
// entry has not expired.
func (s *StopSuppression) IsSuppressed(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.agents[name]
	if !ok {
		return false
	}
	if time.Since(t) > s.timeout {
		delete(s.agents, name)
		return false
	}
	return true
}
