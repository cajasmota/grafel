package progress

import (
	"sync"
	"testing"
	"time"
)

// #6299 — a subscriber cancelling (browser tab closed, SSE handler returning)
// concurrently with a Publish used to race, because Publish snapshots the
// subscriber slice under the read lock and then sends OUTSIDE it, while cancel
// closed the subscriber's channel under the write lock. The two operations were
// unordered with respect to each other, so a send could land on an
// already-closed channel: `panic: send on closed channel`, raised inside the
// daemon's HTTP server goroutine, i.e. a daemon crash while a user watches a
// progress bar.
//
// These tests do not depend on runner scheduling. They widen the window
// mechanically: each round registers many subscribers on one group, so a single
// Publish performs many sends from one snapshot, and cancels run concurrently
// with that fan-out. Every panic is recovered and reported as a test failure
// (rather than taking the binary down), and under -race the detector flags the
// unordered close/send pair independently.

const (
	// raceRounds is the number of independent broker instances hammered.
	raceRounds = 120
	// raceSubs is how many subscribers share one group per round. The publisher
	// iterates a snapshot of all of them outside the lock, which is the window
	// the canceller races into.
	raceSubs = 64
	// racePublishes is how many events each publisher goroutine fans out per
	// round. It is deliberately kept below the per-subscriber buffer
	// (defaultBufferSize, 64) — no subscriber reads during a round, so this is
	// what bounds buffer occupancy — so every send is a real transfer into the
	// buffer rather than a drop-on-full no-op that would never touch a closed
	// channel. The subscriber count has no bearing on it; raceSubs only widens
	// the window by making one snapshot's fan-out longer.
	racePublishes = 8
)

// recoverInto turns a panic in a helper goroutine into a test failure instead
// of a process abort, so the RED state of this test is a readable FAIL.
func recoverInto(t *testing.T, what string) {
	t.Helper()
	if p := recover(); p != nil {
		t.Errorf("%s panicked: %v", what, p)
	}
}

// TestBroker_PublishConcurrentWithCancel_NoPanic is the #6299 regression test
// for the Publish path.
func TestBroker_PublishConcurrentWithCancel_NoPanic(t *testing.T) {
	for round := 0; round < raceRounds; round++ {
		b := NewBroker()
		cancels := make([]func(), raceSubs)
		for i := range cancels {
			_, cancels[i] = b.Subscribe("g")
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			defer recoverInto(t, "Publish")
			<-start
			for i := 0; i < racePublishes; i++ {
				b.Publish(makeEvent("g", "r", PhaseIndexing))
			}
		}()
		go func() {
			defer wg.Done()
			defer recoverInto(t, "cancel")
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

// TestBroker_BroadcastAllConcurrentWithCancel_NoPanic covers the sibling method:
// BroadcastAll has exactly the same snapshot-then-send-outside-the-lock shape as
// Publish, so fixing only Publish would leave the identical crash reachable from
// the daemon-wide SSE endpoint.
func TestBroker_BroadcastAllConcurrentWithCancel_NoPanic(t *testing.T) {
	for round := 0; round < raceRounds; round++ {
		b := NewBroker()
		cancels := make([]func(), raceSubs)
		for i := range cancels {
			// Spread across two groups so BroadcastAll's all-groups walk is
			// exercised, not just one bucket.
			group := "g1"
			if i%2 == 1 {
				group = "g2"
			}
			_, cancels[i] = b.Subscribe(group)
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			defer recoverInto(t, "BroadcastAll")
			<-start
			for i := 0; i < racePublishes; i++ {
				b.BroadcastAll(makeEvent("g1", "r", PhaseIndexing))
			}
		}()
		go func() {
			defer wg.Done()
			defer recoverInto(t, "cancel")
			<-start
			for _, cancel := range cancels {
				cancel()
			}
		}()

		close(start)
		wg.Wait()
		if t.Failed() {
			t.Fatalf("round %d: broadcast/cancel race", round)
		}
	}
}

// TestBroker_CancelStillClosesChannelUnderConcurrentPublish pins the contract
// that the fix must not quietly drop: cancel closes the subscriber's channel, so
// a consumer ranging over it (or checking `e, ok := <-ch`) still terminates —
// even when a publisher was fanning out at the same moment. A fix that simply
// stopped closing the channel would leave the dashboard SSE handler's
// `case e, ok := <-ch:` arm unreachable and hang any `for e := range ch` caller.
func TestBroker_CancelStillClosesChannelUnderConcurrentPublish(t *testing.T) {
	for round := 0; round < 50; round++ {
		b := NewBroker()
		ch, cancel := b.Subscribe("g")

		var wg sync.WaitGroup
		wg.Add(1)
		start := make(chan struct{})
		go func() {
			defer wg.Done()
			defer recoverInto(t, "Publish")
			<-start
			for i := 0; i < racePublishes; i++ {
				b.Publish(makeEvent("g", "r", PhaseIndexing))
			}
		}()
		close(start)
		cancel()
		wg.Wait()

		// Drain whatever was delivered; the channel must reach a closed state.
		// The receive is deadlined so a regression that stops closing the channel
		// fails fast here instead of hanging the package until the test timeout.
		closed := false
		deadline := time.After(5 * time.Second)
	drain:
		for range racePublishes + 1 {
			select {
			case _, ok := <-ch:
				if !ok {
					closed = true
					break drain
				}
			case <-deadline:
				break drain
			}
		}
		if !closed {
			t.Fatalf("round %d: channel not closed after cancel", round)
		}

		// cancel is documented as idempotent — a second call must not
		// double-close.
		func() {
			defer recoverInto(t, "second cancel")
			cancel()
		}()
		if t.Failed() {
			t.Fatalf("round %d: cancel is not idempotent", round)
		}
	}
}
