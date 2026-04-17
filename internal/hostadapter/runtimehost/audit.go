package runtimehost

import (
	"context"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	dockerclient "github.com/docker/docker/client"
	"log/slog"
)

func AuditDockerSocket(ctx context.Context, cli *dockerclient.Client, logger *slog.Logger) []string {
	containers, err := cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", "agency.managed=true")),
	})
	if err != nil {
		if logger != nil {
			logger.Warn("docker socket audit: failed to list containers", "error", err)
		}
		return nil
	}

	violations := CheckDockerSocketMounts(containers)
	for _, name := range violations {
		if logger != nil {
			logger.Error("SECURITY: container has Docker socket mounted", "container", name)
		}
	}
	return violations
}

func CheckDockerSocketMounts(ctrs []container.Summary) []string {
	var violations []string
	for _, c := range ctrs {
		if c.Labels["agency.managed"] != "true" {
			continue
		}
		for _, m := range c.Mounts {
			if strings.Contains(m.Source, "docker.sock") {
				name := ""
				if len(c.Names) > 0 {
					name = c.Names[0]
				}
				violations = append(violations, name)
				break
			}
		}
	}
	return violations
}
