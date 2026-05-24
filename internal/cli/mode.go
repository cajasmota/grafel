package cli

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/daemon/client"
	"github.com/cajasmota/archigraph/internal/daemon/mode"
)

// newModeCmd returns the `archigraph mode <m>` subcommand.
//
// It writes the chosen mode to ~/.archigraph/daemon.config.json and then
// kicks (stop + start) the daemon so the new defaults take effect
// immediately. No-op when the daemon is not running.
func newModeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mode <background|workstation|readonly>",
		Short: "Switch the daemon operational mode and restart",
		Long: `Switch the daemon operational mode and restart.

Modes:
  background   Low-footprint preset for open-source / resource-constrained
               environments. Disables eager algo passes and MiniLM embeddings;
               caps the heap at 60% of available memory.

  workstation  Restores production defaults: eager algo passes, no heap cap
               override, embedding endpoint configurable via env var.

  readonly     Serves queries against the existing graph only. No reindex, no
               watcher, no algo passes. Useful when you want fast read-only
               access without background CPU/memory work.

The chosen mode is persisted in ~/.archigraph/daemon.config.json and read
by the daemon on every boot. Env vars (e.g. ARCHIGRAPH_EAGER_ALGO) always
take precedence over the mode defaults.`,
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"background", "workstation", "readonly"},
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			m, err := mode.Parse(args[0])
			if err != nil {
				return err
			}

			// Resolve the config path via the daemon layout so it respects
			// ARCHIGRAPH_DAEMON_ROOT overrides used in tests.
			layout, err := daemon.DefaultLayout()
			if err != nil {
				return fmt.Errorf("resolve daemon layout: %w", err)
			}
			cfgPath := mode.DefaultConfigPath(layout.Root)

			// Load existing config (if any) so we preserve EnvOverrides.
			existing, err := mode.LoadConfig(cfgPath)
			if err != nil {
				// Non-fatal: start fresh.
				existing = mode.Config{}
			}
			existing.Mode = m
			if err := mode.SaveConfig(cfgPath, existing); err != nil {
				return fmt.Errorf("save daemon config: %w", err)
			}
			fmt.Fprintf(out, "mode set to %q — config written to %s\n", m, cfgPath)

			// Kick the daemon: stop then start. Both errors are soft — if the
			// daemon is already stopped we can still start it; if start fails
			// we report it but don't fail the command (the config is saved).
			fmt.Fprintln(out, "restarting daemon…")
			if err := runModeRestart(out); err != nil {
				fmt.Fprintf(out, "  ⚠ restart: %v\n", err)
				fmt.Fprintf(out, "  Run 'archigraph start' to launch the daemon in %s mode.\n", m)
			} else {
				fmt.Fprintf(out, "daemon restarted in %s mode\n", m)
			}
			return nil
		},
	}
	return cmd
}

// runModeRestart stops a running daemon (if any) then brings it back up.
// It mirrors the restart logic in watcher_ctl.go but is kept separate so
// the mode command can suppress the "daemon not running" stop error.
func runModeRestart(out io.Writer) error {
	// Stop: ignore ErrDaemonNotRunning — the mode config is already saved
	// so the next start will pick it up regardless.
	if c, err := client.Dial(); err == nil {
		defer c.Close()
		_ = c.Stop() // best-effort; the daemon may exit before we read the reply
	} else if !errors.Is(err, client.ErrDaemonNotRunning) {
		fmt.Fprintf(out, "  stop: %v\n", err)
	}
	// Brief pause to let the previous daemon release the socket.
	time.Sleep(200 * time.Millisecond)
	return runDaemonStart(out)
}
