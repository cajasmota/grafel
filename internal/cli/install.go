package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/daemon/mode"
	"github.com/cajasmota/archigraph/internal/daemon/service"
	"github.com/cajasmota/archigraph/internal/install/mcpreg"
	"github.com/cajasmota/archigraph/internal/install/skilllink"
)

// registerMCPInClaudeConfigs registers the archigraph MCP entry in all detected
// Claude Code config directories. It's extracted into a separate function so it
// can be tested independently of service.Install, which requires OS permissions.
//
// binPath is the full path to the archigraph binary.
// claudeConfigDirs, when non-empty, overrides auto-detection of ~/.claude.json dirs.
// Returns a list of successfully registered paths and prints status to out.
func registerMCPInClaudeConfigs(out io.Writer, binPath string, claudeConfigDirs []string) []string {
	claudeDirs := mcpreg.DetectClaudeConfigDirs(claudeConfigDirs)
	registered := []string{}
	for _, cfgPath := range claudeDirs {
		if _, err := mcpreg.RegisterPath(cfgPath, binPath); err != nil {
			fmt.Fprintf(out, "  ⚠ MCP register %s: %v\n", cfgPath, err)
		} else {
			registered = append(registered, cfgPath)
		}
	}
	if len(registered) > 0 {
		fmt.Fprintf(out, "  MCP registered in:\n")
		for _, p := range registered {
			fmt.Fprintf(out, "    %s\n", p)
		}
		fmt.Fprintf(out, "  Restart Claude Code to load the archigraph MCP tools.\n")
	}
	return registered
}

// installSkillsInClaudeConfigs symlinks the 6 archigraph skills into every
// detected Claude Code config directory. It's extracted into a separate
// function so it can be tested independently of service.Install.
//
// binPath is the full path to the archigraph binary (used to infer skills location).
// skillsSourceDir is an explicit override for the skills directory (from --skills-source-dir flag).
// claudeConfigDirs, when non-empty, overrides auto-detection of ~/.claude.json dirs.
// Returns a list of successfully installed paths and prints status to out.
func installSkillsInClaudeConfigs(out io.Writer, binPath, skillsSourceDir string, claudeConfigDirs []string) []string {
	claudeDirs := mcpreg.DetectClaudeConfigDirs(claudeConfigDirs)
	return skilllink.InstallSkillsInClaudeConfigs(out, binPath, skillsSourceDir, claudeDirs)
}

