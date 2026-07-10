package sched

// Issue #5726 / epic #5729 — the daemon must survive a panic raised during an
// index (e.g. the flatbuffers 2-GiB marshal abort). The scheduler worker loop
// must recover, log a degraded-state message, release the reserved budget, and
// keep serving subsequent jobs.

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// TestWorkerSurvivesIndexPanic enqueues a job whose IndexFn panics, then a
// second job for a different repo. The worker must recover from the panic and
// still run the second job (proving the worker goroutine survived).
func TestWorkerSurvivesIndexPanic(t *testing.T) {
	var okCalls atomic.Int32
	okRan := make(chan struct{})

	s := New(Config{
		Workers: 1,
		Index: func(_ context.Context, repo string, _ string) error {
			switch repo {
			case "/panic":
				panic("boom: simulated oversized-graph marshal panic (#5726)")
			case "/ok":
				if okCalls.Add(1) == 1 {
					close(okRan)
				}
			}
			return nil
		},
	})
	s.Start()
	defer s.Stop()

	// This job panics inside runIndex. Without a recover the worker goroutine
	// (and the whole process) aborts.
	s.Enqueue("/panic")
	// Give the worker a moment to pick up and blow up on the first job.
	time.Sleep(100 * time.Millisecond)
	// A subsequent job must still be serviced.
	s.Enqueue("/ok")

	select {
	case <-okRan:
		// Worker survived the panic and processed the next job.
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not survive the index panic: subsequent job was never run")
	}

	if got := okCalls.Load(); got != 1 {
		t.Errorf("expected the follow-up job to run exactly once, got %d", got)
	}
}
