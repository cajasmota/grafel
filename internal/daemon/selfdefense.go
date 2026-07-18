package daemon

// selfdefense.go — daemon hot-loop runway prevention (issue #857)
//
// Two protection layers:
//
//  Layer 1 — startup conflict check:
//    If this binary lives under /tmp AND another grafel daemon is running
//    from a non-/tmp canonical path, refuse to start. Agents in isolated
//    /tmp worktrees should not displace the user's permanent daemon.
//
//  Layer 2 — CPU watchdog:
//    If GRAFEL_DAEMON_ROOT is set AND the binary is under /tmp (i.e., the
//    daemon is an ephemeral test/agent instance), install a background goroutine
//    that self-terminates after 5 minutes of sustained >500% CPU with no
//    inflight work. This is the last-resort safety net for the hot-loop scenario
//    observed on 2026-05-20 (load average 189, fans spiking).

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cajasmota/grafel/internal/process"
)

// EnvDisableSelfDefense, when set to a truthy value ("1", "true"), disables the
// Layer-1 startup conflict check in SelfDefenseCheck. It exists solely so the
// daemon integration tests can boot a fully-isolated daemon (its own
// GRAFEL_DAEMON_ROOT + per-test socket) on a machine where a real canonical
// daemon is already running.
//
// Without this seam, every in-test daemon whose test binary lives under /tmp
// (which is where `go test` compiles and runs its binary) is refused startup by
// SelfDefenseCheck the moment any canonical daemon is alive — producing the
// "daemon never became ready" timeouts tracked in #4022. The in-test daemons
// never touch the canonical socket (they use isolated roots/sockets), so the
// anti-displacement protection the check provides is not needed for them.
//
// This is NEVER set in production: cmd/grafel does not set it, and the
// canonical daemon installed via launchd/systemd runs from a non-/tmp path so
// the check is a no-op for it regardless.
const EnvDisableSelfDefense = "GRAFEL_DISABLE_SELFDEFENSE"

// canonicalBasenames is the set of binary base-names that identify a genuine
// grafel daemon process. A process whose executable base-name is NOT in
// this set is never treated as canonical, even if the full path contains the
// substring "grafel" (e.g. node_modules paths like
// "webui-v2/node_modules/@esbuild/darwin-arm64/bin/esbuild" — see #1719).
var canonicalBasenames = map[string]bool{
	"grafel": true,
}

// isTmpPath reports whether path starts with /tmp (a hard-coded exclusion zone
// for canonical daemons; see issue #857).
func isTmpPath(path string) bool {
	return strings.HasPrefix(path, "/tmp/") || path == "/tmp"
}

// SelfDefenseCheck performs the Layer 1 startup conflict check. It should be
// called once at daemon startup, before the listener is opened. If this binary
// lives under /tmp AND a canonical (non-/tmp) grafel daemon is already
// running, the function returns a descriptive error and the caller must exit.
//
// logger may be nil; a default stderr logger will be used.
func SelfDefenseCheck(logger *slog.Logger) error {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil)).With("pkg", "selfdefense")
	}

	// Test isolation seam (#4022): allow a fully-isolated in-test daemon to boot
	// even when a real canonical daemon is running. See EnvDisableSelfDefense.
	if v := os.Getenv(EnvDisableSelfDefense); v == "1" || strings.EqualFold(v, "true") {
		logger.Warn("selfdefense: Layer-1 conflict check disabled via " + EnvDisableSelfDefense)
		return nil
	}

	self, err := os.Executable()
	if err != nil {
		// Can't determine our own path — skip check rather than blocking startup.
		logger.Warn("selfdefense: os.Executable() failed (skipping check)", "err", err)
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
				"Run 'grafel doctor --kill-stale' to clean up stale processes.",
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
//  2. GRAFEL_DAEMON_ROOT is set (agent isolation pattern)
//
// Both conditions must hold to avoid killing legitimate short-lived binaries
// that happen to live under /tmp.
func StartCPUWatchdog(inflightCounter *int64, logger *slog.Logger) {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil)).With("pkg", "selfdefense")
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
// exposed for testing and for grafel doctor's process scan. It returns
// the pid and executable path of the first running grafel daemon whose
// binary is NOT under /tmp, or (0, "") if none is found.
func FindCanonicalDaemon() (pid int, exe string) {
	return findCanonicalDaemon()
}

