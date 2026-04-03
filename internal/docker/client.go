package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

const prefix = "agency"

// Client wraps the Docker API client with agency-specific helpers.
type Client struct {
	cli *client.Client
}

// NewClient creates a new Docker client from the environment.
func NewClient() (*Client, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("cannot connect to Docker: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := cli.Ping(ctx); err != nil {
		return nil, fmt.Errorf("Docker not responding: %w", err)
	}
	return &Client{cli: cli}, nil
}

// TryNewClient attempts to create a Docker client. Returns nil (not an error)
// if Docker is unavailable — the gateway can start in degraded mode.
func TryNewClient(logger interface{ Warn(msg any, keyvals ...any) }) *Client {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		logger.Warn("Docker client unavailable, starting in degraded mode", "err", err)
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := cli.Ping(ctx); err != nil {
		logger.Warn("Docker not responding, starting in degraded mode", "err", err)
		return nil
	}
	return &Client{cli: cli}
}

// AgentStatus describes a running agent's container state.
type AgentStatus struct {
	Name      string `json:"name"`
	Status    string `json:"status"` // running, paused, stopped
	Workspace string `json:"workspace"`
	Enforcer  string `json:"enforcer"`
}

// ListAgents returns the status of all agency agent containers.
func (c *Client) ListAgents(ctx context.Context) ([]AgentStatus, error) {
	containers, err := c.cli.ContainerList(ctx, container.ListOptions{
		All: true,
		Filters: filters.NewArgs(
			filters.Arg("name", prefix+"-"),
		),
	})
	if err != nil {
		return nil, err
	}

	var agents []AgentStatus
	for _, ctr := range containers {
		name := extractAgentName(ctr.Names)
		if name == "" {
			continue
		}
		a := AgentStatus{
			Name:      name,
			Workspace: ctr.State,
		}
		// Check enforcer
		enforcer, _ := c.containerState(ctx, prefix+"-"+name+"-enforcer")
		a.Enforcer = enforcer
		// Overall status
		switch {
		case ctr.State == "running":
			a.Status = "running"
		case ctr.State == "paused":
			a.Status = "paused"
		default:
			a.Status = "stopped"
		}
		agents = append(agents, a)
	}
	return agents, nil
}

// InfraHealth returns the health status of shared infrastructure containers.
type InfraComponent struct {
	Name    string `json:"name"`
	State   string `json:"state"`  // running, stopped, missing
	Health  string `json:"health"` // healthy, unhealthy, none
	BuildID string `json:"build_id,omitempty"`
}

func (c *Client) InfraStatus(ctx context.Context) ([]InfraComponent, error) {
	components := []string{"egress", "comms", "knowledge", "intake", "web-fetch", "web", "embeddings"}

	// Single ContainerList call is ~50ms vs seconds for individual inspects on WSL2
	containers, err := c.cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("name", prefix+"-infra-")),
	})
	if err != nil {
		// Fallback: return all missing
		var result []InfraComponent
		for _, comp := range components {
			result = append(result, InfraComponent{Name: comp, State: "missing", Health: "none"})
		}
		return result, nil
	}

	// Index by component name
	stateMap := make(map[string]InfraComponent)
	for _, ctr := range containers {
		for _, n := range ctr.Names {
			n = strings.TrimPrefix(n, "/")
			for _, comp := range components {
				if n == prefix+"-infra-"+comp {
					ic := InfraComponent{Name: comp, State: ctr.State}
					if ctr.Status != "" && strings.Contains(ctr.Status, "healthy") {
						ic.Health = "healthy"
					} else if ctr.Status != "" && strings.Contains(ctr.Status, "unhealthy") {
						ic.Health = "unhealthy"
					} else {
						ic.Health = "none"
					}
					if bid, ok := ctr.Labels["agency.build.gateway"]; ok {
						ic.BuildID = bid
					}
					stateMap[comp] = ic
				}
			}
		}
	}

	var result []InfraComponent
	for _, comp := range components {
		if ic, ok := stateMap[comp]; ok {
			result = append(result, ic)
		} else {
			result = append(result, InfraComponent{Name: comp, State: "missing", Health: "none"})
		}
	}
	return result, nil
}

// ExecInContainer runs a command inside a container and returns stdout.
func (c *Client) ExecInContainer(ctx context.Context, containerName string, cmd []string) (string, error) {
	execConfig := container.ExecOptions{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
	}
	exec, err := c.cli.ContainerExecCreate(ctx, containerName, execConfig)
	if err != nil {
		return "", fmt.Errorf("exec create: %w", err)
	}

	resp, err := c.cli.ContainerExecAttach(ctx, exec.ID, container.ExecAttachOptions{})
	if err != nil {
		return "", fmt.Errorf("exec attach: %w", err)
	}
	defer resp.Close()

	var buf bytes.Buffer
	// Docker exec multiplexes stdout/stderr with 8-byte headers.
	// Use stdcopy to demux, falling back to raw read if it fails.
	if _, err := stdcopy.StdCopy(&buf, &bytes.Buffer{}, resp.Reader); err != nil {
		// Fallback: raw read (non-multiplexed)
		buf.Reset()
		_, _ = buf.ReadFrom(resp.Reader)
	}

	// Inspect to get exit code
	inspect, err := c.cli.ContainerExecInspect(ctx, exec.ID)
	if err != nil {
		return buf.String(), fmt.Errorf("exec inspect: %w", err)
	}
	if inspect.ExitCode != 0 {
		return buf.String(), fmt.Errorf("exit code %d", inspect.ExitCode)
	}
	return buf.String(), nil
}

