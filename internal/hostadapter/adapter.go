package hostadapter

import (
	"context"

	"github.com/geoffbelknap/agency/internal/credstore"
	"github.com/geoffbelknap/agency/internal/orchestrate"
)

type DeployOptions struct {
	Home        string
	Version     string
	SourceDir   string
	BuildID     string
	Credentials map[string]string
	CredStore   *credstore.Store
}

type Adapter interface {
	Backend() string
	CountRunningMeeseeks(ctx context.Context) (int, error)
	PruneDanglingAgencyImages(ctx context.Context) (pruned, skipped int, err error)
	TeardownInfrastructure(ctx context.Context, infra *orchestrate.Infra) error
	DryRunDeployPack(ctx context.Context, opts DeployOptions, pack *orchestrate.PackDef, onStatus func(string)) (*orchestrate.DeployResult, error)
	DeployPack(ctx context.Context, opts DeployOptions, pack *orchestrate.PackDef, onStatus func(string)) (*orchestrate.DeployResult, error)
	TeardownPack(ctx context.Context, opts DeployOptions, packName string, delete bool) error
}
