//go:build !windows

package service

import (
	"fmt"
	"os"
)

// resolvePlatformPaths fills in SocketPath and LogDir for Unix platforms
// using ~/.archigraph as the base directory.
func resolvePlatformPaths(opts *Options) error {
	if opts.SocketPath != "" && opts.LogDir != "" {
		return nil
	}
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
	return nil
}
