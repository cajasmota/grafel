// Package service handles registration and removal of the archigraph
// daemon as an OS-level user service (launchd on macOS, systemd on
// Linux). It is the implementation layer for the `archigraph install`
// and `archigraph uninstall` commands introduced in ADR-0017 Phase C.
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
	"net"
	"net/rpc"
	"net/rpc/jsonrpc"
	"os"
	"time"
)

// Options carries install-time parameters. All fields that are empty
// strings are resolved to defaults by Install.
type Options struct {
	// BinPath is the absolute path to the archigraph binary. When
	// empty, Install resolves it via os.Executable().
	BinPath string

	// SocketPath is the Unix-domain socket the daemon listens on.
	// When empty it defaults to ~/.archigraph/sockets/daemon.sock
	// (matches daemon.DefaultLayout).
	SocketPath string

	// LogDir is the directory for stdout/stderr logs. When empty it
	// defaults to ~/.archigraph/logs.
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

// Install registers the archigraph daemon as a user-level OS service
// and starts it. Idempotent: if the service is already installed it
// returns the current status without modifying anything.
//
// On macOS it writes ~/Library/LaunchAgents/com.archigraph.daemon.plist
// and calls `launchctl bootstrap gui/$UID`.
//
// On Linux it writes ~/.config/systemd/user/archigraph-daemon.service
// and calls `systemctl --user enable --now`.
//
// After loading, Install waits up to 5 s for the daemon socket to
// appear, then returns the populated StatusInfo.
func Install(opts Options) (StatusInfo, error) {
	if err := resolveOptions(&opts); err != nil {
		return StatusInfo{}, fmt.Errorf("resolve options: %w", err)
	}
	return install(opts)
}

// Uninstall stops and removes the OS service registration. Idempotent:
// if the service is not installed it returns immediately without error.
func Uninstall(opts Options) error {
	if err := resolveOptions(&opts); err != nil {
		return fmt.Errorf("resolve options: %w", err)
	}
	return uninstall(opts)
}

// Status reports whether the service is installed and/or running.
// It does not modify any state.
func Status(opts Options) (StatusInfo, error) {
	if err := resolveOptions(&opts); err != nil {
		return StatusInfo{}, fmt.Errorf("resolve options: %w", err)
	}
	return status(opts)
}

// resolveOptions fills in empty Options fields from OS defaults. This
// runs before any platform call so platform code can assume opts is
// complete.
func resolveOptions(opts *Options) error {
	if opts.BinPath == "" {
		bin, err := os.Executable()
		if err != nil {
			return fmt.Errorf("os.Executable: %w", err)
		}
		opts.BinPath = bin
	}
	if opts.SocketPath == "" || opts.LogDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("os.UserHomeDir: %w", err)
		}
		if opts.SocketPath == "" {
			opts.SocketPath = home + "/.archigraph/sockets/daemon.sock"
		}
		if opts.LogDir == "" {
			opts.LogDir = home + "/.archigraph/logs"
		}
	}
	return nil
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
	conn, err := net.DialTimeout("unix", socketPath, time.Second)
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

// waitForSocket polls socketPath until the file appears and is
// connectable, or deadline is exceeded. Returns nil on success.
func waitForSocket(socketPath string, deadline time.Duration) error {
	const pollInterval = 200 * time.Millisecond
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		conn, err := net.DialTimeout("unix", socketPath, 200*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(pollInterval)
	}
	return fmt.Errorf("socket %s did not appear within %s", socketPath, deadline)
}
