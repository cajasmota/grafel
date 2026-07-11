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

// TestScheduler_RecoversPanicInRunFunc is the RED-before-fix test for the
// #5736 review follow-up: an emitter panic inside a background-worker run
// func must NOT crash the daemon. Before the fix, the goroutine started by
// Schedule has no recover, so an unrecovered panic in a goroutine terminates
// the whole process — this test (run against the unfixed code) crashes the
// test binary instead of failing normally, which IS the RED signal. After
// the fix, the panic is recovered, the single-worker semaphore is released,
// and done is closed so Wait() returns promptly.
func TestScheduler_RecoversPanicInRunFunc(t *testing.T) {
	s := NewScheduler()

	panicRan := make(chan struct{})
	s.Schedule("repoPanic", func(ctx context.Context) {
		defer close(panicRan)
		panic("boom: simulated emitter panic (#5736 review follow-up)")
	})

	select {
	case <-panicRan:
	case <-time.After(2 * time.Second):
		t.Fatal("panicking job's run func never executed")
	}

	// done must be closed even though run panicked, so Wait returns
	// promptly instead of blocking forever.
	waitDone := make(chan struct{})
	go func() {
		s.Wait("repoPanic")
		close(waitDone)
	}()
	select {
	case <-waitDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Wait never returned after panicking job — done channel was not closed on panic")
	}

	// The single-worker semaphore must have been released despite the
	// panic, so a subsequent job (any repo) can acquire it and run.
	nextRan := make(chan struct{})
	s.Schedule("repoAfterPanic", func(ctx context.Context) {
		close(nextRan)
	})
	select {
	case <-nextRan:
	case <-time.After(2 * time.Second):
		t.Fatal("subsequent job never ran — semaphore appears leaked after panic")
	}
}

// TestScheduler_SupersededWhileParkedOnPrevDone_ReturnsPromptly is the
// RED-before-fix test for the ctx-guard fix on the `<-prevDone` wait: a
// job that is itself superseded WHILE still parked waiting for the run it
// superseded must bail out immediately via ctx.Done(), rather than blocking
// until the original (now doubly-stale) predecessor eventually finishes.
func TestScheduler_SupersededWhileParkedOnPrevDone_ReturnsPromptly(t *testing.T) {
	s := NewScheduler()

	firstStarted := make(chan struct{})
	firstRelease := make(chan struct{})
	s.Schedule("repoX", func(ctx context.Context) {
		close(firstStarted)
		<-firstRelease
	})
	<-firstStarted

	// Second job supersedes first, and (since first is still running) its
	// goroutine parks on <-prevDone before it can even try to acquire sem.
	secondRan := make(chan struct{})
	s.Schedule("repoX", func(ctx context.Context) {
		close(secondRan)
	})

	// White-box (same package): grab a handle on second's own done channel
	// so we can assert it closes promptly once superseded, independent of
	// whether first (which still holds the single-worker sem) has finished
	// — third can't actually RUN until first releases the sem, but second
	// bailing out of its stale <-prevDone wait must not depend on that.
	s.mu.Lock()
	secondDone := s.jobs["repoX"].done
	s.mu.Unlock()

	// Give the second job's goroutine a moment to reach the prevDone wait.
	time.Sleep(50 * time.Millisecond)

	// Third job supersedes second WHILE second is still parked on
	// prevDone (first hasn't finished — firstRelease not yet closed).
	// Ctx-guarded, second must return immediately without ever running.
	thirdRan := make(chan struct{})
	s.Schedule("repoX", func(ctx context.Context) {
		close(thirdRan)
	})

	select {
	case <-secondDone:
	case <-time.After(2 * time.Second):
		t.Fatal("second job's done channel never closed — it appears stuck blocking on the stale <-prevDone wait instead of bailing out via ctx.Done()")
	}

	select {
	case <-secondRan:
		t.Fatal("second job's run func should never execute — it was superseded while still parked on prevDone")
	default:
	}

	// Unblock first so the single-worker sem is released and third (which
	// needs it) can actually run.
	close(firstRelease)

	select {
	case <-thirdRan:
	case <-time.After(2 * time.Second):
		t.Fatal("third job never ran")
	}
	s.Wait("repoX")
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
