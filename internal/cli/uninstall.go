package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/cajasmota/archigraph/internal/daemon/service"
	"github.com/cajasmota/archigraph/internal/install"
	"github.com/cajasmota/archigraph/internal/install/mcpreg"
	"github.com/cajasmota/archigraph/internal/install/skilllink"
)

// unregisterMCPFromClaudeConfigs removes the archigraph MCP entry from all
// detected Claude Code config directories. It's extracted into a separate
// function so it can be tested independently of service.Uninstall, which
// requires OS permissions.
//
// claudeConfigDirs, when non-empty, overrides auto-detection of ~/.claude.json dirs.
// Returns a list of successfully unregistered paths and prints status to out.
func unregisterMCPFromClaudeConfigs(out io.Writer, claudeConfigDirs []string) []string {
	claudeDirs := mcpreg.DetectClaudeConfigDirs(claudeConfigDirs)
	removed := []string{}
	for _, cfgPath := range claudeDirs {
		if err := mcpreg.UnregisterPath(cfgPath); err != nil {
			fmt.Fprintf(out, "  ⚠ MCP unregister %s: %v\n", cfgPath, err)
		} else {
			removed = append(removed, cfgPath)
		}
	}
	if len(removed) > 0 {
		fmt.Fprintf(out, "  MCP removed from:\n")
		for _, p := range removed {
			fmt.Fprintf(out, "    %s\n", p)
		}
	}
	return removed
}

// removeSkillsFromClaudeConfigs removes the symlinked archigraph skills from
// every detected Claude Code config directory. It's extracted into a separate
// function so it can be tested independently of service.Uninstall.
//
// claudeConfigDirs, when non-empty, overrides auto-detection of ~/.claude.json dirs.
// Returns a list of successfully updated paths and prints status to out.
func removeSkillsFromClaudeConfigs(out io.Writer, claudeConfigDirs []string) []string {
	claudeDirs := mcpreg.DetectClaudeConfigDirs(claudeConfigDirs)
	return skilllink.RemoveSkillsFromClaudeConfigs(out, claudeDirs)
}

// newUninstallCmd returns the `archigraph uninstall` subcommand.
//
// Per ADR-0017 Phase C the old "remove from a group" semantic is
// REMOVED. `archigraph uninstall` now stops and deregisters the
// daemon OS service (launchd plist / systemd unit) and removes the
// archigraph MCP entry from every detected Claude config dir.
// Idempotent: if the service is not installed the command succeeds silently.
//
// When install.json is present (new COPY/DEV install path, #2213), the
// atomic RunUninstall path is used instead, which operates precisely on
// what install.json records.
func newUninstallCmd() *cobra.Command {
	var (
		claudeConfigDirs []string
		skipSkillUnlink  bool
		purge            bool
		yes              bool
	)

	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the archigraph install (daemon, MCP, skills)",
		Long: `Uninstall removes the archigraph install:

  - Removes copied/linked skills from ~/.claude/skills/ (only those in install.json)
  - Deregisters the archigraph MCP entry from all detected .claude.json files
  - Stops the archigraph daemon (OS service)
  - Removes the CLI binary (with confirmation unless --yes)
  - Removes ~/.archigraph/install.json

Default: leaves ~/.archigraph/store/ (your graphs) intact.
Use --purge to also remove store/ and docs/.

Idempotent: if archigraph is not installed the command exits 0.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()

			// ── New atomic path: uses install.json when available ──────────────
			// RunUninstall returns (non-nil result, nil error) even when there is
			// no install.json — in that case all fields are zero and we fall through
			// to the legacy path.
			result, err := install.RunUninstall(install.UninstallOptions{
				Purge: purge,
				Yes:   yes,
			})
			if err != nil {
				return fmt.Errorf("uninstall: %w", err)
			}

			// If any structured work was done, report it and return.
			if result != nil && (len(result.SkillsRemoved) > 0 || len(result.MCPPaths) > 0 ||
				result.DaemonStopped || result.BinaryRemoved || result.StateRemoved) {
				if len(result.SkillsRemoved) > 0 {
					fmt.Fprintf(out, "  skills removed: %v\n", result.SkillsRemoved)
				}
				if len(result.MCPPaths) > 0 {
					fmt.Fprintf(out, "  MCP deregistered from: %v\n", result.MCPPaths)
				}
				if result.DaemonStopped {
					fmt.Fprintln(out, "  daemon stopped")
				}
				if result.BinaryRemoved {
					fmt.Fprintln(out, "  binary removed")
				}
				if result.StateRemoved {
					fmt.Fprintln(out, "  install.json removed")
				}
				if result.StoreRemoved {
					fmt.Fprintln(out, "  store/ removed")
				}
				if result.DocsRemoved {
					fmt.Fprintln(out, "  docs/ removed")
				}
				fmt.Fprintln(out, "✓ archigraph uninstalled")
				return nil
			}

			// ── Legacy path: no install.json or nothing to do ─────────────────
			if err := service.Uninstall(service.Options{}); err != nil {
				return err
			}
			fmt.Fprintln(out, "✓ archigraph daemon removed")

			// Remove MCP registrations from every detected Claude config dir.
			unregisterMCPFromClaudeConfigs(out, claudeConfigDirs)

			// Remove symlinked skills from every detected Claude config dir.
			if !skipSkillUnlink {
				removeSkillsFromClaudeConfigs(out, claudeConfigDirs)
			}

			return nil
		},
	}
	cmd.Flags().StringSliceVar(&claudeConfigDirs, "claude-config-dirs", nil,
		"explicit list of .claude.json paths to deregister from (default: auto-detect)")
	cmd.Flags().BoolVar(&skipSkillUnlink, "skip-skill-unlink", false,
		"skip removing skills from Claude Code's skills/ directories")
	cmd.Flags().BoolVar(&purge, "purge", false,
		"also remove ~/.archigraph/store/ and ~/.archigraph/docs/ (user graphs and docs)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false,
		"assume yes / non-interactive: skip the binary-removal confirmation prompt (auto-enabled when stdin is not a TTY)")
	return cmd
}
