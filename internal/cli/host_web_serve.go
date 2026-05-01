package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/geoffbelknap/agency/internal/webhost"
)

func hostWebServeCmd() *cobra.Command {
	var opts webhost.Options
	cmd := &cobra.Command{
		Use:    "host-web-serve",
		Short:  "Serve bundled web assets for host-managed web",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.AgencyHome == "" {
				opts.AgencyHome = os.Getenv("AGENCY_HOME")
			}
			if err := webhost.Serve(opts); err != nil {
				return fmt.Errorf("serve host web: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.DistDir, "dist-dir", "", "web dist directory")
	cmd.Flags().StringVar(&opts.Host, "host", "127.0.0.1", "listen host")
	cmd.Flags().StringVar(&opts.Port, "port", "8280", "listen port")
	_ = cmd.MarkFlagRequired("dist-dir")
	return cmd
}
