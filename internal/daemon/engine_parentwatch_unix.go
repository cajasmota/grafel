//go:build darwin || linux

package daemon

import "os"

// parentWatchGetppid returns the parent-pid observer the engine's
// parent-death watchdog polls on Unix/macOS: os.Getppid directly. When the
// original serve parent dies uncleanly, the OS reparents this process to
// init (or an equivalent pid-namespace init), so os.Getppid()'s return value
// changes — the exact signal startParentDeathWatchdog compares against the
// recorded original parent pid.
func parentWatchGetppid() func() int {
	return os.Getppid
}
