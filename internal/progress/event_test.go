package progress_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/progress"
)

// TestSliceCollector verifies that SliceCollector accumulates events.
func TestSliceCollector(t *testing.T) {
	col := &progress.SliceCollector{}
	pub := col

	pub.Publish(progress.Event{Phase: progress.PhaseScan, FilesTotal: 10})
	pub.Publish(progress.Event{Phase: progress.PhaseExtractAST, FilesDone: 5})

	if len(col.Events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(col.Events))
	}
	if col.Events[0].Phase != progress.PhaseScan {
		t.Errorf("event[0].Phase = %q, want %q", col.Events[0].Phase, progress.PhaseScan)
	}
	if col.Events[1].Phase != progress.PhaseExtractAST {
		t.Errorf("event[1].Phase = %q, want %q", col.Events[1].Phase, progress.PhaseExtractAST)
	}
}

// TestBufferedPublisher_NonBlocking verifies that Publish never blocks even
// when the channel is full and uses drop-oldest backpressure.
func TestBufferedPublisher_NonBlocking(t *testing.T) {
	const cap = 4
	pub := progress.NewBufferedPublisher(cap)

	// Fill channel to capacity + 2 (should drop-oldest, not block).
	for i := 0; i < cap+2; i++ {
		pub.Publish(progress.Event{Phase: progress.PhaseScan, FilesDone: i})
	}
	// Drain remaining events — must be able to drain without deadlock.
	drained := 0
	for {
		select {
		case <-pub.Ch:
			drained++
		default:
			goto done
		}
	}
done:
	// We must have received between 1 and cap events (oldest ones were dropped).
	if drained == 0 {
		t.Fatal("no events received from BufferedPublisher")
	}
	if drained > cap {
		t.Fatalf("drained %d events, expected <= %d (capacity)", drained, cap)
	}
}

// TestNoOpPublisher verifies NoOpPublisher is safe to call.
func TestNoOpPublisher(t *testing.T) {
	var nop progress.NoOpPublisher
	nop.Publish(progress.Event{Phase: progress.PhaseScan})
	// No panic = pass.
}

// TestTracker_PhaseStart verifies that PhaseStart emits an event with the
// correct phase, timestamps, and slug fields.
func TestTracker_PhaseStart(t *testing.T) {
	col := &progress.SliceCollector{}
	trk := progress.NewTracker(col, "mygroup", "myrepo")
	trk.SetFilesTotal(42)

	trk.PhaseStart(progress.PhaseScan, 0, 0)

	if len(col.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(col.Events))
	}
	e := col.Events[0]
	if e.GroupSlug != "mygroup" {
		t.Errorf("GroupSlug = %q, want %q", e.GroupSlug, "mygroup")
	}
	if e.RepoSlug != "myrepo" {
		t.Errorf("RepoSlug = %q, want %q", e.RepoSlug, "myrepo")
	}
	if e.Phase != progress.PhaseScan {
		t.Errorf("Phase = %q, want %q", e.Phase, progress.PhaseScan)
	}
	if e.FilesTotal != 42 {
		t.Errorf("FilesTotal = %d, want 42", e.FilesTotal)
	}
	if e.PhaseStartedAtMS == 0 {
		t.Error("PhaseStartedAtMS should be non-zero")
	}
	if e.TS == 0 {
		t.Error("TS should be non-zero")
	}
}

// TestTracker_Done verifies the final PhaseDone event carries correct totals.
func TestTracker_Done(t *testing.T) {
	col := &progress.SliceCollector{}
	trk := progress.NewTracker(col, "", "repo")
	trk.SetFilesTotal(100)

	trk.Done(100, 500)

	if len(col.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(col.Events))
	}
	e := col.Events[0]
	if e.Phase != progress.PhaseDone {
		t.Errorf("Phase = %q, want %q", e.Phase, progress.PhaseDone)
	}
	if e.FilesDone != 100 {
		t.Errorf("FilesDone = %d, want 100", e.FilesDone)
	}
	if e.FilesTotal != 100 {
		t.Errorf("FilesTotal = %d, want 100", e.FilesTotal)
	}
	if e.EntitiesSoFar != 500 {
		t.Errorf("EntitiesSoFar = %d, want 500", e.EntitiesSoFar)
	}
}

