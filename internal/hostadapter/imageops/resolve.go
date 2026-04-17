package imageops

import (
	"context"
	"log/slog"

	"github.com/geoffbelknap/agency/internal/artifacts"
	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
	"github.com/geoffbelknap/agency/internal/images"
)

const (
	registry       = "ghcr.io/geoffbelknap"
	OllamaUpstream = artifacts.OllamaUpstream
	OllamaVersion  = artifacts.OllamaVersion
)

type dockerResolver struct {
	cli *runtimehost.RawClient
}

func Resolve(ctx context.Context, cli *runtimehost.RawClient, name, version, sourceDir, buildID string, logger *slog.Logger) error {
	return artifacts.Resolve(ctx, dockerResolver{cli: cli}, name, version, sourceDir, buildID, registry, logger)
}

func ResolveUpstream(ctx context.Context, cli *runtimehost.RawClient, name, version, upstreamRef, buildID string, logger *slog.Logger) error {
	return artifacts.ResolveUpstream(ctx, dockerResolver{cli: cli}, name, version, upstreamRef, buildID, registry, logger)
}

func ImageBuildLabel(ctx context.Context, cli *runtimehost.RawClient, ref string) string {
	return images.ImageBuildLabel(ctx, cli, ref)
}

func (r dockerResolver) Exists(ctx context.Context, ref string) (bool, error) {
	return images.ImageExists(ctx, r.cli, ref)
}

func (r dockerResolver) Label(ctx context.Context, ref, key string) string {
	return images.ImageLabel(ctx, r.cli, ref, key)
}

func (r dockerResolver) BuildLabel(ctx context.Context, ref string) string {
	return images.ImageBuildLabel(ctx, r.cli, ref)
}

func (r dockerResolver) BuildServiceFromSource(ctx context.Context, name, sourceDir, tag, buildID, sourceHash string, logger *slog.Logger) error {
	return images.BuildFromSource(ctx, r.cli, name, sourceDir, tag, buildID, sourceHash, logger)
}

func (r dockerResolver) Tag(ctx context.Context, sourceRef, targetRef string) error {
	return r.cli.ImageTag(ctx, sourceRef, targetRef)
}

func (r dockerResolver) PullAndTag(ctx context.Context, remoteRef, localRef string) error {
	return images.PullAndTag(ctx, r.cli, remoteRef, localRef)
}

func (r dockerResolver) PruneOld(ctx context.Context, name, currentBuildID string, logger *slog.Logger) {
	images.PruneOldImages(ctx, r.cli, name, currentBuildID, logger)
}
