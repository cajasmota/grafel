package dashboard

// graphstate_algo_cpu_budget_6108_test.go — the dashboard's BACKGROUND Pass-4
// sweep must run under the same background core policy as the group-algo child
// (#6108).
//
// WHY THIS FILE EXISTS ALONGSIDE THE group-algo FIX. #6108 was reported against
// the group-algo pass, but graph.RunAlgorithms has a SECOND in-daemon caller
// with exactly the same shape and exactly the same defect: schedulePendingAlgo
// runs the full O(V·E) Louvain + PageRank + betweenness sweep on a goroutine
// inside `serve`, with no child process, no GOMAXPROCS of its own, and nothing
// bounding it. Symptomatically it is indistinguishable from the reported defect
// — same function, same process, no child to find in `ps` — so fixing only the
// scheduler path could have left the user's actual symptom in place.

import (
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon/sched"
	"github.com/cajasmota/grafel/internal/process"
)

// TestBackgroundPass4CapSeam_DefaultsToTheRealCap is the M7 guard.
//
// The cap is reached through a package var so a test can observe it. That seam
// is also the obvious way to make the whole fix vacuous: replace the default
// with a passthrough and every assertion below still passes while production
// applies no cap at all. #6091 is exactly that failure, one layer down. So pin
// the DEFAULT — the wiring is the thing #6108 is about.
func TestBackgroundPass4CapSeam_DefaultsToTheRealCap(t *testing.T) {
	got := reflect.ValueOf(backgroundAlgoCapApply).Pointer()
	want := reflect.ValueOf(process.WithGOMAXPROCSCap).Pointer()
	if got != want {
		t.Fatalf("backgroundAlgoCapApply does not default to process.WithGOMAXPROCSCap — production applies no CPU bound and every seam-based test still passes (#6108 / the #6091 shape)")
	}
}

// TestBackgroundPass4_RunsUnderTheBackgroundBatchCap observes the effective
// runtime.GOMAXPROCS at the moment the sweep body runs.
//
// The baseline is raised above the cap through a MANAGED outer region rather
// than a raw runtime.GOMAXPROCS write, so the clamp is observable on any host
// without this test mutating process-global scheduling state that outlives it.
// (The first draft skipped the assertion when NumCPU <= cap, which silently
// disarmed it on small CI runners; the draft after that wrote the global
// directly, and a full-suite run then perturbed an unrelated async test.)
func TestBackgroundPass4_RunsUnderTheBackgroundBatchCap(t *testing.T) {
	want := sched.BackgroundBatchGOMAXPROCS()

	done := make(chan string, 1)
	setBackgroundAlgoDoneForTest(func(k string) { done <- k })
	t.Cleanup(func() { setBackgroundAlgoDoneForTest(nil) })

	var askedFor, baselineSeen, inForce int
	prev := backgroundAlgoCapApply
	t.Cleanup(func() { backgroundAlgoCapApply = prev })
	backgroundAlgoCapApply = func(n int, fn func() error) error {
		askedFor = n
		// Outer region lifts the baseline above n where the host allows it, so
		// the inner clamp is a real transition and not a coincidence.
		return process.WithGOMAXPROCSCap(n+4, func() error {
			baselineSeen = runtime.GOMAXPROCS(0)
			return process.WithGOMAXPROCSCap(n, func() error {
				inForce = runtime.GOMAXPROCS(0)
				return fn()
			})
		})
	}

	c, grp := pendingGrp(t, pathGraphDoc(), t.TempDir(), time.Now())
	c.schedulePendingAlgo("g", grp)

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("background Pass-4 sweep never completed")
	}

	if askedFor == 0 {
		t.Fatalf("the dashboard's background Pass-4 sweep applied NO CPU cap — it runs graph.RunAlgorithms inside the daemon at the daemon's full GOMAXPROCS, with no child process to find and no budget to inherit (#6108)")
	}
	if askedFor != want {
		t.Errorf("background Pass-4 cap = %d, want sched.BackgroundBatchGOMAXPROCS() = %d — the daemon must have ONE background-analytics core policy, not two (#6108 H3)", askedFor, want)
	}
	wantInForce := want
	if baselineSeen < wantInForce {
		wantInForce = baselineSeen // a host smaller than the cap; never raise
	}
	if inForce != wantInForce {
		t.Errorf("GOMAXPROCS in force during the background Pass-4 sweep = %d, want %d (baseline %d)", inForce, wantInForce, baselineSeen)
	}
	if baselineSeen <= want {
		t.Logf("note: host baseline %d <= cap %d, so the clamp was a no-op here; askedFor still pins the policy", baselineSeen, want)
	}
}

// TestBackgroundPass4Cap_NeverFloorsAtOne pins the H3 reconciliation itself.
//
// IndexCoreBudget is NumCPU/4 floored at 1, which on a 4-core host clamps the
// whole MCP-serving daemon to a single P for the length of the sweep. This
// repo's own subprocess_runner.go says why that is wrong ("below 2 the pass
// loses GC/runtime parallelism entirely"), and GOMAXPROCS=1 measures ~2.2x the
// wall time of 12 for identical total CPU — twice as long holding a single-P
// daemon, for nothing.
func TestBackgroundPass4Cap_NeverFloorsAtOne(t *testing.T) {
	if got := sched.BackgroundBatchGOMAXPROCS(); got < 2 && runtime.NumCPU() >= 2 {
		t.Errorf("the background analytics cap resolved to %d on a %d-core host — a background sweep must not pin the MCP daemon to a single P (#6108 H3)", got, runtime.NumCPU())
	}
}
