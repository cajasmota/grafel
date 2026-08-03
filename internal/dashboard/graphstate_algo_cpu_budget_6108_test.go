package dashboard

// graphstate_algo_cpu_budget_6108_test.go — the dashboard's BACKGROUND Pass-4
// sweep must run under the canonical background core budget (#6108).
//
// WHY THIS FILE EXISTS ALONGSIDE THE group-algo FIX. #6108 was reported against
// the group-algo pass, but graph.RunAlgorithms has a SECOND in-daemon caller
// with exactly the same shape and exactly the same defect: schedulePendingAlgo
// runs the full O(V·E) Louvain + PageRank + betweenness sweep on a goroutine
// inside `serve`, with no child process, no GOMAXPROCS of its own, and nothing
// bounding it. Symptomatically it is indistinguishable from the reported defect
// — same function, same process, no child to find in `ps` — so fixing only the
// scheduler path could have left the user's actual symptom in place.
//
// Behavioural, not a source scan: the assertion is the effective
// runtime.GOMAXPROCS at the moment the sweep body runs.

import (
	"runtime"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/process"
)

func TestBackgroundPass4_RunsUnderTheIndexCoreBudget(t *testing.T) {
	done := make(chan string, 1)
	setBackgroundAlgoDoneForTest(func(k string) { done <- k })
	t.Cleanup(func() { setBackgroundAlgoDoneForTest(nil) })

	var askedFor, inForce int
	prev := backgroundAlgoCapApply
	t.Cleanup(func() { backgroundAlgoCapApply = prev })
	backgroundAlgoCapApply = func(n int, fn func() error) error {
		askedFor = n
		return process.WithGOMAXPROCSCap(n, func() error {
			inForce = runtime.GOMAXPROCS(0)
			return fn()
		})
	}

	c, grp := pendingGrp(t, pathGraphDoc(), t.TempDir(), time.Now())
	c.schedulePendingAlgo("g", grp)

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("background Pass-4 sweep never completed")
	}

	want := process.IndexCoreBudget()
	if askedFor == 0 {
		t.Fatalf("the dashboard's background Pass-4 sweep applied NO CPU cap — it runs graph.RunAlgorithms inside the daemon at the daemon's full GOMAXPROCS=%d, with no child process to find and no budget to inherit (#6108)", runtime.GOMAXPROCS(0))
	}
	if askedFor != want {
		t.Errorf("background Pass-4 cap = %d, want the canonical process.IndexCoreBudget() = %d (#6108/#5960)", askedFor, want)
	}
	if runtime.NumCPU() <= want {
		t.Skipf("host NumCPU=%d is at or below the budget %d — no clamp to observe here", runtime.NumCPU(), want)
	}
	if inForce != want {
		t.Errorf("GOMAXPROCS in force during the background Pass-4 sweep = %d, want %d", inForce, want)
	}
}
