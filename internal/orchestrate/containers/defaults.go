package containers

import (
	_ "embed"
	"os"
	"path/filepath"

	"github.com/docker/docker/api/types/container"
)

//go:embed seccomp-workspace.json
var embeddedSeccomp string

// ContainerRole identifies the functional role of a container.
type ContainerRole string

const (
	RoleWorkspace ContainerRole = "workspace"
	RoleEnforcer  ContainerRole = "enforcer"
	RoleInfra     ContainerRole = "infra"
	RoleMeeseeks  ContainerRole = "meeseeks"
)

func ptrInt64(v int64) *int64 { return &v }

// HostConfigDefaults returns a baseline HostConfig for the given role.
// Callers overlay their specific Binds, NetworkMode, Tmpfs, ExtraHosts.
func HostConfigDefaults(role ContainerRole) *container.HostConfig {
	hc := &container.HostConfig{
		CapDrop:        []string{"ALL"},
		CapAdd:         []string{"NET_BIND_SERVICE"},
		SecurityOpt:    []string{"no-new-privileges:true"},
		ReadonlyRootfs: true,
	}

	switch role {
	case RoleWorkspace:
		hc.Resources = container.Resources{
			Memory:    512 * 1024 * 1024,
			NanoCPUs:  2_000_000_000,
			PidsLimit: ptrInt64(512),
		}
		hc.RestartPolicy = container.RestartPolicy{Name: "on-failure", MaximumRetryCount: 3}
		hc.LogConfig = container.LogConfig{
			Type:   "json-file",
			Config: map[string]string{"max-size": "10m", "max-file": "3"},
		}

	case RoleEnforcer:
		hc.Resources = container.Resources{
			Memory:    128 * 1024 * 1024,
			NanoCPUs:  500_000_000,
			PidsLimit: ptrInt64(256),
		}
		hc.RestartPolicy = container.RestartPolicy{Name: "on-failure", MaximumRetryCount: 3}
		hc.LogConfig = container.LogConfig{
			Type:   "json-file",
			Config: map[string]string{"max-size": "10m", "max-file": "3"},
		}

	case RoleInfra:
		hc.Resources = container.Resources{
			Memory:    256 * 1024 * 1024,
			NanoCPUs:  1_000_000_000,
			PidsLimit: ptrInt64(1024),
		}
		hc.RestartPolicy = container.RestartPolicy{Name: "unless-stopped"}
		hc.LogConfig = container.LogConfig{
			Type:   "json-file",
			Config: map[string]string{"max-size": "10m", "max-file": "3"},
		}
		hc.ReadonlyRootfs = false

	case RoleMeeseeks:
		hc.Resources = container.Resources{
			Memory:    512 * 1024 * 1024,
			NanoCPUs:  1_000_000_000,
			PidsLimit: ptrInt64(512),
		}
		hc.RestartPolicy = container.RestartPolicy{Name: "no"}
		hc.LogConfig = container.LogConfig{
			Type:   "json-file",
			Config: map[string]string{"max-size": "5m", "max-file": "2"},
		}
	}

	return hc
}

// SeccompProfile returns the seccomp JSON string for workspace containers.
// It checks {homeDir}/infrastructure/seccomp-workspace.json first (on-disk
// override) and falls back to the embedded profile.
func SeccompProfile(homeDir string) string {
	override := filepath.Join(homeDir, "infrastructure", "seccomp-workspace.json")
	data, err := os.ReadFile(override)
	if err == nil {
		return string(data)
	}
	return embeddedSeccomp
}
