package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cajasmota/archigraph/internal/install"
	"github.com/cajasmota/archigraph/internal/install/mcpreg"
)

func newUninstallCmd() *cobra.Command {
	var purge bool
	cmd := &cobra.Command{
		Use:   "uninstall [group]",
		Short: "Remove archigraph from a group",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errors.New("uninstall expects exactly one group name")
			}
			group := args[0]
			if err := install.Uninstall(group, purge); err != nil {
				return err
			}
			// Best-effort: drop the MCP entry too if no other groups remain.
			_ = mcpreg.Unregister(mcpreg.ClaudeCode)
			_ = mcpreg.Unregister(mcpreg.Windsurf)
			fmt.Fprintf(cmd.OutOrStdout(), "uninstalled group %q (purge=%v)\n", group, purge)
			return nil
		},
	}
	cmd.Flags().BoolVar(&purge, "purge", false, "also delete per-group state and config")
	return cmd
}
