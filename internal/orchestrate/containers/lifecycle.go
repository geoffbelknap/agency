package containers

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	specs "github.com/opencontainers/image-spec/specs-go/v1"
)

// DockerAPI is the subset of the Docker client used by this package.
// Defined as an interface so callers can inject mocks in tests.
type DockerAPI interface {
	ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *specs.Platform, containerName string) (container.CreateResponse, error)
	ContainerInspect(ctx context.Context, containerID string) (container.InspectResponse, error)
	ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error
	ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error
	ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error
}

// CreateAndStart creates a container and starts it. If start fails the
// container is automatically removed — no orphans.
func CreateAndStart(
	ctx context.Context,
	cli DockerAPI,
	name string,
	config *container.Config,
	hostConfig *container.HostConfig,
	netConfig *network.NetworkingConfig,
) (string, error) {
	resp, err := cli.ContainerCreate(ctx, config, hostConfig, netConfig, nil, name)
	if err != nil {
		return "", err
	}

	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		// Remove the created-but-unstarted container to avoid orphans.
		_ = cli.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
		return "", err
	}

	return resp.ID, nil
}

// StopAndRemove stops a container (with timeout) then force-removes it.
// Ignores "not found" errors so it is safe to call on already-removed containers.
func StopAndRemove(ctx context.Context, cli DockerAPI, name string, timeoutSecs int) error {
	stopErr := cli.ContainerStop(ctx, name, container.StopOptions{Timeout: &timeoutSecs})
	if isNotFound(stopErr) {
		return nil
	}

	removeErr := removeUntilGone(ctx, cli, name, 5*time.Second)
	if removeErr != nil {
		if stopErr != nil && !isNotFound(stopErr) {
			return errors.Join(stopErr, removeErr)
		}
		return removeErr
	}

	if stopErr != nil && !isIgnorableStopError(stopErr) {
		return stopErr
	}
	return nil
}

func removeUntilGone(ctx context.Context, cli DockerAPI, name string, timeout time.Duration) error {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		err := cli.ContainerRemove(waitCtx, name, container.RemoveOptions{Force: true})
		switch {
		case err == nil:
		case isNotFound(err):
			return nil
		case isRetryableRemoveError(err):
		default:
			return err
		}

		_, inspectErr := cli.ContainerInspect(waitCtx, name)
		if isNotFound(inspectErr) {
			return nil
		}
		if inspectErr != nil {
			return inspectErr
		}

		select {
		case <-waitCtx.Done():
			if errors.Is(waitCtx.Err(), context.DeadlineExceeded) {
				return errors.New("timed out waiting for container removal")
			}
			return waitCtx.Err()
		case <-ticker.C:
		}
	}
}

// isNotFound returns true for Docker "no such container" / "not found" errors.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no such container") ||
		strings.Contains(msg, "not found")
}

func isRetryableRemoveError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "removal of container") ||
		strings.Contains(msg, "already in progress") ||
		strings.Contains(msg, "is restarting") ||
		strings.Contains(msg, "cannot remove container")
}

func isIgnorableStopError(err error) bool {
	if err == nil || isNotFound(err) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already stopped")
}
