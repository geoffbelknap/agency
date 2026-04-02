package orchestrate

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

// WorkspaceAlertFunc is called when a workspace container exits or restarts.
type WorkspaceAlertFunc func(agentName, reason string)

// WorkspaceWatcher monitors Docker events for workspace container crashes
// and auto-restarts. Comms and knowledge now route through the enforcer
// mediation proxy, so no infra reconnect is needed on restart.
type WorkspaceWatcher struct {
	cli      *client.Client
	alert    WorkspaceAlertFunc
	logger   *log.Logger
	cancel   context.CancelFunc
	suppress *StopSuppression
}

// NewWorkspaceWatcher creates a watcher that monitors workspace container
// lifecycle events. alertFn is called on crash/restart.
func NewWorkspaceWatcher(alertFn WorkspaceAlertFunc, logger *log.Logger, suppress *StopSuppression) (*WorkspaceWatcher, error) {
	return NewWorkspaceWatcherWithClient(alertFn, logger, suppress, nil)
}

// NewWorkspaceWatcherWithClient creates a watcher using the provided Docker client
// (or a new one if cli is nil). Prefer passing a shared client.
func NewWorkspaceWatcherWithClient(alertFn WorkspaceAlertFunc, logger *log.Logger, suppress *StopSuppression, cli *client.Client) (*WorkspaceWatcher, error) {
	if cli == nil {
		var err error
		cli, err = client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
		if err != nil {
			return nil, fmt.Errorf("workspace watcher: docker client: %w", err)
		}
	}
	return &WorkspaceWatcher{
		cli:      cli,
		alert:    alertFn,
		logger:   logger,
		suppress: suppress,
	}, nil
}

// Start launches the background Docker event listener.
func (w *WorkspaceWatcher) Start(ctx context.Context) {
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

func (w *WorkspaceWatcher) eventFilters() filters.Args {
	return filters.NewArgs(
		filters.Arg("type", "container"),
		filters.Arg("event", "die"),
		filters.Arg("event", "start"),
		filters.Arg("name", prefix+"-"),
	)
}

func (w *WorkspaceWatcher) watch(ctx context.Context) {
	eventCh, errCh := w.cli.Events(ctx, events.ListOptions{
		Filters: w.eventFilters(),
	})

	for {
		select {
		case <-ctx.Done():
			return

		case ev := <-eventCh:
			name := ev.Actor.Attributes["name"]
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
					"action", string(ev.Action),
				)
				continue
			}

			switch ev.Action {
			case "die":
				exitCode := ev.Actor.Attributes["exitCode"]
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
			eventCh, errCh = w.cli.Events(ctx, events.ListOptions{
				Filters: w.eventFilters(),
			})
		}
	}
}
