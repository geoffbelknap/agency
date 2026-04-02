package events

import (
	"sync"

	"github.com/geoffbelknap/agency/internal/models"
)

// DefaultRingSize is the default number of events retained in the ring buffer.
const DefaultRingSize = 1000

// RingBuffer is a thread-safe circular buffer holding the most recent events
// for observability. Older events are overwritten when capacity is exceeded.
type RingBuffer struct {
	mu    sync.RWMutex
	buf   []*models.Event
	pos   int
	count int
	size  int
}

// NewRingBuffer creates a ring buffer with the given capacity.
func NewRingBuffer(size int) *RingBuffer {
	if size <= 0 {
		size = DefaultRingSize
	}
	return &RingBuffer{
		buf:  make([]*models.Event, size),
		size: size,
	}
}

// Add inserts an event into the ring buffer. If the buffer is full,
// the oldest event is overwritten.
func (rb *RingBuffer) Add(e *models.Event) {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.buf[rb.pos] = e
	rb.pos = (rb.pos + 1) % rb.size
	if rb.count < rb.size {
		rb.count++
	}
}

// Get retrieves an event by ID, or nil if not found.
func (rb *RingBuffer) Get(id string) *models.Event {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	for i := 0; i < rb.count; i++ {
		idx := (rb.pos - 1 - i + rb.size) % rb.size
		if rb.buf[idx] != nil && rb.buf[idx].ID == id {
			return rb.buf[idx]
		}
	}
	return nil
}

// List returns up to limit events, newest first. If limit <= 0, all events
// in the buffer are returned.
func (rb *RingBuffer) List(limit int) []*models.Event {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	n := rb.count
	if limit > 0 && limit < n {
		n = limit
	}
	result := make([]*models.Event, 0, n)
	for i := 0; i < n; i++ {
		idx := (rb.pos - 1 - i + rb.size) % rb.size
		result = append(result, rb.buf[idx])
	}
	return result
}

// ListFiltered returns events matching the given filters, newest first.
// Empty filter strings are treated as wildcards (match any).
func (rb *RingBuffer) ListFiltered(sourceType, sourceName, eventType string, limit int) []*models.Event {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	var result []*models.Event
	for i := 0; i < rb.count; i++ {
		idx := (rb.pos - 1 - i + rb.size) % rb.size
		e := rb.buf[idx]
		if sourceType != "" && e.SourceType != sourceType {
			continue
		}
		if sourceName != "" && e.SourceName != sourceName {
			continue
		}
		if eventType != "" && e.EventType != eventType {
			continue
		}
		result = append(result, e)
		if limit > 0 && len(result) >= limit {
			break
		}
	}
	return result
}
