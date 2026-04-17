package containers

import "github.com/geoffbelknap/agency/internal/hostadapter/containerops"

type (
	ContainerRole = containerops.ContainerRole
	HostConfig    = containerops.HostConfig
	Config        = containerops.Config
	HealthConfig  = containerops.HealthConfig
)

const (
	RoleWorkspace = containerops.RoleWorkspace
	RoleEnforcer  = containerops.RoleEnforcer
	RoleInfra     = containerops.RoleInfra
	RoleMeeseeks  = containerops.RoleMeeseeks
)

func HostConfigDefaults(role ContainerRole) *HostConfig { return containerops.HostConfigDefaults(role) }
func SeccompProfilePath(homeDir string) string          { return containerops.SeccompProfilePath(homeDir) }
func WorkspaceSecurityOpts(homeDir, backend string) []string {
	return containerops.WorkspaceSecurityOpts(homeDir, backend)
}