// findCanonicalDaemon scans running processes for another grafel daemon
// whose binary path is NOT under /tmp. It returns the pid and executable path
// of the first match, or (0, "") if none is found.
//
// Uses the cross-platform process package which reads /proc on Linux and
// invokes ps on macOS — no shell-out to ps on Linux.
func findCanonicalDaemon() (pid int, exe string) {
	myPID := os.Getpid()

	procs, err := process.FindByName("grafel")
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
		// Use an exact basename match rather than a substring search on the
		// full path. A full-path substring check false-positives on executables
		// whose *directory* contains "grafel" — the classic example is an
		// esbuild binary at webui-v2/node_modules/@esbuild/darwin-arm64/bin/esbuild
		// when the project root is named "grafel" (fixes #1719).
		if canonicalBasenames[strings.ToLower(filepath.Base(cmdBin))] {
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
func cpuWatchdog(inflight *int64, logger *slog.Logger) {
	const (
		pollInterval    = 60 * time.Second
		cpuThresholdPct = 500.0
		sustainedTicks  = 5 // 5 × 60s = 5 minutes
	)
	logger.Info("selfdefense: CPU watchdog started", "threshold_pct", cpuThresholdPct, "sustained_ticks", sustainedTicks)

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
			logger.Warn("selfdefense: hot-loop suspected", "cpu_pct", pct, "inflight", 0, "ticks", hotTicks, "max_ticks", sustainedTicks)
			if hotTicks >= sustainedTicks {
				// Dump a goroutine profile to stderr before exiting so the operator
				// can inspect what was spinning. This is Layer 2 of the diagnostic.
				goroutineDumpPath := dumpGoroutineProfile(logger)
				// #5822 sub-ask 2: also capture a heap profile alongside the
				// goroutine dump — a hot-loop trip is exactly the moment a heap
				// blowup (the dogfooding report's 20GB case) needs to be diagnosed,
				// and the process is about to exit so the live /debug/pprof
				// endpoint (even when enabled) would no longer be reachable.
				dumpHeapProfile(logger, goroutineDumpPath)
				logger.Error("selfdefense: self-detecting hot-loop, exiting", "cpu_pct", pct)
				os.Exit(0)
			}
		} else {
			if hotTicks > 0 {
				logger.Info("selfdefense: CPU normalised, resetting counter", "cpu_pct", pct)
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
//
// Returns the path the dump was written to, or "" if it could only log inline
// (e.g. os.CreateTemp failed). The path is used by dumpHeapProfile to derive a
// sibling filename for the paired heap profile (#5822 sub-ask 2).
func dumpGoroutineProfile(logger *slog.Logger) string {
	var buf bytes.Buffer
	p := pprof.Lookup("goroutine")
	if p == nil {
		return ""
	}
	if err := p.WriteTo(&buf, 1); err != nil {
		logger.Error("selfdefense: goroutine dump failed", "err", err)
		return ""
	}

	f, err := os.CreateTemp("", "grafel-hotloop-*.pprof.txt")
	if err != nil {
		logger.Info("selfdefense: goroutine dump (inline)", "dump", buf.String())
		return ""
	}
	_, _ = f.Write(buf.Bytes())
	_ = f.Close()
	logger.Info("selfdefense: goroutine dump written", "path", f.Name())
	return f.Name()
}

// dumpHeapProfile writes a heap profile to a file sibling to the goroutine
// dump written by dumpGoroutineProfile, so a watchdog hot-loop trip captures
// the heap without needing the live /debug/pprof/heap endpoint (which, even
// when enabled via GRAFEL_DEBUG_PPROF, is about to become unreachable — the
// process calls os.Exit(0) right after this). #5822 sub-ask 2.
//
// runtime.GC() runs first so the profile reflects live (reachable) heap
// objects rather than garbage still pending collection — the standard
// operator workflow for a heap-blowup investigation.
func dumpHeapProfile(logger *slog.Logger, goroutineDumpPath string) {
	p := pprof.Lookup("heap")
	if p == nil {
		return
	}
	runtime.GC()

	heapPath := siblingHeapPath(goroutineDumpPath)
	f, err := os.Create(heapPath)
	if err != nil {
		// The sibling path may be unusable if dumpGoroutineProfile itself
		// failed to create its file in the same directory; fall back to an
		// independent temp file rather than losing the heap capture.
		f, err = os.CreateTemp("", "grafel-hotloop-*.heap.pprof")
		if err != nil {
			logger.Error("selfdefense: heap profile: create file failed", "err", err)
			return
		}
	}
	defer f.Close()
	if err := p.WriteTo(f, 0); err != nil {
		logger.Error("selfdefense: heap dump failed", "err", err)
		return
	}
	logger.Info("selfdefense: heap profile written", "path", f.Name())
}

// siblingHeapPath derives the heap-profile filename that sits next to the
// goroutine dump written by dumpGoroutineProfile (same directory, correlated
// name), so an operator can find both halves of one hot-loop trip together.
// Falls back to a fresh temp path when goroutineDumpPath is empty (the
// goroutine dump failed to write its own file).
func siblingHeapPath(goroutineDumpPath string) string {
	if goroutineDumpPath == "" {
		return filepath.Join(os.TempDir(), fmt.Sprintf("grafel-hotloop-%d.heap.pprof", time.Now().UnixNano()))
	}
	const suffix = ".pprof.txt"
	if strings.HasSuffix(goroutineDumpPath, suffix) {
		return strings.TrimSuffix(goroutineDumpPath, suffix) + ".heap.pprof"
	}
	return goroutineDumpPath + ".heap.pprof"
}
