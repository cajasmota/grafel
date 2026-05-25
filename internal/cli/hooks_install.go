package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cajasmota/archigraph/internal/install"
)

// newInstallHooksCmd returns the `archigraph install-hooks` subcommand.
//
// It installs a pre-push git hook into the current (or specified) repo's
// .git/hooks/pre-push that runs `archigraph doctor --quick` before every
// push. The hook warns on drift but never blocks the push.
//
// If husky, lefthook, or pre-commit is detected in the repo, the command
// prints advice on how to add the hook via those tools instead of writing
// directly to .git/hooks/.
func newInstallHooksCmd() *cobra.Command {
	var (
		repoPath string
		dryRun   bool
		force    bool
	)

	cmd := &cobra.Command{
		Use:   "install-hooks",
		Short: "Install the archigraph pre-push hook into the current repo",
		Long: `Install a pre-push git hook that runs 'archigraph doctor --quick'
before every push. The hook warns on drift but never blocks the push.

If husky, lefthook, or pre-commit is detected in the repo, the command
prints instructions for adding the hook via those tools instead.

The hook is idempotent: running install-hooks a second time replaces
the existing managed block without touching user-written hook content.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()

			opts := install.HookInstallOptions{
				RepoPath: repoPath,
				DryRun:   dryRun,
				Force:    force,
			}

			if err := install.InstallPrePushHook(opts); err != nil {
				fmt.Fprintf(out, "✗ install-hooks failed: %v\n", err)
				return err
			}

			if !dryRun {
				fmt.Fprintln(out, "✓ archigraph pre-push hook installed")
				fmt.Fprintln(out, "  The hook runs 'archigraph doctor --quick' before every push.")
				fmt.Fprintln(out, "  It warns on drift but never blocks the push.")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&repoPath, "repo", "",
		"path to the git repository (default: current working directory)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false,
		"print what would be written without making changes")
	cmd.Flags().BoolVar(&force, "force", false,
		"overwrite an existing pre-push hook (replaces the managed block)")
	return cmd
}
