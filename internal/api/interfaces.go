package api

import "context"

// SignalSender sends OS signals to named containers.
// Used by modules that need to SIGHUP enforcers for config reload.
type SignalSender interface {
	SignalContainer(ctx context.Context, containerName, signal string) error
}

// DockerSignalSender adapts docker.Client to the SignalSender interface.
type DockerSignalSender struct {
	RawClient interface {
		ContainerKill(ctx context.Context, containerID, signal string) error
	}
}

func (d *DockerSignalSender) SignalContainer(ctx context.Context, containerName, signal string) error {
	return d.RawClient.ContainerKill(ctx, containerName, signal)
}
