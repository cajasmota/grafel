package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/daemon/client"
)

// start/stop/restart now drive the per-machine daemon (ADR-0017). The
// old per-repo watcher fanout under launchd/systemd is gone — the
// daemon owns all watchers in Phase B and a single OS service unit
// keeps the daemon alive (Phase C).

func newStartCmd() *cobra.Command {
	var maxRSSBudget int64
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the daemon (manages MCP, indexer, dashboard, and watchers)",
		Long: `Start the archigraph daemon.

The daemon is a single long-running process that owns:
  - MCP server (AI assistant tools)
  - Indexer + file-watcher (reactive re-index on save)
  - Dashboard HTTP server (default http://127.0.0.1:47274/)

Use 'archigraph stop' to stop all of the above at once.
Use 'archigraph dashboard' to open the dashboard in your browser.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDaemonStartWithBudget(cmd.OutOrStdout(), maxRSSBudget)
		},
	}
	cmd.Flags().Int64Var(&maxRSSBudget, "max-rss-budget", 0,
		"max predicted RSS (MB) for concurrent index jobs (0 = use daemon default of 500)")
	return cmd
}

func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the daemon and all managed services",
		Long: `Stop the archigraph daemon.

Stopping the daemon also stops all services it manages:
  - MCP server
  - Indexer + file-watcher
  - Dashboard HTTP server

Use 'archigraph start' to bring everything back up.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDaemonStop(cmd.OutOrStdout())
		},
	}
}

func newRestartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restart",
		Short: "Restart the daemon (MCP, indexer, dashboard, watchers)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := runDaemonStop(cmd.OutOrStdout()); err != nil &&
				!errors.Is(err, client.ErrDaemonNotRunning) {
				return err
			}
			// Brief pause so the previous daemon releases the socket
			// before we try to bind it again. 200ms is enough on
			// darwin and linux; if the socket is still busy the
			// daemon's own listen() returns a clear error.
			time.Sleep(200 * time.Millisecond)
			return runDaemonStart(cmd.OutOrStdout())
		},
	}
}

func newLogsCmd() *cobra.Command {
	var follow bool
	var tail int
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Print the daemon log",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDaemonLogs(cmd.OutOrStdout(), follow, tail)
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "follow the log as it grows")
	cmd.Flags().IntVarP(&tail, "tail", "n", 0, "print only the last N lines (0 = all)")
	return cmd
}

// runDaemonStart is the legacy zero-arg form retained for restart's
// internal use. It forwards to runDaemonStartWithBudget with 0,
// letting the daemon pick its own default.
func runDaemonStart(out io.Writer) error {
	return runDaemonStartWithBudget(out, 0)
}

// runDaemonStartWithBudget forks the current binary in daemon mode and
// detaches. It does not wait for the daemon to become ready beyond a
// short ping poll. If the daemon is already running, start is a no-op
// (the call is idempotent — important for service-managed restarts).
func runDaemonStartWithBudget(out io.Writer, maxRSSBudgetMB int64) error {
	layout, err := daemon.DefaultLayout()
	if err != nil {
		return err
	}
	// Already running? net.Dial succeeds → check for binary mismatch (#855).
	if c, err := client.DialPath(layout.SocketPath); err == nil {
		defer c.Close()
		st, statusErr := c.Status()
		currentBin, _ := os.Executable()
		// If the running daemon is from a different binary path, it's likely stale.
		if statusErr == nil && st.BinaryPath != "" && currentBin != "" &&
			filepath.Clean(st.BinaryPath) != filepath.Clean(currentBin) {
			return fmt.Errorf("stale daemon running from %s (you are %s)\n"+
				"Run: archigraph doctor --kill-stale && archigraph start",
				st.BinaryPath, currentBin)
		}
		fmt.Fprintln(out, "daemon already running")
		return nil
	}
	bin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve own binary: %w", err)
	}
	if err := daemon.EnsureLayout(layout); err != nil {
		return err
	}
	logFile, err := os.OpenFile(layout.LogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open log: %w", err)
	}
	defer logFile.Close()

	args := []string{"daemon"}
	if maxRSSBudgetMB > 0 {
		args = append(args, "--max-rss-budget", strconv.FormatInt(maxRSSBudgetMB, 10))
	}
	cmd := exec.Command(bin, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	// Detach: a fresh process group so the daemon survives this CLI.
	cmd.SysProcAttr = detachSysProcAttr()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("spawn daemon: %w", err)
	}
	// Don't wait — we want the child to outlive us.
	go func() { _ = cmd.Wait() }()

	// Poll for readiness up to 5 seconds. The daemon binds its socket
	// before logging "ready"; once dial succeeds we're done.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := client.DialPath(layout.SocketPath); err == nil {
			_ = c.Close()
			fmt.Fprintln(out, "daemon started")
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("daemon failed to become ready within 5s (check ~/.archigraph/logs/daemon.log)")
}

func runDaemonStop(out io.Writer) error {
	c, err := client.Dial()
	if err != nil {
		if errors.Is(err, client.ErrDaemonNotRunning) {
			fmt.Fprintln(out, "daemon not running")
			return nil
		}
		return err
	}
	defer c.Close()
	if err := c.Stop(); err != nil {
		return err
	}
	fmt.Fprintln(out, "stop requested")
	return nil
}

func runDaemonLogs(out io.Writer, follow bool, tail int) error {
	layout, err := daemon.DefaultLayout()
	if err != nil {
		return err
	}
	f, err := os.Open(layout.LogPath)
	if err != nil {
		if os.IsNotExist(err) {
			return errors.New("no log file yet — has the daemon ever started?")
		}
		return err
	}
	defer f.Close()

	if tail > 0 {
		if err := tailFile(out, f, tail); err != nil {
			return err
		}
	} else if !follow {
		if _, err := io.Copy(out, f); err != nil {
			return err
		}
	}
	if !follow {
		return nil
	}
	// Tail -f: seek to end and stream.
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return err
	}
	buf := make([]byte, 4096)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				return werr
			}
		}
		if err == io.EOF {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		if err != nil {
			return err
		}
	}
}

// tailFile reads the last n lines of f and writes them to out. Naive
// implementation: scan from end backwards in 4KB chunks. Good enough
// for the daemon log; a properly bounded reader can land later.
func tailFile(out io.Writer, f *os.File, n int) error {
	info, err := f.Stat()
	if err != nil {
		return err
	}
	size := info.Size()
	const chunk = 4096
	var (
		buf   = make([]byte, chunk)
		lines = 0
		off   = size
		all   []byte
	)
	for off > 0 && lines <= n {
		read := int64(chunk)
		if off < read {
			read = off
		}
		off -= read
		if _, err := f.ReadAt(buf[:read], off); err != nil {
			return err
		}
		all = append(buf[:read:read], all...)
		lines = 0
		for _, b := range all {
			if b == '\n' {
				lines++
			}
		}
	}
	// Trim to the last n lines.
	if lines > n {
		seen := 0
		for i := len(all) - 1; i >= 0; i-- {
			if all[i] == '\n' {
				seen++
				if seen == n+1 {
					all = all[i+1:]
					break
				}
			}
		}
	}
	_, err = out.Write(all)
	return err
}

// daemonLogPath is a small convenience for callers (status.go) that
// want to mention the log path without re-resolving the layout.
func daemonLogPath() string {
	layout, _ := daemon.DefaultLayout()
	return filepath.Clean(layout.LogPath)
}
