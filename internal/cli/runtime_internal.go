package cli

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"

	runtimepkg "github.com/geoffbelknap/agency/internal/runtime"
	"github.com/spf13/cobra"
)

func runtimeAuthorityServeCmd() *cobra.Command {
	var instanceDir string
	var nodeID string
	var port int

	cmd := &cobra.Command{
		Use:    "runtime-authority-serve",
		Short:  "Run a local authority runtime for an instance node",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if instanceDir == "" {
				return errors.New("--instance-dir is required")
			}
			if nodeID == "" {
				return errors.New("--node-id is required")
			}
			if port <= 0 {
				return errors.New("--port must be greater than zero")
			}

			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			return runtimepkg.ServeAuthorityFromInstanceDir(ctx, instanceDir, nodeID, port)
		},
	}

	cmd.Flags().StringVar(&instanceDir, "instance-dir", "", "instance state directory")
	cmd.Flags().StringVar(&nodeID, "node-id", "", "authority node id")
	cmd.Flags().IntVar(&port, "port", 0, "listen port")
	return cmd
}
