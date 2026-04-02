package events

import (
	"sync"
	"time"

	"github.com/geoffbelknap/agency/internal/models"
	"github.com/robfig/cron/v3"
)

// Scheduler fires cron-based schedule events into the event bus.
type Scheduler struct {
	cron       *cron.Cron
	bus        *Bus
	mu         sync.Mutex
	entries    map[string]cron.EntryID // schedule name -> cron entry ID
	exprs      map[string]string       // schedule name -> cron expression
	deactivated map[string]string      // schedule name -> cron expression (paused)
}

// NewScheduler creates a cron scheduler that publishes events to the bus.
func NewScheduler(bus *Bus) *Scheduler {
	return &Scheduler{
		cron:        cron.New(),
		bus:         bus,
		entries:     make(map[string]cron.EntryID),
		exprs:       make(map[string]string),
		deactivated: make(map[string]string),
	}
}

// Register adds a named cron schedule that fires events into the bus.
// The timezone parameter is currently unused (UTC is always used).
func (s *Scheduler) Register(name, cronExpr string, timezone string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Remove existing schedule with same name
	if id, ok := s.entries[name]; ok {
		s.cron.Remove(id)
		delete(s.entries, name)
		delete(s.exprs, name)
	}
	delete(s.deactivated, name)

	id, err := s.cron.AddFunc(cronExpr, func() {
		event := models.NewEvent(models.EventSourceSchedule, name, "cron_fired", map[string]interface{}{
			"schedule":  cronExpr,
			"fire_time": time.Now().UTC().Format(time.RFC3339),
		})
		s.bus.Publish(event)
	})
	if err != nil {
		return err
	}

	s.entries[name] = id
	s.exprs[name] = cronExpr
	return nil
}

// Deactivate stops a named schedule without removing it.
func (s *Scheduler) Deactivate(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if id, ok := s.entries[name]; ok {
		s.cron.Remove(id)
		s.deactivated[name] = s.exprs[name]
		delete(s.entries, name)
		delete(s.exprs, name)
	}
}

// Activate re-enables a previously deactivated schedule.
func (s *Scheduler) Activate(name string) {
	s.mu.Lock()
	expr, ok := s.deactivated[name]
	s.mu.Unlock()

	if ok {
		// Register will acquire the lock
		s.Register(name, expr, "") //nolint:errcheck
	}
}

// Remove stops and removes a named schedule.
func (s *Scheduler) Remove(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if id, ok := s.entries[name]; ok {
		s.cron.Remove(id)
		delete(s.entries, name)
		delete(s.exprs, name)
	}
	delete(s.deactivated, name)
}

// Start begins the cron scheduler.
func (s *Scheduler) Start() {
	s.cron.Start()
}

// Stop halts the cron scheduler.
func (s *Scheduler) Stop() {
	s.cron.Stop()
}
