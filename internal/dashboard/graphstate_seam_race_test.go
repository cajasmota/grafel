package dashboard

import (
	"sync"
	"testing"
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
