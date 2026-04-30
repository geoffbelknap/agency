package runtimehost

import (
	"context"
	"strings"
)

const (
	RuntimeComponentWorkspace = "workspace"
	RuntimeComponentEnforcer  = "enforcer"

	RuntimeActionStopped  = "stopped"
	RuntimeActionStarted  = "started"
	RuntimeActionDegraded = "degraded"
)

type RuntimeEvent struct {
	RuntimeID string
	Component string
	Action    string
	ExitCode  string
}

func WatchAgencyRuntimeEvents(ctx context.Context, dc *Client, actions ...string) (<-chan RuntimeEvent, <-chan error, error) {
	backendActions := make([]string, 0, len(actions))
	for _, action := range actions {
		switch action {
		case RuntimeActionStopped, RuntimeActionDegraded:
			backendActions = append(backendActions, "die")
		case RuntimeActionStarted:
			backendActions = append(backendActions, "start")
		default:
			backendActions = append(backendActions, action)
		}
	}

	rawEvents, rawErrs, err := WatchAgencyContainerEvents(ctx, dc, backendActions...)
	if err != nil {
		return nil, nil, err
	}
	events := make(chan RuntimeEvent)
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
				normalized, ok := normalizeRuntimeEvent(ev)
				if ok {
					events <- normalized
				}
			}
		}
	}()
	return events, errs, nil
}

func normalizeRuntimeEvent(ev ContainerEvent) (RuntimeEvent, bool) {
	component := ""
	suffix := ""
	switch {
	case strings.HasSuffix(ev.Name, "-workspace"):
		component = RuntimeComponentWorkspace
		suffix = "-workspace"
	case strings.HasSuffix(ev.Name, "-enforcer"):
		component = RuntimeComponentEnforcer
		suffix = "-enforcer"
	default:
		return RuntimeEvent{}, false
	}
	runtimeID := extractRuntimeEventID(ev.Name, suffix)
	if runtimeID == "" {
		return RuntimeEvent{}, false
	}
	action := ev.Action
	switch ev.Action {
	case "die":
		action = RuntimeActionStopped
	case "start":
		action = RuntimeActionStarted
	}
	return RuntimeEvent{
		RuntimeID: runtimeID,
		Component: component,
		Action:    action,
		ExitCode:  ev.ExitCode,
	}, true
}

func extractRuntimeEventID(componentName, suffix string) string {
	componentName = strings.TrimPrefix(componentName, "/")
	if !strings.HasPrefix(componentName, prefix+"-") || !strings.HasSuffix(componentName, suffix) {
		return ""
	}
	name := strings.TrimPrefix(componentName, prefix+"-")
	name = strings.TrimSuffix(name, suffix)
	return name
}
