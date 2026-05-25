package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cajasmota/archigraph/internal/install"
)

// newInstallHooksCmd returns the `archigraph install-hooks` subcommand.
//
// It installs 4 git hooks into the current (or specified) repo's .git/hooks/:
//   - pre-push        — runs `archigraph doctor --quick` before every push
//   - post-checkout   — signals daemon on branch switch
//   - post-merge      — signals daemon after merge
//   - post-rewrite    — signals daemon after rebase/amend
//
// If husky, lefthook, or pre-commit is detected in the repo, the command
// prints advice on how to add the hooks via those tools instead of writing
// directly to .git/hooks/.
func newInstallHooksCmd() *cobra.Command {
	var (
		repoPath string
		dryRun   bool
		force    bool
	)

	cmd := &cobra.Command{
		Use:   "install-hooks",
		Short: "Install archigraph git hooks into the current repo (pre-push, post-checkout, post-merge, post-rewrite)",
		Long: `Install 4 archigraph-managed git hooks into the repo's .git/hooks/:

  pre-push        Runs 'archigraph doctor --quick' before every push.
                  Warns on drift but never blocks the push.
  post-checkout   Signals the daemon to mark the new ref as HOT when
                  switching branches.
  post-merge      Signals the daemon to trigger an incremental reindex
                  after a git merge or git pull.
  post-rewrite    Signals the daemon to trigger an incremental reindex
                  after a git rebase or git commit --amend.

All hooks are idempotent: running install-hooks a second time replaces
the managed block without touching user-written hook content.

If husky, lefthook, or pre-commit is detected, the command prints
instructions for adding the hooks via those tools instead.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()

			opts := install.HookInstallOptions{
				RepoPath: repoPath,
				DryRun:   dryRun,
				Force:    force,
			}

			if err := install.InstallGitHooks(opts); err != nil {
				fmt.Fprintf(out, "✗ install-hooks failed: %v\n", err)
				return err
			}

			if !dryRun {
				fmt.Fprintln(out, "✓ archigraph git hooks installed (pre-push, post-checkout, post-merge, post-rewrite)")
				fmt.Fprintln(out, "  pre-push:      runs 'archigraph doctor --quick' before every push")
				fmt.Fprintln(out, "  post-checkout: signals daemon on branch switch")
				fmt.Fprintln(out, "  post-merge:    signals daemon after merge")
				fmt.Fprintln(out, "  post-rewrite:  signals daemon after rebase/amend")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&repoPath, "repo", "",
		"path to the git repository (default: current working directory)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false,
		"print what would be written without making changes")
	cmd.Flags().BoolVar(&force, "force", false,
		"overwrite existing hooks (replaces the managed block)")
	return cmd
}
