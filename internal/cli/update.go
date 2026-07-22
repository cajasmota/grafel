package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/cajasmota/grafel/internal/install"
	"github.com/cajasmota/grafel/internal/install/hooks"
	"github.com/cajasmota/grafel/internal/install/mcpreg"
	"github.com/cajasmota/grafel/internal/registry"
)

func newUpdateCmd() *cobra.Command {
	var (
		// Legacy flags (preserved for backward compat).
		refreshLite bool

		// New self-update flags (#2213).
		tag string
		pre bool
	)
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update grafel to the latest release (or a pinned version)",
		Long: `Update downloads the latest grafel release from GitHub, atomically
replaces the current binary, and re-runs the install transaction
(skills, MCP, daemon restart).

On success the previous binary is removed. On failure the previous
binary is restored automatically (rollback).

  grafel update                # latest stable release
  grafel update --pre          # latest pre-release
  grafel update --tag v1.2.3   # pin to a specific version

The update is idempotent: if the binary is already at the target
version the command exits 0 without downloading anything.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()

			// Legacy behaviour: when neither --tag nor --pre is set AND no
			// GitHub connectivity is needed, fall through to the old group-
			// refresh path.  The presence of --tag or --pre signals the new
			// self-update path.
			if tag != "" || pre {
				return runSelfUpdate(out, install.UpdateOptions{
					Tag: tag,
					Pre: pre,
				})
			}

			if refreshLite {
				fmt.Fprintln(out, "refreshing rules-lite (no-op in current build)")
			}

			// ── legacy group-refresh path ─────────────────────────────────
			bin, _ := os.Executable()
			groups, err := registry.Groups()
			if err != nil {
				return err
			}
			for _, g := range groups {
				cfg, err := registry.LoadGroupConfig(g.ConfigPath)
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "%s: %v\n", g.Name, err)
					continue
				}
				for _, r := range cfg.Repos {
					if cfg.Features.GitHooks {
						if err := hooks.Install(r.Path, bin); err != nil {
							fmt.Fprintf(cmd.ErrOrStderr(), "hooks %s: %v\n", r.Slug, err)
						}
					}
				}
				// Re-run install to refresh watcher units + MCP entries.
				_, _ = install.Apply(install.Options{
					Group:   g.Name,
					Config:  cfg,
					BinPath: bin,
				})
			}
			// MCP entries should always reflect the live binary path.
			regPath, _ := registry.RegistryPath()
			_, _ = mcpreg.Register(mcpreg.ClaudeCode, bin, regPath)
			_, _ = mcpreg.Register(mcpreg.Windsurf, bin, regPath)

			// Also run the new self-update with latest stable tag (downloads).
			return runSelfUpdate(out, install.UpdateOptions{})
		},
	}
	cmd.Flags().BoolVar(&refreshLite, "refresh-rules-lite", false, "refresh the lite rule-pack (no-op)")
	cmd.Flags().StringVar(&tag, "tag", "",
		"pin update to a specific release tag (e.g. v1.2.3)")
	cmd.Flags().BoolVar(&pre, "pre", false,
		"allow pre-release tags when resolving 'latest'")
	return cmd
}

// runSelfUpdate executes the new atomic self-update path (#2213) and prints a
// summary to out.
func runSelfUpdate(out io.Writer, opts install.UpdateOptions) error {
	opts.SkipDaemonRestart = false // let install handle it

	result, err := install.RunUpdate(opts)
	if err != nil {
		fmt.Fprintf(out, "✗ update failed: %v\n", err)
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "The previous binary has been restored.")
		return err
	}

	if result.Skipped {
		fmt.Fprintf(out, "✓ grafel is already at the latest version (%s)\n", result.Tag)
		return nil
	}

	fmt.Fprintf(out, "✓ grafel updated to %s\n", result.Tag)
	if result.PreviousVersion != "" && len(result.PreviousVersion) >= 16 {
		fmt.Fprintf(out, "  previous: %s...\n", result.PreviousVersion[:16])
	}
	if result.NewVersion != "" && len(result.NewVersion) >= 16 {
		fmt.Fprintf(out, "  new:      %s...\n", result.NewVersion[:16])
	}
	if result.InstallResult != nil {
		if len(result.InstallResult.SkillsInstalled) > 0 {
			fmt.Fprintf(out, "  skills:   %d refreshed\n", len(result.InstallResult.SkillsInstalled))
		}
		if len(result.InstallResult.MCPPaths) > 0 {
			fmt.Fprintf(out, "  MCP:      refreshed in %d config file(s)\n", len(result.InstallResult.MCPPaths))
		}
		if result.InstallResult.DaemonVersion != "" {
			fmt.Fprintf(out, "  daemon:   %s\n", result.InstallResult.DaemonVersion)
		}
	}
	// #5907 FIX4: surface the auto-reindex-on-upgrade the engine has already
	// (loop-guarded) enqueued, so it reads as "reindexing after upgrade"
	// rather than a silent multi-minute stall right after `grafel update`
	// returns. Report-only — nothing here triggers or duplicates the reindex.
	if result.ReposNeedingReindex > 0 {
		fmt.Fprintf(out, "  reindex:  %d repo(s) reindexing after upgrade\n", result.ReposNeedingReindex)
	}
	return nil
}
