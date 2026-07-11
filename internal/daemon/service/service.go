// Package service handles registration and removal of the grafel
// daemon as an OS-level user service (launchd on macOS, systemd on
// Linux). It is the implementation layer for the `grafel install`
// and `grafel uninstall` commands introduced in ADR-0017 Phase C.
//
// The package is platform-agnostic at this level; build-tag'd files
// supply the platform implementations:
//
//   - launchd_darwin.go  — macOS LaunchAgents plist + launchctl
//   - systemd_linux.go   — ~/.config/systemd/user unit + systemctl
//
// No root / sudo is required: both backends use the per-user service
// facilities (launchd gui/$UID domain, systemd --user).
package service

import (
	"fmt"
	"net/rpc"
	"net/rpc/jsonrpc"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/daemon/transport"
	"github.com/cajasmota/grafel/internal/process"
)

// Options carries install-time parameters. All fields that are empty
// strings are resolved to defaults by Install.
type Options struct {
	// BinPath is the absolute path to the grafel binary. When
	// empty, Install resolves it via os.Executable().
	BinPath string

	// SocketPath is the IPC transport address the daemon listens on.
	// On Unix this is a filesystem path (defaults to ~/.grafel/sockets/daemon.sock).
	// On Windows this is a named-pipe path (\\.\pipe\grafel-daemon-<user>).
	// When empty, resolveOptions fills it from daemon.DefaultLayout.
	SocketPath string

	// LogDir is the directory for stdout/stderr logs. When empty it
	// defaults to ~/.grafel/logs.
	LogDir string
}

// StatusInfo is returned by Status to describe the current state of the
// installed service.
type StatusInfo struct {
	Installed bool
	Running   bool
	PID       int    // 0 when not running or unknown
	UnitFile  string // path of the plist / unit file
}

// Install registers the grafel daemon as a user-level OS service
// and starts it. Idempotent: if the service is already installed it
// returns the current status without modifying anything.
//
// On macOS it writes ~/Library/LaunchAgents/com.grafel.daemon.plist
// and calls `launchctl bootstrap gui/$UID`.
//
// On Linux it writes ~/.config/systemd/user/grafel-daemon.service
// and calls `systemctl --user enable --now`.
//
// After loading, Install polls the daemon socket for up to ~60 s (the
// readiness budget — a cold start on a large store legitimately takes
// >5 s) before returning the populated StatusInfo. See manager.go for the
// platform-agnostic clear-then-load + readiness-poll orchestration.
func Install(opts Options) (StatusInfo, error) {
	if err := resolveOptions(&opts); err != nil {
		return StatusInfo{}, fmt.Errorf("resolve options: %w", err)
	}
	return install(opts)
}

// Uninstall stops and removes the OS service registration. Idempotent:
// if the service is not installed it returns immediately without error.
//
// After tearing down the serve unit, it also runs a belt-and-suspenders
// orphan-engine sweep (ADR-0024 PR5, epic #5729): in split mode, serve's own
// graceful drain already SIGTERMs its supervised `grafel engine` child before
// exiting, so under normal shutdown there is nothing left to sweep. But
// Unload can also hard-stop serve (launchctl bootout / systemctl disable
// --now / schtasks /end) without giving its drain defers time to run, which
// could orphan the engine child. The sweep reads engine.pid and, if it names
// a still-live process, terminates it directly. It is a safe no-op in
// monolith mode (no engine.pid exists) or when the engine was already
// reaped.
func Uninstall(opts Options) error {
	if err := resolveOptions(&opts); err != nil {
		return fmt.Errorf("resolve options: %w", err)
	}
	if err := uninstall(opts); err != nil {
		return err
	}
	if layout, lerr := daemon.DefaultLayout(); lerr == nil {
		sweepOrphanEngine(sweepOrphanEngineDeps{
			root:     layout.Root,
			readPID:  defaultReadEnginePID,
			isAlive:  process.IsAlive,
			isGrafel: process.PidIsGrafel,
			kill:     process.Kill,
		})
	}
	return nil
}

// sweepOrphanEngineDeps abstracts the orphan-engine sweep's I/O so it can be
// unit-tested without touching real processes or a real daemon root.
type sweepOrphanEngineDeps struct {
	root     string
	readPID  func(path string) (int, error)
	isAlive  func(pid int) bool
	isGrafel func(pid int) (bool, error)
	kill     func(pid int) error
}

