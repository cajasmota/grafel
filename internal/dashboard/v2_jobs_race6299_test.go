package dashboard

import (
	"sync"
	"testing"
	"time"
)

// #6299 — actionJobRegistry is the fourth copy of the progress broker's fan-out
// shape, and was the worst of them: `update` snapshots the subscriber channels
// under r.mu, releases it and sends outside it, while the cancel returned by
// `subscribe` closed the channel entirely OUTSIDE r.mu. Reachable exactly as the
// issue describes — handleV2JobStream (GET /api/v2/jobs/{id}/stream) does
// `defer cancel()` and returns when the tab closes, while runRebuildJob calls
// `update` from a background goroutine — so a client disconnecting during a
// rebuild could raise `panic: send on closed channel` on a net/http goroutine.
//
// The test drives the mechanism rather than waiting for scheduling luck: many
// subscribers on one job, so a single `update` performs many sends from one
// snapshot, with cancels running concurrently with that fan-out.
func TestActionJobRegistry_UpdateConcurrentWithCancel_NoPanic_6299(t *testing.T) {
	const (
		rounds  = 120
		subs    = 64
		updates = 8
	)
	for round := 0; round < rounds; round++ {
		r := newActionJobRegistry()
		job := r.create("rebuild", "g", "", "tok")

		cancels := make([]func(), 0, subs)
		for i := 0; i < subs; i++ {
			_, cancel, ok := r.subscribe(job.ID)
			if !ok {
				t.Fatalf("round %d: subscribe failed", round)
			}
			cancels = append(cancels, cancel)
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			defer func() {
				if p := recover(); p != nil {
					t.Errorf("update panicked: %v", p)
				}
			}()
			<-start
			for i := 0; i < updates; i++ {
				r.update(job.ID, func(j *actionJob) { j.Progress = i })
			}
		}()
		go func() {
			defer wg.Done()
			defer func() {
				if p := recover(); p != nil {
					t.Errorf("cancel panicked: %v", p)
				}
			}()
			<-start
			for _, cancel := range cancels {
				cancel()
			}
		}()
		close(start)
		wg.Wait()
		if t.Failed() {
			t.Fatalf("round %d: update/cancel race", round)
		}
	}
}

// TestActionJobRegistry_CancelIsIdempotent_6299 pins the second half of the
// blocker: the pre-fix cancel had neither a sync.Once nor a closed guard, so a
// second call panicked with `close of closed channel`. Masked today by the
// single `defer cancel()` in handleV2JobStream, but a live landmine on exactly
// the path #6299 hardens.
func TestActionJobRegistry_CancelIsIdempotent_6299(t *testing.T) {
	r := newActionJobRegistry()
	job := r.create("rebuild", "g", "", "tok")
	ch, cancel, ok := r.subscribe(job.ID)
	if !ok {
		t.Fatal("subscribe failed")
	}

	cancel()
	func() {
		defer func() {
			if p := recover(); p != nil {
				t.Errorf("second cancel panicked: %v", p)
			}
		}()
		cancel()
	}()

	// The channel must still reach a closed state, so handleV2JobStream's
	// `j, open := <-ch` arm fires and the stream emits its `close` event. The
	// receive is deadlined so a regression that stops closing fails fast here
	// rather than hanging the package.
	closed := false
	deadline := time.After(5 * time.Second)
drain:
	for i := 0; i < 10; i++ {
		select {
		case _, open := <-ch:
			if !open {
				closed = true
				break drain
			}
		case <-deadline:
			break drain
		}
	}
	if !closed {
		t.Fatal("channel not closed after cancel")
	}
}
