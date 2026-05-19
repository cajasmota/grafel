package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/daemon/service"
)

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
in this terminal — useful for debugging launchd/systemd issues.`,
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
			return nil
		},
	}
	cmd.Flags().BoolVar(&foreground, "foreground", false,
		"skip service registration; run the daemon directly in this terminal (debug mode)")
	return cmd
}
