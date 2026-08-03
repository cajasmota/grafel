package process

import (
	"runtime"
	"sync"
)

// gomaxprocs.go — the in-process counterpart of the per-child GOMAXPROCS bound
// (#6108).
//
// WHY THIS EXISTS AT ALL, GIVEN THE PASS IS SERIAL. The heavy background
// analytics passes (group-algo's Louvain + PageRank + Brandes betweenness, and
// the cross-repo link pass) contain no goroutine fan-out, and neither does any
// package they reach: internal/graph (including the in-repo louvain.go),
// internal/graph/groupalgo, internal/links, and every gonum package in the
// dependency closure were audited for it. gonum's network.Betweenness even
// carries a literal "TODO: Consider using the parallel algorithm when
// GOMAXPROCS != 1". One mutator thread does the work. That fact was used to
// argue the cap was decorative. It is not, and the argument had the mechanism
// backwards:
//
// The CPU those passes draw is overwhelmingly the GARBAGE COLLECTOR, and the
// GC's parallelism is sized by GOMAXPROCS, not by the application's. A cycle
// runs 25% of GOMAXPROCS as dedicated mark workers, and — decisively for a
// single-threaded mutator — schedules IDLE mark workers on every otherwise-idle
// P. A serial pass on a 12-P runtime leaves 11 idle Ps, all of which the GC is
// free to fill.
//
// MEASURED, on a serial single-goroutine mutator over a pointer-dense retained
// heap (getrusage, so this is total CPU time, not a sampled percentage):
//
//	GOMAXPROCS=12   cpu=0.55s   311.1%
//	GOMAXPROCS= 3   cpu=0.38s   188.0%
//	GOMAXPROCS= 1   cpu=0.38s    99.2%
//
// Note the CPU SECONDS fall 0.55 -> 0.38: the excess is not the same work spread
// wider, it is pure idle-mark-worker overhead that the cap deletes outright. The
// effect depends on mark work over a dense live pointer set — a sparse heap only
// reached 119% — which is exactly what an assembled graph union is. Brandes
// allocates per-source maps over a multi-hundred-thousand node union, so cycles
// are near-continuous. That is how a "fully serial" pass was measured sustaining
// 571.9% CPU inside the daemon (#6108) while the scheduler logged cap=2.
//
// So GOMAXPROCS IS the enforcement lever on this path — it is the only knob the
// Go runtime offers that bounds total process CPU — and it is precisely the one
// the subprocess children get for free from their environment. An in-process
// pass has no environment of its own, which is why it needs this.
//
// THE COST, STATED PLAINLY. runtime.GOMAXPROCS is process-global. For the
// duration of a region every OTHER goroutine in the process — MCP request
// serving included — runs on the same reduced set of Ps. That is a deliberate
// trade and it is the right way round: a daemon serving MCP from 2 Ps is
// responsive; a daemon whose host is at load 76 because the daemon itself took
// six cores is not. It is also strictly bounded in time by the pass, and it
// applies only to BACKGROUND work — foreground callers resolve a cap at or above
// the host core count and take the unsynchronised fast path below, so they are
// left exactly as they were and never wait on anything.
//
// This is a mitigation for the in-process path, not an endorsement of it. A
// subprocess would get the same bound with no global side effect at all and
// would return its heap to the OS on exit.

// The cap state. capMu guards all of it and is held only for the O(len(active))
// bookkeeping — NEVER across a caller's fn. Regions therefore run concurrently
// and are not serialised; the effective GOMAXPROCS is simply the minimum of the
// baseline and every active cap, which bounds the process as a whole regardless
// of how many regions are in flight.
//
// WHY NOT A MUTEX HELD FOR THE WHOLE REGION (the shape this took first): it made
// every caller queue behind every other, so a user-awaited foreground pass —
// which needs no clamp at all — would block for the minutes-to-hours lifetime of
// a background sweep, uncancellably. It also deadlocked on nesting. Neither is
// visible in an assertion about the cap VALUE, which is how both survived the
// first round of tests.
var (
	capMu sync.Mutex
	// capBaseline is the GOMAXPROCS to return to when the last region exits. It
	// is NOT captured once and frozen: ApplyGOMAXPROCS rewrites it so an
	// operator's live cap change (SIGHUP / cpu.json, #5137) is what a region
	// restores to, rather than the value that happened to be in force when the
	// region started.
	capBaseline int
	// capCurrent is the value this package last installed, so recompute can tell
	// a real transition from a no-op.
	capCurrent int
	// activeCaps is the multiset of caps held by in-flight regions. A slice, not
	// a counter: overlapping regions may exit in any order, and restoring to
	// "whatever was in force when I started" would raise the runtime back above
	// a sibling region's still-live cap.
	activeCaps []int
)

