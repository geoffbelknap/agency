package agentruntime

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"log/slog"

	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
	"github.com/geoffbelknap/agency/internal/pkg/envfile"
)

type WorkspaceDeps struct {
	Pip []string          `yaml:"pip" json:"pip,omitempty"`
	Apt []string          `yaml:"apt" json:"apt,omitempty"`
	Env map[string]string `yaml:"env" json:"env,omitempty"`
}

func (d WorkspaceDeps) IsEmpty() bool {
	return len(d.Pip) == 0 && len(d.Apt) == 0 && len(d.Env) == 0
}

func (d *WorkspaceDeps) Merge(other WorkspaceDeps) {
	existingPip := make(map[string]struct{}, len(d.Pip))
	for _, item := range d.Pip {
		existingPip[item] = struct{}{}
	}
	for _, item := range other.Pip {
		if _, ok := existingPip[item]; !ok {
			d.Pip = append(d.Pip, item)
			existingPip[item] = struct{}{}
		}
	}

	existingApt := make(map[string]struct{}, len(d.Apt))
	for _, item := range d.Apt {
		existingApt[item] = struct{}{}
	}
	for _, item := range other.Apt {
		if _, ok := existingApt[item]; !ok {
			d.Apt = append(d.Apt, item)
			existingApt[item] = struct{}{}
		}
	}

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

func resolveWorkspaceEnv(declared map[string]string, home, scopedKey string) map[string]string {
	if len(declared) == 0 {
		return map[string]string{}
	}

	configVars := envfile.Load(filepath.Join(home, ".env"))
	for k, v := range config.Load().ConfigVars {
		configVars[k] = v
	}

	resolved := make(map[string]string, len(declared))
	for k, v := range declared {
		switch {
		case strings.HasPrefix(v, "${credential:") && strings.HasSuffix(v, "}"):
			resolved[k] = scopedKey
		case strings.HasPrefix(v, "${config:") && strings.HasSuffix(v, "}"):
			name := v[len("${config:") : len(v)-1]
			resolved[k] = configVars[name]
		default:
			resolved[k] = v
		}
	}
	return resolved
}

func ResolveWorkspaceEnv(declared map[string]string, home, scopedKey string) map[string]string {
	return resolveWorkspaceEnv(declared, home, scopedKey)
}

func provisionWorkspace(ctx context.Context, cli *runtimehost.RawClient, containerName string, deps WorkspaceDeps, logger *slog.Logger) error {
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

	bundleCmd := []string{"sh", "-c", "cat /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/agency-egress-ca.pem > /tmp/ca-bundle.pem 2>/dev/null || true"}
	if err := dockerExec(ctx, cli, containerName, bundleCmd); err != nil {
		logger.Warn("CA bundle creation failed (TLS through egress may not work)", "err", err)
	}

	if len(deps.Pip) > 0 {
		logger.Info("provisioning pip packages", "container", containerName, "packages", deps.Pip)
		quoted := make([]string, len(deps.Pip))
		for i, pkg := range deps.Pip {
			quoted[i] = "'" + pkg + "'"
		}
		dlCmd := []string{"sh", "-c",
			"mkdir -p /tmp/pip-dl /tmp/pip-packages && " +
				"pip download --cert /tmp/ca-bundle.pem " +
				"--no-cache-dir --dest /tmp/pip-dl " + strings.Join(quoted, " "),
		}
		if err := dockerExec(ctx, cli, containerName, dlCmd); err != nil {
			return fmt.Errorf("pip download %v: %w", deps.Pip, err)
		}
		extractCmd := []string{"python3", "-c",
			"import zipfile,glob,os\n" +
				"for w in glob.glob('/tmp/pip-dl/*.whl'):\n" +
				"  zipfile.ZipFile(w).extractall('/tmp/pip-packages')\n" +
				"print('extracted',len(glob.glob('/tmp/pip-dl/*.whl')),'wheels')",
		}
		if err := dockerExec(ctx, cli, containerName, extractCmd); err != nil {
			return fmt.Errorf("wheel extract: %w", err)
		}
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
		if err := dockerExec(ctx, cli, containerName, wrapperCmd); err != nil {
			logger.Warn("CLI wrapper creation failed (pip entry points may not work)", "err", err)
		}
		_ = dockerExec(ctx, cli, containerName, []string{"rm", "-rf", "/tmp/pip-dl"})
	}

	if len(deps.Apt) > 0 {
		logger.Info("disabling package managers", "container", containerName)
		disableCmd := []string{"sh", "-c",
			"for bin in apt-get apt dpkg; do " +
				"path=$(command -v $bin 2>/dev/null); " +
				"[ -n \"$path\" ] && mv \"$path\" \"${path}.disabled\" || true; " +
				"done",
		}
		if err := dockerExecRoot(ctx, cli, containerName, disableCmd); err != nil {
			logger.Warn("failed to disable package managers", "container", containerName, "err", err)
		}
	}
	return nil
}

func dockerExecRoot(ctx context.Context, cli *runtimehost.RawClient, containerName string, cmd []string) error {
	return dockerExecAs(ctx, cli, containerName, "root", cmd)
}

func dockerExec(ctx context.Context, cli *runtimehost.RawClient, containerName string, cmd []string) error {
	return dockerExecAs(ctx, cli, containerName, "", cmd)
}

func dockerExecAs(ctx context.Context, cli *runtimehost.RawClient, containerName, user string, cmd []string) error {
	out, err := cli.Exec(ctx, containerName, user, cmd)
	if err != nil {
		return fmt.Errorf("%w: %s", err, out)
	}
	return nil
}
