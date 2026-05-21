package progress

import (
	"sync"
	"testing"
	"time"
)

// makeEvent is a helper to build a minimal Event for testing.
func makeEvent(group, repo, phase string) Event {
	return Event{
		GroupSlug: group,
		RepoSlug:  repo,
		Phase:     phase,
		FilesDone: 1,
		FilesTotal: 10,
		TS:        time.Now().UnixMilli(),
	}
}

// drain reads up to n events from ch with a short timeout and returns them.
func drain(ch <-chan Event, n int, timeout time.Duration) []Event {
	var out []Event
	deadline := time.After(timeout)
	for len(out) < n {
		select {
		case e, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, e)
		case <-deadline:
			return out
		}
	}
	return out
}

// TestBroker_TwoSubscribersReceiveSameEvent verifies that two subscribers on the
// same group each receive every published event.
func TestBroker_TwoSubscribersReceiveSameEvent(t *testing.T) {
	b := NewBroker()

	ch1, cancel1 := b.Subscribe("group-a")
	defer cancel1()
	ch2, cancel2 := b.Subscribe("group-a")
	defer cancel2()

	events := []Event{
		makeEvent("group-a", "repo-1", "scanning"),
		makeEvent("group-a", "repo-1", "extracting_ast"),
		makeEvent("group-a", "repo-1", "done"),
	}
	for _, e := range events {
		b.Publish(e)
	}

	got1 := drain(ch1, len(events), 500*time.Millisecond)
	got2 := drain(ch2, len(events), 500*time.Millisecond)

	if len(got1) != len(events) {
		t.Errorf("subscriber 1: want %d events, got %d", len(events), len(got1))
	}
	if len(got2) != len(events) {
		t.Errorf("subscriber 2: want %d events, got %d", len(events), len(got2))
	}
	for i, e := range got1 {
		if e.Phase != events[i].Phase {
			t.Errorf("sub1 event[%d]: want phase %q, got %q", i, events[i].Phase, e.Phase)
		}
	}
}

// TestBroker_PublishNeverBlocks verifies that publishing to a slow (full-buffer)
// subscriber does not block the caller. We publish more events than the buffer
// can hold without consuming from the channel.
func TestBroker_PublishNeverBlocks(t *testing.T) {
	b := NewBroker()

	ch, cancel := b.Subscribe("group-b")
	defer cancel()

	const total = 200
	done := make(chan struct{})

	go func() {
		defer close(done)
		for i := 0; i < total; i++ {
			b.Publish(makeEvent("group-b", "repo-x", "scanning"))
		}
	}()

	select {
	case <-done:
		// All publishes completed without blocking — test passes.
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked for > 2s with a full subscriber buffer")
	}

	// The channel should have received at most defaultBufferSize events since
	// we never read from it during publishing.
	if len(ch) > defaultBufferSize {
		t.Errorf("channel length %d exceeds buffer size %d", len(ch), defaultBufferSize)
	}
}

// TestBroker_CancelRemovesSubscriber verifies that calling cancel closes the
// channel and prevents further delivery.
func TestBroker_CancelRemovesSubscriber(t *testing.T) {
	b := NewBroker()

	ch, cancel := b.Subscribe("group-c")

	// Publish one event before cancelling.
	b.Publish(makeEvent("group-c", "repo-y", "scanning"))

	// Cancel should close the channel.
	cancel()

	// Drain whatever was buffered and confirm the channel is now closed.
	var closed bool
	deadline := time.After(500 * time.Millisecond)
loop:
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				closed = true
				break loop
			}
		case <-deadline:
			break loop
		}
	}
	if !closed {
		t.Error("channel was not closed after cancel()")
	}

	// Calling cancel a second time should be a no-op (not panic).
	cancel()

	// Stats should show zero subscribers for this group.
	stats := b.Stats()
	if n := stats["group-c"]; n != 0 {
		t.Errorf("want 0 subscribers after cancel, got %d", n)
	}
}

