package cli

import (
	"github.com/spf13/cobra"
)

func newHelpCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "help [topic]",
		Short: "Show help; use `help advanced` for the full surface",
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) > 0 && args[0] == "advanced" {
				cmd.OutOrStdout().Write([]byte(advancedHelpText))
				return
			}
			_ = cmd.Root().Help()
		},
	}
	return cmd
}
