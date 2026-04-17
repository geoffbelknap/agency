package orchestrate

import (
	"context"

	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
)

// AuditDockerSocket checks all agency.managed containers for Docker socket mounts.
func (inf *Infra) AuditDockerSocket(ctx context.Context) []string {
	return runtimehost.AuditDockerSocket(ctx, inf.cli, inf.log)
}

func checkDockerSocketMounts(ctrs []runtimehost.ContainerState) []string {
	return runtimehost.CheckDockerSocketMounts(ctrs)
}
