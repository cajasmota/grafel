// Package daemon implements the long-running grafel process per
// ADR-0017. It exposes a JSON-RPC service over a platform-appropriate IPC
// transport (Unix-domain socket on Linux/macOS, named pipe on Windows) and
// owns indexing, cross-repo linking, and (in later phases) fsnotify
// watchers and the MCP query surface.
package daemon

import (
	"os"
	"path/filepath"
)

const (
	// UnixSocketPathMax is the kernel limit for sun_path on most Unix systems.
	UnixSocketPathMax = 104
)

// Layout is the on-disk footprint of a daemon: socket path, pid file,
// log directory. Everything lives under ~/.grafel/ so the daemon
// shares state with the existing registry and group configs.
//
// On Windows, SocketPath holds the named-pipe path instead of a filesystem
// path (e.g. \\.\pipe\grafel-daemon-<user>), and SocketDir is empty
// because named pipes are not filesystem objects that require a directory.
type Layout struct {
	Root       string // ~/.grafel (or %APPDATA%\grafel on Windows)
	SocketDir  string // ~/.grafel/sockets (empty on Windows)
	SocketPath string // ~/.grafel/sockets/daemon.sock  OR  \\.\pipe\grafel-daemon-<user>
	PIDPath    string // ~/.grafel/daemon.pid
	LogDir     string // ~/.grafel/logs
	LogPath    string // ~/.grafel/logs/daemon.log
}

// EnvRoot is honoured by DefaultLayout when set; lets tests point a
// freshly-built daemon at a tempdir without touching the real ~/.grafel.
const EnvRoot = "GRAFEL_DAEMON_ROOT"

// EnginePIDPath returns the split-mode engine child's pidfile path
// (<root>/engine.pid), distinct from serve's daemon.pid so the two planes can
// coexist under one daemon root without contending for a single pidfile
// (ADR-0024, epic #5729). The supervising serve process owns daemon.pid; the
// engine child writes engine.pid.
func EnginePIDPath(root string) string {
	return filepath.Join(root, "engine.pid")
}

// EnsureLayout creates the directories the daemon writes to. The socket
// path itself is not created here — the listener does that. Permissions
// are 0700/0600 because the daemon shares state across nothing and the
// socket is per-user. Note that when XDG_RUNTIME_DIR is used, the socket
// directory may not be under l.Root.
func EnsureLayout(l Layout) error {
	dirs := []string{l.Root, l.SocketDir, l.LogDir}
	// Remove duplicates (happens if SocketDir is outside Root or empty on Windows)
	seen := make(map[string]bool)
	for _, d := range dirs {
		if d == "" {
			continue
		}
		if !seen[d] {
			seen[d] = true
			if err := os.MkdirAll(d, 0o700); err != nil {
				return err
			}
		}
	}
	return nil
}

// layoutFromRoot builds a Layout rooted at root. Used by DefaultLayout
// on all platforms when GRAFEL_DAEMON_ROOT is set.
func layoutFromRoot(root, socketPath string) Layout {
	socketDir := ""
	if socketPath != "" && !isWindowsPipePath(socketPath) {
		socketDir = filepath.Dir(socketPath)
	}
	logDir := filepath.Join(root, "logs")
	// No-rotation contract (issue #2300):
	// daemon.log grows monotonically by design. The bench harness
	// (skills/grafel-graph-quality/prompts/03-with-mcp-run.md) uses
	// byte offsets into daemon.log and assumes the file is append-only and
	// never truncated or renamed. If log rotation is ever added, the bench
	// skill will need a sidecar offset-translation strategy to remain
	// correct. Operators who need bounded log sizes should use an external
	// tool (logrotate, newsyslog) with copytruncate semantics, and must
	// update the bench skill accordingly.
	return Layout{
		Root:       root,
		SocketDir:  socketDir,
		SocketPath: socketPath,
		PIDPath:    filepath.Join(root, "daemon.pid"),
		LogDir:     logDir,
		LogPath:    filepath.Join(logDir, "daemon.log"),
	}
}