// recomputeCapLocked installs min(capBaseline, activeCaps...) if it differs from
// what is currently installed.
func recomputeCapLocked() {
	eff := capBaseline
	for _, c := range activeCaps {
		if c < eff {
			eff = c
		}
	}
	if eff < 1 {
		eff = 1
	}
	if eff != capCurrent {
		runtime.GOMAXPROCS(eff)
		capCurrent = eff
	}
}

// WithGOMAXPROCSCap runs fn with the process-wide runtime.GOMAXPROCS clamped to
// at most n, restoring the previous value before it returns — including when fn
// returns an error or panics.
//
// It NEVER RAISES, and a call that would not lower anything takes no lock and
// blocks on nothing. n at or above the current value, and any n <= 0 (which is
// what a bogus core count looks like), run fn with the runtime untouched. That
// asymmetry is the property foreground work depends on: a foreground cap
// resolves to the host core count, so passing it through here must be a no-op in
// SCHEDULING as well as in value.
//
// Nesting and overlap are safe: the effective limit is the minimum of all live
// caps, and exiting any region restores only what that region contributed.
func WithGOMAXPROCSCap(n int, fn func() error) error {
	if fn == nil {
		return nil
	}
	// Unsynchronised fast path. GOMAXPROCS(0) queries without setting. A racing
	// region may move the value under us, but either answer is correct: if n
	// does not lower what is installed, this call has nothing to install.
	if n <= 0 || n >= runtime.GOMAXPROCS(0) {
		return fn()
	}
	defer enterGOMAXPROCSCap(n)()
	return fn()
}

// enterGOMAXPROCSCap registers a cap and returns its release. Split out from
// WithGOMAXPROCSCap so the release is a plain defer and therefore runs on panic.
func enterGOMAXPROCSCap(n int) func() {
	capMu.Lock()
	if len(activeCaps) == 0 {
		// First region: adopt whatever is installed as the baseline to return to.
		capBaseline = runtime.GOMAXPROCS(0)
		capCurrent = capBaseline
	}
	activeCaps = append(activeCaps, n)
	recomputeCapLocked()
	capMu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			capMu.Lock()
			defer capMu.Unlock()
			for i, c := range activeCaps {
				if c == n {
					activeCaps = append(activeCaps[:i], activeCaps[i+1:]...)
					break
				}
			}
			recomputeCapLocked()
		})
	}
}

// ApplyGOMAXPROCS sets the process's baseline GOMAXPROCS to target and reports
// the previous baseline and whether it changed. It is the ONLY correct way for
// the daemon to apply an operator's cap (cpu.json / SIGHUP, #5137) while this
// package may hold a region open.
//
// WHAT GOES WRONG WITHOUT IT — two interleavings, both real:
//
//   - A SIGHUP asking for 3 lands while a region has clamped to 3. A handler
//     reading runtime.GOMAXPROCS directly sees cur == target, reports "unchanged"
//     and no-ops. The region's restore then writes back the pre-region 12, and the
//     daemon ends PERMANENTLY ABOVE the operator's configured cap.
//   - A SIGHUP applies 6 mid-region. The region's restore writes back 12. Same
//     outcome.
//
// Routing through here makes the operator's value the baseline the region will
// restore TO, and lowers immediately if it is tighter than what is installed. A
// cap change is therefore never lost and never inverted.
func ApplyGOMAXPROCS(target int) (previous int, changed bool) {
	if target < 1 {
		target = 1
	}
	capMu.Lock()
	defer capMu.Unlock()

	if len(activeCaps) == 0 {
		cur := runtime.GOMAXPROCS(0)
		capBaseline, capCurrent = target, target
		if cur == target {
			return cur, false
		}
		return runtime.GOMAXPROCS(target), true
	}

	prev := capBaseline
	capBaseline = target
	recomputeCapLocked()
	return prev, prev != target
}

// GOMAXPROCSBaseline reports the value an in-flight capped region will restore
// to, or the installed GOMAXPROCS when no region is active. Exported for tests
// and diagnostics; production code reads runtime.GOMAXPROCS.
func GOMAXPROCSBaseline() int {
	capMu.Lock()
	defer capMu.Unlock()
	if len(activeCaps) == 0 {
		return runtime.GOMAXPROCS(0)
	}
	return capBaseline
}
