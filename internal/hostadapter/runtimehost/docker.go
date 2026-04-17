package runtimehost

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	dockerclient "github.com/docker/docker/client"
	"log/slog"

	containers "github.com/geoffbelknap/agency/internal/orchestrate/containers"
)

const prefix = "agency"

type RuntimeState string

type ContainerEvent struct {
	Name     string
	Action   string
	ExitCode string
}

const (
	RuntimeStateMissing RuntimeState = "missing"
	RuntimeStateRunning RuntimeState = "running"
	RuntimeStatePaused  RuntimeState = "paused"
	RuntimeStateStopped RuntimeState = "stopped"
)

func CountRunning(ctx context.Context, dc *Client) (agents, meeseeks int, err error) {
	if dc == nil {
		return 0, 0, fmt.Errorf("docker is not available")
	}
	cli := dc.RawClient()

	agentContainers, err := cli.ContainerList(ctx, container.ListOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", "agency.type=workspace"),
			filters.Arg("status", "running"),
		),
	})
	if err != nil {
		return 0, 0, err
	}
	meeseeksContainers, err := cli.ContainerList(ctx, container.ListOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", "agency.type=meeseeks-workspace"),
			filters.Arg("status", "running"),
		),
	})
	if err != nil {
		return 0, 0, err
	}
	return len(agentContainers), len(meeseeksContainers), nil
}

func StopRuntime(ctx context.Context, dc *Client, runtimeID string) error {
	if dc == nil {
		return fmt.Errorf("docker is not available")
	}
	names := []string{
		fmt.Sprintf("%s-%s-workspace", prefix, runtimeID),
		fmt.Sprintf("%s-%s-enforcer", prefix, runtimeID),
	}
	timeout := 30
	cli := dc.RawClient()
	var wg sync.WaitGroup
	for _, name := range names {
		name := name
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = cli.ContainerStop(ctx, name, container.StopOptions{Timeout: &timeout})
			_ = cli.ContainerRemove(ctx, name, container.RemoveOptions{Force: true})
		}()
	}
	wg.Wait()
	return nil
}

func RemoveRuntimeArtifacts(ctx context.Context, dc *Client, runtimeID string) error {
	if dc == nil {
		return fmt.Errorf("docker is not available")
	}
	cli := dc.RawClient()
	_ = cli.VolumeRemove(ctx, fmt.Sprintf("%s-%s-workspace-data", prefix, runtimeID), true)
	_ = cli.NetworkRemove(ctx, fmt.Sprintf("%s-%s-internal", prefix, runtimeID))
	return nil
}

func InspectWorkspaceState(ctx context.Context, dc *Client, runtimeID string) (RuntimeState, error) {
	if dc == nil {
		return RuntimeStateMissing, fmt.Errorf("docker is not available")
	}
	info, err := dc.RawClient().ContainerInspect(ctx, fmt.Sprintf("%s-%s-workspace", prefix, runtimeID))
	if err != nil {
		return RuntimeStateMissing, err
	}
	if info.State == nil {
		return RuntimeStateMissing, nil
	}
	if info.State.Paused {
		return RuntimeStatePaused, nil
	}
	if info.State.Running {
		return RuntimeStateRunning, nil
	}
	return RuntimeStateStopped, nil
}

func ListAgencyContainerStates(ctx context.Context, dc *Client) (map[string]string, error) {
	if dc == nil {
		return nil, fmt.Errorf("docker is not available")
	}
	result := make(map[string]string)
	containers, err := dc.RawClient().ContainerList(ctx, container.ListOptions{
		All: true,
		Filters: filters.NewArgs(
			filters.Arg("label", "agency.agent"),
		),
	})
	if err != nil {
		return nil, err
	}
	for _, c := range containers {
		for _, n := range c.Names {
			name := n
			if len(name) > 0 && name[0] == '/' {
				name = name[1:]
			}
			result[name] = c.State
		}
	}
	return result, nil
}

