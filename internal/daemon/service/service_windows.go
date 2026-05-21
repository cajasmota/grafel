//go:build windows

package service

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/cajasmota/archigraph/internal/daemon/transport"
)

// resolvePlatformPaths fills in SocketPath and LogDir for Windows using
// %APPDATA%\archigraph as the base directory and a named-pipe path for
// SocketPath.
func resolvePlatformPaths(opts *Options) error {
	if opts.SocketPath == "" {
		opts.SocketPath = transport.WindowsPipeName()
	}
	if opts.LogDir == "" {
		appData := os.Getenv("APPDATA")
		if appData == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("os.UserHomeDir: %w", err)
			}
			appData = home
		}
		opts.LogDir = filepath.Join(appData, "archigraph", "logs")
	}
	return nil
}
