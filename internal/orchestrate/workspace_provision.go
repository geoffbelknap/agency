package orchestrate

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"

	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/pkg/envfile"
)

// WorkspaceDeps declares pip/apt packages and env var mappings to provision
// into a workspace container at agent startup.
type WorkspaceDeps struct {
	Pip []string          `yaml:"pip" json:"pip,omitempty"`
	Apt []string          `yaml:"apt" json:"apt,omitempty"`
	Env map[string]string `yaml:"env" json:"env,omitempty"`
}

// IsEmpty returns true if no dependencies are declared.
func (d WorkspaceDeps) IsEmpty() bool {
	return len(d.Pip) == 0 && len(d.Apt) == 0 && len(d.Env) == 0
}

// Merge combines other into d. Pip and apt packages are deduplicated. On env
// var conflicts, d's existing value wins (first wins).
func (d *WorkspaceDeps) Merge(other WorkspaceDeps) {
	// Merge pip — deduplicate
	existing := make(map[string]struct{}, len(d.Pip))
	for _, p := range d.Pip {
		existing[p] = struct{}{}
	}
	for _, p := range other.Pip {
		if _, ok := existing[p]; !ok {
			d.Pip = append(d.Pip, p)
			existing[p] = struct{}{}
		}
	}

	// Merge apt — deduplicate
	existingApt := make(map[string]struct{}, len(d.Apt))
	for _, p := range d.Apt {
		existingApt[p] = struct{}{}
	}
	for _, p := range other.Apt {
		if _, ok := existingApt[p]; !ok {
			d.Apt = append(d.Apt, p)
			existingApt[p] = struct{}{}
		}
	}

	// Merge env — first wins
	if len(other.Env) > 0 {
		if d.Env == nil {
			d.Env = make(map[string]string, len(other.Env))
		}
		for k, v := range other.Env {
			if _, exists := d.Env[k]; !exists {
				d.Env[k] = v
			}
		}
	}
}

// workspaceTmpfs returns the tmpfs mounts for the workspace container.
// When pip packages are declared, /tmp gets exec permission so wrapper
// scripts for CLI tools can run.
func workspaceTmpfs(deps WorkspaceDeps) map[string]string {
	tmpOpts := "size=512M,noexec"
	if len(deps.Pip) > 0 {
		tmpOpts = "size=512M,exec"
	}
	return map[string]string{
		"/tmp": tmpOpts,
		"/run": "size=64M",
	}
}

// resolveWorkspaceEnv resolves env var values from declared mappings.
//
// Resolution rules:
//   - "${credential:name}" → scopedKey (the agent's scoped placeholder key)
//   - "${config:name}"     → value from ~/.agency/.env; missing keys resolve to ""
//   - static values        → passed through unchanged
func resolveWorkspaceEnv(declared map[string]string, home, scopedKey string) map[string]string {
	if len(declared) == 0 {
		return map[string]string{}
	}

	configVars := envfile.Load(filepath.Join(home, ".env"))
	// Merge config.yaml config: section (takes precedence over .env)
	for k, v := range config.Load().ConfigVars {
		configVars[k] = v
	}
	resolved := make(map[string]string, len(declared))

	for k, v := range declared {
		switch {
		case strings.HasPrefix(v, "${credential:") && strings.HasSuffix(v, "}"):
			// Any credential reference resolves to the agent's scoped key.
			resolved[k] = scopedKey
		case strings.HasPrefix(v, "${config:") && strings.HasSuffix(v, "}"):
			name := v[len("${config:")  : len(v)-1]
			resolved[k] = configVars[name] // empty string if missing
		default:
			resolved[k] = v
		}
	}
	return resolved
}


