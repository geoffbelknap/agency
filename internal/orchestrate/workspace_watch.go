package orchestrate

import (
	"context"
	"fmt"
	"strings"

	"log/slog"
	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
)

// WorkspaceAlertFunc is called when a workspace container exits or restarts.
type WorkspaceAlertFunc func(agentName, reason string)

// WorkspaceWatcher monitors Docker events for workspace container crashes
// and auto-restarts. Comms and knowledge now route through the enforcer
// mediation proxy, so no infra reconnect is needed on restart.
type WorkspaceWatcher struct {
	docker   *runtimehost.DockerHandle
	alert    WorkspaceAlertFunc
	logger   *slog.Logger
	cancel   context.CancelFunc
	suppress *StopSuppression
}

// NewWorkspaceWatcher creates a watcher that monitors workspace container
// lifecycle events. alertFn is called on crash/restart.
func NewWorkspaceWatcher(alertFn WorkspaceAlertFunc, logger *slog.Logger, suppress *StopSuppression) (*WorkspaceWatcher, error) {
	return NewWorkspaceWatcherWithClient(alertFn, logger, suppress, nil)
}

// NewWorkspaceWatcherWithClient creates a watcher using the provided Docker client.
func NewWorkspaceWatcherWithClient(alertFn WorkspaceAlertFunc, logger *slog.Logger, suppress *StopSuppression, dc *runtimehost.DockerHandle) (*WorkspaceWatcher, error) {
	if logger == nil {
		logger = slog.Default()
	}
	return &WorkspaceWatcher{
		docker:   dc,
		alert:    alertFn,
		logger:   logger,
		suppress: suppress,
	}, nil
}

// Start launches the background Docker event listener.
func (w *WorkspaceWatcher) Start(ctx context.Context) {
	if w.docker == nil {
		w.logger.Info("workspace watcher disabled: docker client unavailable")
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
	eventCh, errCh, err := runtimehost.WatchAgencyContainerEvents(ctx, w.docker, "die", "start")
	if err != nil {
		w.logger.Warn("workspace watcher disabled", "error", err)
		return
	}

	for {
		select {
		case <-ctx.Done():
			return

		case ev := <-eventCh:
			name := ev.Name
			if name == "" || !strings.HasSuffix(name, "-workspace") {
				continue
			}
			agentName := extractAgentName(name, "-workspace")
			if agentName == "" {
				continue
			}

			if w.suppress != nil && w.suppress.IsSuppressed(agentName) {
				w.logger.Info("workspace event suppressed (intentional stop/restart)",
					"agent", agentName,
					"action", ev.Action,
				)
				continue
			}

			switch ev.Action {
			case "die":
				exitCode := ev.ExitCode
				reason := fmt.Sprintf("workspace exited (code %s) — body runtime crashed", exitCode)
				w.logger.Warn("workspace container died",
					"agent", agentName,
					"exit_code", exitCode,
				)
				w.alert(agentName, reason)

			case "start":
				w.logger.Info("workspace container auto-restarted",
					"agent", agentName,
				)
				reason := "workspace auto-restarted by Docker — body runtime recovered"
				w.alert(agentName, reason)
			}

		case err := <-errCh:
			if ctx.Err() != nil {
				return
			}
			w.logger.Warn("workspace watcher: event stream error, restarting", "error", err)
			eventCh, errCh, err = runtimehost.WatchAgencyContainerEvents(ctx, w.docker, "die", "start")
			if err != nil {
				w.logger.Warn("workspace watcher reconnect failed", "error", err)
				return
			}
		}
	}
}
