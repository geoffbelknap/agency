package runtimehost

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	dockerevents "github.com/docker/docker/api/types/events"
	dockerfilters "github.com/docker/docker/api/types/filters"
)

type appleContainerEventHub struct {
	mu          sync.Mutex
	subscribers map[chan AppleContainerHelperEvent]struct{}
}

func newAppleContainerEventHub() *appleContainerEventHub {
	return &appleContainerEventHub{subscribers: make(map[chan AppleContainerHelperEvent]struct{})}
}

func (h *appleContainerEventHub) subscribe(ctx context.Context) <-chan AppleContainerHelperEvent {
	ch := make(chan AppleContainerHelperEvent, 64)
	h.mu.Lock()
	h.subscribers[ch] = struct{}{}
	h.mu.Unlock()
	go func() {
		<-ctx.Done()
		h.mu.Lock()
		if _, ok := h.subscribers[ch]; ok {
			delete(h.subscribers, ch)
			close(ch)
		}
		h.mu.Unlock()
	}()
	return ch
}

func (h *appleContainerEventHub) publish(ev AppleContainerHelperEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subscribers {
		select {
		case ch <- ev:
		default:
		}
	}
}

func (c *RawClient) ensureAppleContainerEventHub() *appleContainerEventHub {
	if c == nil || c.appleContainer == nil {
		return nil
	}
	if c.appleContainer.events == nil {
		c.appleContainer.events = newAppleContainerEventHub()
	}
	return c.appleContainer.events
}

func (c *RawClient) publishAppleContainerHelperEvent(ev *AppleContainerHelperEvent) {
	if ev == nil {
		return
	}
	if hub := c.ensureAppleContainerEventHub(); hub != nil {
		hub.publish(*ev)
	}
}

func appleContainerEvents(ctx context.Context, hub *appleContainerEventHub, options dockerevents.ListOptions) (<-chan dockerevents.Message, <-chan error) {
	out := make(chan dockerevents.Message)
	errOut := make(chan error)
	events := hub.subscribe(ctx)
	wantActions := filterValues(options.Filters, "event")

	go func() {
		defer close(out)
		defer close(errOut)
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-events:
				if !ok {
					return
				}
				msg, ok := appleHelperEventToDockerMessage(ev)
				if !ok || !eventActionAllowed(string(msg.Action), wantActions) {
					continue
				}
				out <- msg
			}
		}
	}()
	return out, errOut
}

func appleHelperEventToDockerMessage(ev AppleContainerHelperEvent) (dockerevents.Message, bool) {
	action := ""
	switch ev.EventType {
	case "runtime.container.started":
		return dockerevents.Message{}, false
	case "runtime.container.exited", "runtime.container.stopped", "runtime.container.killed", "runtime.container.state_unknown":
		action = "die"
	case "runtime.container.deleted":
		action = "destroy"
	default:
		return dockerevents.Message{}, false
	}
	containerID := stringValue(ev.Data["container_id"])
	if containerID == "" {
		containerID = ev.SourceName
	}
	if containerID == "" {
		return dockerevents.Message{}, false
	}
	attrs := map[string]string{
		"name":      containerID,
		"backend":   BackendAppleContainer,
		"eventType": ev.EventType,
	}
	if exitCode := stringValue(ev.Data["exit_code"]); exitCode != "" {
		attrs["exitCode"] = exitCode
	}
	ts := time.Now()
	if parsed, err := time.Parse(time.RFC3339Nano, ev.Timestamp); err == nil {
		ts = parsed
	}
	return dockerevents.Message{
		Type:     dockerevents.ContainerEventType,
		Action:   dockerevents.Action(action),
		Actor:    dockerevents.Actor{ID: containerID, Attributes: attrs},
		Time:     ts.Unix(),
		TimeNano: ts.UnixNano(),
	}, true
}

func filterValues(args dockerfilters.Args, key string) []string {
	if !args.Contains(key) {
		return nil
	}
	return args.Get(key)
}

func eventActionAllowed(action string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, candidate := range allowed {
		if candidate == action {
			return true
		}
	}
	return false
}

func stringValue(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatInt(int64(v), 10)
	default:
		return ""
	}
}
