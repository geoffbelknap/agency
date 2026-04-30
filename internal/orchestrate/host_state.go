package orchestrate

import (
	"context"
	"fmt"

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
	runtimeActions := make([]string, 0, len(actions))
	for _, action := range actions {
		switch action {
		case HostStateActionStopped:
			runtimeActions = append(runtimeActions, runtimehost.RuntimeActionStopped)
		case HostStateActionDegraded:
			runtimeActions = append(runtimeActions, runtimehost.RuntimeActionDegraded)
		case HostStateActionStarted:
			runtimeActions = append(runtimeActions, runtimehost.RuntimeActionStarted)
		default:
			runtimeActions = append(runtimeActions, action)
		}
	}
	rawEvents, rawErrs, err := runtimehost.WatchAgencyRuntimeEvents(ctx, s.backend, runtimeActions...)
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

func normalizeBackendHostStateEvent(ev runtimehost.RuntimeEvent) (HostStateEvent, bool) {
	if ev.RuntimeID == "" {
		return HostStateEvent{}, false
	}
	component := ev.Component
	switch ev.Component {
	case runtimehost.RuntimeComponentWorkspace:
		component = HostStateComponentWorkspace
	case runtimehost.RuntimeComponentEnforcer:
		component = HostStateComponentEnforcer
	default:
		return HostStateEvent{}, false
	}
	action := ev.Action
	switch ev.Action {
	case runtimehost.RuntimeActionStopped:
		action = HostStateActionStopped
	case runtimehost.RuntimeActionStarted:
		action = HostStateActionStarted
	case runtimehost.RuntimeActionDegraded:
		action = HostStateActionDegraded
	}
	return HostStateEvent{
		AgentName: ev.RuntimeID,
		Component: component,
		Action:    action,
		ExitCode:  ev.ExitCode,
	}, true
}