// newInstallCmd returns the `archigraph install` subcommand.
//
// Per ADR-0017 Phase C the old "apply a group config" semantic is
// REMOVED. `archigraph install` is now the canonical one-liner that
// registers the daemon as a user-level OS service (launchd on macOS,
// systemd on Linux) and starts it.
//
// The --foreground flag skips service registration and just starts the
// daemon in the foreground — useful when launchd/systemd isn't
// cooperating and you need debug output directly in the terminal.
func newInstallCmd() *cobra.Command {
	var foreground bool
	var claudeConfigDirs []string
	var skillsSourceDir string
	var skipSkillLink bool
	var installMode string

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Register archigraph daemon as a system service and start it",
		Long: `Install registers the archigraph daemon as a user-level OS service
and starts it immediately.

On macOS: writes ~/Library/LaunchAgents/com.archigraph.daemon.plist and
calls 'launchctl bootstrap'. The daemon auto-starts at every login.

On Linux: writes ~/.config/systemd/user/archigraph-daemon.service and
calls 'systemctl --user enable --now'.

No sudo or root is required.

Re-running install when the service is already active prints the current
status and exits successfully (idempotent).

Use --foreground to skip service registration and run the daemon directly
in this terminal — useful for debugging launchd/systemd issues.

Use --mode to select the operational preset (background, workstation, readonly).
The default is background. See 'archigraph mode --help' for details.

Install also symlinks the 6 archigraph skills into every detected Claude Code
config directory's skills/ subdirectory. Use --skip-skill-link to disable this,
or --skills-source-dir to override the skills discovery location.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()

			if foreground {
				// --foreground: skip service registration, just run the daemon
				// in this process. Useful when launchd/systemd is misbehaving.
				fmt.Fprintln(out, "starting archigraph daemon in foreground (Ctrl-C to stop)…")
				if activeHooks.RunDaemon == nil {
					return fmt.Errorf("daemon entrypoint not wired")
				}
				return activeHooks.RunDaemon(nil)
			}

			bin, err := os.Executable()
			if err != nil {
				return fmt.Errorf("resolve binary path: %w", err)
			}

			layout, err := daemon.DefaultLayout()
			if err != nil {
				return fmt.Errorf("resolve daemon layout: %w", err)
			}

			// Persist the selected mode so the daemon reads it on every boot.
			// Default is background (low-footprint for open-source installs).
			selectedMode := mode.Background
			if installMode != "" {
				parsed, merr := mode.Parse(installMode)
				if merr != nil {
					return merr
				}
				selectedMode = parsed
			}
			cfgPath := mode.DefaultConfigPath(layout.Root)
			existing, _ := mode.LoadConfig(cfgPath) // best-effort; ignore missing-file error
			existing.Mode = selectedMode
			if serr := mode.SaveConfig(cfgPath, existing); serr != nil {
				fmt.Fprintf(out, "  ⚠ save daemon config: %v\n", serr)
			} else {
				fmt.Fprintf(out, "  mode:    %s\n", selectedMode)
			}

			opts := service.Options{
				BinPath:    bin,
				SocketPath: layout.SocketPath,
				LogDir:     layout.LogDir,
			}

			st, err := service.Install(opts)
			if err != nil {
				fmt.Fprintf(out, "✗ install failed: %v\n", err)
				fmt.Fprintln(out, "")
				fmt.Fprintln(out, "Try 'archigraph install --foreground' to run the daemon directly")
				fmt.Fprintln(out, "and see error output.")
				return err
			}

			pidStr := ""
			if st.PID > 0 {
				pidStr = fmt.Sprintf(" pid=%d", st.PID)
			}
			fmt.Fprintf(out, "✓ archigraph daemon installed and running%s\n", pidStr)
			fmt.Fprintf(out, "  socket:  %s\n", opts.SocketPath)
			fmt.Fprintf(out, "  service: %s\n", st.UnitFile)

			// Register archigraph MCP bridge in every detected Claude Code
			// config dir (primary ~/.claude.json + any ~/.claude-*/). Per
			// ADR-0017 #827 the bridge translates MCP JSON-RPC 2.0 from
			// Claude Code to the daemon's JSON-RPC 1.0 socket. Failures are
			// soft — we report them but do not abort the install.
			registerMCPInClaudeConfigs(out, bin, claudeConfigDirs)

			// Symlink the 6 archigraph skills into every detected Claude Code
			// config directory's skills/ subdirectory. This allows Claude Code
			// to discover and run the skills directly (e.g. /archigraph-quality-check).
			// Failures are soft — we report them but do not abort the install.
			if !skipSkillLink {
				installSkillsInClaudeConfigs(out, bin, skillsSourceDir, claudeConfigDirs)
			}

			return nil
		},
	}
	cmd.Flags().BoolVar(&foreground, "foreground", false,
		"skip service registration; run the daemon directly in this terminal (debug mode)")
	cmd.Flags().StringSliceVar(&claudeConfigDirs, "claude-config-dirs", nil,
		"explicit list of .claude.json paths to register MCP in (default: auto-detect ~/.claude.json + ~/.claude-*/)")
	cmd.Flags().StringVar(&skillsSourceDir, "skills-source-dir", "",
		"override the skills directory location (default: auto-detect from binary location or dev paths)")
	cmd.Flags().BoolVar(&skipSkillLink, "skip-skill-link", false,
		"skip symlinking skills into Claude Code's skills/ directories")
	cmd.Flags().StringVar(&installMode, "mode", "",
		"operational mode: background (default), workstation, or readonly")
	return cmd
}
