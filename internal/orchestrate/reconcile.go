package orchestrate

import (
	"context"

	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
	"log/slog"
)

func Reconcile(ctx context.Context, cli *runtimehost.RawClient, knownAgents []string, logger *slog.Logger) {
	runtimehost.Reconcile(ctx, cli, knownAgents, logger)
}
