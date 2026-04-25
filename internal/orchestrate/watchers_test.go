package orchestrate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/geoffbelknap/agency/internal/models"
	runtimecontract "github.com/geoffbelknap/agency/internal/runtime/contract"
)

type fakeHostStateSource struct {
	events chan HostStateEvent
	errs   chan error
}

func newFakeHostStateSource() *fakeHostStateSource {
	return &fakeHostStateSource{
		events: make(chan HostStateEvent, 4),
		errs:   make(chan error, 1),
	}
}

func (f *fakeHostStateSource) Watch(context.Context, ...string) (<-chan HostStateEvent, <-chan error, error) {
	return f.events, f.errs, nil
}

type fakeMissionRuntime struct {
	status      runtimecontract.RuntimeStatus
	getErr      error
	validateErr error
	gets        []string
	validates   []string
}

func (f *fakeMissionRuntime) Get(_ context.Context, runtimeID string) (runtimecontract.RuntimeStatus, error) {
	f.gets = append(f.gets, runtimeID)
	if f.getErr != nil {
		return runtimecontract.RuntimeStatus{}, f.getErr
	}
	return f.status, nil
}

func (f *fakeMissionRuntime) Validate(_ context.Context, runtimeID string) error {
	f.validates = append(f.validates, runtimeID)
	return f.validateErr
}

func TestWorkspaceWatcherWithNilClientStartsDisabled(t *testing.T) {
	watcher, err := NewWorkspaceWatcherWithClient(nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewWorkspaceWatcherWithClient() error = %v", err)
	}
	watcher.Start(context.Background())
}

func TestEnforcerWatcherWithNilClientStartsDisabled(t *testing.T) {
	watcher, err := NewEnforcerWatcherWithClient(nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewEnforcerWatcherWithClient() error = %v", err)
	}
	watcher.Start(context.Background())
}

func TestEnforcerWatcherConsumesNormalizedHostStateEvents(t *testing.T) {
	source := newFakeHostStateSource()
	alerts := make(chan string, 1)
	watcher, err := NewEnforcerWatcherWithSource(func(agentName, reason string) {
		alerts <- agentName + ":" + reason
	}, nil, nil, source)
	if err != nil {
		t.Fatalf("NewEnforcerWatcherWithSource() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watcher.Start(ctx)

	source.events <- HostStateEvent{
		AgentName: "alice",
		Component: HostStateComponentEnforcer,
		Action:    HostStateActionStopped,
		ExitCode:  "137",
	}

	select {
	case got := <-alerts:
		if got != "alice:enforcer exited (code 137) — agent has no API mediation" {
			t.Fatalf("alert = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for enforcer alert")
	}
}

func TestWorkspaceWatcherConsumesNormalizedHostStateEvents(t *testing.T) {
	source := newFakeHostStateSource()
	alerts := make(chan string, 1)
	watcher, err := NewWorkspaceWatcherWithSource(func(agentName, reason string) {
		alerts <- agentName + ":" + reason
	}, nil, nil, source)
	if err != nil {
		t.Fatalf("NewWorkspaceWatcherWithSource() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watcher.Start(ctx)

	source.events <- HostStateEvent{
		AgentName: "bob",
		Component: HostStateComponentWorkspace,
		Action:    HostStateActionStarted,
	}

	select {
	case got := <-alerts:
		if got != "bob:workspace auto-restarted by runtime backend — body runtime recovered" {
			t.Fatalf("alert = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for workspace alert")
	}
}

func TestMissionHealthMonitorWithNilClientStartsDisabled(t *testing.T) {
	monitor, err := NewMissionHealthMonitorWithClient(NewMissionManager(t.TempDir()), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewMissionHealthMonitorWithClient() error = %v", err)
	}
	monitor.Start(context.Background())
}

func TestMissionHealthMonitorUsesRuntimeStatusBeforeBackendListing(t *testing.T) {
	runtime := &fakeMissionRuntime{
		status: runtimecontract.RuntimeStatus{
			RuntimeID: "alice",
			Phase:     runtimecontract.RuntimePhaseRunning,
			Healthy:   true,
			Transport: runtimecontract.RuntimeTransportStatus{EnforcerConnected: true},
		},
	}
	var paused []string
	monitor, err := NewMissionHealthMonitorWithRuntime(
		NewMissionManager(t.TempDir()),
		runtime,
		nil,
		func(name, reason string) error {
			paused = append(paused, name+":"+reason)
			return nil
		},
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("NewMissionHealthMonitorWithRuntime() error = %v", err)
	}

	monitor.checkMission(context.Background(), &models.Mission{
		Name:         "runtime-mission",
		Status:       "active",
		AssignedTo:   "alice",
		AssignedType: "agent",
	}, map[string]string{})

	if len(paused) != 0 {
		t.Fatalf("healthy runtime should not pause mission: %#v", paused)
	}
	if len(runtime.gets) != 1 || runtime.gets[0] != "alice" {
		t.Fatalf("runtime Get calls = %#v", runtime.gets)
	}
	if len(runtime.validates) != 1 || runtime.validates[0] != "alice" {
		t.Fatalf("runtime Validate calls = %#v", runtime.validates)
	}
}

func TestMissionHealthMonitorPausesOnRuntimeValidationFailure(t *testing.T) {
	runtime := &fakeMissionRuntime{
		status: runtimecontract.RuntimeStatus{
			RuntimeID: "alice",
			Phase:     runtimecontract.RuntimePhaseRunning,
			Healthy:   true,
			Transport: runtimecontract.RuntimeTransportStatus{EnforcerConnected: true},
		},
		validateErr: errors.New("mediation missing"),
	}
	var paused []string
	monitor, err := NewMissionHealthMonitorWithRuntime(
		NewMissionManager(t.TempDir()),
		runtime,
		nil,
		func(name, reason string) error {
			paused = append(paused, name+":"+reason)
			return nil
		},
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("NewMissionHealthMonitorWithRuntime() error = %v", err)
	}

	monitor.checkMission(context.Background(), &models.Mission{
		Name:         "runtime-mission",
		Status:       "active",
		AssignedTo:   "alice",
		AssignedType: "agent",
	}, map[string]string{})

	if len(paused) != 1 {
		t.Fatalf("runtime validation failure should pause once, got %#v", paused)
	}
}
