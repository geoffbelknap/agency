package orchestrate

import (
	"context"
	"fmt"

	"log/slog"

	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
)

// WorkspaceAlertFunc is called when a workspace runtime exits or restarts.
type WorkspaceAlertFunc func(agentName, reason string)

// WorkspaceWatcher monitors host-backend events for workspace runtime crashes
// and auto-restarts. Comms and knowledge now route through the enforcer
// mediation proxy, so no infra reconnect is needed on restart.
type WorkspaceWatcher struct {
	source   HostStateSource
	alert    WorkspaceAlertFunc
	logger   *slog.Logger
	cancel   context.CancelFunc
	suppress *StopSuppression
}

// NewWorkspaceWatcher creates a watcher that monitors workspace runtime
// lifecycle events. alertFn is called on crash/restart.
func NewWorkspaceWatcher(alertFn WorkspaceAlertFunc, logger *slog.Logger, suppress *StopSuppression) (*WorkspaceWatcher, error) {
	return NewWorkspaceWatcherWithClient(alertFn, logger, suppress, nil)
}

// NewWorkspaceWatcherWithClient creates a watcher using the provided backend client.
func NewWorkspaceWatcherWithClient(alertFn WorkspaceAlertFunc, logger *slog.Logger, suppress *StopSuppression, dc *runtimehost.BackendHandle) (*WorkspaceWatcher, error) {
	return NewWorkspaceWatcherWithSource(alertFn, logger, suppress, NewBackendHostStateSource(dc))
}

func NewWorkspaceWatcherWithSource(alertFn WorkspaceAlertFunc, logger *slog.Logger, suppress *StopSuppression, source HostStateSource) (*WorkspaceWatcher, error) {
	if logger == nil {
		logger = slog.Default()
	}
	return &WorkspaceWatcher{
		source:   source,
		alert:    alertFn,
		logger:   logger,
		suppress: suppress,
	}, nil
}

// Start launches the background host-backend event listener.
func (w *WorkspaceWatcher) Start(ctx context.Context) {
	if w.source == nil {
		w.logger.Info("workspace watcher disabled: runtime backend client unavailable")
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	w.cancel = cancel
	go w.watch(ctx)
}

// Stop cancels the background goroutine.
func (w *WorkspaceWatcher) Stop() {
	if w.cancel != nil {
		w.cancel()
	}
}

func (w *WorkspaceWatcher) watch(ctx context.Context) {
	eventCh, errCh, err := w.source.Watch(ctx, HostStateActionStopped, HostStateActionStarted)
	if err != nil {
		w.logger.Warn("workspace watcher disabled", "error", err)
		return
	}

	for {
		select {
		case <-ctx.Done():
			return

		case ev, ok := <-eventCh:
			if !ok {
				return
			}
			if ev.Component != HostStateComponentWorkspace || ev.AgentName == "" {
				continue
			}

			if w.suppress != nil && w.suppress.IsSuppressed(ev.AgentName) {
				w.logger.Info("workspace event suppressed (intentional stop/restart)",
					"agent", ev.AgentName,
					"action", ev.Action,
				)
				continue
			}

			switch ev.Action {
			case HostStateActionStopped:
				exitCode := ev.ExitCode
				reason := fmt.Sprintf("workspace exited (code %s) — body runtime crashed", exitCode)
				w.logger.Warn("workspace container died",
					"agent", ev.AgentName,
					"exit_code", exitCode,
				)
				w.alert(ev.AgentName, reason)

			case HostStateActionStarted:
				w.logger.Info("workspace container auto-restarted",
					"agent", ev.AgentName,
				)
				reason := "workspace auto-restarted by runtime backend — body runtime recovered"
				w.alert(ev.AgentName, reason)
			}

		case err, ok := <-errCh:
			if !ok {
				return
			}
			if ctx.Err() != nil {
				return
			}
			w.logger.Warn("workspace watcher: event stream error, restarting", "error", err)
			eventCh, errCh, err = w.source.Watch(ctx, HostStateActionStopped, HostStateActionStarted)
			if err != nil {
				w.logger.Warn("workspace watcher reconnect failed", "error", err)
				return
			}
		}
	}
}
