package main

import (
	"fmt"
	"os"
	"strings"

	hostruntimebackend "github.com/geoffbelknap/agency/internal/hostadapter/runtimebackend"
	"github.com/geoffbelknap/agency/internal/runtimeprovision"
	"github.com/spf13/cobra"
)

func runtimeProvisionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "provision",
		Short: "Provision pinned runtime artifacts",
	}

	var force bool
	firecracker := &cobra.Command{
		Use:   "firecracker",
		Short: "Provision pinned Firecracker binary and Agency vmlinux artifacts",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, cfg, err := selectRuntimeBackend(hostruntimebackend.BackendFirecracker)
			if err != nil {
				return err
			}
			if err := runtimeprovision.ProvisionFirecracker(cmd.Context(), runtimeprovision.FirecrackerOptions{
				AgencyVersion:        version,
				Home:                 configHome(),
				BinaryPath:           cfg["binary_path"],
				KernelPath:           cfg["kernel_path"],
				FirecrackerBaseURL:   strings.TrimSpace(os.Getenv("AGENCY_FIRECRACKER_RELEASE_BASE_URL")),
				KernelReleaseBaseURL: strings.TrimSpace(os.Getenv("AGENCY_FIRECRACKER_KERNEL_RELEASE_BASE_URL")),
				Force:                force,
				Logf: func(format string, args ...any) {
					fmt.Fprintf(cmd.OutOrStdout(), format+"\n", args...)
				},
			}); err != nil {
				return err
			}
			if err := verifyFirecrackerRuntimeArtifacts(cfg); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Firecracker runtime artifacts are ready.")
			return nil
		},
	}
	firecracker.Flags().BoolVar(&force, "force", false, "Re-download pinned artifacts even if existing files are present")
	cmd.AddCommand(firecracker)
	return cmd
}
