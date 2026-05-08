package cli

import (
	"errors"

	"github.com/spf13/cobra"
)

// newIndexCmd is a thin shim that forwards to the cmd/main-provided
// implementation through the activeHooks indirection.
func newIndexCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                "index <repo>",
		Short:              "Walk a repository and write graph.json",
		DisableFlagParsing: true, // delegate to the cmd-package flag.FlagSet
		RunE: func(_ *cobra.Command, args []string) error {
			if activeHooks.RunIndex == nil {
				return errors.New("index handler not wired")
			}
			return activeHooks.RunIndex(args)
		},
	}
	return cmd
}

func newMCPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                "mcp",
		Short:              "MCP server controls",
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, args []string) error {
			if activeHooks.RunMCP == nil {
				return errors.New("mcp handler not wired")
			}
			return activeHooks.RunMCP(args)
		},
	}
	return cmd
}
