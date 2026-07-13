package progress

import (
	"sync"
	"testing"
)

// blockingPublisher blocks forever on the first Publish unless released. It is
// used to prove a TeePublisher does not stall siblings on a slow child (the
// contract holds because children are themselves non-blocking; this guards
// against a future regression that fans out synchronously into a blocking sink).
type countingPublisher struct {
	mu     sync.Mutex
	events []Event
}

func (c *countingPublisher) Publish(e Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, e)
}

func (c *countingPublisher) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.events)
}

// TestTeeFanOut asserts every child receives every published event.
func TestTeeFanOut(t *testing.T) {
	a := &countingPublisher{}
	b := &countingPublisher{}
	c := &countingPublisher{}
	tee := NewTeePublisher(a, b, c)

	for i := 0; i < 10; i++ {
		tee.Publish(Event{GroupSlug: "g", RepoSlug: "r", Phase: PhaseExtractAST, FilesDone: i})
	}
	for name, p := range map[string]*countingPublisher{"a": a, "b": b, "c": c} {
		if got := p.count(); got != 10 {
			t.Fatalf("child %s got %d events, want 10", name, got)
		}
	}
}

// TestTeeNilChildIgnored asserts a nil child in the list is skipped rather than
// panicking (defensive fan-out).
func TestTeeNilChildIgnored(t *testing.T) {
	a := &countingPublisher{}
	tee := NewTeePublisher(a, nil)
	tee.Publish(Event{GroupSlug: "g", Phase: PhaseDone})
	if a.count() != 1 {
		t.Fatalf("child a got %d events, want 1", a.count())
	}
}

// TestTeeImplementsPublisher asserts TeePublisher satisfies the Publisher
// interface (compile-time + can wrap a broker + sidecar writer).
func TestTeeImplementsPublisher(t *testing.T) {
	var _ Publisher = NewTeePublisher()
	var _ Publisher = (*TeePublisher)(nil)
}
