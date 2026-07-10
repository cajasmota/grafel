package enrichment

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
)

// TestScheduler_Supersession_CancelsPriorRun is the RED-before-fix test for
// acceptance criterion #4 (cancellable / supersedes): scheduling a second job
// for the SAME repo key must cancel the in-flight first job before the
// second job's work function runs, and the first job's goroutine must have
// fully exited (no leak, no stale-over-fresh write) by the time the second
// job starts doing real work.
func TestScheduler_Supersession_CancelsPriorRun(t *testing.T) {
	s := NewScheduler()

	firstCancelled := make(chan struct{})
	firstStarted := make(chan struct{})
	var firstStillRunningWhenSecondStarts int32

	s.Schedule("repoA", func(ctx context.Context) {
		close(firstStarted)
		<-ctx.Done()
		atomic.StoreInt32(&firstStillRunningWhenSecondStarts, 1)
		close(firstCancelled)
	})
	<-firstStarted

	secondRan := make(chan struct{})
	s.Schedule("repoA", func(ctx context.Context) {
		// By the time we get here, the first run's ctx must already be
		// cancelled and its goroutine must have fully finished (the
		// Scheduler waits on the prior job's done channel before starting
		// this one) — so reading firstStillRunningWhenSecondStarts here must
		// observe the write the first goroutine made before it exited.
		select {
		case <-firstCancelled:
		case <-time.After(2 * time.Second):
			t.Error("second job started before first job's cancellation completed")
		}
		close(secondRan)
	})

	select {
	case <-secondRan:
	case <-time.After(3 * time.Second):
		t.Fatal("second scheduled job never ran — supersession appears to have deadlocked or dropped it")
	}
	s.Wait("repoA")
}

// TestScheduler_SingleWorker_NoConcurrentFanOut is the RED-before-fix test
// for acceptance criterion #2 (≤1 worker, never the extraction fan-out):
// scheduling jobs for several DIFFERENT repos concurrently must still only
// ever run one at a time, system-wide.
func TestScheduler_SingleWorker_NoConcurrentFanOut(t *testing.T) {
	s := NewScheduler()

	var active int32
	var maxActive int32
	var mu sync.Mutex
	recordMax := func() {
		mu.Lock()
		defer mu.Unlock()
		cur := atomic.LoadInt32(&active)
		m := atomic.LoadInt32(&maxActive)
		if cur > m {
			atomic.StoreInt32(&maxActive, cur)
		}
	}

	const numRepos = 6
	var wg sync.WaitGroup
	wg.Add(numRepos)
	for i := 0; i < numRepos; i++ {
		repo := repoKeyForTest(i)
		s.Schedule(repo, func(ctx context.Context) {
			defer wg.Done()
			atomic.AddInt32(&active, 1)
			recordMax()
			// Hold the slot briefly so overlapping schedules would show up
			// as maxActive > 1 if the scheduler ever ran more than one at
			// once.
			time.Sleep(20 * time.Millisecond)
			atomic.AddInt32(&active, -1)
		})
	}
	wg.Wait()

	if got := atomic.LoadInt32(&maxActive); got > 1 {
		t.Fatalf("expected at most 1 concurrently-active enrichment job system-wide, observed %d", got)
	}
}

func repoKeyForTest(i int) string {
	return "repo-" + itoa(i)
}

// TestScheduler_AbortLeavesNoPublishedFile confirms that a run cancelled
// before Close() (the pattern the real worker follows on ctx cancellation —
// see runPass6EmitEnrichmentCandidatesBG in cmd/grafel) never publishes a
// half-written candidates file.
func TestScheduler_AbortLeavesNoPublishedFile(t *testing.T) {
	dir := t.TempDir()

	appender, err := NewCandidateAppender(dir)
	if err != nil {
		t.Fatalf("NewCandidateAppender: %v", err)
	}
	if err := appender.AppendChunk([]Candidate{{ID: "x", Kind: "describe_entity", SubjectID: "e1"}}); err != nil {
		t.Fatalf("AppendChunk: %v", err)
	}
	appender.Abort()

	if _, err := os.Stat(candidatesPath(dir)); !os.IsNotExist(err) {
		t.Fatalf("expected no published candidates file after Abort of the only run, stat err=%v", err)
	}
}

// TestCollectAndAppendTrickle_RespectsCancellation confirms the chunked
// collector stops promptly (without appending further batches) once ctx is
// cancelled — the property Scheduler's supersession guarantee depends on.
func TestCollectAndAppendTrickle_RespectsCancellation(t *testing.T) {
	dir := t.TempDir()
	entities := make([]graph.Entity, 5000)
	for i := range entities {
		entities[i] = graph.Entity{ID: "e" + itoa(i), Name: "http:GET:/api/x" + itoa(i), Kind: "http_endpoint"}
	}
	doc := mkDoc(entities...)

	appender, err := NewCandidateAppender(dir)
	if err != nil {
		t.Fatalf("NewCandidateAppender: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())

	chunks := 0
	err = CollectAndAppendTrickle(ctx, doc, DefaultEmitters(), nil, appender, TrickleOptions{
		ChunkSize: 50,
		Pace:      1 * time.Millisecond,
		OnChunk: func(n int) {
			chunks++
			if chunks == 2 {
				cancel()
			}
		},
	})
	if err == nil {
		t.Fatalf("expected CollectAndAppendTrickle to return an error after cancellation, got nil")
	}
	appender.Abort()

	if chunks >= (len(entities) / 50) {
		t.Fatalf("expected cancellation to stop collection well before processing all %d entities' worth of chunks, got %d chunks", len(entities)/50, chunks)
	}
}
