package groupalgo

// phase.go — the phase state the group-algo child's memory trace is tagged
// with (#5954).
//
// WHY THIS EXISTS. Epic #5954 cut the `index-internal` child from 4247MB to
// 2975MB, and whole-machine measurement then showed the peak instant had moved
// entirely POST-index: at that instant the index child is at 0MB while
// `group-algo` is frequently the single largest process on the machine — and it
// reported nothing at all. No memstats, no heap profiles, no phases. This epic
// has already lost a day to tuning allocation sites off a misread heap_inuse;
// a 2.4GB process that emits no numbers must not be tuned before it emits some.
//
// SHAPE. The index child feeds memtrace's phaseFn from progress.Tracker's
// CurrentPhase — the same state the UI reports — so the trace can never drift
// from what the process is actually doing. group-algo has no Tracker (it is a
// short batch child with no progress plane), so this file is the minimal
// equivalent: one process-wide holder, stamped by the pass at each stage and
// polled by the memtrace sampler goroutine on its ticker. Still ONE source of
// phase state, which is the property that matters.
//
// A process-wide holder is correct here because the child runs exactly one
// group-algo pass and exits; there is no second concurrent pass to interleave
// with. It is a package-level var rather than a field on a result struct
// precisely so the stamps can live at the call sites inside the pass without
// threading a parameter through every entrypoint.

import (
	"sync"
	"sync/atomic"
)

// The phase labels the pass stamps, in the order the pass stamps them. They are
// the operator-facing contract with the measurement harness — they appear
// verbatim in the memtrace NDJSON `phase` field and in the per-phase
// heap-profile filenames — so they are stable identifiers, not display strings
// to be reworded.
//
//   - assembling         — AssembleGroupGraph: reads every repo's graph.fb and
//     concatenates the union. First measurement on the real corpus: 303MB live
//     over 148,455 entities / 700,321 rels.
//   - hashing            — CommunityInputHash over that union. 457MB live, from
//     only 2 samples out of 775 — see the sampling caveat below.
//   - running_algorithms — graph.RunAlgorithms: Louvain + PageRank +
//     betweenness. The measured MEMORY peak as well as the CPU peak: 623MB
//     live, more than double assembly. Any future presizing work belongs here,
//     not in assembly.
//   - writing_overlay    — serialising and atomically swapping in the
//     <group>-algo.json overlay.
//
// BOTH entrypoints stamp this same order. RunGroupAlgorithms hashes before
// running the algorithms purely so that it does — inputHash does not depend on
// the algorithm result, so the ordering is free, and a single true order beats
// a comment explaining two.
//
// WHERE A TRACE CAN END. On the incremental entrypoint a skip (unchanged input
// hash) never reaches running_algorithms, so the LIBRARY stops after hashing.
// The daemon's real `--write` invocation nonetheless still stamps
// writing_overlay afterwards, skip or not, because cmd/grafel/group_algo.go
// stamps it unconditionally on that path. A `--dry-run` ends at
// running_algorithms. So the last label is not by itself evidence of a
// truncated run, nor of a skip.
//
// READING A GROUP-ALGO TRACE — three inherited memtrace properties that will
// otherwise mislead you:
//
//   - Samples are written ONLY on ticker ticks, and Stop writes no final
//     sample. A run shorter than one tick therefore produces a 0-byte NDJSON
//     and zero heap profiles — indistinguishable from "instrumentation is
//     broken". A 1.7s empty-group probe hit exactly this.
//   - At the 250ms default interval fast phases are near-invisible. On the
//     first real run `hashing` got 2 samples of 775 and `writing_overlay` got
//     ZERO. Treat writing_overlay as UNMEASURED unless GRAFEL_MEMTRACE_INTERVAL
//     is lowered for the run.
//   - `group-algo` is on the GC-pacing allow-list unconditionally
//     (cmd/grafel/index_gcpercent.go), so a human running `grafel group-algo
//     <g> --dry-run` by hand gets GOGC=50 too. That is a deliberate choice, not
//     an oversight — it is a hidden diagnostic command, and pacing the manual
//     invocation exactly as the daemon's is paced is what makes a hand-run
//     trace representative of the real child. It does mean this one foreground
//     invocation is capped, contrary to the general rule.
const (
	PhaseAssembling        = "assembling"
	PhaseHashing           = "hashing"
	PhaseRunningAlgorithms = "running_algorithms"
	PhaseWritingOverlay    = "writing_overlay"
)

// phaseHolder is a race-free string cell plus the transition log. The pass
// goroutine stores; the memtrace sampler goroutine loads on every tick.
//
// The current value is an atomic.Value rather than a mutex-guarded field
// because the READ side is the hot one — polled on a ticker for the life of the
// process — and must not contend with the writer. The transition log takes a
// mutex, which costs nothing: it is only touched on a genuine phase CHANGE, of
// which a whole pass has four.
type phaseHolder struct {
	v atomic.Value

	mu      sync.Mutex
	history []string
}

// set records a stamp, appending to the transition log only when the phase
// actually changes. Re-stamping the same phase is a no-op for the log, so the
// log is a true transition sequence rather than a call counter.
func (h *phaseHolder) set(p string) {
	h.v.Store(p)
	h.mu.Lock()
	defer h.mu.Unlock()
	if n := len(h.history); n == 0 || h.history[n-1] != p {
		h.history = append(h.history, p)
	}
}

// get returns "" before the first store: atomic.Value's zero Load yields a nil
// any, and the failed type assertion leaves the zero string. memtrace may well
// poll before the pass has stamped anything, and an empty phase is exactly the
// right answer there.
func (h *phaseHolder) get() string { s, _ := h.v.Load().(string); return s }

func (h *phaseHolder) snapshot() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.history...)
}

// reset stores the empty phase rather than re-zeroing the atomic.Value, so it
// stays safe against a concurrent sampler poll (and copies no sync type).
func (h *phaseHolder) reset() {
	h.v.Store("")
	h.mu.Lock()
	defer h.mu.Unlock()
	h.history = nil
}

var currentPhase phaseHolder

// SetPhase records the stage the group-algo pass has entered. Called from the
// pass itself; safe to call from any goroutine.
func SetPhase(p string) { currentPhase.set(p) }

// CurrentPhase reports the stage last stamped, or "" if none has been. Pass it
// to memtrace.Start as the phaseFn — it is polled once per sampling tick.
func CurrentPhase() string { return currentPhase.get() }

// PhaseHistory returns the sequence of distinct phases stamped so far.
//
// It exists because CurrentPhase alone cannot answer the question that actually
// matters when reading a trace: which stages ran, in what order. The memtrace
// NDJSON only tells you which phases were SAMPLED, and a phase shorter than the
// sampling interval leaves no sample at all (writing_overlay got zero on the
// first real run). The history is the ground truth a sampled record is read
// against — and it is what lets a test observe the stamps the pass really
// makes, rather than round-tripping the constants through the holder.
func PhaseHistory() []string { return currentPhase.snapshot() }

// ResetPhaseHistory clears both the current phase and the transition log,
// returning the package to its pre-pass state. The child runs one pass and then
// exits, so production never needs this; it exists so a test can observe one
// pass's stamps in isolation from another's.
func ResetPhaseHistory() { currentPhase.reset() }
