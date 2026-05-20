package cli

import (
	"errors"

	"github.com/spf13/cobra"
)

// newDashboardCmd is the cobra shim for `archigraph dashboard ...`. The
// real implementation lives in cmd/archigraph/dashboard.go and is wired
// in via activeHooks.RunDashboard.
//
// With no arguments (or `open`), opens the daemon's dashboard in the
// default browser — auto-starting the daemon if necessary.
// With `serve`, runs a standalone HTTP server for dev use.
func newDashboardCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "dashboard [serve] [flags]",
		Short:              "Open the dashboard in browser (or run standalone with 'serve')",
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, args []string) error {
			if activeHooks.RunDashboard == nil {
				return errors.New("dashboard handler not wired")
			}
			return activeHooks.RunDashboard(args)
		},
	}
}