// TestBroker_GroupIsolation verifies that subscribers to group-A do not receive
// events published to group-B.
func TestBroker_GroupIsolation(t *testing.T) {
	b := NewBroker()

	chA, cancelA := b.Subscribe("group-alpha")
	defer cancelA()
	chB, cancelB := b.Subscribe("group-beta")
	defer cancelB()

	b.Publish(makeEvent("group-alpha", "repo-1", "scanning"))
	b.Publish(makeEvent("group-beta", "repo-2", "done"))

	gotA := drain(chA, 1, 300*time.Millisecond)
	gotB := drain(chB, 1, 300*time.Millisecond)

	if len(gotA) != 1 || gotA[0].GroupSlug != "group-alpha" {
		t.Errorf("group-alpha sub: want 1 event with group_slug=group-alpha, got %+v", gotA)
	}
	if len(gotB) != 1 || gotB[0].GroupSlug != "group-beta" {
		t.Errorf("group-beta sub: want 1 event with group_slug=group-beta, got %+v", gotB)
	}

	// Verify no cross-contamination: drain both for an extra 200ms.
	extraA := drain(chA, 1, 200*time.Millisecond)
	if len(extraA) != 0 {
		t.Errorf("group-alpha sub received unexpected extra event: %+v", extraA)
	}
	extraB := drain(chB, 1, 200*time.Millisecond)
	if len(extraB) != 0 {
		t.Errorf("group-beta sub received unexpected extra event: %+v", extraB)
	}
}

// TestBroker_BroadcastAll verifies that BroadcastAll reaches subscribers in
// every group.
func TestBroker_BroadcastAll(t *testing.T) {
	b := NewBroker()

	chA, cancelA := b.Subscribe("grp-1")
	defer cancelA()
	chB, cancelB := b.Subscribe("grp-2")
	defer cancelB()

	b.BroadcastAll(makeEvent("", "", "done")) // group_slug intentionally empty

	gotA := drain(chA, 1, 300*time.Millisecond)
	gotB := drain(chB, 1, 300*time.Millisecond)

	if len(gotA) != 1 {
		t.Errorf("grp-1 subscriber: want 1 broadcast event, got %d", len(gotA))
	}
	if len(gotB) != 1 {
		t.Errorf("grp-2 subscriber: want 1 broadcast event, got %d", len(gotB))
	}
}

// TestBroker_Stats verifies the Stats snapshot reflects live subscriber counts.
func TestBroker_Stats(t *testing.T) {
	b := NewBroker()

	_, cancel1 := b.Subscribe("s-group")
	_, cancel2 := b.Subscribe("s-group")
	_, cancel3 := b.Subscribe("other-group")

	stats := b.Stats()
	if stats["s-group"] != 2 {
		t.Errorf("want 2 subs for s-group, got %d", stats["s-group"])
	}
	if stats["other-group"] != 1 {
		t.Errorf("want 1 sub for other-group, got %d", stats["other-group"])
	}

	cancel1()
	cancel2()
	cancel3()

	stats = b.Stats()
	if n := stats["s-group"]; n != 0 {
		t.Errorf("want 0 subs after cancel, got %d", n)
	}
}

// TestBroker_ConcurrentPublishSubscribe stress-tests the broker under concurrent
// access from multiple goroutines.
func TestBroker_ConcurrentPublishSubscribe(t *testing.T) {
	b := NewBroker()
	const group = "stress"
	const publishers = 8
	const eventsPerPublisher = 50

	var wg sync.WaitGroup

	// Subscribe and immediately start consuming.
	ch, cancel := b.Subscribe(group)
	defer cancel()

	// Count events received concurrently.
	received := make(chan int, 1)
	go func() {
		count := 0
		timeout := time.After(3 * time.Second)
		for {
			select {
			case _, ok := <-ch:
				if !ok {
					received <- count
					return
				}
				count++
			case <-timeout:
				received <- count
				return
			}
		}
	}()

	for i := 0; i < publishers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < eventsPerPublisher; j++ {
				b.Publish(makeEvent(group, "repo-stress", "extracting_ast"))
			}
		}()
	}
	wg.Wait()

	// Allow the receiver goroutine to drain.
	cancel()
	total := <-received

	// We can't assert exact count (buffer drop is expected) but at least some
	// events must have been received.
	if total == 0 {
		t.Error("concurrent stress: received zero events")
	}
	t.Logf("concurrent stress: received %d / %d events (buffer=%d)",
		total, publishers*eventsPerPublisher, defaultBufferSize)
}
