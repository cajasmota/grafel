package watch

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestEventLedgerAndPerRepoTallyMoveUnderOneLock is the deterministic guard for
// the ordering defect behind the intermittent macOS failure of
// TestRefusalReturnsEventChargesToo ("a refused subscription left 1 descriptors
// on the ledger, want 0").
//
// chargeEventOpen and releaseEventClose each record ONE fact in TWO places: the
// per-repo tally w.fdReserved, under w.mu, and the global ledger w.fdb, under
// its own mutex. They used to move the second AFTER dropping w.mu, so there was
// a window in which an observer holding w.mu saw the two disagree. Every reader
// that matters is exactly such an observer — the refusal unwind takes
// `reserved + w.fdReserved[abs]` under w.mu, releases that, and deletes the
// repo, so a charge caught mid-window is released by the unwind and then
// applied, stranding a descriptor on a ledger no repo can hand back. RemoveRepo,
// Stop and restartBackend read the same pair the same way.
//
// Rather than try to hit the refusal race — which on an unloaded machine needs
// thousands of runs, and is why CI went red on one run of a commit and green on
// the next — this asserts the invariant those readers depend on, directly and
// under the same lock they hold. Only plain files are churned, never
// directories: subscribeDirRecursive reserves before it takes w.mu and folds
// the total in afterwards, a legitimate transient disagreement that would make
// this a flake in the other direction.
func TestEventLedgerAndPerRepoTallyMoveUnderOneLock(t *testing.T) {
	root := makePrunedTree(t)
	w := newBudgetedWatcher(t, 1_000_000)
	if _, err := w.AddRepo(root); err != nil {
		t.Fatalf("premise broken: AddRepo: %v", err)
	}
	base, _ := w.fdb.snapshot()
	if base == 0 {
		t.Fatalf("premise broken: nothing charged, so nothing can disagree")
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Churn: create and delete files directly under the watched root, so every
	// ledger movement comes from chargeEventOpen or releaseEventClose.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			p := filepath.Join(root, fmt.Sprintf("churn%04d.go", i%64))
			if err := os.WriteFile(p, []byte("package p\n"), 0o644); err != nil {
				return
			}
			if err := os.Remove(p); err != nil {
				return
			}
		}
	}()

	// Observer: the invariant as the refusal unwind sees it.
	var (
		mu     sync.Mutex
		badSum int
		badLed int
		bad    bool
		peak   int
	)
	// Several of them: the window is one mutex handoff wide, so a single
	// observer that has to re-acquire w.mu from scratch loses the race almost
	// every time. With observers already parked on the lock, one of them is
	// handed it the instant the event goroutine drops it — which is precisely
	// the position the refusal unwind is in.
	for obs := 0; obs < 4; obs++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				w.mu.Lock()
				sum := 0
				for _, n := range w.fdReserved {
					sum += n
				}
				used, _ := w.fdb.snapshot()
				w.mu.Unlock()
				mu.Lock()
				if used > peak {
					peak = used
				}
				mu.Unlock()
				if sum != used {
					mu.Lock()
					if !bad {
						bad, badSum, badLed = true, sum, used
					}
					mu.Unlock()
					return
				}
			}
		}()
	}

	time.Sleep(2 * time.Second)
	close(stop)
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	// Non-vacuity, checked before the verdict: a run in which the ledger never
	// moved observed nothing, and its clean result means nothing. The churn
	// must have driven chargeEventOpen at least once past the subscription
	// baseline for the window under test to have existed at all.
	if peak <= base {
		t.Fatalf("premise broken: the ledger never rose above its %d-descriptor "+
			"subscription baseline (peak %d), so no event charge was ever observed "+
			"and this run proves nothing", base, peak)
	}
	if bad {
		t.Fatalf("under w.mu the per-repo tally summed to %d but the ledger held %d — "+
			"the two moved in separate critical sections, so the refusal unwind, "+
			"RemoveRepo and Stop can all read a total that is already stale",
			badSum, badLed)
	}
}