// provisionWorkspace installs declared apt/pip packages into the named
// container and then disables the package managers to prevent agent misuse.
func provisionWorkspace(ctx context.Context, cli *client.Client, containerName string, deps WorkspaceDeps, logger *log.Logger) error {
	if deps.IsEmpty() {
		return nil
	}

	if len(deps.Apt) > 0 {
		logger.Info("provisioning apt packages", "container", containerName, "packages", deps.Apt)
		cmd := []string{"sh", "-c", "apt-get update -qq && apt-get install -y " + strings.Join(deps.Apt, " ")}
		if err := dockerExecRoot(ctx, cli, containerName, cmd); err != nil {
			return fmt.Errorf("apt install %v: %w", deps.Apt, err)
		}
	}

	// Create a CA bundle that combines system CAs with the egress MITM CA.
	// This lets tools like pip and the limacharlie CLI verify TLS through the proxy.
	bundleCmd := []string{"sh", "-c",
		"cat /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/agency-egress-ca.pem > /tmp/ca-bundle.pem 2>/dev/null || true",
	}
	_ = dockerExec(ctx, cli, containerName, bundleCmd)

	if len(deps.Pip) > 0 {
		logger.Info("provisioning pip packages", "container", containerName, "packages", deps.Pip)
		// The workspace has a read-only rootfs with strict seccomp. pip install
		// fails because fsync is blocked. Instead: download wheels, then extract
		// with Python's zipfile module (no fsync needed).
		//
		// Layout: /tmp/pip-packages/ contains extracted Python packages.
		// PYTHONPATH and PATH are set in the container env to find them.
		// CLI entry points: /tmp/pip-packages/bin/ created via wrapper scripts.
		quoted := make([]string, len(deps.Pip))
		for i, p := range deps.Pip {
			quoted[i] = "'" + p + "'"
		}
		dlCmd := []string{"sh", "-c",
			"mkdir -p /tmp/pip-dl /tmp/pip-packages && " +
				"pip download --cert /tmp/ca-bundle.pem " +
				"--no-cache-dir --dest /tmp/pip-dl " + strings.Join(quoted, " "),
		}
		if err := dockerExec(ctx, cli, containerName, dlCmd); err != nil {
			return fmt.Errorf("pip download %v: %w", deps.Pip, err)
		}
		// Extract wheels into /tmp/pip-packages using Python zipfile (no fsync)
		extractCmd := []string{"python3", "-c",
			"import zipfile,glob,os\n" +
				"for w in glob.glob('/tmp/pip-dl/*.whl'):\n" +
				"  zipfile.ZipFile(w).extractall('/tmp/pip-packages')\n" +
				"print('extracted',len(glob.glob('/tmp/pip-dl/*.whl')),'wheels')",
		}
		if err := dockerExec(ctx, cli, containerName, extractCmd); err != nil {
			return fmt.Errorf("wheel extract: %w", err)
		}
		// Create CLI wrapper scripts for packages that have entry points.
		// Common pattern: package_name has __main__.py → python3 -m package_name
		wrapperCmd := []string{"sh", "-c",
			"mkdir -p /tmp/pip-packages/bin && " +
				"for pkg in /tmp/pip-packages/*/; do " +
				"  name=$(basename \"$pkg\"); " +
				"  if [ -f \"$pkg/__main__.py\" ]; then " +
				"    printf '#!/bin/sh\\nexec python3 -m %s \"$@\"\\n' \"$name\" > /tmp/pip-packages/bin/\"$name\"; " +
				"    chmod +x /tmp/pip-packages/bin/\"$name\"; " +
				"  fi; " +
				"done",
		}
		_ = dockerExec(ctx, cli, containerName, wrapperCmd)
		// Clean up downloads
		_ = dockerExec(ctx, cli, containerName, []string{"rm", "-rf", "/tmp/pip-dl"})
	}

	// Disable apt-get/apt/dpkg after install to prevent agent abuse.
	if len(deps.Apt) > 0 {
		logger.Info("disabling package managers", "container", containerName)
		disableCmd := []string{"sh", "-c",
			"for bin in apt-get apt dpkg; do " +
				"path=$(command -v $bin 2>/dev/null); " +
				"[ -n \"$path\" ] && mv \"$path\" \"${path}.disabled\" || true; " +
				"done",
		}
		if err := dockerExecRoot(ctx, cli, containerName, disableCmd); err != nil {
			// Non-fatal: log and continue. The install succeeded.
			logger.Warn("failed to disable package managers", "container", containerName, "err", err)
		}
	}

	return nil
}

// dockerExecRoot runs cmd inside containerName as root and waits for completion.
func dockerExecRoot(ctx context.Context, cli *client.Client, containerName string, cmd []string) error {
	return dockerExecAs(ctx, cli, containerName, "root", cmd)
}

// dockerExec runs cmd inside containerName as the default user.
func dockerExec(ctx context.Context, cli *client.Client, containerName string, cmd []string) error {
	return dockerExecAs(ctx, cli, containerName, "", cmd)
}

// dockerExecAs runs cmd inside containerName as the specified user.
func dockerExecAs(ctx context.Context, cli *client.Client, containerName, user string, cmd []string) error {
	execID, err := cli.ContainerExecCreate(ctx, containerName, container.ExecOptions{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
		User:         user,
	})
	if err != nil {
		return fmt.Errorf("exec create: %w", err)
	}

	resp, err := cli.ContainerExecAttach(ctx, execID.ID, container.ExecAttachOptions{})
	if err != nil {
		return fmt.Errorf("exec attach: %w", err)
	}
	defer resp.Close()

	// Drain multiplexed stdout/stderr to completion.
	var out bytes.Buffer
	if _, err := stdcopy.StdCopy(&out, &out, resp.Reader); err != nil {
		// Fallback: raw read
		out.Reset()
		_, _ = out.ReadFrom(resp.Reader)
	}

	inspect, err := cli.ContainerExecInspect(ctx, execID.ID)
	if err != nil {
		return fmt.Errorf("exec inspect: %w", err)
	}
	if inspect.ExitCode != 0 {
		return fmt.Errorf("exit code %d: %s", inspect.ExitCode, out.String())
	}
	return nil
}
