//go:build !darwin && !linux

package process

// listWatchProcesses is not implemented on platforms without cheap full-argv
// process enumeration (Windows and others). It returns ErrUnsupported so the
// daemon's watcher reaper treats the platform as "cannot enumerate" and skips
// the foreign/orphan-watcher sweep rather than failing — the watchreg-based
// reaper (#5142) remains the fallback on these platforms.
func listWatchProcesses() ([]WatchProc, error) {
	return nil, ErrUnsupported
}
