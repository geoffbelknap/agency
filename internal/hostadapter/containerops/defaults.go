package containerops

import (
	_ "embed"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/docker/api/types/container"
)

//go:embed seccomp-workspace.json
var embeddedSeccomp string

type ContainerRole string

const (
	RoleWorkspace ContainerRole = "workspace"
	RoleEnforcer  ContainerRole = "enforcer"
	RoleInfra     ContainerRole = "infra"
	RoleMeeseeks  ContainerRole = "meeseeks"
)

type (
	HostConfig   = container.HostConfig
	Config       = container.Config
	HealthConfig = container.HealthConfig
	NetworkMode  = container.NetworkMode
)

func ptrInt64(v int64) *int64 { return &v }

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

func SeccompProfilePath(homeDir string) string {
	override := filepath.Join(homeDir, "infrastructure", "seccomp-workspace.json")
	if _, err := os.Stat(override); err == nil {
		return override
	}
	defaultDir := filepath.Join(homeDir, "runtime", "security")
	defaultPath := filepath.Join(defaultDir, "seccomp-workspace.json")
	if err := os.MkdirAll(defaultDir, 0o755); err == nil {
		if _, err := os.Stat(defaultPath); err == nil {
			return defaultPath
		}
		if err := os.WriteFile(defaultPath, []byte(embeddedSeccomp), 0o644); err == nil {
			return defaultPath
		}
	}
	return override
}

func WorkspaceSecurityOpts(homeDir, backend string) []string {
	opts := []string{"no-new-privileges:true"}
	switch strings.ToLower(strings.TrimSpace(backend)) {
	case "podman", "containerd":
		return opts
	}
	if profile := dockerSeccompProfile(homeDir); profile != "" {
		opts = append(opts, "seccomp="+profile)
	}
	return opts
}

func dockerSeccompProfile(homeDir string) string {
	profilePath := SeccompProfilePath(homeDir)
	if profilePath == "" {
		return strings.TrimSpace(embeddedSeccomp)
	}
	data, err := os.ReadFile(profilePath)
	if err != nil {
		return strings.TrimSpace(embeddedSeccomp)
	}
	return strings.TrimSpace(string(data))
}