// TestTracker_RunToken verifies that a Tracker constructed with SetRunToken
// stamps RunToken on every event it emits (#5937), and that a Tracker with no
// run token leaves RunToken empty on every event — a background/watcher
// reindex (no ProgressToken) must behave exactly as before this change.
func TestTracker_RunToken(t *testing.T) {
	col := &progress.SliceCollector{}
	trk := progress.NewTracker(col, "g", "r")
	trk.SetRunToken("run-tok-42")
	trk.SetFilesTotal(10)

	trk.PhaseStart(progress.PhaseScan, 0, 0)
	trk.Tick(progress.PhaseExtractAST, 1, 100, "a.go", 0)
	trk.TickModule(progress.PhaseExtractAST, "mod", 1, 2, 50, "b.go", 0)
	trk.Phase(progress.PhaseDetectLinks, "cross-repo links", 0)
	trk.AlgorithmEvent("PageRank", 5)
	trk.Done(10, 20)
	trk.Fail("boom")

	if len(col.Events) != 7 {
		t.Fatalf("expected 7 events, got %d", len(col.Events))
	}
	for i, e := range col.Events {
		if e.RunToken != "run-tok-42" {
			t.Errorf("event[%d] (phase=%s) RunToken = %q, want %q", i, e.Phase, e.RunToken, "run-tok-42")
		}
	}
}

// TestTracker_EmptyRunTokenUnaffected verifies a Tracker with no SetRunToken
// call (the default, e.g. a background/watcher reindex) emits events with an
// empty RunToken — additive-only, no behaviour change for untokened runs.
func TestTracker_EmptyRunTokenUnaffected(t *testing.T) {
	col := &progress.SliceCollector{}
	trk := progress.NewTracker(col, "g", "r")
	trk.Done(1, 1)

	if len(col.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(col.Events))
	}
	if got := col.Events[0].RunToken; got != "" {
		t.Errorf("RunToken = %q, want empty for a Tracker with no run token set", got)
	}
}

// TestTracker_Fail verifies that Fail emits a PhaseError event with the
// error message.
func TestTracker_Fail(t *testing.T) {
	col := &progress.SliceCollector{}
	trk := progress.NewTracker(col, "g", "r")
	trk.Fail("something went wrong")

	if len(col.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(col.Events))
	}
	e := col.Events[0]
	if e.Phase != progress.PhaseError {
		t.Errorf("Phase = %q, want %q", e.Phase, progress.PhaseError)
	}
	if e.Error != "something went wrong" {
		t.Errorf("Error = %q, want %q", e.Error, "something went wrong")
	}
}

// TestTracker_AlgorithmEvent verifies AlgorithmEvent sets the algorithm name.
func TestTracker_AlgorithmEvent(t *testing.T) {
	col := &progress.SliceCollector{}
	trk := progress.NewTracker(col, "", "r")
	trk.SetFilesTotal(50)

	trk.AlgorithmEvent("PageRank", 200)

	if len(col.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(col.Events))
	}
	e := col.Events[0]
	if e.Phase != progress.PhaseAlgorithms {
		t.Errorf("Phase = %q, want %q", e.Phase, progress.PhaseAlgorithms)
	}
	if e.AlgorithmName != "PageRank" {
		t.Errorf("AlgorithmName = %q, want PageRank", e.AlgorithmName)
	}
	if e.EntitiesSoFar != 200 {
		t.Errorf("EntitiesSoFar = %d, want 200", e.EntitiesSoFar)
	}
	// FilesDone should equal FilesTotal for algorithm events.
	if e.FilesDone != e.FilesTotal {
		t.Errorf("FilesDone (%d) != FilesTotal (%d) for algorithm event", e.FilesDone, e.FilesTotal)
	}
}
