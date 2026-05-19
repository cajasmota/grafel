package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cajasmota/archigraph/internal/daemon/service"
)

// newUninstallCmd returns the `archigraph uninstall` subcommand.
//
// Per ADR-0017 Phase C the old "remove from a group" semantic is
// REMOVED. `archigraph uninstall` now stops and deregisters the
// daemon OS service (launchd plist / systemd unit). Idempotent: if
// the service is not installed the command succeeds silently.
func newUninstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Stop and remove the archigraph daemon service",
		Long: `Uninstall stops the archigraph daemon and removes its OS service
registration (launchd plist on macOS, systemd unit on Linux).

Idempotent: if the service is not installed the command exits 0 without
printing an error.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			if err := service.Uninstall(service.Options{}); err != nil {
				return err
			}
			fmt.Fprintln(out, "✓ archigraph daemon removed")
			return nil
		},
	}
	return cmd
}
