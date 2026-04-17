package containers

import (
	"context"
	"errors"
	"time"

	"github.com/geoffbelknap/agency/internal/hostadapter/containerops"
)

type DockerAPI = containerops.DockerAPI

func CreateAndStart(ctx context.Context, cli DockerAPI, name string, config *Config, hostConfig *HostConfig, netConfig *NetworkingConfig) (string, error) {
	return containerops.CreateAndStart(ctx, cli, name, config, hostConfig, netConfig)
}

func StopAndRemove(ctx context.Context, cli DockerAPI, name string, timeoutSecs int) error {
	return containerops.StopAndRemove(ctx, cli, name, timeoutSecs)
}

func removeUntilGone(ctx context.Context, cli DockerAPI, name string, timeout time.Duration) error {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		err := cli.ContainerRemove(waitCtx, name, containerops.RemoveOptions{Force: true})
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

func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	return containerops.IsContainerNotFound(err)
}

func isRetryableRemoveError(err error) bool {
	if err == nil {
		return false
	}
	return containerops.IsRetryableRemoveError(err)
}
