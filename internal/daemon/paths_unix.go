//go:build !windows

package daemon

import (
	"fmt"
	"os"
	"path/filepath"
)

// isWindowsPipePath always returns false on non-Windows platforms.
func isWindowsPipePath(_ string) bool { return false }

// selectSocketPath returns a suitable Unix-domain socket path, trying
// XDG_RUNTIME_DIR first (Linux standard) then falling back to
// ~/.archigraph/sockets. Returns an error if both candidate paths exceed
// the kernel's sun_path limit (104 bytes on most Unix systems).
func selectSocketPath() (string, error) {
	// Prefer XDG_RUNTIME_DIR when set — common on Linux desktop sessions.
	xdgRuntime := os.Getenv("XDG_RUNTIME_DIR")
	if xdgRuntime != "" {
		path := filepath.Join(xdgRuntime, "archigraph", "daemon.sock")
		if len(path) <= UnixSocketPathMax {
			return path, nil
		}
	}

	// Fall back to ~/.archigraph/sockets/daemon.sock
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(home, ".archigraph", "sockets", "daemon.sock")
	if len(path) <= UnixSocketPathMax {
		return path, nil
	}

	return "", fmt.Errorf("socket path exceeds %d character limit: "+
		"XDG_RUNTIME_DIR=%q, home=%q", UnixSocketPathMax, xdgRuntime, home)
}

// DefaultLayout returns the standard on-disk layout for the current
// user. It does not create directories; callers responsible for daemon
// startup invoke EnsureLayout. We split the two so client-side code
// (which only reads the socket path) does not have to be a writer.
//
// When ARCHIGRAPH_DAEMON_ROOT is set, that directory replaces ~/.archigraph
// for all paths in the returned layout. This is used exclusively by tests.
func DefaultLayout() (Layout, error) {
	root := os.Getenv(EnvRoot)
	if root != "" {
		// Test / agent isolation mode — use root for everything including socket.
		socketDir := filepath.Join(root, "sockets")
		socketPath := filepath.Join(socketDir, "daemon.sock")
		logDir := filepath.Join(root, "logs")
		return Layout{
			Root:       root,
			SocketDir:  socketDir,
			SocketPath: socketPath,
			PIDPath:    filepath.Join(root, "daemon.pid"),
			LogDir:     logDir,
			LogPath:    filepath.Join(logDir, "daemon.log"),
		}, nil
	}

	socketPath, err := selectSocketPath()
	if err != nil {
		return Layout{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Layout{}, err
	}
	root = filepath.Join(home, ".archigraph")
	return Layout{
		Root:       root,
		SocketDir:  filepath.Dir(socketPath),
		SocketPath: socketPath,
		PIDPath:    filepath.Join(root, "daemon.pid"),
		LogDir:     filepath.Join(root, "logs"),
		LogPath:    filepath.Join(root, "logs", "daemon.log"),
	}, nil
}
