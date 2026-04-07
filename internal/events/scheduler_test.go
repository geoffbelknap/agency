package events

import (
	"sync"
	"testing"
	"time"

	"log/slog"
	"github.com/geoffbelknap/agency/internal/models"
)

func TestSchedulerRegister(t *testing.T) {
	bus := NewBus(slog.Default(), nil)
	sched := NewScheduler(bus)
	defer sched.Stop()

	if err := sched.Register("test-sched", "* * * * *", ""); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	sched.mu.Lock()
	if _, ok := sched.entries["test-sched"]; !ok {
		t.Error("expected entry in entries map")
	}
	sched.mu.Unlock()
}

func TestSchedulerInvalidCron(t *testing.T) {
	bus := NewBus(slog.Default(), nil)
	sched := NewScheduler(bus)

	if err := sched.Register("bad", "not a cron", ""); err == nil {
		t.Error("expected error for invalid cron expression")
	}
}

func TestSchedulerDeactivateActivate(t *testing.T) {
	bus := NewBus(slog.Default(), nil)
	sched := NewScheduler(bus)
	defer sched.Stop()

	sched.Register("sched-a", "* * * * *", "") //nolint:errcheck

	sched.Deactivate("sched-a")
	sched.mu.Lock()
	if _, ok := sched.entries["sched-a"]; ok {
		t.Error("expected entry removed from entries after deactivate")
	}
	if _, ok := sched.deactivated["sched-a"]; !ok {
		t.Error("expected entry in deactivated map")
	}
	sched.mu.Unlock()

	sched.Activate("sched-a")
	sched.mu.Lock()
	if _, ok := sched.entries["sched-a"]; !ok {
		t.Error("expected entry back in entries after activate")
	}
	if _, ok := sched.deactivated["sched-a"]; ok {
		t.Error("expected entry removed from deactivated after activate")
	}
	sched.mu.Unlock()
}

func TestSchedulerRemove(t *testing.T) {
	bus := NewBus(slog.Default(), nil)
	sched := NewScheduler(bus)
	defer sched.Stop()

	sched.Register("sched-r", "* * * * *", "") //nolint:errcheck
	sched.Remove("sched-r")

	sched.mu.Lock()
	if _, ok := sched.entries["sched-r"]; ok {
		t.Error("expected entry removed after Remove()")
	}
	sched.mu.Unlock()
}

func TestSchedulerFiresEvent(t *testing.T) {
	var mu sync.Mutex
	var received []*models.Event

	bus := NewBus(slog.Default(), nil)
	bus.RegisterDelivery(DestAgent, func(sub *Subscription, event *models.Event) error {
		mu.Lock()
		received = append(received, event)
		mu.Unlock()
		return nil
	})

	sched := NewScheduler(bus)
	// Use @every 1s for fast testing
	if err := sched.Register("fast-sched", "@every 1s", ""); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	sched.Start()
	defer sched.Stop()

	// Wait for at least one fire
	time.Sleep(2 * time.Second)

	// Check ring buffer
	events := bus.Events().List(10)
	found := false
	for _, e := range events {
		if e.SourceType == models.EventSourceSchedule && e.SourceName == "fast-sched" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected schedule event in ring buffer")
	}
}
