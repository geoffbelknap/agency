package orchestrate

import (
	"context"
	"fmt"
	"strings"

	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
)

const (
	HostStateComponentWorkspace = "workspace"
	HostStateComponentEnforcer  = "enforcer"

	HostStateActionStopped  = "stopped"
	HostStateActionStarted  = "started"
	HostStateActionDegraded = "degraded"
)

type HostStateEvent struct {
	AgentName string
	Component string
	Action    string
	ExitCode  string
}

type HostStateSource interface {
	Watch(ctx context.Context, actions ...string) (<-chan HostStateEvent, <-chan error, error)
}

type backendHostStateSource struct {
	backend *runtimehost.BackendHandle
}

func NewBackendHostStateSource(backend *runtimehost.BackendHandle) HostStateSource {
	if backend == nil {
		return nil
	}
	return &backendHostStateSource{backend: backend}
}

func (s *backendHostStateSource) Watch(ctx context.Context, actions ...string) (<-chan HostStateEvent, <-chan error, error) {
	if s == nil || s.backend == nil {
		return nil, nil, fmt.Errorf("runtime backend client unavailable")
	}
	backendActions := make([]string, 0, len(actions))
	for _, action := range actions {
		switch action {
		case HostStateActionStopped, HostStateActionDegraded:
			backendActions = append(backendActions, "die")
		case HostStateActionStarted:
			backendActions = append(backendActions, "start")
		default:
			backendActions = append(backendActions, action)
		}
	}
	rawEvents, rawErrs, err := runtimehost.WatchAgencyContainerEvents(ctx, s.backend, backendActions...)
	if err != nil {
		return nil, nil, err
	}
	events := make(chan HostStateEvent)
	errs := make(chan error)
	go func() {
		defer close(events)
		defer close(errs)
		for {
			select {
			case <-ctx.Done():
				return
			case err, ok := <-rawErrs:
				if !ok {
					return
				}
				errs <- err
			case ev, ok := <-rawEvents:
				if !ok {
					return
				}
				normalized, ok := normalizeBackendHostStateEvent(ev)
				if ok {
					events <- normalized
				}
			}
		}
	}()
	return events, errs, nil
}

func normalizeBackendHostStateEvent(ev runtimehost.ContainerEvent) (HostStateEvent, bool) {
	component := ""
	suffix := ""
	switch {
	case strings.HasSuffix(ev.Name, "-workspace"):
		component = HostStateComponentWorkspace
		suffix = "-workspace"
	case strings.HasSuffix(ev.Name, "-enforcer"):
		component = HostStateComponentEnforcer
		suffix = "-enforcer"
	default:
		return HostStateEvent{}, false
	}
	agentName := extractAgentName(ev.Name, suffix)
	if agentName == "" {
		return HostStateEvent{}, false
	}
	action := ev.Action
	switch ev.Action {
	case "die":
		action = HostStateActionStopped
	case "start":
		action = HostStateActionStarted
	}
	return HostStateEvent{
		AgentName: agentName,
		Component: component,
		Action:    action,
		ExitCode:  ev.ExitCode,
	}, true
}
