// index_enrichment_bg_test.go — #5720: move enrichment off the index
// critical path.
//
// These tests assert the acceptance criteria from issue #5720:
//  1. graph.fb is written / the index reports complete BEFORE Pass 6
//     enrichment-candidate emission runs.
//  4. a new index of the SAME repo cancels/supersedes any in-flight
//     background enrichment for that repo.
package main

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/enrichment"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbreader"
)

// TestIndex_GraphWrittenBeforeEnrichment is the RED-before-fix test for
// #5720 requirement 1: enrichment must run AFTER graph.fb is on disk.
// Before the fix, runPass6EmitEnrichmentCandidates ran inside Run(), which
// completes strictly BEFORE Index() writes graph.fb — so this test would
// have observed "enrichment_started" before "graph_written".
func TestIndex_GraphWrittenBeforeEnrichment(t *testing.T) {
	t.Setenv("GRAFEL_DAEMON_ROOT", t.TempDir())
	tmp := t.TempDir()
	outPath := filepath.Join(tmp, "graph.json")

	var mu sync.Mutex
	var order []string
	prev := enrichmentOrderHook
	enrichmentOrderHook = func(stage string) {
		mu.Lock()
		order = append(order, stage)
		mu.Unlock()
	}
	defer func() { enrichmentOrderHook = prev }()

	if err := Index("testdata/crossfile_go", outPath, "test-repo-order", nil, false, false); err != nil {
		t.Fatalf("Index: %v", err)
	}

	fbPath := graph.CurrentGraphPath(tmp) // #5891: resolve active gen (graph.<gen>.fb)
	if _, err := os.Stat(fbPath); err != nil {
		t.Fatalf("graph.fb not written: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(order) == 0 {
		t.Fatalf("enrichmentOrderHook never fired — is PassEnrichment being skipped unexpectedly?")
	}
	if order[0] != "graph_written" {
		t.Fatalf("expected \"graph_written\" to be the first lifecycle event, got order=%v", order)
	}
	// The crossfile_go fixture is small, so this run takes the INLINE path —
	// enrichment_started/enrichment_done should both appear strictly after
	// graph_written.
	graphIdx, startedIdx := -1, -1
	for i, s := range order {
		if s == "graph_written" && graphIdx == -1 {
			graphIdx = i
		}
		if s == "enrichment_started" && startedIdx == -1 {
			startedIdx = i
		}
	}
	if startedIdx != -1 && startedIdx < graphIdx {
		t.Fatalf("enrichment started before graph.fb was written: order=%v", order)
	}

	// Confirm the FB file is actually valid/queryable at this point — the
	// requirement is not just "written" but "loadable/queryable".
	r, err := fbreader.Open(fbPath)
	if err != nil {
		t.Fatalf("fbreader.Open graph.fb: %v", err)
	}
	defer r.Close()
	if r.EntityCount() == 0 {
		t.Fatalf("graph.fb has 0 entities")
	}
}

// TestIndex_LargeGraphDefersEnrichmentToBackground confirms that a graph
// above enrichment.InlineEntityThreshold schedules enrichment on the
// background worker rather than running it inline — i.e. Index() returns
// (and the caller can treat the index as "done") before enrichment_done
// fires, and enrichment-candidates.json still eventually appears once the
// background job completes (#5720 requirement 6 + "eventually appears").
func TestIndex_LargeGraphDefersEnrichmentToBackground(t *testing.T) {
	t.Setenv("GRAFEL_DAEMON_ROOT", t.TempDir())
	tmp := t.TempDir()
	outPath := filepath.Join(tmp, "graph.json")

	var mu sync.Mutex
	var enrichmentDoneBeforeIndexReturned bool
	indexReturned := false

	prev := enrichmentOrderHook
	enrichmentOrderHook = func(stage string) {
		mu.Lock()
		defer mu.Unlock()
		if stage == "enrichment_done" && !indexReturned {
			enrichmentDoneBeforeIndexReturned = true
		}
	}
	defer func() { enrichmentOrderHook = prev }()

	absRepo, _ := filepath.Abs("testdata/crossfile_go")

	// Force the deferred (background) path regardless of the fixture's
	// actual entity count, by temporarily lowering the inline threshold to
	// -1 (always defer). This isolates the "large graph defers" behavior
	// from needing a genuinely huge fixture in the test corpus.
	origThreshold := enrichment.InlineEntityThreshold
	enrichment.InlineEntityThreshold = -1
	defer func() { enrichment.InlineEntityThreshold = origThreshold }()

	if err := Index("testdata/crossfile_go", outPath, "test-repo-bg", nil, false, false); err != nil {
		t.Fatalf("Index: %v", err)
	}

	mu.Lock()
	indexReturned = true
	mu.Unlock()

	if enrichmentDoneBeforeIndexReturned {
		t.Fatalf("expected enrichment to still be running (or not yet started) when Index() returned for a graph above the inline threshold")
	}

	// Eventually (once the background worker finishes) the candidates file
	// must appear and be valid.
	enrichment.DefaultScheduler.Wait(daemon.StateDirForRepo(absRepo))

	candPath := filepath.Join(daemon.StateDirForRepo(absRepo), "enrichment-candidates.json")
	if _, err := os.Stat(candPath); err != nil {
		t.Fatalf("expected enrichment-candidates.json to eventually appear after the background worker completes: %v", err)
	}
}

// panickyEmitter is a CandidateEmitter that panics on the first entity it
// sees — used to simulate a bug in a real emitter panicking mid-trickle.
type panickyEmitter struct{}

func (panickyEmitter) Name() string { return "panicky_test_emitter" }
func (panickyEmitter) EmitFor(entity *graph.Entity, doc *graph.Document) []enrichment.Candidate {
	panic("boom: simulated emitter panic mid-trickle")
}

// TestRunPass6EmitEnrichmentCandidatesBG_PanicLeavesNoOrphanTemp is the
// RED-before-fix test for #5739 item d (refs #5736 review): before the fix,
// a panic raised by CollectAndAppendTrickle (e.g. a pathological emitter)
// unwound past every explicit appender.Abort() call in
// runPass6EmitEnrichmentCandidatesBG, leaving an orphaned
// enrichment-candidates.json.trickle.tmp-* file in the state dir with no
// glob sweep to reclaim it. The fix adds `defer appender.Abort()` right
// after the appender is created, so a panic still removes the temp file.
func TestRunPass6EmitEnrichmentCandidatesBG_PanicLeavesNoOrphanTemp(t *testing.T) {
	t.Setenv("GRAFEL_DAEMON_ROOT", t.TempDir())

	prevEmitters := enrichmentEmittersForBG
	enrichmentEmittersForBG = func() []enrichment.CandidateEmitter {
		return []enrichment.CandidateEmitter{panickyEmitter{}}
	}
	defer func() { enrichmentEmittersForBG = prevEmitters }()

	absRepo, err := filepath.Abs(filepath.Join(t.TempDir(), "panic-repo"))
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	grafelDir := daemon.StateDirForRepo(absRepo)

	doc := &graph.Document{
		Entities: []graph.Entity{{ID: "e1", Kind: "SCOPE.Operation", Name: "f", SourceFile: "f.go"}},
	}

	idx := &Indexer{skipPasses: map[string]bool{}}

	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatalf("expected runPass6EmitEnrichmentCandidatesBG to panic (via the injected emitter) — test setup is wrong")
			}
		}()
		idx.runPass6EmitEnrichmentCandidatesBG(context.Background(), doc, absRepo)
	}()

	matches, err := filepath.Glob(filepath.Join(grafelDir, "enrichment-candidates.json.trickle.tmp-*"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("expected no orphaned trickle temp files after a panic, found: %v", matches)
	}
	if _, err := os.Stat(filepath.Join(grafelDir, "enrichment-candidates.json")); !os.IsNotExist(err) {
		t.Fatalf("expected no published candidates file after an aborted (panicked) run, stat err=%v", err)
	}
}

// TestScheduler_SupersedesPriorRunForSameRepo confirms that calling
// Index() twice in quick succession for the SAME repo does not leave two
// competing background enrichment goroutines racing to write
// enrichment-candidates.json — the second run's Schedule() call cancels the
// first.
func TestScheduler_SupersedesPriorRunForSameRepo(t *testing.T) {
	s := enrichment.NewScheduler()
	repoKey := "same-repo"

	var firstSawCancel int32
	firstStarted := make(chan struct{})
	s.Schedule(repoKey, func(ctx context.Context) {
		close(firstStarted)
		<-ctx.Done()
		firstSawCancel = 1
	})
	<-firstStarted

	secondDone := make(chan struct{})
	s.Schedule(repoKey, func(ctx context.Context) {
		close(secondDone)
	})

	select {
	case <-secondDone:
	case <-time.After(3 * time.Second):
		t.Fatal("second Schedule() for the same repo never ran")
	}
	s.Wait(repoKey)
	if firstSawCancel != 1 {
		t.Fatalf("expected the first job's context to be cancelled once superseded")
	}
}
