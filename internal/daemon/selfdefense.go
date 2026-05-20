package daemon

// selfdefense.go — daemon hot-loop runway prevention (issue #857)
//
// Two protection layers:
//
//  Layer 1 — startup conflict check:
//    If this binary lives under /tmp AND another archigraph daemon is running
//    from a non-/tmp canonical path, refuse to start. Agents in isolated
//    /tmp worktrees should not displace the user's permanent daemon.
//
//  Layer 2 — CPU watchdog:
//    If ARCHIGRAPH_DAEMON_ROOT is set AND the binary is under /tmp (i.e., the
//    daemon is an ephemeral test/agent instance), install a background goroutine
//    that self-terminates after 5 minutes of sustained >500% CPU with no
//    inflight work. This is the last-resort safety net for the hot-loop scenario
//    observed on 2026-05-20 (load average 189, fans spiking).

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"runtime/pprof"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cajasmota/archigraph/internal/process"
)

// isTmpPath reports whether path starts with /tmp (a hard-coded exclusion zone
// for canonical daemons; see issue #857).
func isTmpPath(path string) bool {
	return strings.HasPrefix(path, "/tmp/") || path == "/tmp"
}

// SelfDefenseCheck performs the Layer 1 startup conflict check. It should be
// called once at daemon startup, before the listener is opened. If this binary
// lives under /tmp AND a canonical (non-/tmp) archigraph daemon is already
// running, the function returns a descriptive error and the caller must exit.
//
// logger may be nil; a default stderr logger will be used.
func SelfDefenseCheck(logger *log.Logger) error {
	if logger == nil {
		logger = log.New(os.Stderr, "archigraph-daemon: ", log.LstdFlags)
	}

	self, err := os.Executable()
	if err != nil {
		// Can't determine our own path — skip check rather than blocking startup.
		logger.Printf("selfdefense: os.Executable() failed: %v (skipping check)", err)
		return nil
	}

	if !isTmpPath(self) {
		return nil // canonical binary — no restriction
	}

	// This binary is under /tmp — check for a conflicting canonical daemon.
	canonPID, canonExe := findCanonicalDaemon()
	if canonPID > 0 {
		return fmt.Errorf(
			"daemon refusing to start: another daemon (pid %d) is running on the canonical socket "+
				"from %s; this binary at %s is under /tmp and should not displace it. "+
				"Run 'archigraph doctor --kill-stale' to clean up stale processes.",
			canonPID, canonExe, self)
	}
	return nil
}

// StartCPUWatchdog starts the Layer 2 CPU watchdog goroutine for ephemeral
// daemons. It should be called once after the service is fully constructed,
// passing the service's in-flight counter so the watchdog can distinguish
// actual hot-loops from legitimate sustained work.
//
// The watchdog is only active when:
//  1. The binary path is under /tmp, AND
//  2. ARCHIGRAPH_DAEMON_ROOT is set (agent isolation pattern)
//
// Both conditions must hold to avoid killing legitimate short-lived binaries
// that happen to live under /tmp.
func StartCPUWatchdog(inflightCounter *int64, logger *log.Logger) {
	if logger == nil {
		logger = log.New(os.Stderr, "archigraph-daemon: ", log.LstdFlags)
	}

	self, err := os.Executable()
	if err != nil {
		return
	}
	if !isTmpPath(self) || os.Getenv(EnvRoot) == "" {
		return // not an ephemeral agent daemon — skip
	}

	var counter *int64
	if inflightCounter != nil {
		counter = inflightCounter
	} else {
		var zero int64
		counter = &zero
	}
	go cpuWatchdog(counter, logger)
}

// FindCanonicalDaemon is the exported counterpart of findCanonicalDaemon,
// exposed for testing and for archigraph doctor's process scan. It returns
// the pid and executable path of the first running archigraph daemon whose
// binary is NOT under /tmp, or (0, "") if none is found.
func FindCanonicalDaemon() (pid int, exe string) {
	return findCanonicalDaemon()
}

