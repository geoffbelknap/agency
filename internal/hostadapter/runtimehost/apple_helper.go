package runtimehost

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type appleContainerHelperClient struct {
	binary string
	run    func(context.Context, ...string) ([]byte, []byte, error)
}

type AppleContainerHelperHealth struct {
	OK           bool   `json:"ok"`
	Backend      string `json:"backend"`
	EventSupport string `json:"event_support"`
	Version      string `json:"version,omitempty"`
	Error        string `json:"error,omitempty"`
}

type appleContainerHelperListOwnedResponse struct {
	OK         bool                    `json:"ok"`
	Backend    string                  `json:"backend"`
	Containers []appleContainerInspect `json:"containers"`
}

type appleContainerHelperOperationResponse struct {
	OK          bool                       `json:"ok"`
	Backend     string                     `json:"backend"`
	ContainerID string                     `json:"container_id,omitempty"`
	Output      string                     `json:"output,omitempty"`
	Error       string                     `json:"error,omitempty"`
	Event       *AppleContainerHelperEvent `json:"event,omitempty"`
}

type AppleContainerHelperEvent struct {
	ID         string         `json:"id"`
	SourceType string         `json:"source_type"`
	SourceName string         `json:"source_name"`
	EventType  string         `json:"event_type"`
	Timestamp  string         `json:"timestamp"`
	Data       map[string]any `json:"data"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// AppleContainerHelperStatus probes the optional Apple Container host-adapter
// helper. The helper is intentionally separate from the Apple `container` CLI:
// it is the boundary where Agency will normalize lifecycle events.
func AppleContainerHelperStatus(ctx context.Context, backendConfig map[string]string) (AppleContainerHelperHealth, error) {
	helper, ok := appleContainerHelperFromConfig(backendConfig)
	if !ok {
		return AppleContainerHelperHealth{}, fmt.Errorf("apple-container helper is not configured")
	}
	return helper.Health(ctx)
}

func appleContainerHelperFromConfig(backendConfig map[string]string) (*appleContainerHelperClient, bool) {
	helperBinary := strings.TrimSpace(os.Getenv("AGENCY_APPLE_CONTAINER_HELPER_BIN"))
	if backendConfig != nil {
		if configured := strings.TrimSpace(backendConfig["helper_binary"]); configured != "" {
			helperBinary = configured
		}
	}
	if helperBinary == "" {
		return nil, false
	}
	return &appleContainerHelperClient{binary: helperBinary}, true
}

func (c *appleContainerHelperClient) Health(ctx context.Context) (AppleContainerHelperHealth, error) {
	var resp AppleContainerHelperHealth
	if err := c.runJSON(ctx, &resp, "health"); err != nil {
		return AppleContainerHelperHealth{}, err
	}
	if !resp.OK {
		if strings.TrimSpace(resp.Error) != "" {
			return AppleContainerHelperHealth{}, fmt.Errorf("apple-container helper health check failed: %s", resp.Error)
		}
		return AppleContainerHelperHealth{}, fmt.Errorf("apple-container helper health check failed")
	}
	return resp, nil
}

func (c *appleContainerHelperClient) ListOwned(ctx context.Context, homeHash string) ([]appleContainerInspect, error) {
	args := []string{"list-owned"}
	if strings.TrimSpace(homeHash) != "" {
		args = append(args, "--home-hash", homeHash)
	}
	var resp appleContainerHelperListOwnedResponse
	if err := c.runJSON(ctx, &resp, args...); err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, fmt.Errorf("apple-container helper list-owned failed")
	}
	return resp.Containers, nil
}

func (c *appleContainerHelperClient) Start(ctx context.Context, containerID string) (*AppleContainerHelperEvent, error) {
	resp, err := c.operation(ctx, "start", containerID)
	return resp.Event, err
}

func (c *appleContainerHelperClient) Stop(ctx context.Context, containerID string, timeoutSeconds int) (*AppleContainerHelperEvent, error) {
	args := []string{"stop"}
	if timeoutSeconds > 0 {
		args = append(args, "--time", fmt.Sprintf("%d", timeoutSeconds))
	}
	args = append(args, containerID)
	resp, err := c.operation(ctx, args...)
	return resp.Event, err
}

func (c *appleContainerHelperClient) Kill(ctx context.Context, containerID, signal string) (*AppleContainerHelperEvent, error) {
	args := []string{"kill"}
	if strings.TrimSpace(signal) != "" {
		args = append(args, "--signal", strings.TrimSpace(signal))
	}
	args = append(args, containerID)
	resp, err := c.operation(ctx, args...)
	return resp.Event, err
}

func (c *appleContainerHelperClient) Delete(ctx context.Context, containerID string, force bool) (*AppleContainerHelperEvent, error) {
	args := []string{"delete"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, containerID)
	resp, err := c.operation(ctx, args...)
	return resp.Event, err
}

func (c *appleContainerHelperClient) Exec(ctx context.Context, containerID, user, workdir string, cmd []string) (string, error) {
	args := []string{"exec"}
	if strings.TrimSpace(user) != "" {
		args = append(args, "--user", strings.TrimSpace(user))
	}
	if strings.TrimSpace(workdir) != "" {
		args = append(args, "--workdir", strings.TrimSpace(workdir))
	}
	args = append(args, containerID)
	args = append(args, cmd...)
	resp, err := c.operation(ctx, args...)
	return resp.Output, err
}

func (c *appleContainerHelperClient) EventsOnce(ctx context.Context, homeHash string) ([]AppleContainerHelperEvent, error) {
	args := []string{"events", "--once"}
	if strings.TrimSpace(homeHash) != "" {
		args = append(args, "--home-hash", strings.TrimSpace(homeHash))
	}
	stdout, _, err := c.runHelper(ctx, args...)
	if err != nil {
		return nil, err
	}
	scanner := bufio.NewScanner(bytes.NewReader(stdout))
	var events []AppleContainerHelperEvent
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var event AppleContainerHelperEvent
		if err := json.Unmarshal(line, &event); err != nil {
			return nil, fmt.Errorf("apple-container helper event is invalid JSON: %w", err)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

func (c *appleContainerHelperClient) operation(ctx context.Context, args ...string) (appleContainerHelperOperationResponse, error) {
	var resp appleContainerHelperOperationResponse
	if err := c.runJSON(ctx, &resp, args...); err != nil {
		return appleContainerHelperOperationResponse{}, err
	}
	if !resp.OK {
		if strings.TrimSpace(resp.Error) != "" {
			return resp, fmt.Errorf("apple-container helper %s failed: %s", strings.Join(args, " "), resp.Error)
		}
		return resp, fmt.Errorf("apple-container helper %s failed", strings.Join(args, " "))
	}
	return resp, nil
}

func (c *appleContainerHelperClient) runJSON(ctx context.Context, out any, args ...string) error {
	stdout, _, err := c.runHelper(ctx, args...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdout), out); err != nil {
		return fmt.Errorf("apple-container helper %s returned invalid JSON: %w", strings.Join(args, " "), err)
	}
	return nil
}

func (c *appleContainerHelperClient) runHelper(ctx context.Context, args ...string) ([]byte, []byte, error) {
	if c != nil && c.run != nil {
		return c.run(ctx, args...)
	}
	binary := "agency-apple-container-helper"
	if c != nil && strings.TrimSpace(c.binary) != "" {
		binary = c.binary
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			msg = err.Error()
		}
		return stdout.Bytes(), stderr.Bytes(), fmt.Errorf("apple-container helper %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.Bytes(), stderr.Bytes(), nil
}
