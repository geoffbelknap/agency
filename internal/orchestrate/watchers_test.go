package orchestrate

import (
	"context"
	"testing"
)

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

func TestMissionHealthMonitorWithNilClientStartsDisabled(t *testing.T) {
	monitor, err := NewMissionHealthMonitorWithClient(NewMissionManager(t.TempDir()), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewMissionHealthMonitorWithClient() error = %v", err)
	}
	monitor.Start(context.Background())
}