// sweepOrphanEngine implements the belt-and-suspenders orphan-engine sweep
// documented on Uninstall. It is intentionally conservative: any failure to
// read/parse engine.pid (including the common case — it does not exist,
// because split mode is off or the engine already exited) is treated as
// "nothing to do", never an error.
//
// PID-reuse safety (review #5729): a stale engine.pid can name a pid the OS
// has since recycled to an unrelated process (the engine was SIGKILLed or the
// box crashed, so its `defer os.Remove(pidPath)` never ran). Before signaling,
// confirm the pid is actually a grafel process; treat isGrafel returning an
// error (e.g. a platform that can't enumerate processes) OR false as "not
// ours" and skip the kill — we never signal a process we cannot positively
// confirm is grafel.
func sweepOrphanEngine(deps sweepOrphanEngineDeps) {
	if deps.root == "" {
		return
	}
	pidPath := daemon.EnginePIDPath(deps.root)
	pid, err := deps.readPID(pidPath)
	if err != nil || pid <= 0 {
		return
	}
	if !deps.isAlive(pid) {
		return
	}
	if ok, gerr := deps.isGrafel(pid); gerr != nil || !ok {
		return
	}
	_ = deps.kill(pid)
}

// defaultReadEnginePID reads and parses an engine.pid file. Returns an error
// (including os.IsNotExist) when the file is absent or unparseable — the
// caller treats any error as "no orphan to sweep".
func defaultReadEnginePID(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, err
	}
	return pid, nil
}

// Status reports whether the service is installed and/or running.
// It does not modify any state.
func Status(opts Options) (StatusInfo, error) {
	if err := resolveOptions(&opts); err != nil {
		return StatusInfo{}, fmt.Errorf("resolve options: %w", err)
	}
	return status(opts)
}

// RegisteredRoot returns the daemon root directory recorded in the currently
// installed OS service unit (the HOME baked into the launchd plist / systemd
// unit at install time, or the root derived from the installed task's socket on
// Windows). The returned root is the home/GRAFEL_DAEMON_ROOT the LIVE daemon
// actually serves — which is what an uninstall must compare against before
// stopping a GLOBAL service label (issue #5277).
//
// found is false (with a nil error) when no service is installed — there is no
// recorded root to compare, so the caller should treat that as "nothing to
// guard against". A non-nil error means the unit file existed but could not be
// read/parsed; callers should fail closed (skip the stop) on error.
func RegisteredRoot() (root string, found bool, err error) {
	return registeredRoot()
}

// resolveOptions fills in empty Options fields from OS defaults. This
// runs before any platform call so platform code can assume opts is
// complete. Platform-specific path resolution is in service_unix.go and
// service_windows.go.
func resolveOptions(opts *Options) error {
	if opts.BinPath == "" {
		bin, err := os.Executable()
		if err != nil {
			return fmt.Errorf("os.Executable: %w", err)
		}
		opts.BinPath = bin
	}
	return resolvePlatformPaths(opts)
}

// stopRunningDaemon sends a Stop RPC to any daemon currently listening
// on socketPath and waits up to 3 s for the socket to disappear. It is
// called by install before bootstrapping the new service so a leftover
// daemon from a previous session (or a different binary) doesn't hold
// the PID file and block the new one from starting.
//
// Errors are intentionally ignored: if no daemon is running, the RPC
// dial fails silently; if the socket never disappears we proceed anyway
// and let the OS service manager restart the daemon after it crashes.
func stopRunningDaemon(socketPath string) {
	conn, err := transport.DialTimeout(socketPath, time.Second)
	if err != nil {
		return // nothing listening — nothing to stop
	}
	client := rpc.NewClientWithCodec(jsonrpc.NewClientCodec(conn))
	// Fire-and-forget Stop RPC. We don't care about the reply.
	_ = client.Call("Daemon.Stop", struct{}{}, &struct{}{})
	_ = client.Close()

	// Wait up to 3 s for the socket to disappear (daemon shut down).
	end := time.Now().Add(3 * time.Second)
	for time.Now().Before(end) {
		if _, err := os.Stat(socketPath); os.IsNotExist(err) {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
}
