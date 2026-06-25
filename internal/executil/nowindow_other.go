//go:build !windows

package executil

import "os/exec"

// NoWindow is a no-op on non-Windows platforms.
func NoWindow(cmd *exec.Cmd) {}
