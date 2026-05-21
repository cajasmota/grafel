//go:build windows

package daemon

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/cajasmota/archigraph/internal/daemon/transport"
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
// When ARCHIGRAPH_DAEMON_ROOT is set, that directory is used for all
// filesystem paths (pid file, logs). The named-pipe path is always
// user-scoped and does not change based on ARCHIGRAPH_DAEMON_ROOT.
func DefaultLayout() (Layout, error) {
	pipePath := transport.WindowsPipeName()

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
		root = filepath.Join(appData, "archigraph")
	}

	logDir := filepath.Join(root, "logs")
	return Layout{
		Root:       root,
		SocketDir:  "", // named pipes have no filesystem directory
		SocketPath: pipePath,
		PIDPath:    filepath.Join(root, "daemon.pid"),
		LogDir:     logDir,
		LogPath:    filepath.Join(logDir, "daemon.log"),
	}, nil
}
