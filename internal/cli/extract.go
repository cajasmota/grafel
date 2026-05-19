package cli

import (
	"errors"

	"github.com/spf13/cobra"
)

// newExtractCmd registers the hidden `archigraph extract` subcommand.
// This is the subprocess-side entrypoint for Phase F: the daemon's
// extract coordinator fork-execs the same binary with `extract --lang
// --batch ...` to run per-file passes (1, 2.5, 3) on a bounded set of
// files, streaming JSONL records back over stdout.
//
// The command is hidden from the user-facing help surface: it is not
// intended for direct invocation. It is reachable via `archigraph
// extract` for diagnostics, but the help template hides it.
func newExtractCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                "extract",
		Short:              "(internal) Run extractors on a bounded batch — Phase F subprocess",
		Hidden:             true,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if activeHooks.RunExtract == nil {
				return errors.New("extract subcommand not wired (build error)")
			}
			return activeHooks.RunExtract(args)
		},
	}
	return cmd
}
