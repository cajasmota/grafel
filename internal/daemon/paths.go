// Package daemon implements the long-running archigraph process per
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
// log directory. Everything lives under ~/.archigraph/ so the daemon
// shares state with the existing registry and group configs.
//
// On Windows, SocketPath holds the named-pipe path instead of a filesystem
// path (e.g. \\.\pipe\archigraph-daemon-<user>), and SocketDir is empty
// because named pipes are not filesystem objects that require a directory.
type Layout struct {
	Root       string // ~/.archigraph (or %APPDATA%\archigraph on Windows)
	SocketDir  string // ~/.archigraph/sockets (empty on Windows)
	SocketPath string // ~/.archigraph/sockets/daemon.sock  OR  \\.\pipe\archigraph-daemon-<user>
	PIDPath    string // ~/.archigraph/daemon.pid
	LogDir     string // ~/.archigraph/logs
	LogPath    string // ~/.archigraph/logs/daemon.log
}

// EnvRoot is honoured by DefaultLayout when set; lets tests point a
// freshly-built daemon at a tempdir without touching the real ~/.archigraph.
const EnvRoot = "ARCHIGRAPH_DAEMON_ROOT"

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
// on all platforms when ARCHIGRAPH_DAEMON_ROOT is set.
func layoutFromRoot(root, socketPath string) Layout {
	socketDir := ""
	if socketPath != "" && !isWindowsPipePath(socketPath) {
		socketDir = filepath.Dir(socketPath)
	}
	logDir := filepath.Join(root, "logs")
	return Layout{
		Root:       root,
		SocketDir:  socketDir,
		SocketPath: socketPath,
		PIDPath:    filepath.Join(root, "daemon.pid"),
		LogDir:     logDir,
		LogPath:    filepath.Join(logDir, "daemon.log"),
	}
}
