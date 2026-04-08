package orchestrate

import (
	"context"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
)

// AuditDockerSocket checks all agency.managed containers for Docker socket mounts.
func (inf *Infra) AuditDockerSocket(ctx context.Context) []string {
	containers, err := inf.cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", "agency.managed=true")),
	})
	if err != nil {
		inf.log.Warn("docker socket audit: failed to list containers", "error", err)
		return nil
	}

	violations := checkDockerSocketMounts(containers)
	for _, name := range violations {
		inf.log.Error("SECURITY: container has Docker socket mounted", "container", name)
	}
	return violations
}

func checkDockerSocketMounts(ctrs []container.Summary) []string {
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
