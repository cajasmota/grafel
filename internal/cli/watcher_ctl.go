package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/daemon/client"
	"github.com/cajasmota/grafel/internal/daemon/service"
	"github.com/cajasmota/grafel/internal/process"
)

// start/stop/restart now drive the per-machine daemon (ADR-0017). The
// old per-repo watcher fanout under launchd/systemd is gone — the
// daemon owns all watchers in Phase B and a single OS service unit
// keeps the daemon alive (Phase C).

func newStartCmd() *cobra.Command {
	var maxRSSBudget int64
	var noAutoCleanup bool
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the daemon (manages MCP, indexer, dashboard, and watchers)",
		Long: `Start the grafel daemon.

The daemon is a single long-running process that owns:
  - MCP server (AI assistant tools)
  - Indexer + file-watcher (reactive re-index on save)
  - Dashboard HTTP server (default http://127.0.0.1:47274/)

Use 'grafel stop' to stop all of the above at once.
Use 'grafel dashboard' to open the dashboard in your browser.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDaemonStartOpts(cmd.OutOrStdout(), maxRSSBudget, noAutoCleanup)
		},
	}
	cmd.Flags().Int64Var(&maxRSSBudget, "max-rss-budget", 0,
		"max predicted RSS (MB) for concurrent index jobs; persists to settings.json (0 = configured/auto)")
	cmd.Flags().BoolVar(&noAutoCleanup, "no-auto-cleanup", false,
		"disable the background docgen cleanup sweeper (default: enabled)")
	return cmd
}

func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the daemon and all managed services",
		Long: `Stop the grafel daemon.

Stopping the daemon also stops all services it manages:
  - MCP server
  - Indexer + file-watcher
  - Dashboard HTTP server

Use 'grafel start' to bring everything back up.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDaemonStop(cmd.OutOrStdout())
		},
	}
}

func newRestartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restart",
		Short: "Restart the daemon (MCP, indexer, dashboard, watchers)",
		Long: `Restart the grafel daemon as a single idempotent operation.

restart stops the running daemon gracefully, verifies the process is actually
dead (escalating to SIGKILL if needed), clears any stale pidfile/socket left by
a crash or hard kill, then starts a fresh daemon. It is safe to run whether the
daemon is currently up or down.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDaemonRestart(cmd.OutOrStdout())
		},
	}
}

// runDaemonRestart is the idempotent stop→verify-dead→cleanup→start sequence
// for issue #4549. It is correct from BOTH an up and a down starting state:
//
//   - Up:   request graceful stop, wait for the process to actually exit
//     (polling the recorded pid), SIGKILL if it overstays, then start.
//   - Down: stop is a no-op (ErrDaemonNotRunning is swallowed), stale pidfile
//     and socket are cleared, then start.
//
// The critical bug it fixes: the previous restart did a blind 200 ms sleep and
// relied on `start`'s dial probe, so a daemon that ignored SIGTERM, or a stale
// pidfile naming a dead/recycled pid, could wedge the next start. We now treat
// "the old daemon is gone and its on-disk artifacts are clean" as an explicit
// precondition of start.
func runDaemonRestart(out io.Writer) error {
	// #5789: if an OS service (launchd/systemd/schtasks) is registered for
	// THIS root, route straight through the service-manager-aware restart
	// instead of the stop→wait→clear-pidfile→blind-fork sequence below. That
	// sequence forks a manual, service-manager-blind daemon; during an
	// update/restart window the OS service's own KeepAlive/Restart respawn
	// races it over the pidfile+socket, and AcquirePIDFile's wedged-daemon
	// reclaim can then SIGKILL one of the two mid-startup. service.Restart
	// owns the correct unload→load→wait-ready dance internally, so none of
	// the pidfile bookkeeping below is needed (or safe) on this path.
	if serviceInstalledForThisRoot() {
		return serviceRestartForThisRoot(out)
	}

	layout, err := daemon.DefaultLayout()
	if err != nil {
		return err
	}

	// Record the pid BEFORE asking the daemon to stop, so we can confirm that
	// exact process exits (rather than racing a freshly-spawned one).
	oldPID := daemon.ReadPIDFile(layout.PIDPath)

	if err := runDaemonStop(out); err != nil && !errors.Is(err, client.ErrDaemonNotRunning) {
		return err
	}

	// Wait for the old process to actually exit, then SIGKILL if it overstays.
	if oldPID > 0 {
		if waitForExit(oldPID, 5*time.Second) {
			// graceful exit
		} else if pidStillAlive(oldPID) {
			fmt.Fprintf(out, "  daemon (pid %d) did not exit gracefully; sending SIGKILL\n", oldPID)
			_ = forceKill(oldPID)
			if !waitForExit(oldPID, 3*time.Second) {
				return fmt.Errorf("daemon (pid %d) survived SIGKILL; not starting a second instance", oldPID)
			}
		}
	}

	// Clear stale on-disk artifacts so start cannot see a phantom owner. Only
	// remove the pidfile if it no longer names a live grafel daemon — we
	// must never delete a pidfile owned by a daemon a concurrent caller just
	// started.
	cleanStaleArtifacts(out, layout)

	return runDaemonStart(out)
}

// waitForExit polls until pid is gone or the timeout elapses. Returns true if
// the process exited within the window.
func waitForExit(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !pidStillAlive(pid) {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return !pidStillAlive(pid)
}

// forceKill forcibly terminates pid (no-op-safe if the pid is already gone).
// os.Process.Kill maps to SIGKILL on unix and TerminateProcess on Windows, so
// this is the cross-platform escalation path when SIGTERM was ignored.
func forceKill(pid int) error {
	if pid <= 0 {
		return nil
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}

// cleanStaleArtifacts removes a stale pidfile and socket left by a crashed or
// hard-killed daemon. It is conservative: the pidfile is only removed if it no
// longer names a live grafel process (so a concurrently-started daemon is
// never disturbed). The socket file is removed unconditionally on unix — a
// fresh daemon re-creates it on listen, and a live daemon holding the same
// path keeps its open fd regardless of the directory entry. On Windows the
// socket path is a named pipe (not a filesystem object) and removal is a no-op.
func cleanStaleArtifacts(out io.Writer, layout daemon.Layout) {
	if pid := daemon.ReadPIDFile(layout.PIDPath); pid > 0 && !pidStillAlive(pid) {
		if err := os.Remove(layout.PIDPath); err == nil {
			fmt.Fprintf(out, "  cleared stale pidfile (pid %d was dead)\n", pid)
		}
	}
	if isUnixSocketPath(layout.SocketPath) {
		_ = os.Remove(layout.SocketPath)
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
// internal use. It forwards to runDaemonStartOpts with default settings.
func runDaemonStart(out io.Writer) error {
	return runDaemonStartOpts(out, 0, false)
}

// runDaemonStartWithBudget retains backward-compat for callers that only
// pass the RSS budget (no cleanup flag).
func runDaemonStartWithBudget(out io.Writer, maxRSSBudgetMB int64) error {
	return runDaemonStartOpts(out, maxRSSBudgetMB, false)
}

// runDaemonStartOpts forks the current binary in daemon mode and
// detaches. It does not wait for the daemon to become ready beyond a
// short ping poll. If the daemon is already running, start is a no-op
// (the call is idempotent — important for service-managed restarts).
func runDaemonStartOpts(out io.Writer, maxRSSBudgetMB int64, noAutoCleanup bool) error {
	layout, err := daemon.DefaultLayout()
	if err != nil {
		return err
	}
	if maxRSSBudgetMB > 0 {
		if err := daemon.PersistConfiguredRSSBudgetMB(maxRSSBudgetMB); err != nil {
			return fmt.Errorf("persist --max-rss-budget: %w", err)
		}
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
				"Run: grafel doctor --kill-stale && grafel start",
				st.BinaryPath, currentBin)
		}
		fmt.Fprintln(out, "daemon already running")
		return nil
	}

	// #5789: before forking a manual, service-manager-blind daemon, check
	// whether an OS service (launchd/systemd/schtasks) is registered for
	// THIS root — i.e. whether `grafel install`/`update` already owns
	// lifecycle management here. If so, route through the OS-service-aware
	// restart instead: a manual fork here would go unregistered with the
	// service manager, which then races it over the pidfile/socket via its
	// own KeepAlive/Restart respawn. Only fall back to the manual fork when
	// no OS service is registered (the dev/foreground case).
	if serviceInstalledForThisRoot() {
		return serviceRestartForThisRoot(out)
	}

	return manualForkStart(out, layout, maxRSSBudgetMB, noAutoCleanup)
}

// manualForkStart forks the current binary in daemon mode, detaches it, and
// polls for socket readiness. This is the launchd/systemd/schtasks-BLIND
// path: it must only run when no OS service is registered for this root
// (issue #5789) — otherwise the forked child races the service manager's own
// respawn over the pidfile/socket. Overridable for tests; production default
// is defaultManualForkStart, invoked via the manualForkStart var below.
var manualForkStart = defaultManualForkStart

func defaultManualForkStart(out io.Writer, layout daemon.Layout, maxRSSBudgetMB int64, noAutoCleanup bool) error {
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
	if noAutoCleanup {
		args = append(args, "--no-auto-cleanup")
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

	// Poll for readiness up to the startup-readiness budget. The daemon binds
	// its socket only AFTER its first startup index pass, which on a large
	// store legitimately takes far longer than the old 5 s cliff (issue #4549
	// observed ~82 s). Failing at 5 s reported a false failure while a healthy
	// daemon was still indexing, triggering rollback/retry churn. We now wait
	// up to startupReadinessBudget() and emit progress so the user can see the
	// daemon is coming up rather than wedged. If the child PROCESS exits before
	// the socket appears, we bail early with the log path — that's a real
	// failure, not a slow start.
	budget := startupReadinessBudget()
	deadline := time.Now().Add(budget)
	lastProgress := time.Now()
	for time.Now().Before(deadline) {
		if c, err := client.DialPath(layout.SocketPath); err == nil {
			_ = c.Close()
			fmt.Fprintln(out, "daemon started")
			return nil
		}
		// If the spawned process has already died, stop waiting — a dead
		// child will never open the socket, so the full budget is wasted.
		if cmd.Process != nil && !pidStillAlive(cmd.Process.Pid) {
			return fmt.Errorf("daemon process exited before becoming ready "+
				"(check %s)", layout.LogPath)
		}
		if now := time.Now(); now.Sub(lastProgress) >= 5*time.Second {
			remaining := time.Until(deadline).Round(time.Second)
			fmt.Fprintf(out, "  waiting for daemon socket… (initial index may be running; %s remaining)\n", remaining)
			lastProgress = now
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("daemon failed to become ready within %s (check %s)", budget, layout.LogPath)
}

// serviceInstalledForThisRoot reports whether an OS service (launchd on
// macOS, systemd on Linux, Task Scheduler on Windows) is registered for THIS
// daemon root — i.e. whether `grafel install`/`update` already owns
// lifecycle management here. Overridable for tests; production default is
// defaultServiceInstalledForThisRoot.
var serviceInstalledForThisRoot = defaultServiceInstalledForThisRoot

func defaultServiceInstalledForThisRoot() bool {
	root, found, err := service.RegisteredRoot()
	if err != nil {
		// Reading the recorded root FAILED (parse/IO) — fail closed and fall
		// back to the manual path rather than routing to a service we can't
		// confirm.
		return false
	}

	// Ownership guard (issue #5277 dimension). service.RegisteredRoot() returns
	// the HOME baked into the unit — the darwin plist <key>HOME</key> and the
	// systemd Environment=HOME= are BOTH os.UserHomeDir() (e.g. /Users/foo,
	// /home/foo), NOT ~/.grafel. So the comparison MUST be on the HOME
	// dimension, exactly like the uninstall guard
	// (internal/install/daemon_guard.go uninstallTargetRoot), whose docstring
	// notes the target must be resolved on the SAME dimension the unit files
	// record: HOME. Comparing against layout.Root (~/.grafel) would never match
	// and the gate would be a permanent no-op (the #5789 regression).
	//
	// When found==true but root=="" (a legacy unit with no baked HOME) OR
	// found==false (Windows, whose registeredRoot is a stub, or no unit on
	// disk) we CANNOT disprove ownership on the HOME dimension, so we do not
	// bail here — we let service.Status() below be the authority on whether a
	// service is actually installed for this user.
	if found && root != "" {
		if canonicalRoot(root) != canonicalRoot(targetHomeRoot()) {
			// A service IS installed, but for a different HOME/user — not ours
			// to route through.
			return false
		}
	}

	// service.Status() is the authoritative "is a service installed" check: on
	// darwin/linux it stats the plist/unit; on Windows it stats the task XML
	// and falls back to querying the scheduler. This is what makes the gate
	// fire for a real Windows schtasks service despite registeredRoot being a
	// stub there (found==false above).
	st, err := service.Status(service.Options{})
	if err != nil {
		return false
	}
	return st.Installed
}

// targetHomeRoot resolves THIS process's HOME — the dimension
// service.RegisteredRoot() records in the unit files. It mirrors
// internal/install/daemon_guard.go uninstallTargetRoot: prefer the HOME env
// var (so an isolated sandbox home is honoured) and fall back to
// os.UserHomeDir(). Kept local to avoid importing the install package (and any
// cycle) purely for one helper.
func targetHomeRoot() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return ""
}

// canonicalRoot normalises a root path for comparison: trimmed, cleaned, and
// lower-cased so the match is robust to spelling variants and case-insensitive
// filesystems. Mirrors internal/install/daemon_guard.go canonicalRoot. An
// empty input stays empty ("unknown").
func canonicalRoot(root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return ""
	}
	return strings.ToLower(filepath.Clean(root))
}

// serviceRestartForThisRoot performs the OS-service-aware restart (launchd
// bootout→bootstrap / systemctl disable→enable / schtasks /end→/run) via
// service.Restart, instead of forking a manual daemon that the service
// manager doesn't know about. Overridable for tests; production default is
// defaultServiceRestartForThisRoot.
var serviceRestartForThisRoot = defaultServiceRestartForThisRoot

func defaultServiceRestartForThisRoot(out io.Writer) error {
	bin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve own binary: %w", err)
	}
	layout, err := daemon.DefaultLayout()
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "OS service detected for this daemon; restarting via the OS service manager")
	if _, err := service.Restart(service.Options{
		BinPath:    bin,
		SocketPath: layout.SocketPath,
		LogDir:     layout.LogDir,
	}); err != nil {
		return fmt.Errorf("service restart: %w", err)
	}
	fmt.Fprintln(out, "daemon started")
	return nil
}

// startupReadinessDefault is the time `grafel start` waits for the daemon
// socket to appear. It must cover the daemon's first startup index pass, which
// on large stores runs well past a minute (issue #4549 observed ~82 s before
// the socket was ready). It is deliberately generous: a slow-but-healthy start
// must NOT be reported as a failure.
const startupReadinessDefault = 120 * time.Second

// startupReadinessBudget returns the readiness budget for `grafel start`,
// overridable via GRAFEL_START_READINESS (a Go duration, e.g. "180s" or
// "3m") so operators on very large stores can extend it without a rebuild.
// Invalid or non-positive values fall back to the default.
func startupReadinessBudget() time.Duration {
	if v := os.Getenv("GRAFEL_START_READINESS"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return startupReadinessDefault
}

// isUnixSocketPath reports whether path is a filesystem unix-domain socket
// (as opposed to a Windows named pipe). Named pipes use the reserved
// `\\.\pipe\` prefix and are NOT filesystem objects, so os.Remove must not be
// attempted on them. This check is value-based (no syscalls) so it is correct
// regardless of the host OS — relevant because the socket path is recorded in
// the layout and may be inspected cross-platform.
func isUnixSocketPath(path string) bool {
	return !strings.HasPrefix(path, `\\.\pipe\`)
}

// pidStillAlive reports whether the process with the given pid is still
// running. Used by the start readiness loop to bail out early when the
// spawned daemon dies instead of waiting out the whole budget. The
// platform-specific liveness probe lives in internal/process (signal 0
// on unix, OpenProcess + GetExitCodeProcess on windows).
func pidStillAlive(pid int) bool {
	return process.IsAlive(pid)
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
