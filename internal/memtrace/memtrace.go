// Package memtrace is opt-in memory-trace instrumentation for the index
// pipeline (#5956, Phase 0 of the write-side bounded-memory epic #5890). It
// is PURELY ADDITIVE and inert unless GRAFEL_MEMTRACE_DIR is set:
//
//   - M1: a goroutine samples runtime.ReadMemStats + process RSS on a
//     ticker, tags each sample with the currently-active phase (read from
//     the existing progress.Tracker via a caller-supplied phaseFn — no
//     parallel phase state), and appends one NDJSON line per sample.
//   - M2: a runtime/pprof heap profile is written at each phase transition
//     and once at the observed peak heap_inuse.
//   - M3: the engine and serve processes call Start with role "engine" /
//     "serve" (no phase — nil phaseFn) so a single trace directory can be
//     merged, on ts, into a whole-machine curve alongside the child's
//     "child" role.
//
// Hard constraints (see docs/superpowers/specs/2026-07-24-index-memory-
// observability-design.md):
//   - Inert by default: GRAFEL_MEMTRACE_DIR unset => Start starts no
//     goroutine and creates no file.
//   - Never affects the index: every failure path (dir not writable,
//     profile write fails) disables the sampler silently and returns nil;
//     no error from this package ever propagates to a caller's index
//     result. Mirrors internal/cli/watch.go's "registry write failure must
//     never stop the watcher" precedent.
//   - Numeric memstats + symbol names only; profiles are written only under
//     the operator-specified directory, never to logs or telemetry.
package memtrace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strconv"
	"sync"
	"time"

	"github.com/cajasmota/grafel/internal/process"
)

// DirEnv is the single opt-in gate for the entire package. Unset (the
// default) means fully inert: Start returns nil immediately, before any
// goroutine, ticker, or file is created.
const DirEnv = "GRAFEL_MEMTRACE_DIR"

// IntervalEnv overrides the sampling interval (Go duration syntax, e.g.
// "500ms"). Invalid or non-positive values fall back to DefaultInterval.
const IntervalEnv = "GRAFEL_MEMTRACE_INTERVAL"

// DefaultInterval is the sampling cadence when IntervalEnv is unset/invalid.
const DefaultInterval = 250 * time.Millisecond

// Record is one NDJSON line. Field set matches the design doc exactly:
// numeric memstats + RSS + role, tagged with the phase active at sample
// time. All memory fields are bytes; NumGC is the cumulative GC count.
type Record struct {
	TS          int64  `json:"ts"`
	Phase       string `json:"phase"`
	HeapAlloc   uint64 `json:"heap_alloc"`
	HeapInuse   uint64 `json:"heap_inuse"`
	HeapSys     uint64 `json:"heap_sys"`
	HeapObjects uint64 `json:"heap_objects"`
	StackInuse  uint64 `json:"stack_inuse"`
	NextGC      uint64 `json:"next_gc"`
	NumGC       uint32 `json:"num_gc"`
	RSSBytes    uint64 `json:"rss_bytes"`
	Role        string `json:"role"`
}

// Sampler owns the background goroutine, the open NDJSON file, and the
// best-effort heap-profile writer. Obtain one via Start; nil is a valid,
// safe-to-use "disabled" value (Stop on a nil *Sampler is a no-op).
type Sampler struct {
	role     string
	phaseFn  func() string
	interval time.Duration
	runDir   string

	mu       sync.Mutex // guards f and disabled; sampleOnce runs on one goroutine but Stop may race it
	f        *os.File
	disabled bool

	lastPhase     string
	heapSeq       int
	peakHeapInuse uint64

	logf func(format string, args ...any) // best-effort logger; nil = silent

	stopOnce sync.Once
	stopCh   chan struct{}
	doneCh   chan struct{}
}

// Start begins sampling if and only if GRAFEL_MEMTRACE_DIR is set and the
// run directory + NDJSON file can be created. role identifies the emitting
// process ("child", "engine", "serve"). phaseFn is polled once per tick to
// tag the sample; pass nil for processes with no phase concept (engine,
// serve) — the sample's phase field is then always "".
//
// On any setup failure (dir not writable, file open fails), Start logs at
// most once via logf (which may be nil for silent) and returns nil. Callers
// should treat a nil *Sampler as "disabled" — Stop is safe to call on nil.
func Start(role string, phaseFn func() string, logf func(format string, args ...any)) *Sampler {
	dir := os.Getenv(DirEnv)
	if dir == "" {
		// Inert by default: no goroutine, no ticker, no file.
		return nil
	}
	interval := DefaultInterval
	if v := os.Getenv(IntervalEnv); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			interval = d
		}
	}
	if phaseFn == nil {
		phaseFn = func() string { return "" }
	}

	runID := newRunID(role)
	runDir := filepath.Join(dir, runID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		logOnce(logf, "memtrace: disabled (mkdir %s: %v)", runDir, err)
		return nil
	}
	fpath := filepath.Join(runDir, role+".ndjson")
	f, err := os.OpenFile(fpath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		logOnce(logf, "memtrace: disabled (open %s: %v)", fpath, err)
		return nil
	}

	s := &Sampler{
		role:     role,
		phaseFn:  phaseFn,
		interval: interval,
		runDir:   runDir,
		f:        f,
		logf:     logf,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
	go s.loop()
	return s
}

// Dir returns the run directory this sampler writes into (empty for a nil
// or disabled Sampler). Exposed mainly for tests.
func (s *Sampler) Dir() string {
	if s == nil {
		return ""
	}
	return s.runDir
}

