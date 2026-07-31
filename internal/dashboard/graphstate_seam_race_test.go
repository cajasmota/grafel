package dashboard

import (
	"os"
	"sync"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
)

// TestBackgroundAlgoSeams_ConcurrentSetAndRead pins the second fix for #6056.
//
// schedulePendingAlgo starts a LIVE background goroutine that reads
// backgroundAlgoGate (and later backgroundAlgoDone) after the request that
// started it has returned. Fixtures set those seams and clear them from
// t.Cleanup, which is precisely the write-during-live-read shape that made
// TestHandleGroupStats_NoRef fail under -race.
//
// Two independent failure modes, so this cannot silently stop protecting:
//
//   - Under -race: if either seam goes back to a plain package var, the
//     set/clear here races the reader loop and the detector fails this test.
//   - Without -race: the install/clear assertions below go RED if
//     setBackgroundAlgoGateForTest / setBackgroundAlgoDoneForTest stop
//     actually installing or actually clearing the seam.
func TestBackgroundAlgoSeams_ConcurrentSetAndRead(t *testing.T) {
	// Save and restore whatever the package-level state was, so this test does
	// not leak a seam into any other test in this package.
	prevGate := backgroundAlgoGate.Load()
	prevDone := backgroundAlgoDone.Load()
	t.Cleanup(func() {
		backgroundAlgoGate.Store(prevGate)
		backgroundAlgoDone.Store(prevDone)
	})

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			// Read both seams the way the background sweep does. We never
			// receive on the gate (that would park us) and never invoke done.
			_ = backgroundAlgoGate.Load()
			_ = backgroundAlgoDone.Load()
		}
	}()

	ch := make(chan struct{})
	for i := 0; i < 2000; i++ {
		setBackgroundAlgoGateForTest(ch)
		setBackgroundAlgoDoneForTest(func(string) {})
		setBackgroundAlgoGateForTest(nil)
		setBackgroundAlgoDoneForTest(nil)
	}

	close(stop)
	wg.Wait()

	// Install must actually install (fails without -race).
	setBackgroundAlgoGateForTest(ch)
	got := backgroundAlgoGate.Load()
	if got == nil {
		t.Fatal("setBackgroundAlgoGateForTest(ch) did not install the gate")
	}
	if (chan struct{})(*got) != ch {
		t.Fatal("setBackgroundAlgoGateForTest installed a different channel")
	}
	var called bool
	setBackgroundAlgoDoneForTest(func(string) { called = true })
	d := backgroundAlgoDone.Load()
	if d == nil {
		t.Fatal("setBackgroundAlgoDoneForTest(fn) did not install the callback")
	}
	(*d)("k")
	if !called {
		t.Fatal("installed done callback was not the one provided")
	}

	// Clear must actually clear (fails without -race).
	setBackgroundAlgoGateForTest(nil)
	if backgroundAlgoGate.Load() != nil {
		t.Fatal("setBackgroundAlgoGateForTest(nil) did not clear the gate")
	}
	setBackgroundAlgoDoneForTest(nil)
	if backgroundAlgoDone.Load() != nil {
		t.Fatal("setBackgroundAlgoDoneForTest(nil) did not clear the callback")
	}
}

