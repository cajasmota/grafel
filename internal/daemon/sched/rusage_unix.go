//go:build unix

package sched

import (
	"os"
	"runtime"
	"syscall"
)

// maxRSSBytes returns the kernel-tracked HIGH-WATER resident-set size of a
// reaped child process, in bytes (#6107, epic #5954).
//
// Why ru_maxrss and not a sampler: the value is maintained by the kernel over
// the child's entire lifetime, so it cannot miss a peak between ticks and
// costs nothing to read — wait4 already fills it in when the daemon reaps the
// child. A sampler over a short-lived child observes zero samples and reports
// zero, which is exactly the defect this replaces.
//
// What it MEANS, precisely: peak resident set size — the largest number of
// bytes of the child's address space that were simultaneously resident in
// physical memory. It is NOT Go heap size, and on Darwin it is an UPPER BOUND
// on the footprint rather than the footprint itself, because pages the Go
// runtime has released with MADV_FREE stay counted as resident until the
// kernel is under pressure and actually reclaims them. Report it as RSS, never
// as "heap".
//
// Units differ by platform and getting this wrong is a 1024x error:
// Darwin reports ru_maxrss in BYTES, every other Unix in KiB.
//
// Returns ok=false when the process has not been reaped or the platform
// supplies no rusage.
func maxRSSBytes(ps *os.ProcessState) (uint64, bool) {
	if ps == nil {
		return 0, false
	}
	ru, okType := ps.SysUsage().(*syscall.Rusage)
	if !okType || ru == nil || ru.Maxrss <= 0 {
		return 0, false
	}
	v := uint64(ru.Maxrss)
	if runtime.GOOS == "darwin" || runtime.GOOS == "ios" {
		return v, true
	}
	return v * 1024, true
}
