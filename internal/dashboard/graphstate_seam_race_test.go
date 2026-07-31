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
// Two ingredients make the window reachable, and both are load-bearing:
//
//  1. A CLOSED channel as the installed gate. Every other fixture installs
//     either an open channel (the sweep parks on load #2 and never returns) or
//     nil (load #1 is false, so load #2 never executes). Neither traverses the
//     window more than once. A closed channel makes the receive non-blocking,
//     so the reader streams through the window as fast as it can be scheduled.
//  2. BOUNDED in-flight sweeps, via the backgroundAlgoDone seam: wait for each
//     sweep to finish before scheduling the next. Unbounded, the harness dies
//     of "pthread_create failed: Resource temporarily unavailable" on CORRECT
//     code — a false positive that reds both arms and proves nothing.
//
// Third ingredient: an EMPTY doc and a READ-ONLY state dir, so a sweep costs
// almost nothing but the window itself. With a real doc and a writable dir the
// sweep rate drops ~50x and the mutated arm survives.
//
// Verified to separate, on the final fixture (M-series laptop):
//
//	              correct code            double-read mutation
//	-race         8/8 PASS  1.5-4.0s      5/5 FAIL  panic in 1.2-1.5s
//	no -race      8/8 PASS  0.76-0.92s    3/3 FAIL  panic in 0.8-1.5s
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
	// speed. A fixed time budget would therefore hand a slow CI runner far less
	// sampling than the machine it was tuned on — and this test now sits behind
	// a REQUIRED release checkbox, so a fixture that reds on slowness would be
	// worse than no fixture. Targeting a count makes the sampling identical
	// everywhere; the wall cap only bounds a pathological machine.
	// The two modes need DIFFERENT targets, and both were measured against the
	// mutated arm rather than guessed:
	//   -race    → 400 traversals. -race widens the window, so the double-read
	//              mutation panics within a few hundred; 400 costs ~3s.
	//   no -race → 5000 traversals. The window is a couple of instructions
	//              wide, so detection needs volume; at 400 the mutated arm
	//              survived 3/3 runs, at 5000 it fails. Costs ~1s.
	// Using the -race number in both modes would leave the no-race arm unable
	// to exhibit the bug — the failure mode this repo hits most often.
	targetSweeps := 5000
	if raceEnabled {
		targetSweeps = 400
	}
	const (
		minSweeps = 100 // below this the fixture is not driving the reader
		maxWall   = 45 * time.Second
	)

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

	// Vacuity guard. The strong signal is the 10s no-completion Fatal above;
	// this catches the subtler case where sweeps DO complete but the fixture
	// has stopped re-entering schedulePendingAlgo (e.g. a filterAlreadyComputed
	// change that makes the zero-mtime re-entry a no-op), leaving a handful of
	// traversals that could never sample the window.
	if sweeps < minSweeps {
		t.Fatalf("only %d sweeps completed in %s — the fixture is not driving "+
			"the reader through the check-then-use window; it cannot exhibit "+
			"the bug it guards", sweeps, maxWall)
	}
	if sweeps < targetSweeps {
		// Reached the wall cap on a slow machine. Still meaningful sampling, so
		// do not red the run — but say so rather than implying full coverage.
		t.Logf("WARNING: reduced sampling — %d/%d traversals before the %s cap",
			sweeps, targetSweeps, maxWall)
	}
	t.Logf("drove %d bounded sweeps through the gate window", sweeps)
}
