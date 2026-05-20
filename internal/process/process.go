// Package process provides cross-platform process introspection and
// management without shell-outs to ps/pkill/pgrep. Platform-specific
// implementations live in process_linux.go and process_darwin.go.
//
// API surface:
//
//	FindByName(name) — returns all running processes whose command name
//	                    contains the given string (case-insensitive).
//	Kill(pid)         — sends SIGTERM on unix; calls TerminateProcess on windows.
//	CPUPercent(pid)   — returns the instantaneous CPU percent for pid.
//	RSSBytes(pid)     — returns the resident-set size of pid in bytes.
//
// All functions return errors rather than panicking on unsupported
// platforms, so callers can degrade gracefully.
package process

// Info describes a running process.
type Info struct {
	PID  int
	PPID int
	// Name is the short command name (basename of the executable).
	Name string
	// Exe is the full path to the executable, if readable.
	Exe string
}
