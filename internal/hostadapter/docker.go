package hostadapter

import (
	"context"
	"fmt"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"log/slog"

	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
	"github.com/geoffbelknap/agency/internal/orchestrate"
)

type ContainerAdapter struct {
	dc      *runtimehost.Client
	logger  *slog.Logger
	backend string
}

type DockerAdapter = ContainerAdapter
type PodmanAdapter = ContainerAdapter
type ContainerdAdapter = ContainerAdapter

func NewDockerAdapter(dc *runtimehost.Client, logger *slog.Logger) *ContainerAdapter {
	return &ContainerAdapter{dc: dc, logger: logger, backend: runtimehost.BackendDocker}
}

func NewPodmanAdapter(dc *runtimehost.Client, logger *slog.Logger) *ContainerAdapter {
	return &ContainerAdapter{dc: dc, logger: logger, backend: runtimehost.BackendPodman}
}

func NewContainerdAdapter(dc *runtimehost.Client, logger *slog.Logger) *ContainerAdapter {
	return &ContainerAdapter{dc: dc, logger: logger, backend: runtimehost.BackendContainerd}
}

func NewAdapter(backend string, dc *runtimehost.Client, logger *slog.Logger) Adapter {
	switch strings.TrimSpace(strings.ToLower(backend)) {
	case runtimehost.BackendContainerd:
		return NewContainerdAdapter(dc, logger)
	case runtimehost.BackendPodman:
		return NewPodmanAdapter(dc, logger)
	case "", runtimehost.BackendDocker:
		return NewDockerAdapter(dc, logger)
	default:
		return nil
	}
}

func (a *ContainerAdapter) Backend() string {
	return a.backend
}

func (a *ContainerAdapter) requireContainerBackend() error {
	if a == nil || a.dc == nil {
		return fmt.Errorf("%s client is not initialized", strings.TrimSpace(a.backend))
	}
	return nil
}

func (a *ContainerAdapter) ListRunningAgents(ctx context.Context) ([]string, error) {
	if err := a.requireContainerBackend(); err != nil {
		return nil, err
	}
	return a.dc.ListAgentWorkspaces(ctx)
}

func (a *ContainerAdapter) CountRunningMeeseeks(ctx context.Context) (int, error) {
	if err := a.requireContainerBackend(); err != nil {
		return 0, err
	}
	containers, err := a.dc.RawClient().ContainerList(ctx, container.ListOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", "agency.role=meeseeks"),
			filters.Arg("status", "running"),
		),
	})
	if err != nil {
		return 0, err
	}
	return len(containers), nil
}

func (a *ContainerAdapter) PruneDanglingAgencyImages(ctx context.Context) (pruned, skipped int, err error) {
	if err := a.requireContainerBackend(); err != nil {
		return 0, 0, err
	}
	images, err := a.dc.ListDanglingAgencyImages(ctx)
	if err != nil {
		return 0, 0, err
	}
	for _, image := range images {
		if _, err := a.dc.RemoveImage(ctx, image.ID); err != nil {
			if a.logger != nil {
				a.logger.Debug("prune untagged image skip", "id", image.ID, "err", err)
			}
			skipped++
			continue
		}
		if a.logger != nil {
			a.logger.Info("pruned dangling image", "id", image.ID)
		}
		pruned++
	}
	return pruned, skipped, nil
}

func (a *ContainerAdapter) TeardownInfrastructure(ctx context.Context, infra *orchestrate.Infra) error {
	if err := a.requireContainerBackend(); err != nil {
		return err
	}
	if infra == nil {
		return nil
	}
	return infra.Teardown(ctx)
}

func (a *ContainerAdapter) DryRunDeployPack(ctx context.Context, opts DeployOptions, pack *orchestrate.PackDef, onStatus func(string)) (*orchestrate.DeployResult, error) {
	if err := a.requireContainerBackend(); err != nil {
		return nil, err
	}
	deployer := &dockerDeployer{
		BackendName: optsBackendName(a.backend),
		Home:        opts.Home,
		Version:     opts.Version,
		SourceDir:   opts.SourceDir,
		BuildID:     opts.BuildID,
		Docker:      a.dc,
		Logger:      a.logger,
		Credentials: opts.Credentials,
		CredStore:   opts.CredStore,
	}
	return deployer.dryRun(ctx, pack, onStatus)
}

func (a *ContainerAdapter) DeployPack(ctx context.Context, opts DeployOptions, pack *orchestrate.PackDef, onStatus func(string)) (*orchestrate.DeployResult, error) {
	if err := a.requireContainerBackend(); err != nil {
		return nil, err
	}
	deployer := &dockerDeployer{
		BackendName: optsBackendName(a.backend),
		Home:        opts.Home,
		Version:     opts.Version,
		SourceDir:   opts.SourceDir,
		BuildID:     opts.BuildID,
		Docker:      a.dc,
		Logger:      a.logger,
		Credentials: opts.Credentials,
		CredStore:   opts.CredStore,
	}
	return deployer.deploy(ctx, pack, onStatus)
}

func (a *ContainerAdapter) TeardownPack(ctx context.Context, opts DeployOptions, packName string, delete bool) error {
	if err := a.requireContainerBackend(); err != nil {
		return err
	}
	deployer := &dockerDeployer{
		BackendName: optsBackendName(a.backend),
		Home:        opts.Home,
		Version:     opts.Version,
		Docker:      a.dc,
		Logger:      a.logger,
		CredStore:   opts.CredStore,
	}
	return deployer.teardown(ctx, packName, delete)
}

func optsBackendName(name string) string {
	if normalized := runtimehost.NormalizeContainerBackend(name); normalized != "" {
		return normalized
	}
	return runtimehost.BackendDocker
}
