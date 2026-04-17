package api

import (
	"context"
	"fmt"
)

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
	if d == nil || d.RawClient == nil {
		return fmt.Errorf("signal sender unavailable")
	}
	return d.RawClient.ContainerKill(ctx, containerName, signal)
}

type noopSignalSender struct{}

func (noopSignalSender) SignalContainer(context.Context, string, string) error {
	return fmt.Errorf("signal sender unavailable")
}

type noopCommsClient struct{}

func (noopCommsClient) CommsRequest(context.Context, string, string, interface{}) ([]byte, error) {
	return nil, fmt.Errorf("comms client unavailable")
}

type noopDockerExecClient struct{}

func (noopDockerExecClient) ExecInContainer(context.Context, string, []string) (string, error) {
	return "", fmt.Errorf("docker exec unavailable")
}

func (noopDockerExecClient) ContainerShortID(context.Context, string) string {
	return ""
}
