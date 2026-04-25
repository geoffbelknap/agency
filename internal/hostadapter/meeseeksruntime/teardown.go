package meeseeksruntime

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/container"
	"log/slog"

	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
	"github.com/geoffbelknap/agency/internal/models"
)

func TeardownMeeseeks(ctx context.Context, mks *models.Meeseeks, logger *slog.Logger, dc ...*runtimehost.Client) error {
	var cli *runtimehost.RawClient
	if len(dc) > 0 && dc[0] != nil {
		cli = dc[0].RawClient()
	} else {
		var err error
		cli, err = runtimehost.NewRawClient()
		if err != nil {
			return fmt.Errorf("runtime backend client: %w", err)
		}
	}

	timeout := 10
	logger.Info("meeseeks teardown: stopping workspace", "id", mks.ID, "container", mks.ContainerName)
	_ = cli.ContainerStop(ctx, mks.ContainerName, container.StopOptions{Timeout: &timeout})
	_ = cli.ContainerRemove(ctx, mks.ContainerName, container.RemoveOptions{Force: true})

	logger.Info("meeseeks teardown: stopping enforcer", "id", mks.ID, "container", mks.EnforcerName)
	_ = cli.ContainerStop(ctx, mks.EnforcerName, container.StopOptions{Timeout: &timeout})
	_ = cli.ContainerRemove(ctx, mks.EnforcerName, container.RemoveOptions{Force: true})

	logger.Info("meeseeks teardown: removing network", "id", mks.ID, "network", mks.NetworkName)
	_ = cli.NetworkRemove(ctx, mks.NetworkName)
	logger.Info("meeseeks teardown complete", "id", mks.ID)
	return nil
}