// CommsRequest makes a direct HTTP request to the comms service via localhost port binding.
// Platform-only endpoints (grant-access, archive) get the X-Agency-Platform header.
func (c *Client) CommsRequest(ctx context.Context, method, path string, body interface{}) ([]byte, error) {
	url := "http://localhost:8202" + path
	httpClient := &http.Client{Timeout: 15 * time.Second}

	var req *http.Request
	var err error

	if body != nil && (method == "POST" || method == "PUT" || method == "DELETE") {
		jsonBody, _ := json.Marshal(body)
		req, err = http.NewRequestWithContext(ctx, method, url, bytes.NewReader(jsonBody))
		if err != nil {
			return nil, fmt.Errorf("comms request %s %s: %w", method, path, err)
		}
		req.Header.Set("Content-Type", "application/json")
	} else {
		req, err = http.NewRequestWithContext(ctx, method, url, nil)
		if err != nil {
			return nil, fmt.Errorf("comms request %s %s: %w", method, path, err)
		}
	}

	// Platform-only endpoints need this header
	if strings.Contains(path, "grant-access") || strings.Contains(path, "archive") {
		req.Header.Set("X-Agency-Platform", "true")
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("comms request %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("comms request %s %s: read body: %w", method, path, err)
	}
	if resp.StatusCode >= 400 {
		return out, fmt.Errorf("comms returned %d: %s", resp.StatusCode, string(out))
	}
	return out, nil
}

// ContainerInspect holds the subset of docker inspect data needed for doctor checks.
type ContainerInspect struct {
	Name     string
	State    string // running, paused, exited, etc.
	Health   string // healthy, unhealthy, none
	Env      []string
	Mounts   []MountInfo
	Networks []string
	Labels   map[string]string
}

// MountInfo describes a single container mount.
type MountInfo struct {
	Source      string
	Destination string
	RW          bool
}

// InspectContainer returns structured inspect data for a named container.
func (c *Client) InspectContainer(ctx context.Context, name string) (*ContainerInspect, error) {
	info, err := c.cli.ContainerInspect(ctx, name)
	if err != nil {
		return nil, err
	}

	ci := &ContainerInspect{
		Name:  name,
		State: info.State.Status,
	}
	if info.State.Health != nil {
		ci.Health = info.State.Health.Status
	} else {
		ci.Health = "none"
	}

	// Environment variables and labels
	if info.Config != nil {
		ci.Env = info.Config.Env
		ci.Labels = info.Config.Labels
	}

	// Mounts
	for _, m := range info.Mounts {
		ci.Mounts = append(ci.Mounts, MountInfo{
			Source:      m.Source,
			Destination: m.Destination,
			RW:          m.RW,
		})
	}

	// Networks
	if info.NetworkSettings != nil {
		for netName := range info.NetworkSettings.Networks {
			ci.Networks = append(ci.Networks, netName)
		}
	}

	return ci, nil
}

// ListAgentWorkspaces returns names of all running agency-*-workspace containers.
func (c *Client) ListAgentWorkspaces(ctx context.Context) ([]string, error) {
	containers, err := c.cli.ContainerList(ctx, container.ListOptions{
		All: false, // running only
		Filters: filters.NewArgs(
			filters.Arg("name", prefix+"-"),
		),
	})
	if err != nil {
		return nil, err
	}
	var agents []string
	for _, ctr := range containers {
		name := extractAgentName(ctr.Names)
		if name != "" {
			agents = append(agents, name)
		}
	}
	return agents, nil
}

func (c *Client) containerState(ctx context.Context, name string) (string, error) {
	info, err := c.cli.ContainerInspect(ctx, name)
	if err != nil {
		return "missing", err
	}
	return info.State.Status, nil
}

// ContainerShortID returns the first 12 hex chars of a container's Docker ID.
func (c *Client) ContainerShortID(ctx context.Context, name string) string {
	info, err := c.cli.ContainerInspect(ctx, name)
	if err != nil {
		return ""
	}
	id := info.ID
	if len(id) >= 12 {
		return id[:12]
	}
	return id
}

// WaitRunning blocks until the named container is running, using Docker's
// event stream instead of polling. Falls back to a single inspect if the
// container is already running. Returns error on timeout or context cancel.
func (c *Client) WaitRunning(ctx context.Context, name string, timeout time.Duration) error {
	// Quick check — already running?
	if info, err := c.cli.ContainerInspect(ctx, name); err == nil && info.State.Running {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	eventCh, errCh := c.cli.Events(ctx, events.ListOptions{
		Filters: filters.NewArgs(
			filters.Arg("container", name),
			filters.Arg("event", "start"),
		),
	})

	for {
		select {
		case <-eventCh:
			// Container started — verify with inspect
			if info, err := c.cli.ContainerInspect(ctx, name); err == nil && info.State.Running {
				return nil
			}
		case err := <-errCh:
			if ctx.Err() != nil {
				return fmt.Errorf("container %s did not start within %v", name, timeout)
			}
			return fmt.Errorf("event stream error for %s: %w", name, err)
		case <-ctx.Done():
			return fmt.Errorf("container %s did not start within %v", name, timeout)
		}
	}
}

// WaitHealthy blocks until the named container reports healthy, using Docker's
// event stream. Returns error if the container stops, disappears, or times out.
func (c *Client) WaitHealthy(ctx context.Context, name string, timeout time.Duration) error {
	// Quick check — already healthy?
	if info, err := c.cli.ContainerInspect(ctx, name); err == nil {
		if info.State.Health != nil && info.State.Health.Status == "healthy" {
			return nil
		}
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	eventCh, errCh := c.cli.Events(ctx, events.ListOptions{
		Filters: filters.NewArgs(
			filters.Arg("container", name),
			filters.Arg("event", "health_status"),
		),
	})

	for {
		select {
		case ev := <-eventCh:
			// health_status events have "health_status: healthy" in attributes
			status := ev.Actor.Attributes["health_status"]
			if status == "" {
				status = ev.Status
			}
			if strings.Contains(status, "healthy") && !strings.Contains(status, "unhealthy") {
				return nil
			}
		case err := <-errCh:
			if ctx.Err() != nil {
				return fmt.Errorf("container %s did not become healthy within %v", name, timeout)
			}
			return fmt.Errorf("event stream error for %s: %w", name, err)
		case <-ctx.Done():
			return fmt.Errorf("container %s did not become healthy within %v", name, timeout)
		}
	}
}

// RawClient returns the underlying Docker API client.
// Nil-safe: returns nil if the receiver is nil.
func (c *Client) RawClient() *client.Client {
	if c == nil {
		return nil
	}
	return c.cli
}

// ── Doctor check helpers ─────────────────────────────────────────────────────

// ContainerInspectRaw returns the full Docker inspect response for a container.
// Callers use this for checks that need HostConfig (PidsLimit, LogConfig).
func (c *Client) ContainerInspectRaw(ctx context.Context, name string) (*container.InspectResponse, error) {
	info, err := c.cli.ContainerInspect(ctx, name)
	if err != nil {
		return nil, err
	}
	return &info, nil
}

// ListAgencyContainers returns all containers (running or stopped) whose names
// match the agency- prefix AND have at least one label key starting with "agency.".
func (c *Client) ListAgencyContainers(ctx context.Context, runningOnly bool) ([]container.Summary, error) {
	opts := container.ListOptions{
		All: !runningOnly,
		Filters: filters.NewArgs(
			filters.Arg("name", prefix+"-"),
		),
	}
	containers, err := c.cli.ContainerList(ctx, opts)
	if err != nil {
		return nil, err
	}
	// Keep only containers that have at least one agency.* label
	var result []container.Summary
	for _, ctr := range containers {
		for k := range ctr.Labels {
			if strings.HasPrefix(k, "agency.") {
				result = append(result, ctr)
				break
			}
		}
	}
	return result, nil
}

// ListNetworksByLabel returns all Docker networks whose labels include the
// given key=value pair.
func (c *Client) ListNetworksByLabel(ctx context.Context, label string) ([]network.Summary, error) {
	return c.cli.NetworkList(ctx, network.ListOptions{
		Filters: filters.NewArgs(filters.Arg("label", label)),
	})
}

// ListAgencyImages returns images whose repository name starts with "agency-".
func (c *Client) ListAgencyImages(ctx context.Context) ([]image.Summary, error) {
	return c.cli.ImageList(ctx, image.ListOptions{
		Filters: filters.NewArgs(filters.Arg("reference", "agency-*")),
	})
}

// LogFileSize returns the size in bytes of the log file for the named container.
// It reads the LogPath from the inspect response and stats the file on the host.
func (c *Client) LogFileSize(ctx context.Context, name string) (int64, error) {
	info, err := c.cli.ContainerInspect(ctx, name)
	if err != nil {
		return 0, err
	}
	if info.LogPath == "" {
		return 0, nil
	}
	fi, err := os.Stat(info.LogPath)
	if err != nil {
		// Log file may not exist yet (e.g. no output produced) — treat as 0.
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	return fi.Size(), nil
}

func extractAgentName(names []string) string {
	for _, n := range names {
		n = strings.TrimPrefix(n, "/")
		// Pattern: agency-{name}-workspace
		if strings.HasPrefix(n, prefix+"-") && strings.HasSuffix(n, "-workspace") {
			name := strings.TrimPrefix(n, prefix+"-")
			name = strings.TrimSuffix(name, "-workspace")
			// Skip infra containers
			if name == "infra" {
				continue
			}
			return name
		}
	}
	return ""
}
