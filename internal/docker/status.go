package docker

import (
	"strings"
	"sync/atomic"
)

// Status tracks Docker availability reactively. No polling — availability
// is determined by observing successes and failures on Docker API calls.
type Status struct {
	available   atomic.Bool
	OnReconnect func() // called once when Docker transitions from unavailable to available
}

// NewStatus creates a Docker status tracker. If dc is nil, starts unavailable.
func NewStatus(dc *Client) *Status {
	s := &Status{}
	s.available.Store(dc != nil)
	return s
}

// Available returns whether Docker is currently considered reachable.
func (s *Status) Available() bool {
	return s.available.Load()
}

// RecordSuccess marks Docker as available. If transitioning from unavailable,
// fires the OnReconnect callback (if set).
func (s *Status) RecordSuccess() {
	was := s.available.Swap(true)
	if !was && s.OnReconnect != nil {
		s.OnReconnect()
	}
}

// RecordError checks if the error indicates Docker itself is unavailable
// (connection refused, not responding, etc.) vs a normal operational error
// (container not found, image pull failed). Only Docker-level failures
// flip the availability flag.
func (s *Status) RecordError(err error) {
	if err == nil {
		return
	}
	if isDockerUnavailable(err) {
		s.available.Store(false)
	}
}

// isDockerUnavailable returns true if the error indicates the Docker daemon
// itself is unreachable, as opposed to a normal API error.
func isDockerUnavailable(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	patterns := []string{
		"Cannot connect to the Docker daemon",
		"connection refused",
		"no such host",
		"i/o timeout",
		"context deadline exceeded",
		"Docker not responding",
		"dial unix",
	}
	for _, p := range patterns {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}
