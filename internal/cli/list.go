package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cajasmota/archigraph/internal/registry"
)

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List registered groups",
		RunE: func(cmd *cobra.Command, _ []string) error {
			groups, err := registry.Groups()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(groups) == 0 {
				fmt.Fprintln(out, "No groups registered. Run `archigraph wizard` to create one.")
				return nil
			}
			fmt.Fprintf(out, "%-24s  %s\n", "GROUP", "CONFIG")
			for _, g := range groups {
				fmt.Fprintf(out, "%-24s  %s\n", g.Name, g.ConfigPath)
			}
			return nil
		},
	}
}
