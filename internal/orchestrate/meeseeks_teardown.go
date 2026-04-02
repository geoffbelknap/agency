package orchestrate

import (
	"context"
	"fmt"

	"github.com/charmbracelet/log"
	"github.com/docker/docker/api/types/container"
	dockerclient "github.com/docker/docker/client"

	agencyDocker "github.com/geoffbelknap/agency/internal/docker"
	"github.com/geoffbelknap/agency/internal/models"
)

// TeardownMeeseeks removes all containers and networks for a Meeseeks.
// Audit logs are preserved at ~/.agency/audit/{meeseeks_id}/ and are NOT deleted.
// Pass a non-nil dc to reuse a shared Docker client; otherwise a new connection is opened.
func TeardownMeeseeks(ctx context.Context, mks *models.Meeseeks, logger *log.Logger, dc ...*agencyDocker.Client) error {
	var cli *dockerclient.Client
	if len(dc) > 0 && dc[0] != nil {
		cli = dc[0].RawClient()
	} else {
		var err error
		cli, err = dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
		if err != nil {
			return fmt.Errorf("docker client: %w", err)
		}
	}

	t := 10

	// 1. Stop and remove workspace container
	logger.Info("meeseeks teardown: stopping workspace", "id", mks.ID, "container", mks.ContainerName)
	_ = cli.ContainerStop(ctx, mks.ContainerName, container.StopOptions{Timeout: &t})
	_ = cli.ContainerRemove(ctx, mks.ContainerName, container.RemoveOptions{Force: true})

	// 2. Stop and remove enforcer container
	logger.Info("meeseeks teardown: stopping enforcer", "id", mks.ID, "container", mks.EnforcerName)
	_ = cli.ContainerStop(ctx, mks.EnforcerName, container.StopOptions{Timeout: &t})
	_ = cli.ContainerRemove(ctx, mks.EnforcerName, container.RemoveOptions{Force: true})

	// 3. Disconnect and remove internal network
	// Disconnect any remaining endpoints before removing
	logger.Info("meeseeks teardown: removing network", "id", mks.ID, "network", mks.NetworkName)
	_ = cli.NetworkRemove(ctx, mks.NetworkName)

	// 4. Audit logs persist — do NOT delete ~/.agency/audit/{meeseeks_id}/

	logger.Info("meeseeks teardown complete", "id", mks.ID)
	return nil
}
