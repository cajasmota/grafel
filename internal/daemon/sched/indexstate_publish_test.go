package sched

import (
	"context"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/indexstate"
)

// TestPublishIndexState proves the P5 wiring: the scheduler mirrors its
// in-flight index count to the process-global indexstate record, so the
// in-daemon MCP server's grafel_stats can report is_indexing without a
// scheduler reference. We gate a single Index call so the job is observably
// in flight, assert indexstate reports busy, then release and assert it
// returns to idle.
func TestPublishIndexState(t *testing.T) {
	t.Cleanup(func() { indexstate.Set(0) })
	indexstate.Set(0) // clean baseline regardless of other tests

	started := make(chan struct{})
	release := make(chan struct{})

	s := New(Config{
		Workers: 1,
		Index: func(_ context.Context, _ string, _ string) error {
			close(started)
			<-release
			return nil
		},
	})
	s.Start()
	defer s.Stop()

	s.Enqueue("/repo-a")

	// Wait until the Index callback is actually running.
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("Index callback never entered")
	}

	if got := indexstate.Get(); !got.IsIndexing || got.InFlight < 1 {
		t.Fatalf("mid-run: indexstate = %+v, want IsIndexing with >=1 in flight", got)
	}
	if indexstate.Get().StartedAt.IsZero() {
		t.Fatal("mid-run: StartedAt should be stamped")
	}

	// Release the job and wait for the scheduler to drain back to idle.
	close(release)
	deadline := time.After(10 * time.Second)
	for {
		if !indexstate.Get().IsIndexing {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("indexstate never returned to idle: %+v", indexstate.Get())
		case <-time.After(10 * time.Millisecond):
		}
	}
}
