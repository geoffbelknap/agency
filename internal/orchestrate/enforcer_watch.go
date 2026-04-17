package orchestrate

import (
	"context"
	"fmt"
	"strings"

	"log/slog"
	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
)

// EnforcerAlertFunc is called when an enforcer container exits unexpectedly.
type EnforcerAlertFunc func(agentName, reason string)

// EnforcerWatcher monitors Docker events for enforcer container exits.
// When an enforcer dies while its workspace is still running, the agent
// has lost all API mediation (ASK Tenet 3). This watcher detects that
// condition in real-time via Docker's event stream — no polling.
type EnforcerWatcher struct {
	docker     *runtimehost.DockerHandle
	alert      EnforcerAlertFunc
	logger     *slog.Logger
	cancel     context.CancelFunc
	suppress   *StopSuppression
}

// NewEnforcerWatcher creates a watcher that calls alertFn when an enforcer
// container exits. The alertFn receives the agent name (extracted from the
// container name) and a human-readable reason string.
func NewEnforcerWatcher(alertFn EnforcerAlertFunc, logger *slog.Logger, suppress *StopSuppression) (*EnforcerWatcher, error) {
	return NewEnforcerWatcherWithClient(alertFn, logger, suppress, nil)
}

// NewEnforcerWatcherWithClient creates a watcher using the provided Docker client.
func NewEnforcerWatcherWithClient(alertFn EnforcerAlertFunc, logger *slog.Logger, suppress *StopSuppression, dc *runtimehost.DockerHandle) (*EnforcerWatcher, error) {
	if logger == nil {
		logger = slog.Default()
	}
	return &EnforcerWatcher{
		docker:   dc,
		alert:    alertFn,
		logger:   logger,
		suppress: suppress,
	}, nil
}

// Start launches the background Docker event listener.
func (w *EnforcerWatcher) Start(ctx context.Context) {
	if w.docker == nil {
		w.logger.Info("enforcer watcher disabled: docker client unavailable")
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
	eventCh, errCh, err := runtimehost.WatchAgencyContainerEvents(ctx, w.docker, "die")
	if err != nil {
		w.logger.Warn("enforcer watcher disabled", "error", err)
		return
	}

	for {
		select {
		case <-ctx.Done():
			return

		case ev := <-eventCh:
			name := ev.Name
			if name == "" {
				continue
			}
			// Only care about enforcer containers.
			if !strings.HasSuffix(name, "-enforcer") {
				continue
			}
			agentName := extractAgentName(name, "-enforcer")
			if agentName == "" {
				continue
			}

			exitCode := ev.ExitCode
			if w.suppress != nil && w.suppress.IsSuppressed(agentName) {
				w.logger.Info("enforcer exit suppressed (intentional stop/restart)",
					"agent", agentName,
					"exit_code", exitCode,
				)
				continue
			}
			reason := fmt.Sprintf("enforcer exited (code %s) — agent has no API mediation", exitCode)
			w.logger.Warn("enforcer died while agent may be running",
				"agent", agentName,
				"exit_code", exitCode,
			)
			w.alert(agentName, reason)

		case err := <-errCh:
			if ctx.Err() != nil {
				return
			}
			w.logger.Warn("enforcer watcher: event stream error, restarting", "error", err)
			eventCh, errCh, err = runtimehost.WatchAgencyContainerEvents(ctx, w.docker, "die")
			if err != nil {
				w.logger.Warn("enforcer watcher reconnect failed", "error", err)
				return
			}
		}
	}
}

// extractAgentName pulls the agent name from a container name like
// "agency-henrybot900-enforcer" → "henrybot900".
func extractAgentName(containerName, suffix string) string {
	containerName = strings.TrimPrefix(containerName, "/")
	if !strings.HasPrefix(containerName, prefix+"-") || !strings.HasSuffix(containerName, suffix) {
		return ""
	}
	name := strings.TrimPrefix(containerName, prefix+"-")
	name = strings.TrimSuffix(name, suffix)
	return name
}
