package orchestrate

import (
	"context"
	"fmt"

	"log/slog"

	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
)

// EnforcerAlertFunc is called when an enforcer runtime exits unexpectedly.
type EnforcerAlertFunc func(agentName, reason string)

// EnforcerWatcher monitors host-backend events for enforcer runtime exits.
// When an enforcer dies while its workspace is still running, the agent
// has lost all API mediation (ASK Tenet 3). This watcher detects that
// condition in real time via the host backend's event stream, without polling.
type EnforcerWatcher struct {
	source   HostStateSource
	alert    EnforcerAlertFunc
	logger   *slog.Logger
	cancel   context.CancelFunc
	suppress *StopSuppression
}

// NewEnforcerWatcher creates a watcher that calls alertFn when an enforcer
// runtime exits. The alertFn receives the agent name and a human-readable
// reason string.
func NewEnforcerWatcher(alertFn EnforcerAlertFunc, logger *slog.Logger, suppress *StopSuppression) (*EnforcerWatcher, error) {
	return NewEnforcerWatcherWithClient(alertFn, logger, suppress, nil)
}

// NewEnforcerWatcherWithClient creates a watcher using the provided backend client.
func NewEnforcerWatcherWithClient(alertFn EnforcerAlertFunc, logger *slog.Logger, suppress *StopSuppression, dc *runtimehost.BackendHandle) (*EnforcerWatcher, error) {
	return NewEnforcerWatcherWithSource(alertFn, logger, suppress, NewBackendHostStateSource(dc))
}

func NewEnforcerWatcherWithSource(alertFn EnforcerAlertFunc, logger *slog.Logger, suppress *StopSuppression, source HostStateSource) (*EnforcerWatcher, error) {
	if logger == nil {
		logger = slog.Default()
	}
	return &EnforcerWatcher{
		source:   source,
		alert:    alertFn,
		logger:   logger,
		suppress: suppress,
	}, nil
}

// Start launches the background host-backend event listener.
func (w *EnforcerWatcher) Start(ctx context.Context) {
	if w.source == nil {
		w.logger.Info("enforcer watcher disabled: runtime backend client unavailable")
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	w.cancel = cancel

	go w.watch(ctx)
}

// Stop cancels the background goroutine.
func (w *EnforcerWatcher) Stop() {
	if w.cancel != nil {
		w.cancel()
	}
}

func (w *EnforcerWatcher) watch(ctx context.Context) {
	eventCh, errCh, err := w.source.Watch(ctx, HostStateActionStopped)
	if err != nil {
		w.logger.Warn("enforcer watcher disabled", "error", err)
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
			if ev.Component != HostStateComponentEnforcer || ev.AgentName == "" {
				continue
			}

			exitCode := ev.ExitCode
			if w.suppress != nil && w.suppress.IsSuppressed(ev.AgentName) {
				w.logger.Info("enforcer exit suppressed (intentional stop/restart)",
					"agent", ev.AgentName,
					"exit_code", exitCode,
				)
				continue
			}
			reason := fmt.Sprintf("enforcer exited (code %s) — agent has no API mediation", exitCode)
			w.logger.Warn("enforcer died while agent may be running",
				"agent", ev.AgentName,
				"exit_code", exitCode,
			)
			w.alert(ev.AgentName, reason)

		case err, ok := <-errCh:
			if !ok {
				return
			}
			if ctx.Err() != nil {
				return
			}
			w.logger.Warn("enforcer watcher: event stream error, restarting", "error", err)
			eventCh, errCh, err = w.source.Watch(ctx, HostStateActionStopped)
			if err != nil {
				w.logger.Warn("enforcer watcher reconnect failed", "error", err)
				return
			}
		}
	}
}