func WatchAgencyContainerEvents(ctx context.Context, dc *Client, actions ...string) (<-chan ContainerEvent, <-chan error, error) {
	if dc == nil {
		return nil, nil, fmt.Errorf("docker is not available")
	}
	args := filters.NewArgs(
		filters.Arg("type", "container"),
		filters.Arg("label", "agency.agent"),
	)
	for _, action := range actions {
		args.Add("event", action)
	}

	rawEvents, rawErrs := dc.RawClient().Events(ctx, events.ListOptions{Filters: args})
	out := make(chan ContainerEvent)
	errOut := make(chan error)

	go func() {
		defer close(out)
		defer close(errOut)
		for {
			select {
			case <-ctx.Done():
				return
			case err, ok := <-rawErrs:
				if !ok {
					return
				}
				errOut <- err
			case ev, ok := <-rawEvents:
				if !ok {
					return
				}
				out <- ContainerEvent{
					Name:     ev.Actor.Attributes["name"],
					Action:   string(ev.Action),
					ExitCode: ev.Actor.Attributes["exitCode"],
				}
			}
		}
	}()

	return out, errOut, nil
}

func WaitRunning(ctx context.Context, cli *dockerclient.Client, name string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		if inspect, err := cli.ContainerInspect(ctx, name); err == nil && inspect.State != nil {
			if inspect.State.Running {
				return nil
			}
			if inspect.State.Status == "exited" || inspect.State.Status == "dead" {
				return fmt.Errorf("container %s exited before becoming running", name)
			}
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return fmt.Errorf("container %s did not start within %v", name, timeout)
		}
	}
}

func WaitHealthy(ctx context.Context, cli *dockerclient.Client, name string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		info, err := cli.ContainerInspect(ctx, name)
		if err == nil {
			if info.State != nil && info.State.Health == nil {
				return nil
			}
			if info.State != nil && info.State.Health != nil && info.State.Health.Status == "healthy" {
				return nil
			}
			if info.State != nil && (info.State.Status == "exited" || info.State.Status == "dead") {
				return fmt.Errorf("container %s exited before becoming healthy", name)
			}
		}

		select {
		case <-ticker.C:
		case <-ctx.Done():
			return fmt.Errorf("container %s did not become healthy within %v", name, timeout)
		}
	}
}

func ConnectIfNeeded(ctx context.Context, cli *dockerclient.Client, containerID, netName string, aliases []string, logger *slog.Logger) {
	err := cli.NetworkConnect(ctx, netName, containerID, &network.EndpointSettings{
		Aliases: aliases,
	})
	if err != nil && !strings.Contains(err.Error(), "already exists") && logger != nil {
		logger.Warn("network connect", "container", containerID, "network", netName, "err", err)
	}
}

func EnsureInternalNetworkReady(ctx context.Context, cli *dockerclient.Client, netName string) error {
	if _, err := cli.NetworkInspect(ctx, netName, network.InspectOptions{}); err == nil {
		return nil
	} else if !containers.IsNetworkNotFound(err) {
		return fmt.Errorf("inspect agent network %s: %w", netName, err)
	}

	createErr := containers.CreateInternalNetwork(ctx, cli, netName, nil)
	if createErr != nil && !containers.IsNetworkAlreadyExists(createErr) {
		return createErr
	}

	backoff := []time.Duration{50 * time.Millisecond, 100 * time.Millisecond, 250 * time.Millisecond, 500 * time.Millisecond, time.Second}
	for _, delay := range backoff {
		if _, err := cli.NetworkInspect(ctx, netName, network.InspectOptions{}); err == nil {
			return nil
		} else if !containers.IsNetworkNotFound(err) {
			return fmt.Errorf("verify agent network %s: %w", netName, err)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}

	if _, err := cli.NetworkInspect(ctx, netName, network.InspectOptions{}); err == nil {
		return nil
	} else if containers.IsNetworkNotFound(err) {
		return fmt.Errorf("agent network %s not ready after create", netName)
	} else {
		return fmt.Errorf("verify agent network %s: %w", netName, err)
	}
}

func CleanManagedNetworks(ctx context.Context, cli *dockerclient.Client, logger *slog.Logger) {
	networks, err := cli.NetworkList(ctx, network.ListOptions{})
	if err != nil {
		return
	}
	for _, n := range networks {
		if n.Labels["agency.managed"] != "true" {
			continue
		}
		detail, err := cli.NetworkInspect(ctx, n.ID, network.InspectOptions{})
		if err != nil {
			continue
		}
		if len(detail.Containers) == 0 {
			if err := cli.NetworkRemove(ctx, n.ID); err != nil {
				if logger != nil {
					logger.Debug("clean network skip", "network", n.Name, "err", err)
				}
			} else if logger != nil {
				logger.Info("cleaned orphan network", "network", n.Name)
			}
		}
	}
}
