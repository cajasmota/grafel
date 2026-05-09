package cli

import (
	"errors"

	"github.com/spf13/cobra"
)

// newDashboardCmd is the cobra shim for `archigraph dashboard ...`. The
// real implementation lives in cmd/archigraph/dashboard.go and is wired
// in via activeHooks.RunDashboard.
func newDashboardCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "dashboard <serve> [flags]",
		Short:              "Run the local archigraph dashboard server",
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, args []string) error {
			if activeHooks.RunDashboard == nil {
				return errors.New("dashboard handler not wired")
			}
			return activeHooks.RunDashboard(args)
		},
	}
}
