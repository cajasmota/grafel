package sched

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// currentProcessRSSMB returns the current process resident-set size in
// megabytes. Used by the per-job sampler to capture the daemon's peak
// during an index. The implementation is best-effort: on darwin it
// shells out to `ps -o rss=`; on linux it reads /proc/self/status; on
// any other platform it falls back to runtime.MemStats.Sys.
//
// The function MUST NOT block — pollers call it on a 5s tick. The ps
// shell-out costs ~1ms on macOS in practice.
func currentProcessRSSMB() int64 {
	switch runtime.GOOS {
	case "linux":
		b, err := os.ReadFile("/proc/self/status")
		if err == nil {
			for _, line := range strings.Split(string(b), "\n") {
				if strings.HasPrefix(line, "VmRSS:") {
					fields := strings.Fields(line)
					if len(fields) >= 2 {
						kb, _ := strconv.ParseInt(fields[1], 10, 64)
						return kb / 1024
					}
				}
			}
		}
	case "darwin":
		pid := strconv.Itoa(os.Getpid())
		out, err := exec.Command("ps", "-o", "rss=", "-p", pid).Output()
		if err == nil {
			kb, _ := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
			return kb / 1024
		}
	}
	// Fallback — runtime memstats is a poor RSS proxy (it reports Go
	// heap + arenas, not the OS-level RSS) but it never errors and
	// keeps the sampler functional in tests.
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return int64(ms.Sys / (1024 * 1024))
}
