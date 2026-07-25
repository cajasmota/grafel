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

import "sync/atomic"

// The phase labels the pass stamps, in the order a full (non-skipped) run
// stamps them. They are the operator-facing contract with the measurement
// harness — they appear verbatim in the memtrace NDJSON `phase` field and in
// the per-phase heap-profile filenames — so they are stable identifiers, not
// display strings to be reworded.
//
//   - assembling         — AssembleGroupGraph: reads every repo's graph.fb and
//     concatenates the union. Expected to be allocation-heavy: this is where
//     the whole group's entities + relationships become live at once.
//   - hashing            — CommunityInputHash over that union.
//   - running_algorithms — graph.RunAlgorithms: Louvain + PageRank +
//     betweenness. The CPU peak; whether it is also the memory peak is exactly
//     what this instrumentation exists to answer.
//   - writing_overlay    — serialising and atomically swapping in the
//     <group>-algo.json overlay.
//
// The incremental entrypoint may stamp only the first two before taking the
// skip path — an unchanged input hash reconstitutes the prior overlay and never
// reaches running_algorithms. A trace that stops at `hashing` is therefore a
// successful skip, not a truncated run.
const (
	PhaseAssembling        = "assembling"
	PhaseHashing           = "hashing"
	PhaseRunningAlgorithms = "running_algorithms"
	PhaseWritingOverlay    = "writing_overlay"
)

// phaseHolder is a race-free string cell. The pass goroutine stores; the
// memtrace sampler goroutine loads on every tick. atomic.Value (rather than a
// mutex) keeps the read side — the hot one, polled on a ticker for the life of
// the process — free of any contention with the writer.
type phaseHolder struct{ v atomic.Value }

func (h *phaseHolder) set(p string) { h.v.Store(p) }

// get returns "" before the first store: atomic.Value's zero Load yields a nil
// any, and the failed type assertion leaves the zero string. memtrace may well
// poll before the pass has stamped anything, and an empty phase is exactly the
// right answer there.
func (h *phaseHolder) get() string { s, _ := h.v.Load().(string); return s }

var currentPhase phaseHolder

// SetPhase records the stage the group-algo pass has entered. Called from the
// pass itself; safe to call from any goroutine.
func SetPhase(p string) { currentPhase.set(p) }

// CurrentPhase reports the stage last stamped, or "" if none has been. Pass it
// to memtrace.Start as the phaseFn — it is polled once per sampling tick.
func CurrentPhase() string { return currentPhase.get() }