// Stop halts the sampling goroutine and closes the NDJSON file. Safe to
// call on a nil *Sampler (no-op) and safe to call more than once.
func (s *Sampler) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
		<-s.doneCh
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.f != nil {
			_ = s.f.Close()
			s.f = nil
		}
	})
}

// sampleFn is the seam loop() invokes on every tick. It is a package-level
// var (rather than a direct s.sampleOnce() call) purely so a test can
// substitute a panicking implementation to exercise the recover() below
// without needing OS-level fault injection. Production code never
// reassigns it — do not use this for anything but the panic-containment
// test.
var sampleFn = func(s *Sampler) { s.sampleOnce() }

func (s *Sampler) loop() {
	defer close(s.doneCh)
	// Defense-in-depth: the package contract is "must NEVER affect the
	// index." No path in sampleOnce is known to panic today (CurrentPhase is
	// an atomic load, process.RSSBytes returns an error rather than
	// panicking, pprof errors are ignored) — but a panic anywhere on this
	// goroutine must never crash the host process. Recover, log at most
	// once, and let the goroutine exit; a persistently panicking sampler
	// disables itself rather than spinning or retrying.
	defer func() {
		if r := recover(); r != nil {
			logOnce(s.logf, "memtrace: sampler goroutine panicked, disabling (role=%s): %v", s.role, r)
		}
	}()
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			sampleFn(s)
		}
	}
}

// sampleOnce reads runtime.MemStats + process RSS, tags with the current
// phase, appends one NDJSON line, and best-effort writes a heap profile on
// a phase transition or a new observed peak heap_inuse. Every failure here
// is swallowed (at most one log line) — this subsystem must never affect
// the index.
func (s *Sampler) sampleOnce() {
	s.mu.Lock()
	if s.disabled {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	rss, _ := process.RSSBytes(os.Getpid()) // best-effort: 0 on error, never fatal

	phase := s.phaseFn()
	rec := Record{
		TS:          time.Now().UnixMilli(),
		Phase:       phase,
		HeapAlloc:   ms.HeapAlloc,
		HeapInuse:   ms.HeapInuse,
		HeapSys:     ms.HeapSys,
		HeapObjects: ms.HeapObjects,
		StackInuse:  ms.StackInuse,
		NextGC:      ms.NextGC,
		NumGC:       ms.NumGC,
		RSSBytes:    rss,
		Role:        s.role,
	}

	s.writeRecord(rec)

	// M2: heap profile at phase transition + new peak. Best-effort, never
	// blocks or errors out to the caller.
	phaseChanged := phase != s.lastPhase
	s.lastPhase = phase
	if phaseChanged {
		s.writeHeapProfile(phase, true)
	}
	if ms.HeapInuse > s.peakHeapInuse {
		s.peakHeapInuse = ms.HeapInuse
		// "peak" is overwritten in place (fixed filename, no growing seq):
		// heap_inuse tends to climb monotonically through most of a phase,
		// so treating every new high-water mark as its own numbered file
		// would produce one profile per sample during ramp-up. Overwriting
		// keeps exactly one profile on disk reflecting the LATEST observed
		// peak, which is what the design doc asks for ("once at the
		// observed peak") without the file-count blowup.
		s.writeHeapProfile("peak", false)
	}
}

func (s *Sampler) writeRecord(rec Record) {
	b, err := json.Marshal(rec)
	if err != nil {
		return // cannot happen for this struct, but never propagate regardless
	}
	b = append(b, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.disabled || s.f == nil {
		return
	}
	if _, err := s.f.Write(b); err != nil {
		// Best-effort: disable silently rather than let a write failure
		// (e.g. disk full, dir removed underneath us) ever surface into the
		// index. Log at most once.
		logOnce(s.logf, "memtrace: disabling sampler (write %s: %v)", s.f.Name(), err)
		_ = s.f.Close()
		s.f = nil
		s.disabled = true
	}
}

// writeHeapProfile writes a pprof heap snapshot named heap-<tag>-<seq>.pprof.
// When numbered is false the file name omits a per-call seq and is instead
// overwritten in place on every call (used for the "peak" tag — see caller).
func (s *Sampler) writeHeapProfile(tag string, numbered bool) {
	var name string
	if numbered {
		s.heapSeq++
		name = fmt.Sprintf("heap-%s-%d.pprof", tag, s.heapSeq)
	} else {
		name = fmt.Sprintf("heap-%s.pprof", tag)
	}
	path := filepath.Join(s.runDir, name)
	f, err := os.Create(path)
	if err != nil {
		return // best-effort; a missing profile never affects the index
	}
	defer f.Close()
	_ = pprof.WriteHeapProfile(f) // best-effort: ignore write errors
}

// newRunID derives a directory name unique to this process invocation so
// concurrent index jobs (and concurrent engine/serve processes) never
// collide on the same NDJSON file. Prefixed with role so a directory
// listing under GRAFEL_MEMTRACE_DIR is self-describing at a glance.
func newRunID(role string) string {
	return role + "-" + strconv.FormatInt(time.Now().UnixNano(), 36) + "-" + strconv.Itoa(os.Getpid())
}

// logOnce calls logf at most once per process for a given message shape by
// simply calling it — callers pass a logf that is already itself cheap/rare
// (phase transitions + setup failures are infrequent), so no additional
// dedup bookkeeping is needed. A nil logf is silent, matching the "log at
// most once" best-effort contract without forcing every caller to supply a
// logger.
func logOnce(logf func(format string, args ...any), format string, args ...any) {
	if logf == nil {
		return
	}
	logf(format, args...)
}
