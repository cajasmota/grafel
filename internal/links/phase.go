package links

// phase.go — the stage state the cross-repo link pass's memory trace is tagged
// with (#5954).
//
// WHY THIS EXISTS. After the index child was cut to ~2984MB and group-algo was
// paced and instrumented (#5992), the remaining unprofiled plane was the
// ENGINE: memtrace.Start is called for it with a nil phaseFn, so every sample
// it writes is tagged "" and heap-peak.pprof is the only usable artifact. The
// engine peaks AFTER the index child exits and holds ~1.9GB for minutes,
// because the cross-repo link pass runs in-process there and materialises the
// group union TWICE, sequentially:
//
//   - RunAllPasses → loadAllGraphs: every repo's full Document, held live
//     across all ~19 passes, several of which mutate entity Properties in
//     place;
//   - then the phantom-edge pass (internal/cli/links.go) reloads every repo's
//     Document again and re-runs process flow with every OTHER repo's Document
//     held live as a companion.
//
// Those two windows want completely different fixes, and an untagged trace
// cannot tell them apart. This epic has twice picked the wrong target by
// reasoning from allocation-site size instead of measured live heap, so the
// stamps land BEFORE any refactor.
//
// SHAPE. Mirrors internal/graph/groupalgo/phase.go deliberately: one
// process-wide holder, stamped by the pass at each stage boundary and polled by
// the memtrace sampler goroutine on its ticker — ONE source of phase state, so
// the trace cannot drift from what the process is actually doing.
//
// The holder is process-wide (not per-run) because the link pass is serialised
// by the daemon's heavy-stage gate: fireLinks acquires an EXCLUSIVE token for
// "links:<group>" before calling the pass, so two passes never run
// concurrently in one process. In the child added alongside this
// (`grafel links-internal <group>`) there is exactly one pass and then exit.
// The ONE caller that can interleave is a test running passes back-to-back,
// which is what ResetPhaseHistory is for.
//
// READING A LINK TRACE — three memtrace properties inherited from the sampler
// that will otherwise mislead you:
//
//   - Samples are written only on ticker ticks and Stop writes no final
//     sample, so a phase shorter than one tick leaves NO sample. Absence of a
//     phase in the NDJSON is not evidence it did not run — PhaseHistory is.
//   - `heap_inuse` is live heap PLUS uncollected garbage. Live heap is
//     next_gc / (1 + GOGC/100). Read the per-phase live heap off next_gc, not
//     off heap_inuse; the latter is how this epic mis-targeted twice already.
//   - The pass stamps `idle` when it finishes, so a long tail of `idle`
//     samples in an ENGINE trace is the post-pass plateau — memory the process
//     is holding but no longer working on. That plateau is the whole reason
//     the background pass was moved into a child.

import (
	"sync"
	"sync/atomic"
)

// The stage labels, in the order a full pass stamps them. They are the
// operator-facing contract with the measurement harness — they appear verbatim
// in the memtrace NDJSON `phase` field and in per-phase heap-profile filenames
// — so they are stable identifiers, not display strings to be reworded.
//
//	link_stage        — staging the graphs dir (symlink farm; near-zero heap).
//	link_pass_load    — loadAllGraphs: the FIRST materialisation of the union.
//	link_pass_<name>  — one per pass, labelled with the pass's own name as it
//	                    appears in PassResult.Pass (import, http, string, …).
//	phantom_load      — the SECOND materialisation: the docs-map reload.
//	phantom_promote   — phantom CALLS edge promotion over that map.
//	phantom_flow      — per-repo process/event-flow recompute with every other
//	                    repo's Document held live as a companion, plus the flow
//	                    side-table writes. The widest live set in the pass.
//	phantom_cleanup   — removing stale flow side-tables for unaffected repos.
//	link_stats        — the per-repo link-stats.json sidecar writes.
//	idle              — the pass has finished; anything still resident here is
//	                    plateau, not working set.
const (
	PhaseStage          = "link_stage"
	PhaseLoad           = "link_pass_load"
	PhasePhantomLoad    = "phantom_load"
	PhasePhantomPromote = "phantom_promote"
	PhasePhantomFlow    = "phantom_flow"
	PhasePhantomCleanup = "phantom_cleanup"
	PhaseLinkStats      = "link_stats"
	PhaseIdle           = "idle"
)

// passPhasePrefix namespaces the per-pass labels so a trace can be filtered to
// "the eight-pass window" with a single prefix match, and so a pass name can
// never collide with a stage label.
const passPhasePrefix = "link_pass_"

// PhaseForPass returns the phase label for a link pass. The argument is the
// pass's OWN name — the same string it puts in PassResult.Pass — so the label
// and the reported pass cannot drift apart, and a new pass added without a
// stamp is visible as a missing entry in the history rather than as a
// mislabelled sample.
func PhaseForPass(name string) string { return passPhasePrefix + name }

// phaseHolder is a race-free string cell plus the transition log. The pass
// goroutine stores; the memtrace sampler goroutine loads on every tick.
//
// The current value is an atomic.Value rather than a mutex-guarded field
// because the READ side is the hot one — polled on a ticker for the life of the
// process — and must not contend with the writer. The transition log takes a
// mutex, which costs nothing: it is only touched on a genuine phase CHANGE, of
// which a whole pass has a couple of dozen.
type phaseHolder struct {
	v atomic.Value

	mu      sync.Mutex
	history []string
}

// set records a stamp, appending to the transition log only when the phase
// actually changes, so the log is a true transition sequence rather than a call
// counter.
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

// SetPhase records the stage the link pass has entered. Called from the pass
// itself (and from the phantom-edge pass in internal/cli, which is the same
// logical pass split across a package boundary to keep the dependency arrow
// pointing inward); safe to call from any goroutine.
func SetPhase(p string) { currentPhase.set(p) }

// CurrentPhase reports the stage last stamped, or "" if none has been. Pass it
// to memtrace.Start as the phaseFn — it is polled once per sampling tick.
func CurrentPhase() string { return currentPhase.get() }

// PhaseHistory returns the sequence of distinct phases stamped so far.
//
// It exists because CurrentPhase alone cannot answer the question that actually
// matters when reading a trace: which stages ran, in what order. The memtrace
// NDJSON only tells you which phases were SAMPLED, and a phase shorter than the
// sampling interval leaves no sample at all. The history is the ground truth a
// sampled record is read against — and it is what lets a test observe the
// stamps the pass really makes, rather than round-tripping constants through
// the holder.
func PhaseHistory() []string { return currentPhase.snapshot() }

// ResetPhaseHistory clears both the current phase and the transition log. The
// child runs one pass and exits, so production never needs it; it exists so a
// test can observe one pass's stamps in isolation from another's.
func ResetPhaseHistory() { currentPhase.reset() }