// TestBackgroundAlgoGate_ClearedBetweenCheckAndUse pins the OTHER half of the
// #6056 seam fix: the production reader must load the gate EXACTLY ONCE.
//
// The pre-fix shape was a double read:
//
//	if backgroundAlgoGate != nil {  // load #1
//	    <-backgroundAlgoGate       // load #2
//	}
//
// A clear that lands between the two loads is a TOCTOU. With the original plain
// `chan struct{}` var it wedged the sweep goroutine forever on a receive from a
// nil channel — silent, and effectively unobservable from a test. Converting the
// seam to atomic.Pointer changed the failure mode: load #2 now returns a nil
// *algoGateChan and the dereference panics. Louder, and therefore pinnable —
// worth recording, because "the fix made the latent bug catchable" is the only
// reason this test can exist at all.
//
// THREE ingredients make the window reachable. Each has been verified
// load-bearing by mutating it away:
//
//  1. A CLOSED channel as the installed gate. Every other fixture installs
//     either an open channel (the sweep parks on load #2 and never returns) or
//     nil (load #1 is false, so load #2 never executes). Neither traverses the
//     window more than once. A closed channel makes the receive non-blocking,
//     so the reader streams through the window as fast as it can be scheduled.
//     Mutate the close() away and this test fails with "background sweep never
//     reported completion" instead of exercising anything.
//  2. BOUNDED in-flight sweeps, via the backgroundAlgoDone seam: wait for each
//     sweep to finish before scheduling the next. Unbounded, the harness dies
//     of "pthread_create failed: Resource temporarily unavailable" on CORRECT
//     code — a false positive that reds both arms and proves nothing.
//  3. An EMPTY doc and a READ-ONLY state dir, so a sweep costs almost nothing
//     but the window itself. With a real doc and a writable dir the sweep rate
//     drops ~50x and the mutated arm survives.
//
// Verified to separate, on the final fixture (M-series laptop, 16 runs):
//
//	              correct code            double-read mutation
//	-race         5/5 PASS  2.5-4.5s      5/5 FAIL  panic in 1.1-1.7s
//	no -race      3/3 PASS  1.0-1.3s      3/3 FAIL  panic in 0.8-1.4s
//
// Note the panic is a plain TOCTOU, not a data race: the detector is NOT what
// catches it, which is why the no-race arm has to work too and why it needs its
// own traversal target (see below).
func TestBackgroundAlgoGate_ClearedBetweenCheckAndUse(t *testing.T) {
	prevGate := backgroundAlgoGate.Load()
	prevDone := backgroundAlgoDone.Load()
	t.Cleanup(func() {
		backgroundAlgoGate.Store(prevGate)
		backgroundAlgoDone.Store(prevDone)
	})

	// Closed: receiving from it never blocks, so the sweep goroutine runs
	// straight through the check-then-use window instead of parking in it.
	closed := make(chan struct{})
	close(closed)

	swept := make(chan string, 1)
	setBackgroundAlgoDoneForTest(func(k string) {
		select {
		case swept <- k:
		default:
		}
	})
	setBackgroundAlgoGateForTest(closed)

	// Writers: several goroutines hammering install/clear, so a clear can land
	// inside the window on whichever core the sweep goroutine is running on.
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				setBackgroundAlgoGateForTest(closed)
				setBackgroundAlgoGateForTest(nil)
			}
		}()
	}
	t.Cleanup(func() { close(stop); wg.Wait() })

	// An EMPTY doc plus a read-only state dir strips ~all per-sweep cost:
	// RunAlgorithms over zero entities is trivial, and persistAlgoResults'
	// os.CreateTemp fails immediately (its error is swallowed by design). What
	// remains per sweep is essentially the gate window itself, which is the only
	// part this test samples. With a real doc and a writable dir the sweep rate
	// drops ~50x and the mutated arm survives the time budget.
	stateDir := t.TempDir()
	if err := os.Chmod(stateDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(stateDir, 0o700) })
	doc := &graph.Document{Repo: "gate-window-test"}

	// fbMtime is deliberately the ZERO time: filterAlreadyComputed only skips
	// repos with a non-zero mtime it has already computed, so a zero mtime lets
	// us re-enter schedulePendingAlgo unboundedly. With a real mtime the second
	// iteration would be filtered out and the loop below would drive the reader
	// exactly once — a fixture that cannot exhibit what it claims to check.
	var zeroMtime time.Time

	// Drive to a TARGET TRAVERSAL COUNT, not a wall-clock budget. Detection
	// power is a function of how many times the reader crosses the window, and
	// the rate varies ~30x between modes (measured on an M-series laptop:
	// ~5000 traversals/s without -race, ~150/s with it) and again with machine
	// speed. A fixed time budget would hand a slow CI runner far less sampling
	// than the machine it was tuned on; targeting a count makes the sampling
	// identical everywhere.
	//
	// The two modes need DIFFERENT targets, both measured against the mutated
	// arm rather than guessed. Detection vs traversal count without -race,
	// 5 runs per level:
	//
	//	  100 → 0/5 detected      2000 → 3/5
	//	  400 → 1/5               3000 → 5/5   ← knee
	//	 1000 → 1/5               5000 → 4/4
	//
	// Hence 5000 without -race (1.67x above the knee). Under -race the window
	// is far wider, 400 detects 4/4, and 5000 would cost ~33s for nothing.
	//
	// FALLING SHORT OF THE TARGET IS A FAILURE, not a warning. An earlier
	// version logged "reduced sampling" and PASSED above a floor of 100 — but
	// the table above shows detection at 100 is exactly ZERO, and most of the
	// 100..2500 band is non-detecting. A hatch like that does not trade full
	// coverage for reduced coverage; across most of its range it trades
	// coverage for none while still reporting PASS. That is the precise defect
	// class #6056 exists to close, so it is gone. Wiring failures are caught by
	// the 10s no-completion Fatal below and by the zero-mtime re-entry guard
	// above, both of which are wiring checks rather than coverage floors.
	//
	// maxWall is 180s so that failure can never be a speed red: it is ~34x the
	// -race arm's cost on a loaded machine (400 traversals in ~5.3s) and ~138x
	// the no-race arm's (5000 in ~1.3s). It is 0.75% of the 40m per-binary CI
	// budget on a test that runs once. The point is to stop NEEDING a hatch,
	// not to make the hatch quiet.
	targetSweeps := 5000
	if raceEnabled {
		targetSweeps = 400
	}
	const maxWall = 180 * time.Second

	hardDeadline := time.Now().Add(maxWall)
	sweeps := 0
	for sweeps < targetSweeps && time.Now().Before(hardDeadline) {
		c, grp := pendingGrp(t, doc, stateDir, zeroMtime)
		c.schedulePendingAlgo("g", grp)
		// Bound in-flight sweeps to one: without this the loop spawns
		// goroutines far faster than they retire and the process dies of
		// "pthread_create failed: Resource temporarily unavailable" on CORRECT
		// code — a false positive that reds both arms and proves nothing.
		select {
		case <-swept:
			sweeps++
		case <-time.After(10 * time.Second):
			t.Fatal("background sweep never reported completion — seam wiring broken")
		}
	}

	// Short of target = FAIL. Below the target this fixture cannot reliably
	// exhibit the bug it guards, so passing would be a false green — see the
	// detection table above. If this ever fires on a machine that is merely
	// slow, raise maxWall; do not lower the target and do not restore a
	// warn-and-pass band.
	if sweeps < targetSweeps {
		t.Fatalf("only %d/%d traversals of the check-then-use window within %s — "+
			"below target this fixture cannot detect the double-read regression "+
			"it exists to catch (measured: 0/5 at 100 traversals, 5/5 at 3000), "+
			"so a PASS here would be a false green", sweeps, targetSweeps, maxWall)
	}
	t.Logf("drove %d bounded sweeps through the gate window", sweeps)
}