// findCanonicalDaemon scans running processes for another archigraph daemon
// whose binary path is NOT under /tmp. It returns the pid and executable path
// of the first match, or (0, "") if none is found.
//
// Uses the cross-platform process package which reads /proc on Linux and
// invokes ps on macOS — no shell-out to ps on Linux.
func findCanonicalDaemon() (pid int, exe string) {
	myPID := os.Getpid()

	procs, err := process.FindByName("archigraph")
	if err != nil {
		return 0, ""
	}

	for _, p := range procs {
		if p.PID == myPID {
			continue
		}
		cmdBin := p.Exe
		if cmdBin == "" {
			cmdBin = p.Name
		}
		if isTmpPath(cmdBin) {
			continue // also a temp daemon — not canonical
		}
		if strings.Contains(strings.ToLower(cmdBin), "archigraph") {
			return p.PID, cmdBin
		}
	}
	return 0, ""
}

// cpuWatchdog monitors CPU usage of the current process. If the process
// consumes >500% CPU for 5 consecutive minutes with no inflight work, it logs
// a pprof goroutine dump and calls os.Exit(0).
//
// CPU percentage is measured as: (user+sys CPU time delta) / wall time * 100.
// Because Go's runtime/pprof doesn't expose per-process CPU %, we shell out
// to `ps -o %cpu=` — the same approach as the RSS sampler in sched/rss_proc.go.
//
// The watchdog targets the specific failure mode from issue #857:
//   - parent exits, daemon adopted by launchd (PPID=1)
//   - daemon enters hot-loop at ~1000% CPU
//   - no inflight RPC work
//   - watchdog self-terminates within ~5 minutes
func cpuWatchdog(inflight *int64, logger *log.Logger) {
	const (
		pollInterval    = 60 * time.Second
		cpuThresholdPct = 500.0
		sustainedTicks  = 5 // 5 × 60s = 5 minutes
	)
	logger.Printf("selfdefense: CPU watchdog started (threshold=%.0f%% for %d ticks)",
		cpuThresholdPct, sustainedTicks)

	hotTicks := 0
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for range ticker.C {
		if atomic.LoadInt64(inflight) > 0 {
			// Real work in flight — reset the counter; don't kill during legit work.
			hotTicks = 0
			continue
		}
		pct := selfCPUPercent()
		if pct > cpuThresholdPct {
			hotTicks++
			logger.Printf("selfdefense: hot-loop suspected (cpu=%.1f%% inflight=0 ticks=%d/%d)",
				pct, hotTicks, sustainedTicks)
			if hotTicks >= sustainedTicks {
				// Dump a goroutine profile to stderr before exiting so the operator
				// can inspect what was spinning. This is Layer 2 of the diagnostic.
				dumpGoroutineProfile(logger)
				logger.Printf("selfdefense: self-detecting hot-loop (cpu=%.1f%%), exiting", pct)
				os.Exit(0)
			}
		} else {
			if hotTicks > 0 {
				logger.Printf("selfdefense: CPU normalised (%.1f%%), resetting counter", pct)
			}
			hotTicks = 0
		}
	}
}

// selfCPUPercent returns the instantaneous CPU percentage of the current
// process using the cross-platform process package. Returns 0 on error.
func selfCPUPercent() float64 {
	pct, err := process.CPUPercent(os.Getpid())
	if err != nil {
		return 0
	}
	return pct
}

// dumpGoroutineProfile writes a goroutine stack dump (for diagnosing the hot
// function) to a temporary file and logs the path. Also logs a brief in-memory
// summary to logger. This is the Layer 2 pprof integration mentioned in #857.
func dumpGoroutineProfile(logger *log.Logger) {
	var buf bytes.Buffer
	p := pprof.Lookup("goroutine")
	if p == nil {
		return
	}
	if err := p.WriteTo(&buf, 1); err != nil {
		logger.Printf("selfdefense: goroutine dump failed: %v", err)
		return
	}

	f, err := os.CreateTemp("", "archigraph-hotloop-*.pprof.txt")
	if err != nil {
		logger.Printf("selfdefense: goroutine dump (inline):\n%s", buf.String())
		return
	}
	_, _ = f.Write(buf.Bytes())
	_ = f.Close()
	logger.Printf("selfdefense: goroutine dump written to %s", f.Name())
}
