//go:build windows

package daemon

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/cajasmota/grafel/internal/daemon/transport"
)

// isWindowsPipePath reports whether path is a Windows named-pipe path
// (starts with \\.\pipe\ or \\?\pipe\).
func isWindowsPipePath(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasPrefix(lower, `\\.\pipe\`) || strings.HasPrefix(lower, `\\?\pipe\`)
}

// DefaultLayout returns the standard on-disk layout for the current user
// on Windows. The socket path is a named-pipe path; SocketDir is empty
// because pipes are not filesystem objects.
//
// When GRAFEL_DAEMON_ROOT is set, that directory is used for all
// filesystem paths (pid file, logs) AND the named-pipe path is scoped to
// that root (via a hash of the root in the pipe name). This mirrors the
// Unix transport, where the socket already lives under the root at
// <root>/sockets/daemon.sock. Without root-scoping the pipe was a single
// process-global object per user, so an isolated daemon (selftest, parallel
// agent, or a second instance with a different root) collided on the shared
// pipe and wedged at socket-listen (issue #5264).
func DefaultLayout() (Layout, error) {
	root := os.Getenv(EnvRoot)
	if root == "" {
		appData := os.Getenv("APPDATA")
		if appData == "" {
			var err error
			appData, err = os.UserHomeDir()
			if err != nil {
				return Layout{}, err
			}
		}
		root = filepath.Join(appData, "grafel")
	}

	// Derive the pipe name from the SAME root used for the filesystem paths
	// so the listen side and the dial side (which both call DefaultLayout)
	// agree on the pipe name.
	pipePath := transport.WindowsPipeName(root)

	logDir := logDirForRoot(root)
	// See no-rotation contract in layoutFromRoot (paths.go).
	return Layout{
		Root:       root,
		SocketDir:  "", // named pipes have no filesystem directory
		SocketPath: pipePath,
		PIDPath:    filepath.Join(root, "daemon.pid"),
		LogDir:     logDir,
		LogPath:    logPathForRoot(root),
	}, nil
}
