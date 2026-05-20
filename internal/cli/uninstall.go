package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/cajasmota/archigraph/internal/daemon/service"
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
func newUninstallCmd() *cobra.Command {
	var claudeConfigDirs []string
	var skipSkillUnlink bool

	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Stop and remove the archigraph daemon service",
		Long: `Uninstall stops the archigraph daemon and removes its OS service
registration (launchd plist on macOS, systemd unit on Linux).

Also removes the archigraph MCP entry from ~/.claude.json and any
~/.claude-*/.claude.json files, so Claude Code no longer tries to
connect to the daemon.

Also removes the symlinked archigraph skills from every detected Claude Code
config directory's skills/ subdirectory. Use --skip-skill-unlink to disable this.

Idempotent: if the service is not installed the command exits 0 without
printing an error.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
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
	return cmd
}
