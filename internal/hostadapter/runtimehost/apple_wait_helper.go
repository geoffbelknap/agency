package runtimehost

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
)

type appleContainerWaitHelperClient struct {
	binary    string
	run       func(context.Context, string) (<-chan AppleContainerHelperEvent, <-chan error, error)
	runHealth func(context.Context) ([]byte, []byte, error)
}

// AppleContainerWaitHelperStatus probes the optional SwiftPM wait helper. The
// wait helper owns Apple Container process wait registration and reports the
// lifecycle event support that the Go CLI helper cannot provide by itself.
func AppleContainerWaitHelperStatus(ctx context.Context, backendConfig map[string]string) (AppleContainerHelperHealth, error) {
	helper, ok := appleContainerWaitHelperFromConfig(backendConfig)
	if !ok {
		return AppleContainerHelperHealth{}, fmt.Errorf("apple-container wait helper is not configured")
	}
	return helper.Health(ctx)
}

func appleContainerWaitHelperFromConfig(backendConfig map[string]string) (*appleContainerWaitHelperClient, bool) {
	helperBinary := strings.TrimSpace(os.Getenv("AGENCY_APPLE_CONTAINER_WAIT_HELPER_BIN"))
	if backendConfig != nil {
		if configured := strings.TrimSpace(backendConfig["wait_helper_binary"]); configured != "" {
			helperBinary = configured
		}
	}
	if helperBinary == "" {
		return nil, false
	}
	return &appleContainerWaitHelperClient{binary: helperBinary}, true
}

func (c *appleContainerWaitHelperClient) Health(ctx context.Context) (AppleContainerHelperHealth, error) {
	var stdout, stderr []byte
	var err error
	if c != nil && c.runHealth != nil {
		stdout, stderr, err = c.runHealth(ctx)
	} else {
		binary := "agency-apple-container-wait-helper"
		if c != nil && strings.TrimSpace(c.binary) != "" {
			binary = c.binary
		}
		cmd := exec.CommandContext(ctx, binary, "health")
		stdout, err = cmd.Output()
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr = exitErr.Stderr
		}
	}
	if err != nil {
		msg := strings.TrimSpace(string(stderr))
		if msg == "" {
			msg = err.Error()
		}
		return AppleContainerHelperHealth{}, fmt.Errorf("apple-container wait helper health check failed: %s", msg)
	}
	var resp AppleContainerHelperHealth
	if err := json.Unmarshal(bytes.TrimSpace(stdout), &resp); err != nil {
		return AppleContainerHelperHealth{}, err
	}
	if !resp.OK {
		if strings.TrimSpace(resp.Error) != "" {
			return AppleContainerHelperHealth{}, fmt.Errorf("apple-container wait helper health check failed: %s", resp.Error)
		}
		return AppleContainerHelperHealth{}, fmt.Errorf("apple-container wait helper health check failed")
	}
	return resp, nil
}

func (c *appleContainerWaitHelperClient) StartAndMonitor(ctx context.Context, containerID string, publish func(*AppleContainerHelperEvent)) (*AppleContainerHelperEvent, error) {
	events, errs, err := c.startWait(ctx, containerID)
	if err != nil {
		return nil, err
	}

	var started *AppleContainerHelperEvent
	for started == nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case err, ok := <-errs:
			if ok && err != nil {
				return nil, err
			}
			if !ok {
				errs = nil
				if events == nil {
					return nil, fmt.Errorf("apple-container wait helper exited before start event for %s", containerID)
				}
			}
		case ev, ok := <-events:
			if !ok {
				events = nil
				if errs == nil {
					return nil, fmt.Errorf("apple-container wait helper exited before start event for %s", containerID)
				}
				continue
			}
			if ev.EventType == "runtime.container.started" {
				started = &ev
			} else if publish != nil {
				publish(&ev)
			}
		}
	}

	go func() {
		for events != nil || errs != nil {
			select {
			case ev, ok := <-events:
				if !ok {
					events = nil
					continue
				}
				if publish != nil {
					ev := ev
					publish(&ev)
				}
			case _, ok := <-errs:
				if !ok {
					errs = nil
				}
			}
		}
	}()

	return started, nil
}

func (c *appleContainerWaitHelperClient) startWait(ctx context.Context, containerID string) (<-chan AppleContainerHelperEvent, <-chan error, error) {
	if c != nil && c.run != nil {
		return c.run(ctx, containerID)
	}
	binary := "agency-apple-container-wait-helper"
	if c != nil && strings.TrimSpace(c.binary) != "" {
		binary = c.binary
	}
	cmd := exec.Command(binary, "start-wait", containerID)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}

	events := make(chan AppleContainerHelperEvent)
	errs := make(chan error, 1)
	var parseDone sync.WaitGroup
	parseDone.Add(1)
	go func() {
		defer parseDone.Done()
		parseWaitHelperEvents(stdout, events, errs)
	}()
	go func() {
		errText, _ := io.ReadAll(stderr)
		if err := cmd.Wait(); err != nil {
			msg := strings.TrimSpace(string(errText))
			if msg == "" {
				msg = err.Error()
			}
			errs <- fmt.Errorf("apple-container wait helper start-wait %s: %s", containerID, msg)
		}
		parseDone.Wait()
		close(errs)
	}()
	return events, errs, nil
}

func parseWaitHelperEvents(r io.Reader, events chan<- AppleContainerHelperEvent, errs chan<- error) {
	defer close(events)
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var ev AppleContainerHelperEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			errs <- fmt.Errorf("apple-container wait helper emitted invalid JSON: %w", err)
			continue
		}
		events <- ev
	}
	if err := scanner.Err(); err != nil {
		errs <- err
	}
}
