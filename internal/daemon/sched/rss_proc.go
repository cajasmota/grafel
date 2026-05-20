package sched

import (
	"os"
	"runtime"

	"github.com/cajasmota/archigraph/internal/process"
)

// currentProcessRSSMB returns the current process resident-set size in
// megabytes. Used by the per-job sampler to capture the daemon's peak
// during an index. The implementation is best-effort: it delegates to
// the cross-platform process.RSSBytes which reads /proc on Linux and
// uses ps on macOS. Any other platform falls back to runtime.MemStats.Sys.
//
// The function MUST NOT block — pollers call it on a 5s tick.
func currentProcessRSSMB() int64 {
	rss, err := process.RSSBytes(os.Getpid())
	if err == nil {
		return int64(rss / (1024 * 1024))
	}
	// Fallback — runtime memstats is a poor RSS proxy (it reports Go
	// heap + arenas, not the OS-level RSS) but it never errors and
	// keeps the sampler functional in tests.
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return int64(ms.Sys / (1024 * 1024))
}
