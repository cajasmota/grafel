// Package process provides cross-platform process introspection and
// management without shell-outs to ps/pkill/pgrep. Platform-specific
// implementations live in process_linux.go and process_darwin.go.
//
// API surface:
//
//	FindByName(name) — returns all running processes whose command name
//	                    contains the given string (case-insensitive).
//	Kill(pid)         — sends SIGTERM on unix; calls TerminateProcess on windows.
//	CPUPercent(pid)   — PLATFORM-INCONSISTENT, avoid for new code: on darwin
//	                    it returns instantaneous %cpu (`ps -o %cpu`); on linux
//	                    it returns CUMULATIVE cpu-seconds since start (from
//	                    /proc stat), NOT a percentage; unsupported elsewhere.
//	                    Kept only for the existing hot-loop watchdog. New
//	                    callers that want a real percentage should sample
//	                    CPUTimeSeconds twice and diff over wall-clock.
//	CPUTimeSeconds(pid)— returns pid's CUMULATIVE CPU time (user+system) in
//	                    seconds since start, with IDENTICAL semantics on linux
//	                    and darwin (unsupported elsewhere). Diff two readings
//	                    over a wall-clock interval to get an instantaneous %.
//	RSSBytes(pid)     — returns the resident-set size of pid in bytes.
//	FootprintBytes()  — returns the best honest physical-memory number for
//	                    the CURRENT process plus a label describing what it
//	                    measured (see FootprintBytes for the per-OS caveats).
//
// All functions return errors rather than panicking on unsupported
// platforms, so callers can degrade gracefully.
package process

import (
	"os"
	"runtime"
	"strings"
)

// Footprint is an honest physical-memory reading for the current process.
//
// Bytes is the best number we can obtain WITHOUT cgo. Label names exactly
// what was measured so callers never mislabel it (the bug this type was
// introduced to kill — see #3648). Source is a short machine-readable tag
// ("resident_rss", "memstats_sys") for clients that branch on it.
//
// IMPORTANT macOS caveat: the metric Activity Monitor shows is
// `phys_footprint`, which counts dirty + swapped-out + compressed pages.
// Reading it requires the mach `task_info(TASK_VM_INFO)` trap, which is a
// MIG routine over mach_msg and is not reachable from pure Go without cgo.
// grafel is a pure-Go, cgo-free binary, so on darwin we report the
// RESIDENT set size (`ps -o rss`), which UNDER-counts swapped/compressed
// pages — under heavy memory pressure the real footprint can be much
// larger than what we report here. The Label makes that explicit rather
// than pretending RSS is the footprint.
type Footprint struct {
	Bytes  uint64
	Label  string
	Source string
}

// FootprintBytes returns the most honest physical-memory number available
// for the current process, without cgo. It never errors: on any failure it
// falls back to runtime.MemStats and labels the value accordingly so the
// caller still reports a non-zero, correctly-described number.
//
// Resolution order:
//  1. resident set size via RSSBytes(self) — /proc on Linux, `ps -o rss` on
//     macOS. Honest "resident in RAM"; on macOS it under-counts swapped and
//     compressed pages (see Footprint doc).
//  2. runtime.MemStats.Sys — the virtual address space Go has reserved from
//     the OS. This is NOT process RSS; it is the absolute last resort so the
//     daemon reports SOMETHING when even ps/proc are unavailable.
func FootprintBytes() Footprint {
	if rss, err := RSSBytes(os.Getpid()); err == nil && rss > 0 {
		label := "resident set size (RSS)"
		if runtime.GOOS == "darwin" {
			label = "resident set size (RSS; under-counts swapped/compressed pages — Activity Monitor's phys_footprint may be larger)"
		}
		return Footprint{Bytes: rss, Label: label, Source: "resident_rss"}
	}
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return Footprint{
		Bytes:  ms.Sys,
		Label:  "Go runtime reserved virtual address space (MemStats.Sys — NOT process RSS; ps/proc unavailable)",
		Source: "memstats_sys",
	}
}

// Info describes a running process.
type Info struct {
	PID  int
	PPID int
	// Name is the short command name (basename of the executable).
	Name string
	// Exe is the full path to the executable, if readable.
	Exe string
}

// ErrUnsupported is returned by introspection helpers on platforms where
// process enumeration is not implemented (currently Windows). Callers
// should treat it as "cannot determine" and fall back to a coarser check
// rather than assuming the negative.
var ErrUnsupported = errUnsupported{}

type errUnsupported struct{}

func (errUnsupported) Error() string {
	return "process introspection unsupported on " + runtime.GOOS
}

// PidIsGrafel reports whether the live process with the given pid is an
// grafel binary. It is the PID-reuse-safe companion to a bare
// kill(pid,0) liveness probe: after a daemon dies, its pid can be recycled
// by an unrelated process, and honoring a stale pidfile purely because
// "some process with that pid is alive" produces the false "daemon already
// running" wedge in issue #4549.
//
// Returns:
//   - (true,  nil)            — pid is live AND its command name/exe matches "grafel".
//   - (false, nil)            — pid is not a live grafel process (dead, or a different program).
//   - (false, ErrUnsupported) — this platform cannot enumerate processes; caller must
//     fall back to a coarser liveness check and NOT treat the pid as definitively foreign.
func PidIsGrafel(pid int) (bool, error) {
	if pid <= 0 {
		return false, nil
	}
	// Fast, PID-targeted path: some platforms (Windows) cannot enumerate
	// every process cheaply but CAN open a single pid and read its image
	// path. When the primitive is available we compare the executable
	// basename directly — this is more precise than a name scan and avoids
	// the ErrUnsupported fallback that would otherwise honor a recycled
	// foreign pid as "the daemon".
	if base, ok := pidExeBaseName(pid); ok {
		return exeBaseNameIsGrafel(base), nil
	}

	procs, err := FindByName("grafel")
	if err != nil {
		// Distinguish "platform can't enumerate" from a transient scan error.
		// Both surface as an error so the caller can decide how to degrade,
		// but we normalize the unsupported-platform case to ErrUnsupported.
		if _, ok := err.(errUnsupported); ok {
			return false, ErrUnsupported
		}
		return false, err
	}
	for _, p := range procs {
		if p.PID == pid {
			return true, nil
		}
	}
	return false, nil
}

// exeBaseNameIsGrafel reports whether an executable basename names the grafel
// binary. It mirrors FindByName's case-insensitive substring match, and strips
// a trailing platform extension (".exe" on Windows) so "grafel.exe" matches.
func exeBaseNameIsGrafel(base string) bool {
	b := strings.ToLower(base)
	b = strings.TrimSuffix(b, ".exe")
	return strings.Contains(b, "grafel")
}
