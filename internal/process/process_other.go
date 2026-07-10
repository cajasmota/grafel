//go:build !linux && !darwin && !windows

package process

import (
	"fmt"
	"runtime"
)

// FindByName is not implemented on this platform. It returns an error
// with a note about the unsupported OS. On Windows, callers should use
// the Windows Management Instrumentation (WMI) API or tasklist.exe
// instead; this is tracked as a future improvement.
func FindByName(_ string) ([]Info, error) {
	return nil, ErrUnsupported
}

// Kill sends a termination signal to the given PID.
// On Windows, os.FindProcess + Process.Kill() is used.
func Kill(pid int) error {
	return fmt.Errorf("process.Kill: unsupported platform %s", runtime.GOOS)
}

// ForceKill sends an unconditional termination signal to the given PID.
func ForceKill(pid int) error {
	return fmt.Errorf("process.ForceKill: unsupported platform %s", runtime.GOOS)
}

// CPUPercent is not implemented on this platform.
func CPUPercent(_ int) (float64, error) {
	return 0, fmt.Errorf("process.CPUPercent: unsupported platform %s", runtime.GOOS)
}

// RSSBytes is not implemented on this platform.
func RSSBytes(_ int) (uint64, error) {
	return 0, fmt.Errorf("process.RSSBytes: unsupported platform %s", runtime.GOOS)
}
