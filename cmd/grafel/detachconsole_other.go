//go:build !windows

package main

// detachConsole is a no-op on non-Windows platforms. On macOS/Linux the daemon
// is launched by launchd/systemd (or `grafel start` with Setsid), which already
// detach it from any controlling terminal — there is no console to free.
func detachConsole() {}
