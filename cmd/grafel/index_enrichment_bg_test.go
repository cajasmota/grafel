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
	"sync/atomic"
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

// hookHoldEscape bounds how long the enrichment job parks inside the
// test's "enrichment_started" hook: an inline run calls that hook on
// Index()'s own goroutine, so an unbounded park would park the test
// itself. releaseSendEscape is a last-resort deadlock escape on the
// other side of the same handshake, for a job that is registered with the
// scheduler yet never reaches the hook at all.
//
// hookHoldEscape DOES decide verdicts, and can decide them wrongly. Cut
// to 1ns with production untouched, this test goes red at "no enrichment
// job was parked ... fired 1 time(s)" — a correct tree failing purely
// because the constant expired. So the window is held open for at most
// hookHoldEscape, NOT indefinitely: if the test goroutine is descheduled
// for longer than that between Index() returning and indexReturned being
// set, the parked job escapes without a send, runs, and fires
// enrichment_done. Whichever assertion then wins — doneBefore if the
// event beats the assignment, !released otherwise (the 1ns probe hit the
// second) — the run is red with nothing wrong in production. That is why
// this is minutes rather than seconds: `go test -timeout` is per test
// BINARY (15m on the no-race PR leg, test.yml:505-509) and this package
// measures ~160s, so one expiry's worth of headroom is affordable.
//
// What IS true of both constants: an expiry FAILS rather than passing, so
// neither can make a red run green.
const (
	hookHoldEscape    = 4 * time.Minute
	releaseSendEscape = 120 * time.Second
)

