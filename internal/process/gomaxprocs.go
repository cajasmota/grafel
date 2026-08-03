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
// the cross-repo link pass) contain no goroutine fan-out, and neither do the
// gonum packages they call — gonum's network.Betweenness carries a literal
// "TODO: Consider using the parallel algorithm when GOMAXPROCS != 1". One
// mutator thread does the work. That fact was used to argue the cap was
// decorative. It is not, and the argument had the mechanism backwards:
//
// The CPU those passes draw is overwhelmingly the GARBAGE COLLECTOR, and the
// GC's parallelism is sized by GOMAXPROCS, not by the application's. A cycle
// runs 25% of GOMAXPROCS as dedicated mark workers, and — decisively for a
// single-threaded mutator — schedules IDLE mark workers on every otherwise-idle
// P. A serial pass on a 12-P runtime leaves 11 idle Ps, all of which the GC is
// free to fill. Brandes allocates per-source maps over a multi-hundred-thousand
// node union, so cycles are near-continuous. That is how a "fully serial" pass
// was measured sustaining 571.9% CPU inside the daemon (#6108) while the
// scheduler logged cap=2.
//
// So GOMAXPROCS IS the enforcement lever on this path — it is the only knob the
// Go runtime offers that bounds total process CPU — and it is precisely the one
// the subprocess children get for free from their environment. An in-process
// pass has no environment of its own, which is why it needs this.
//
// THE COST, STATED PLAINLY. runtime.GOMAXPROCS is process-global. For the
// duration of the region every OTHER goroutine in the process — MCP request
// serving included — runs on the same reduced set of Ps. That is a deliberate
// trade and it is the right way round: a daemon serving MCP from 2 Ps is
// responsive; a daemon whose host is at load 76 because the daemon itself took
// six cores is not. It is also strictly bounded in time by the pass, and it
// applies only to BACKGROUND work — foreground callers resolve a cap at or
// above the host core count, and WithGOMAXPROCSCap never raises, so they are
// left exactly as they were.
//
// This is a mitigation for the in-process path, not an endorsement of it. A
// subprocess would get the same bound with no global side effect at all and
// would return its heap to the OS on exit.

// gomaxprocsCapMu serialises capped regions. Overlapping regions would
// otherwise have the inner region's restore write back the OUTER region's
// clamped value as if it were the baseline, leaving the process throttled after
// both had returned. Serialising costs nothing in production: the daemon's
// exclusive heavy-stage token already admits at most one background heavy pass
// at a time.
var gomaxprocsCapMu sync.Mutex

// WithGOMAXPROCSCap runs fn with the process-wide runtime.GOMAXPROCS clamped to
// at most n, restoring the previous value before it returns — including when fn
// returns an error or panics.
//
// It NEVER RAISES. n at or above the current value (and any n <= 0, which is
// what a bogus core count looks like) runs fn with the runtime untouched. That
// asymmetry is the property foreground work depends on: a foreground cap
// resolves to the host core count, so passing it through here is a no-op rather
// than a widening.
//
// Callers must not hold a lock that fn's work can block on: this takes a
// package mutex for the whole region.
func WithGOMAXPROCSCap(n int, fn func() error) error {
	if fn == nil {
		return nil
	}
	if n <= 0 {
		return fn()
	}

	gomaxprocsCapMu.Lock()
	defer gomaxprocsCapMu.Unlock()

	// GOMAXPROCS(0) reads without setting; only lower.
	prev := runtime.GOMAXPROCS(0)
	if n >= prev {
		return fn()
	}
	runtime.GOMAXPROCS(n)
	defer runtime.GOMAXPROCS(prev)
	return fn()
}
