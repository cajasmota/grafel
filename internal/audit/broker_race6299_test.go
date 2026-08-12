package audit

import (
	"sync"
	"testing"
)

// #6299 — the audit broker is the third copy of the progress broker's fan-out
// shape: Publish snapshots the subscriber slice under the read lock and sends
// outside it, while cancel closes the subscriber's channel. Unordered, that is a
// data race and a `panic: send on closed channel` in the daemon's HTTP server
// when a browser tab watching /api/audit/stream goes away mid-publish.
//
// The test drives the mechanism rather than waiting for scheduling luck: many
// subscribers share one broker, so a single Publish performs many sends from one
// snapshot while cancels run concurrently with that fan-out.
func TestBroker_PublishConcurrentWithCancel_NoPanic_6299(t *testing.T) {
	const (
		rounds   = 120
		subs     = 64
		publishs = 8
	)
	for round := 0; round < rounds; round++ {
		b := NewBroker()
		cancels := make([]func(), subs)
		for i := range cancels {
			_, cancels[i] = b.Subscribe()
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			defer func() {
				if p := recover(); p != nil {
					t.Errorf("Publish panicked: %v", p)
				}
			}()
			<-start
			for i := 0; i < publishs; i++ {
				b.Publish(Entry{Operation: "rebuild", Target: "g"})
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
			t.Fatalf("round %d: publish/cancel race", round)
		}
	}
}

// TestBroker_CancelStillClosesChannel_6299 pins the contract the fix must keep:
// cancel closes the channel, so the dashboard SSE handler's `e, ok := <-ch` arm
// still fires. A "never close the channel" fix would silently break it.
func TestBroker_CancelStillClosesChannel_6299(t *testing.T) {
	b := NewBroker()
	ch, cancel := b.Subscribe()
	b.Publish(Entry{Operation: "rebuild"})
	cancel()

	if _, ok := <-ch; !ok {
		t.Fatal("expected the buffered entry before close")
	}
	if _, ok := <-ch; ok {
		t.Fatal("channel not closed after cancel")
	}
	cancel() // idempotent: must not double-close
}