// TestIndex_LargeGraphDefersEnrichmentToBackground confirms that a graph
// above enrichment.InlineEntityThreshold schedules enrichment on the
// background worker rather than running it inline — i.e. Index() returns
// (and the caller can treat the index as "done") before enrichment_done
// fires, and enrichment-candidates.json still eventually appears once the
// background job completes (#5720 requirement 6 + "eventually appears").
//
// #7298 row 1: the observation is a handshake, not a sampled gap. Before
// this, the test set indexReturned immediately after Index() returned and
// the background job raced those few statements. #7298's census recorded
// the assertion below firing on four unrelated PRs, ubuntu green on the
// same runs, and that gap is the only window it can fire through — there
// is no Windows repro (windows.yml is disabled_manually, #7248), so the
// contention mechanism is that triage's reading, not a measurement. The
// hook now parks the job in "enrichment_started" on an UNBUFFERED receive
// from release, and the test releases it with a send — a send that can
// only complete against a job actually parked there.
//
// The park is BOUNDED, not indefinite: hookHoldEscape is a second path
// past it (see the constants above). Within that bound, a parked job
// cannot reach enrichment_done before the send, and the send is issued
// after indexReturned is set — so the few statements between Index()'s
// return and that assignment are still raced, but nothing can pass the
// park while they run. Beyond the bound the job escapes on its own and
// the guarantee is gone.
//
// Splitting this into "signal that I arrived" + "wait to be released"
// would put the gap back on the test's side; one unbuffered send carries
// both directions, and `released` below is what grades it.
//
// What is NOT graded by any single local run: that `indexReturned = true`
// is executed BEFORE the `release <- struct{}{}` send (not the later
// `doneBefore := ...` read, which is after the send by design). Swapping
// the assignment past the send is the pre-#7298 race exactly, and a race
// is what no local run can observe — that is the whole reason this row
// reached CI four times. `released` grades that a job was parked, not the
// order in which we released it.
func TestIndex_LargeGraphDefersEnrichmentToBackground(t *testing.T) {
	t.Setenv("GRAFEL_DAEMON_ROOT", t.TempDir())
	tmp := t.TempDir()
	outPath := filepath.Join(tmp, "graph.json")

	var mu sync.Mutex
	var enrichmentDoneBeforeIndexReturned bool
	indexReturned := false

	// release is UNBUFFERED: the job parks on the receive, and the test's
	// send completes only once a job is parked there. Adding a buffer of
	// one is enough to make `released` below assert nothing at all — the
	// send then completes into the buffer with no job parked anywhere —
	// so the capacity is pinned rather than left to a reader's care.
	release := make(chan struct{})
	if cap(release) != 0 {
		t.Fatalf("release must stay unbuffered — a buffered send completes with no job parked, and `released` then asserts nothing")
	}
	// hookStarts counts entries into the "enrichment_started" arm so a
	// failure can say whether the job never ran at all or ran without
	// parking.
	var hookStarts atomic.Int32
	// releaseOnce keeps the release path single-shot: either the send
	// below happens, or the cleanup closes the channel — never both.
	var releaseOnce sync.Once

	prev := enrichmentOrderHook
	enrichmentOrderHook = func(stage string) {
		switch stage {
		case "enrichment_started":
			hookStarts.Add(1)
			select {
			case <-release:
			case <-time.After(hookHoldEscape):
			}
		case "enrichment_done":
			mu.Lock()
			defer mu.Unlock()
			if !indexReturned {
				enrichmentDoneBeforeIndexReturned = true
			}
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

	// Release the parked job. The send completing is positive evidence
	// that a job was held at the hook across the lines above, rather than
	// having been sampled and hoped about.
	//
	// The losing case is decided by an EVENT, not a timer: if the job
	// finished (or was never scheduled), Wait returns and jobDone closes,
	// so "nothing was parked" is reported at once instead of after a
	// timeout. A job that IS parked keeps Wait blocked: `done` is closed
	// only by `defer close(done)` at the job goroutine's exit
	// (background.go:98), and Schedule registers the job under s.mu before
	// returning (background.go:86-95), so no reachable state closes
	// jobDone while a job is parked in the hook.
	//
	// That exclusivity is what keeps the select honest. Go picks uniformly
	// at random among ready cases, so if `release <-` and `<-jobDone` were
	// ever both ready the verdict would be a coin flip reported with the
	// wrong message. Nothing grades that invariant — it rests on the two
	// source facts cited above.
	//
	// NOT handled: a job queued behind another repo on the scheduler's
	// single worker slot. It blocks at `s.sem <- struct{}{}`
	// (background.go:118) without ever reaching the hook, so jobDone stays
	// open, the send stays unready, and this select sits until
	// releaseSendEscape and then fails with the misleading "nothing was
	// parked" message. Reaching that state needs a second job on the
	// scheduler, i.e. another index in this package crossing the
	// 2000-entity default (background.go:37) — this test is the only
	// writer of that var, and review found no cmd/grafel fixture near it.
	// So the case is unexercised, not handled.
	schedKey := daemon.StateDirForRepo(absRepo)
	jobDone := make(chan struct{})
	go func() {
		enrichment.DefaultScheduler.Wait(schedKey)
		close(jobDone)
	}()
	// Every t.Fatalf below returns before the final Wait, which would
	// leave a parked job live: t.TempDir's cleanup would then delete the
	// state dir under it, and — the hook having been restored by the defer
	// above — a later test's hook would receive its stages. Closing
	// release unparks it and this Wait reaps it. Registered after
	// t.TempDir/t.Setenv, so t.Cleanup's documented LIFO order runs it
	// BEFORE their cleanups. It also retires the jobDone goroutine, which
	// is otherwise left blocked on a fatal path.
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
		enrichment.DefaultScheduler.Wait(schedKey)
	})

	released := false
	select {
	case release <- struct{}{}:
		released = true
		releaseOnce.Do(func() {}) // the send stands in for the close
	case <-jobDone:
	case <-time.After(releaseSendEscape):
	}

	mu.Lock()
	doneBefore := enrichmentDoneBeforeIndexReturned
	mu.Unlock()

	if doneBefore {
		t.Fatalf("expected enrichment to still be running (or not yet started) when Index() returned for a graph above the inline threshold")
	}
	if !released {
		t.Fatalf("no enrichment job was parked in the hook to release (enrichment_started fired %d time(s)) — the reading above was sampled, not held open, and asserted nothing", hookStarts.Load())
	}

	// Eventually (once the background worker finishes) the candidates file
	// must appear and be valid.
	enrichment.DefaultScheduler.Wait(schedKey)

	candPath := filepath.Join(schedKey, "enrichment-candidates.json")
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
