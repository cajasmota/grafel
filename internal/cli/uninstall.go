package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cajasmota/archigraph/internal/daemon/service"
	"github.com/cajasmota/archigraph/internal/install/mcpreg"
)

// newUninstallCmd returns the `archigraph uninstall` subcommand.
//
// Per ADR-0017 Phase C the old "remove from a group" semantic is
// REMOVED. `archigraph uninstall` now stops and deregisters the
// daemon OS service (launchd plist / systemd unit) and removes the
// archigraph MCP entry from every detected Claude config dir.
// Idempotent: if the service is not installed the command succeeds silently.
func newUninstallCmd() *cobra.Command {
	var claudeConfigDirs []string
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Stop and remove the archigraph daemon service",
		Long: `Uninstall stops the archigraph daemon and removes its OS service
registration (launchd plist on macOS, systemd unit on Linux).

Also removes the archigraph MCP entry from ~/.claude.json and any
~/.claude-*/.claude.json files, so Claude Code no longer tries to
connect to the daemon.

Idempotent: if the service is not installed the command exits 0 without
printing an error.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			if err := service.Uninstall(service.Options{}); err != nil {
				return err
			}
			fmt.Fprintln(out, "✓ archigraph daemon removed")

			// Remove MCP registrations from every detected Claude config dir.
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
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&claudeConfigDirs, "claude-config-dirs", nil,
		"explicit list of .claude.json paths to deregister MCP from (default: auto-detect)")
	return cmd
}
