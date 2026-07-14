package cli

// wizard_split_progress_test.go — TDD coverage for split-mode wizard completion
// detection. All fakes; no real daemon, no real index, no real sleeps.

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/cli/wiztui"
	"github.com/cajasmota/grafel/internal/install/detect"
	"github.com/cajasmota/grafel/internal/progress"
	"github.com/cajasmota/grafel/internal/statusfile"
	"github.com/cajasmota/grafel/internal/testsupport"
)

// fakeSplitClock advances virtual time on Sleep so the poll loop's timeout
// accounting works without any real delay.
type fakeSplitClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeSplitClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeSplitClock) Sleep(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

// fakeProbe returns a scripted Poll sequence (by 1-based call count) and a fixed
// Classify result.
type fakeProbe struct {
	mu       sync.Mutex
	calls    int
	pollFn   func(call int) splitPoll
	classify splitResult
}

func (p *fakeProbe) Poll() (splitPoll, error) {
	p.mu.Lock()
	p.calls++
	n := p.calls
	fn := p.pollFn
	p.mu.Unlock()
	return fn(n), nil
}

func (p *fakeProbe) Classify() (splitResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.classify, nil
}

func (p *fakeProbe) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func testPollCfg() splitPollConfig {
	return splitPollConfig{interval: 10 * time.Millisecond, startupWindow: 0, timeout: 5 * time.Minute}
}

func mkSSE(t *testing.T, e progress.Event) sseEvent {
	t.Helper()
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	return sseEvent{name: "progress", data: string(b)}
}

func noTrigger() error { return nil }

// fakeStatusStore is a mutable in-memory status plane for probe tests.
type fakeStatusStore struct {
	mu    sync.Mutex
	files map[string]*statusfile.File
}

func newFakeStatusStore() *fakeStatusStore {
	return &fakeStatusStore{files: map[string]*statusfile.File{}}
}

func (s *fakeStatusStore) read(rp string) (*statusfile.File, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.files[rp]
	if !ok || f == nil {
		return nil, false
	}
	cp := *f
	return &cp, true
}

func (s *fakeStatusStore) set(rp string, f *statusfile.File) {
	s.mu.Lock()
	s.files[rp] = f
	s.mu.Unlock()
}

func aliveLiveness(string) (*statusfile.File, bool) { return &statusfile.File{}, true }

// pendingUntil returns a pendingReader that reports "pending" for the first
// (n-1) calls, then "gone" (acked) from the n-th call onward.
func pendingUntil(n int) pendingReader {
	var mu sync.Mutex
	calls := 0
	return func() (bool, error) {
		mu.Lock()
		calls++
		c := calls
		mu.Unlock()
		return c < n, nil
	}
}

// 1. SPLIT: completion fires only AFTER the engine acks our rebuild request,
// never at the instant Rebuild returns.
func TestSplit_CompletionWaitsForRequestAck(t *testing.T) {
	const wantEntities, wantRels = int64(4321), int64(8765)
	probe := &fakeProbe{
		pollFn: func(call int) splitPoll {
			return splitPoll{RequestPending: call < 4, EngineAlive: true}
		},
		classify: splitResult{Entities: wantEntities, Rels: wantRels},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sseCh := make(chan sseEvent) // never delivers
	evCh := make(chan progress.Event, 8)
	rebuildCalled := false

	o := runSplitIndexCore(ctx, cancel, func() error { rebuildCalled = true; return nil }, sseCh, evCh, probe, &fakeSplitClock{}, testPollCfg(), true, nil)

	if !rebuildCalled {
		t.Fatal("triggerRebuild was never called")
	}
	if o.err != nil {
		t.Fatalf("unexpected error: %v", o.err)
	}
	if probe.count() < 4 {
		t.Fatalf("completed after %d polls; want >=4 (must wait for the request ack, not the enqueue return)", probe.count())
	}
	if o.entities != wantEntities || o.rels != wantRels {
		t.Fatalf("stats = (%d,%d); want (%d,%d)", o.entities, o.rels, wantEntities, wantRels)
	}
}

// 2. SPLIT: per-module SSE events emitted during the indexing window ARE
// forwarded to the TUI event channel (bars render), not cut off early.
func TestSplit_ForwardsPerModuleEventsDuringWindow(t *testing.T) {
	events := []progress.Event{
		{GroupSlug: "g", RepoSlug: "backend", Phase: "scanning", FilesDone: 10, FilesTotal: 100},
		{GroupSlug: "g", RepoSlug: "backend", Phase: "extracting_ast", FilesDone: 50, FilesTotal: 100},
		{GroupSlug: "g", RepoSlug: "frontend", Phase: "resolving_refs", FilesDone: 90, FilesTotal: 100},
	}
	const n = 3
	sseCh := make(chan sseEvent, n)
	for _, e := range events {
		sseCh <- mkSSE(t, e)
	}
	evCh := make(chan progress.Event, n)

	// Ack (complete) only once all n events have been forwarded into evCh.
	probe := &fakeProbe{
		pollFn:   func(int) splitPoll { return splitPoll{RequestPending: len(evCh) < n, EngineAlive: true} },
		classify: splitResult{Entities: 1, Rels: 1},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	o := runSplitIndexCore(ctx, cancel, noTrigger, sseCh, evCh, probe, &fakeSplitClock{}, testPollCfg(), true, nil)
	if o.err != nil {
		t.Fatalf("unexpected error: %v", o.err)
	}
	if got := len(evCh); got != n {
		t.Fatalf("forwarded %d events; want %d (per-module bars must render throughout the window)", got, n)
	}
	first := <-evCh
	if first.RepoSlug != "backend" || first.Phase != "scanning" {
		t.Fatalf("first forwarded event = %+v; want backend/scanning", first)
	}
}

// 3. SPLIT: the final outcome carries the real stats sourced from the status
// classification (entities=E, rels=R).
func TestSplit_OutcomeCarriesClassifiedStats(t *testing.T) {
	const E, R = int64(12345), int64(67890)
	probe := &fakeProbe{
		pollFn:   func(call int) splitPoll { return splitPoll{RequestPending: call < 2, EngineAlive: true} },
		classify: splitResult{Entities: E, Rels: R},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	o := runSplitIndexCore(ctx, cancel, noTrigger, make(chan sseEvent), make(chan progress.Event, 4), probe, &fakeSplitClock{}, testPollCfg(), true, nil)
	if o.err != nil {
		t.Fatalf("unexpected error: %v", o.err)
	}
	oc := toIndexOutcome(o, wiztui.InstallSummary{})
	if oc.Entities != E || oc.Rels != R {
		t.Fatalf("IndexOutcome stats = (%d,%d); want (%d,%d)", oc.Entities, oc.Rels, E, R)
	}
	if oc.Err != nil {
		t.Fatalf("IndexOutcome.Err = %v; want nil", oc.Err)
	}
}

// 4. SPLIT + engine dies: the engine-liveness heartbeat goes stale after having
// been alive and our request never acks → real ERROR, never a fake Done.
func TestSplit_EngineDiesSurfacesError(t *testing.T) {
	probe := &fakeProbe{pollFn: func(call int) splitPoll {
		if call <= 2 {
			return splitPoll{RequestPending: true, EngineAlive: true}
		}
		return splitPoll{RequestPending: true, EngineAlive: false}
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	o := runSplitIndexCore(ctx, cancel, noTrigger, make(chan sseEvent), make(chan progress.Event, 4), probe, &fakeSplitClock{}, testPollCfg(), true, nil)
	if o.err == nil {
		t.Fatal("engine died but outcome carried no error (fake Done)")
	}
	if o.entities != 0 {
		t.Fatalf("failed outcome should carry no stats, got entities=%d", o.entities)
	}
}

// 4b. SPLIT + never acks while alive: bounded last-resort timeout → real error.
func TestSplit_TimeoutSurfacesError(t *testing.T) {
	probe := &fakeProbe{pollFn: func(int) splitPoll {
		return splitPoll{RequestPending: true, EngineAlive: true} // alive forever, never acks
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := splitPollConfig{interval: time.Second, startupWindow: 0, timeout: 3 * time.Second}
	o := runSplitIndexCore(ctx, cancel, noTrigger, make(chan sseEvent), make(chan progress.Event, 4), probe, &fakeSplitClock{}, cfg, true, nil)
	if o.err == nil {
		t.Fatal("rebuild never acked but no timeout error surfaced")
	}
}

// S1. SPLIT + engine NEVER live: fail fast within the startup window, NOT after
// the full 45m timeout.
func TestSplit_NeverAliveEngineFailsFast(t *testing.T) {
	probe := &fakeProbe{pollFn: func(int) splitPoll {
		return splitPoll{RequestPending: true, EngineAlive: false} // never live
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := splitPollConfig{interval: 100 * time.Millisecond, startupWindow: time.Second, timeout: 45 * time.Minute}
	o := runSplitIndexCore(ctx, cancel, noTrigger, make(chan sseEvent), make(chan progress.Event, 4), probe, &fakeSplitClock{}, cfg, true, nil)
	if o.err == nil {
		t.Fatal("engine never came alive but no error surfaced")
	}
	if !strings.Contains(o.err.Error(), "never became live") {
		t.Fatalf("err = %v; want a fast never-live failure (not a 45m timeout)", o.err)
	}
	// Fast: it must have failed near the 1s startup window (~10 polls), not spin
	// to the 45m timeout.
	if probe.count() > 100 {
		t.Fatalf("polled %d times before fast-failing; want ~10 (startup window, not the full timeout)", probe.count())
	}
}

// B3. SPLIT multi-repo PARTIAL FAILURE: the engine acks the rebuild but repo B
// never produced a graph. This is the exact case that hangs today. It must
// reach a PROMPT terminal error naming B — not spin to the timeout. Uses the
// REAL statusPlaneProbe (request-ack + mtime classification) with fake readers.
func TestSplit_MultiRepoPartialFailure_PromptError(t *testing.T) {
	const repoA, repoB = "/repo/backend", "/repo/frontend"
	store := newFakeStatusStore() // both absent at construction → baseline 0

	// Engine acks after 2 polls (drained our rebuild request).
	probe := newStatusPlaneProbeWith([]string{repoA, repoB}, "/root", store.read, aliveLiveness, pendingUntil(3))

	// The engine finished: A produced a graph (mtime advanced), B did NOT
	// (no status file / no graph). Set A's completed state AFTER construction so
	// its mtime is past the captured baseline.
	store.set(repoA, &statusfile.File{Indexing: false, GraphFBMtime: 500, Entities: 1000, Relationships: 2000})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Short interval + comfortably large timeout: a PROMPT terminal state must
	// arrive from the ack, long before the timeout.
	cfg := splitPollConfig{interval: 10 * time.Millisecond, startupWindow: 0, timeout: 45 * time.Minute}
	o := runSplitIndexCore(ctx, cancel, noTrigger, make(chan sseEvent), make(chan progress.Event, 4), probe, &fakeSplitClock{}, cfg, true, nil)

	if o.err == nil {
		t.Fatal("partial failure (repo B never indexed) produced no error — this is the hang/fake-done regression")
	}
	if !strings.Contains(o.err.Error(), "frontend") {
		t.Fatalf("err = %v; want it to name the repo that failed (frontend)", o.err)
	}
}

// B3b. EMPTY REPO: a single repo that legitimately produces no graph → prompt
// terminal state, not a hang.
func TestSplit_EmptyRepo_PromptTerminal(t *testing.T) {
	const repo = "/repo/docs-only"
	store := newFakeStatusStore() // never produces a graph
	probe := newStatusPlaneProbeWith([]string{repo}, "/root", store.read, aliveLiveness, pendingUntil(2))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := splitPollConfig{interval: 10 * time.Millisecond, startupWindow: 0, timeout: 45 * time.Minute}
	o := runSplitIndexCore(ctx, cancel, noTrigger, make(chan sseEvent), make(chan progress.Event, 4), probe, &fakeSplitClock{}, cfg, true, nil)
	if o.err == nil {
		t.Fatal("empty repo produced no terminal error (would hang in production)")
	}
	if !strings.Contains(o.err.Error(), "docs-only") {
		t.Fatalf("err = %v; want it to name the empty repo", o.err)
	}
}

// PROBE: a fresh-group first index whose status file shows Indexing:false and an
// ADVANCED graph_fb_mtime but Entities==0 (the wizard/rebuild path does not
// write the graph-stats sidecar) MUST classify as indexed-OK (no Failed) — never
// spin. This is the exact live-daemon regression the coordinator flagged.
func TestStatusProbe_ClassifiesOKOnMtimeAdvanceEvenWithZeroEntities(t *testing.T) {
	const repo = "/repo/monorepo"
	store := newFakeStatusStore() // fresh group: no status file yet → baseline 0
	probe := newStatusPlaneProbeWith([]string{repo}, "/root", store.read, aliveLiveness, pendingUntil(1))

	// Index completes: graph.fb (re)written (mtime advances), but Entities stay
	// 0 because the sidecar was never written on this path.
	store.set(repo, &statusfile.File{Indexing: false, GraphFBMtime: 1_000_000, Entities: 0, Relationships: 0})

	res, _ := probe.Classify()
	if len(res.Failed) != 0 {
		t.Fatalf("Classify marked %v as failed on a graph_fb_mtime advance with Entities==0 (would surface a false failure)", res.Failed)
	}
	if res.Entities != 0 {
		t.Fatalf("Entities = %d; want 0 passed through untouched", res.Entities)
	}
}

// PROBE: a repo whose status file ALREADY exists with an OLD graph_fb_mtime and
// Indexing:false must NOT be classified as freshly indexed until graph_fb_mtime
// advances past the captured baseline (re-index correctness).
func TestStatusProbe_ClassifyWaitsForMtimeAdvancePastBaseline(t *testing.T) {
	const repo = "/repo/already-indexed"
	store := newFakeStatusStore()
	// Pre-existing graph from a prior index: mtime=1000, not indexing, has stats.
	store.set(repo, &statusfile.File{Indexing: false, GraphFBMtime: 1000, Entities: 500, Relationships: 900})

	probe := newStatusPlaneProbeWith([]string{repo}, "/root", store.read, aliveLiveness, pendingUntil(1)) // baseline=1000

	// Before the re-index rewrites graph.fb, the stale graph must be treated as
	// NOT freshly indexed.
	if res, _ := probe.Classify(); len(res.Failed) == 0 {
		t.Fatal("classified a pre-existing OLD graph as freshly indexed (no baseline advance)")
	}
	// Re-index finishes: graph.fb rewritten, mtime advances past baseline.
	store.set(repo, &statusfile.File{Indexing: false, GraphFBMtime: 2000, Entities: 550, Relationships: 950})
	res, _ := probe.Classify()
	if len(res.Failed) != 0 {
		t.Fatalf("still failed after graph_fb_mtime advanced past baseline: %v", res.Failed)
	}
	if res.Entities != 550 || res.Rels != 950 {
		t.Fatalf("stats = (%d,%d); want (550,950)", res.Entities, res.Rels)
	}
}

// pendingUntilThen is like pendingUntil, but invokes onAck exactly once on the
// first call that reports "gone" (acked) — modeling the engine flushing fresh
// per-repo status RIGHT BEFORE it writes the ack (blocker #5 ordering).
func pendingUntilThen(n int, onAck func()) pendingReader {
	var mu sync.Mutex
	calls := 0
	fired := false
	return func() (bool, error) {
		mu.Lock()
		calls++
		c := calls
		doFire := c >= n && !fired
		if doFire {
			fired = true
		}
		mu.Unlock()
		if c >= n {
			if doFire && onAck != nil {
				onAck()
			}
			return false, nil
		}
		return true, nil
	}
}

// #10. SPLIT with NO dashboard (nil SSE channel): completion must STILL wait for
// the request ack — never return instantly at the fire-and-forget enqueue. This
// is the path that previously bypassed the whole fix when dashPort==0.
func TestSplit_NoDashboard_WaitsForAck(t *testing.T) {
	probe := &fakeProbe{
		pollFn:   func(call int) splitPoll { return splitPoll{RequestPending: call < 5, EngineAlive: true} },
		classify: splitResult{Entities: 7, Rels: 9},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var sseCh <-chan sseEvent // nil: no dashboard, no live bars
	o := runSplitIndexCore(ctx, cancel, noTrigger, sseCh, make(chan progress.Event, 4), probe, &fakeSplitClock{}, testPollCfg(), true, nil)
	if o.err != nil {
		t.Fatalf("unexpected error: %v", o.err)
	}
	if probe.count() < 5 {
		t.Fatalf("completed after %d polls with no dashboard; want >=5 (must wait for the ack, not the enqueue return)", probe.count())
	}
	if o.entities != 7 || o.rels != 9 {
		t.Fatalf("stats = (%d,%d); want (7,9)", o.entities, o.rels)
	}
}

// #5a. Status-write-vs-ack RACE: if a rebuilt repo's status is STILL stale
// (graph_fb_mtime == baseline) at the instant the request acks, Classify
// false-fails a repo that actually succeeded. Documents the race the engine-side
// flush fixes.
func TestSplit_StaleStatusAtAck_FalseFailsWithoutFlush(t *testing.T) {
	const repo = "/repo/monorepo"
	store := newFakeStatusStore()
	// Pre-existing graph from a prior index → baseline captured at mtime 1000.
	store.set(repo, &statusfile.File{Indexing: false, GraphFBMtime: 1000})

	// Engine acks WITHOUT flushing fresh status (status stays at 1000 == baseline).
	probe := newStatusPlaneProbeWith([]string{repo}, "/root", store.read, aliveLiveness, pendingUntil(3))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	o := runSplitIndexCore(ctx, cancel, noTrigger, make(chan sseEvent), make(chan progress.Event, 4), probe, &fakeSplitClock{}, testPollCfg(), true, nil)
	if o.err == nil {
		t.Fatal("expected a (false) failure when status is stale at ack — documents the race the flush closes")
	}
}

// #5b. With the engine-side fix, fresh per-repo status is flushed BEFORE the ack
// is written, so by the time the request goes !pending the status already shows
// an advanced graph_fb_mtime and Classify passes.
func TestSplit_FreshStatusFlushedBeforeAck_ClassifiesOK(t *testing.T) {
	const repo = "/repo/monorepo"
	store := newFakeStatusStore()
	store.set(repo, &statusfile.File{Indexing: false, GraphFBMtime: 1000}) // baseline 1000

	// Model the fix: the engine flushes fresh status (mtime advances) RIGHT
	// BEFORE it writes the ack that makes the request go !pending.
	flush := func() {
		store.set(repo, &statusfile.File{Indexing: false, GraphFBMtime: 5000, Entities: 42, Relationships: 84})
	}
	probe := newStatusPlaneProbeWith([]string{repo}, "/root", store.read, aliveLiveness, pendingUntilThen(3, flush))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	o := runSplitIndexCore(ctx, cancel, noTrigger, make(chan sseEvent), make(chan progress.Event, 4), probe, &fakeSplitClock{}, testPollCfg(), true, nil)
	if o.err != nil {
		t.Fatalf("fresh status flushed before ack should classify OK, got: %v", o.err)
	}
	if o.entities != 42 || o.rels != 84 {
		t.Fatalf("stats = (%d,%d); want (42,84) from the freshly-flushed status", o.entities, o.rels)
	}
}

// HYBRID-1. SPLIT: every repo becomes graph-queryable (graph_fb_mtime advanced,
// !Indexing) while our rebuild request is STILL pending. Per the queryable-state
// UX (the graph is queryable, but background enhancement — the linksFn tail —
// is still running), this must NOT complete early anymore: it fires
// onQueryable EXACTLY ONCE with the queryable-time stats, then keeps polling
// until the real ack, returning the (here, identical) final stats.
func TestSplit_AllReposAdvance_FiresQueryableThenWaitsForAck(t *testing.T) {
	const repoA, repoB = "/repo/backend", "/repo/frontend"
	store := newFakeStatusStore() // both absent at construction → baseline 0
	pending := pendingUntil(6)    // acks well after AllAdvanced first goes true
	probe := newStatusPlaneProbeWith([]string{repoA, repoB}, "/root", store.read, aliveLiveness, pending)

	// Both repos produce a fresh graph (mtime advanced past baseline 0) while the
	// request is still pending — the ~6-min "graph queryable" moment.
	store.set(repoA, &statusfile.File{Indexing: false, GraphFBMtime: 100, Entities: 5, Relationships: 6})
	store.set(repoB, &statusfile.File{Indexing: false, GraphFBMtime: 200, Entities: 7, Relationships: 8})

	var qmu sync.Mutex
	fired := 0
	var interim splitResult
	onQueryable := func(res splitResult) {
		qmu.Lock()
		fired++
		interim = res
		qmu.Unlock()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := splitPollConfig{interval: 10 * time.Millisecond, startupWindow: 0, timeout: 45 * time.Minute}
	o := runSplitIndexCore(ctx, cancel, noTrigger, make(chan sseEvent), make(chan progress.Event, 4), probe, &fakeSplitClock{}, cfg, true, onQueryable)
	if o.err != nil {
		t.Fatalf("unexpected error: %v", o.err)
	}
	if o.entities != 12 || o.rels != 14 {
		t.Fatalf("final stats = (%d,%d); want (12,14) summed across the advanced repos", o.entities, o.rels)
	}
	qmu.Lock()
	gotFired, gotInterim := fired, interim
	qmu.Unlock()
	if gotFired != 1 {
		t.Fatalf("onQueryable fired %d times; want exactly 1", gotFired)
	}
	if gotInterim.Entities != 12 || gotInterim.Rels != 14 {
		t.Fatalf("interim stats = (%d,%d); want (12,14)", gotInterim.Entities, gotInterim.Rels)
	}
}

// HYBRID-2. SPLIT: repo A advances but repo B NEVER produces a graph. AllAdvanced
// can never be true, so the early path never fires; completion must fall through
// to the ack backstop and surface a PROMPT error naming B — not an early false
// success, not a hang.
func TestSplit_OneRepoNeverAdvances_AckBackstopPromptError(t *testing.T) {
	const repoA, repoB = "/repo/backend", "/repo/frontend"
	store := newFakeStatusStore() // baseline 0 for both
	var pmu sync.Mutex
	pollCount := 0
	inner := pendingUntil(5)
	counting := func() (bool, error) {
		pmu.Lock()
		pollCount++
		pmu.Unlock()
		return inner()
	}
	probe := newStatusPlaneProbeWith([]string{repoA, repoB}, "/root", store.read, aliveLiveness, counting)

	// Only A ever advances; B stays absent → AllAdvanced never becomes true.
	store.set(repoA, &statusfile.File{Indexing: false, GraphFBMtime: 500, Entities: 10, Relationships: 20})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := splitPollConfig{interval: 10 * time.Millisecond, startupWindow: 0, timeout: 45 * time.Minute}
	o := runSplitIndexCore(ctx, cancel, noTrigger, make(chan sseEvent), make(chan progress.Event, 4), probe, &fakeSplitClock{}, cfg, true, nil)
	if o.err == nil {
		t.Fatal("repo B never advanced but completion reported success (AllAdvanced early false-success or hang)")
	}
	if !strings.Contains(o.err.Error(), "frontend") {
		t.Fatalf("err = %v; want it to name the non-advanced repo (frontend) via the ack backstop", o.err)
	}
	// It must have WAITED for the ack (pendingUntil(5)) rather than short-circuit.
	pmu.Lock()
	got := pollCount
	pmu.Unlock()
	if got < 4 {
		t.Fatalf("completed after %d polls; want >=4 (must wait for the ack backstop, not early-complete)", got)
	}
}

// HYBRID-3. FALSE-FAILURE guard: while a repo's status is STALE (graph_fb_mtime
// == baseline) the AllAdvanced predicate stays false, so onQueryable must NOT
// fire on a stale read — only once graph_fb_mtime genuinely advances. It still
// waits for the real ack to complete (queryable-state UX), firing onQueryable
// exactly once at the advance.
func TestSplit_StaleStatus_QueryableFiresOnceThenWaitsForAck(t *testing.T) {
	const repo = "/repo/monorepo"
	var mu sync.Mutex
	calls := 0
	// Call 1 is the baseline capture in the constructor (mtime 1000). Reads while
	// still stale return 1000 (== baseline). After several polls the graph is
	// rewritten (mtime 2000) → genuinely advanced.
	read := func(string) (*statusfile.File, bool) {
		mu.Lock()
		calls++
		c := calls
		mu.Unlock()
		if c <= 4 {
			return &statusfile.File{Indexing: false, GraphFBMtime: 1000}, true // stale
		}
		return &statusfile.File{Indexing: false, GraphFBMtime: 2000, Entities: 11, Relationships: 22}, true
	}
	pending := pendingUntil(10) // acks well after the advance
	probe := newStatusPlaneProbeWith([]string{repo}, "/root", read, aliveLiveness, pending)

	var qmu sync.Mutex
	fired := 0
	onQueryable := func(splitResult) {
		qmu.Lock()
		fired++
		qmu.Unlock()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := splitPollConfig{interval: 10 * time.Millisecond, startupWindow: 0, timeout: 45 * time.Minute}
	o := runSplitIndexCore(ctx, cancel, noTrigger, make(chan sseEvent), make(chan progress.Event, 4), probe, &fakeSplitClock{}, cfg, true, onQueryable)
	if o.err != nil {
		t.Fatalf("should complete once the ack lands, got: %v", o.err)
	}
	if o.entities != 11 || o.rels != 22 {
		t.Fatalf("stats = (%d,%d); want (11,22) from the advanced status", o.entities, o.rels)
	}
	qmu.Lock()
	gotFired := fired
	qmu.Unlock()
	if gotFired != 1 {
		t.Fatalf("onQueryable fired %d times; want exactly 1 (must not fire on a stale read)", gotFired)
	}
}

// QUERYABLE-0a. Fast path (AllAdvanced and the ack land in the SAME poll —
// i.e. the rebuild was already fully done by the time we first noticed): no
// interim is emitted, straight to the terminal return.
func TestSplit_FastPath_AllAdvancedAndAckedSamePoll_NoInterim(t *testing.T) {
	probe := &fakeProbe{
		pollFn:   func(int) splitPoll { return splitPoll{RequestPending: false, AllAdvanced: true, EngineAlive: true} },
		classify: splitResult{Entities: 42, Rels: 7},
	}
	fired := 0
	onQueryable := func(splitResult) { fired++ }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	o := runSplitIndexCore(ctx, cancel, noTrigger, make(chan sseEvent), make(chan progress.Event, 4), probe, &fakeSplitClock{}, testPollCfg(), true, onQueryable)
	if o.err != nil {
		t.Fatalf("unexpected error: %v", o.err)
	}
	if fired != 0 {
		t.Fatalf("onQueryable fired %d times; want 0 (AllAdvanced and ack landed in the same poll — nothing left to wait for)", fired)
	}
	if o.entities != 42 || o.rels != 7 {
		t.Fatalf("stats = (%d,%d); want (42,7)", o.entities, o.rels)
	}
}

// QUERYABLE-0b. Ordinary fast path (small index: never even observed
// AllAdvanced before the ack lands): no interim, unchanged from the pre-#queryable
// behavior — mirrors TestSplit_CompletionWaitsForRequestAck but asserts the
// callback specifically.
func TestSplit_FastAck_NeverAllAdvanced_NoInterim(t *testing.T) {
	probe := &fakeProbe{
		pollFn: func(call int) splitPoll {
			return splitPoll{RequestPending: call < 3, EngineAlive: true} // never AllAdvanced
		},
		classify: splitResult{Entities: 9, Rels: 3},
	}
	fired := 0
	onQueryable := func(splitResult) { fired++ }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	o := runSplitIndexCore(ctx, cancel, noTrigger, make(chan sseEvent), make(chan progress.Event, 4), probe, &fakeSplitClock{}, testPollCfg(), true, onQueryable)
	if o.err != nil {
		t.Fatalf("unexpected error: %v", o.err)
	}
	if fired != 0 {
		t.Fatalf("onQueryable fired %d times; want 0 (AllAdvanced never observed)", fired)
	}
}

// QUERYABLE-0c. Partial-failure path: AllAdvanced never becomes true (one repo
// never advances) and the ack eventually surfaces the classification failure —
// no interim, error classification unchanged from the pre-#queryable behavior.
func TestSplit_PartialFailure_NoInterim(t *testing.T) {
	const repoA, repoB = "/repo/backend", "/repo/frontend"
	store := newFakeStatusStore()
	probe := newStatusPlaneProbeWith([]string{repoA, repoB}, "/root", store.read, aliveLiveness, pendingUntil(3))
	store.set(repoA, &statusfile.File{Indexing: false, GraphFBMtime: 500, Entities: 1000, Relationships: 2000})
	// repoB never produces a graph → AllAdvanced never true.

	fired := 0
	onQueryable := func(splitResult) { fired++ }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := splitPollConfig{interval: 10 * time.Millisecond, startupWindow: 0, timeout: 45 * time.Minute}
	o := runSplitIndexCore(ctx, cancel, noTrigger, make(chan sseEvent), make(chan progress.Event, 4), probe, &fakeSplitClock{}, cfg, true, onQueryable)
	if o.err == nil {
		t.Fatal("expected a partial-failure error naming the non-advanced repo")
	}
	if !strings.Contains(o.err.Error(), "frontend") {
		t.Fatalf("err = %v; want it to name frontend", o.err)
	}
	if fired != 0 {
		t.Fatalf("onQueryable fired %d times; want 0 on a partial-failure path", fired)
	}
}

// 5. MONOLITH: unchanged — completion is the RPC return, carrying the RPC's own
// stats, and in-window SSE events still forward. Exercises the monolith path
// (forwardBrokerToChannel), which the fix must leave byte-identical.
func TestMonolith_CompletesAtRPCReturnWithRPCStats(t *testing.T) {
	sseCh := make(chan sseEvent, 2)
	sseCh <- mkSSE(t, progress.Event{RepoSlug: "backend", Phase: "scanning"})
	rpcCh := make(chan rebuildOutcome, 1)
	rpcCh <- rebuildOutcome{entities: 111, rels: 222, elapsed: 3}
	evCh := make(chan progress.Event, 4)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	o := forwardBrokerToChannel(ctx, sseCh, rpcCh, evCh)
	if o.err != nil {
		t.Fatalf("unexpected error: %v", o.err)
	}
	if o.entities != 111 || o.rels != 222 {
		t.Fatalf("monolith stats = (%d,%d); want (111,222) from the RPC reply", o.entities, o.rels)
	}
}

// --- Per-repo classify stats (#seed-rows dropped-row fix, part 2) ---

// TestStatusProbe_ClassifyPopulatesPerRepoStats: Classify must return a
// splitRepoResult per group repo (slug/entities/rels/advanced-vs-failed), not
// just the aggregate — this is what lets the wiztui model populate a row for
// a repo that emitted zero progress events, sourced from the status plane
// rather than folded SSE ticks.
func TestStatusProbe_ClassifyPopulatesPerRepoStats(t *testing.T) {
	const repoA, repoB, repoC = "/repo/core-mobile", "/repo/upvate_core", "/repo/upvate_core_frontend"
	store := newFakeStatusStore()
	probe := newStatusPlaneProbeWith([]string{repoA, repoB, repoC}, "/root", store.read, aliveLiveness, pendingUntil(1))

	// All three advance past baseline (0) — including repoC, which never
	// reported a single progress tick over SSE (the live bug scenario).
	store.set(repoA, &statusfile.File{Indexing: false, GraphFBMtime: 100, Entities: 8383, Relationships: 9000})
	store.set(repoB, &statusfile.File{Indexing: false, GraphFBMtime: 100, Entities: 6039, Relationships: 7000})
	store.set(repoC, &statusfile.File{Indexing: false, GraphFBMtime: 100, Entities: 17270, Relationships: 18000})

	res, err := probe.Classify()
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if len(res.Repos) != 3 {
		t.Fatalf("len(Repos) = %d, want 3 (one per group repo, even one with zero progress events)", len(res.Repos))
	}
	byslug := map[string]splitRepoResult{}
	for _, r := range res.Repos {
		byslug[r.Slug] = r
	}
	c, ok := byslug["upvate_core_frontend"]
	if !ok {
		t.Fatal("silent repo (upvate_core_frontend) missing from Classify's per-repo stats")
	}
	if c.Failed {
		t.Error("silent-but-advanced repo incorrectly marked Failed")
	}
	if c.Entities != 17270 || c.Rels != 18000 {
		t.Errorf("silent repo stats = (%d,%d), want (17270,18000)", c.Entities, c.Rels)
	}

	var sum int64
	for _, r := range res.Repos {
		sum += r.Entities
	}
	if sum != res.Entities {
		t.Errorf("sum of per-repo Entities = %d, want %d (must match the aggregate)", sum, res.Entities)
	}
}

// TestStatusProbe_ClassifyPerRepoStats_MarksFailedRepo: a repo that never
// advances is reported in Repos as Failed with a reason, alongside the
// existing aggregate Failed slice.
func TestStatusProbe_ClassifyPerRepoStats_MarksFailedRepo(t *testing.T) {
	const repoOK, repoBad = "/repo/backend", "/repo/docs-only"
	store := newFakeStatusStore()
	probe := newStatusPlaneProbeWith([]string{repoOK, repoBad}, "/root", store.read, aliveLiveness, pendingUntil(1))
	store.set(repoOK, &statusfile.File{Indexing: false, GraphFBMtime: 100, Entities: 10})
	// repoBad never produces a graph.

	res, _ := probe.Classify()
	var bad splitRepoResult
	found := false
	for _, r := range res.Repos {
		if r.Slug == "docs-only" {
			bad = r
			found = true
		}
	}
	if !found {
		t.Fatal("failed repo missing from per-repo Repos")
	}
	if !bad.Failed {
		t.Error("Failed = false, want true")
	}
	if bad.Reason == "" {
		t.Error("Reason empty for a failed repo")
	}
}

// TestRunSplitIndexCore_ThreadsPerRepoStatsIntoOutcome: a successful split
// index's rebuildOutcome carries the classify's per-repo breakdown, and
// toIndexOutcome maps it through to wiztui.IndexOutcome.RepoStats so the
// model can populate a silent repo's row.
func TestRunSplitIndexCore_ThreadsPerRepoStatsIntoOutcome(t *testing.T) {
	probe := &fakeProbe{
		pollFn: func(call int) splitPoll { return splitPoll{RequestPending: call < 2, EngineAlive: true} },
		classify: splitResult{
			Entities: 31692, Rels: 40000,
			Repos: []splitRepoResult{
				{Slug: "core-mobile", Entities: 8383, Rels: 9000},
				{Slug: "upvate_core", Entities: 6039, Rels: 7000},
				{Slug: "upvate_core_frontend", Entities: 17270, Rels: 24000},
			},
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	o := runSplitIndexCore(ctx, cancel, noTrigger, make(chan sseEvent), make(chan progress.Event, 4), probe, &fakeSplitClock{}, testPollCfg(), true, nil)
	if o.err != nil {
		t.Fatalf("unexpected error: %v", o.err)
	}
	if len(o.repoStats) != 3 {
		t.Fatalf("len(o.repoStats) = %d, want 3", len(o.repoStats))
	}

	oc := toIndexOutcome(o, wiztui.InstallSummary{})
	if len(oc.RepoStats) != 3 {
		t.Fatalf("len(IndexOutcome.RepoStats) = %d, want 3", len(oc.RepoStats))
	}
	found := false
	for _, rs := range oc.RepoStats {
		if rs.Slug == "upvate_core_frontend" {
			found = true
			if rs.Entities != 17270 {
				t.Errorf("silent repo Entities = %d, want 17270", rs.Entities)
			}
			if rs.Failed {
				t.Error("silent-but-advanced repo incorrectly Failed")
			}
		}
	}
	if !found {
		t.Fatal("silent repo missing from mapped IndexOutcome.RepoStats")
	}
}

// TestMakeIndexFunc_SeedsQueuedRowForEverySelectedRepo: the IndexFunc must
// emit a PhaseQueued progress event for every repo in the selection BEFORE
// any real indexing work — the fix for the dropped-row bug where a repo with
// no real progress events never got a row at all. Uses --no-index so the
// test never touches a real daemon; group registration still runs against a
// throwaway GRAFEL_HOME.
func TestMakeIndexFunc_SeedsQueuedRowForEverySelectedRepo(t *testing.T) {
	dir := testsupport.IsolateHome(t)

	repoA := t.TempDir()
	repoB := t.TempDir()

	var out, errOut bytes.Buffer
	class, _ := detect.ClassifyPath(repoA)
	opts := wizardOptions{NoIndex: true, RunInstall: false}
	idxFn := makeIndexFunc(&out, &errOut, class, opts, nil)

	res := wiztui.Result{
		Action:    wiztui.ActionGroup,
		Repos:     []string{repoA, repoB},
		GroupName: "seed-test-group-" + filepath.Base(dir),
	}

	evCh, outCh := idxFn(res)

	var seeded []string
	for e := range evCh {
		if e.Phase == wiztui.PhaseQueued {
			seeded = append(seeded, e.RepoSlug)
		}
	}
	<-outCh // drain to let the goroutine finish cleanly

	wantA, wantB := filepath.Base(repoA), filepath.Base(repoB)
	gotA, gotB := false, false
	for _, s := range seeded {
		if s == wantA {
			gotA = true
		}
		if s == wantB {
			gotB = true
		}
	}
	if !gotA || !gotB {
		t.Fatalf("seeded slugs = %v, want both %q and %q", seeded, wantA, wantB)
	}
}

// TestMakeIndexFunc_MonorepoDoesNotSeedQueuedRows is the BLOCKING-regression
// guard: for a MONOREPO action, reposForResult collapses to ONE registry.Repo
// (root slug, packages in .Modules) but real progress is PER-MODULE (#5751),
// so seeding a bare repo-level row would neither merge with the per-module
// event keys nor be suppressed by the repo-row guard — producing a spurious
// repo-level row that doubles the visible entity total. The IndexFunc must
// therefore emit ZERO PhaseQueued events for a monorepo.
func TestMakeIndexFunc_MonorepoDoesNotSeedQueuedRows(t *testing.T) {
	dir := testsupport.IsolateHome(t)
	monorepo := t.TempDir()

	var out, errOut bytes.Buffer
	class, _ := detect.ClassifyPath(monorepo)
	opts := wizardOptions{NoIndex: true, RunInstall: false}
	idxFn := makeIndexFunc(&out, &errOut, class, opts, nil)

	res := wiztui.Result{
		Action:    wiztui.ActionMonorepo,
		Repos:     []string{"services/auth", "packages/ui"}, // chosen packages → Modules
		GroupName: "mono-test-group-" + filepath.Base(dir),
	}

	evCh, outCh := idxFn(res)
	seeded := 0
	for e := range evCh {
		if e.Phase == wiztui.PhaseQueued {
			seeded++
		}
	}
	<-outCh

	if seeded != 0 {
		t.Fatalf("monorepo emitted %d PhaseQueued seed events; want 0 (a bare repo-level seed row doubles the entity total)", seeded)
	}
}

// --- Live status-plane row driving (#liverows: fast/warm re-index rows must
// not sit at "Queued…" the whole time and jump to Done all at once) ---

// LIVE-1. SPLIT status-plane row driving: a flat-group repo whose status goes
// indexing→done with NO SSE events at all must drive its row queued→active→
// Done LIVE during the poll, not sit at "Queued…" until the terminal
// classify. This is the exact live-daemon symptom: on a fast/warm re-index
// (or whenever no dashboard SSE is delivering), the seeded row used to render
// 0% the entire time and then jump straight to Done at the very end.
func TestSplit_StatusPlaneDrivesRowLiveWithNoSSE(t *testing.T) {
	const repo = "/repo/backend"
	wantSlug := filepath.Base(repo)

	var mu sync.Mutex
	calls := 0
	read := func(string) (*statusfile.File, bool) {
		mu.Lock()
		calls++
		c := calls
		mu.Unlock()
		if c <= 3 {
			return &statusfile.File{Indexing: true, Entities: 0}, true // actively indexing
		}
		return &statusfile.File{Indexing: false, GraphFBMtime: 1000, Entities: 4242}, true // done
	}
	probe := newStatusPlaneProbeWith([]string{repo}, "/root", read, aliveLiveness, pendingUntil(8))

	var sseCh <-chan sseEvent // nil: NEVER delivers — no dashboard / fast-path race
	evCh := make(chan progress.Event, 64)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := splitPollConfig{interval: time.Millisecond, startupWindow: 0, timeout: 45 * time.Minute}
	o := runSplitIndexCore(ctx, cancel, noTrigger, sseCh, evCh, probe, &fakeSplitClock{}, cfg, true, nil)
	if o.err != nil {
		t.Fatalf("unexpected error: %v", o.err)
	}

	rows := map[string]wiztui.Row{}
	sawActive := false
	n := len(evCh)
	if n == 0 {
		t.Fatal("no status-plane row events were forwarded onto evCh — row would sit at Queued the whole index")
	}
	for i := 0; i < n; i++ {
		e := <-evCh
		rows = wiztui.Fold(rows, e)
		if rows[wantSlug].Phase == progress.PhaseIndexing {
			sawActive = true
		}
	}
	if !sawActive {
		t.Fatal("row never passed through the PhaseIndexing active phase — it would appear stuck at Queued until the terminal classify")
	}
	final := rows[wantSlug]
	if final.Phase != progress.PhaseDone {
		t.Fatalf("final row phase = %q, want %q (done)", final.Phase, progress.PhaseDone)
	}
	if final.EntitiesSoFar != 4242 {
		t.Fatalf("final row EntitiesSoFar = %d, want 4242 (from the status plane)", final.EntitiesSoFar)
	}
}

// LIVE-2. The aggregate progress bar must LEAVE 0% as soon as a repo goes
// active/done via the status plane — not sit at 0 until the terminal
// classify (the same live symptom viewed through AggregateProgress, which
// drives the wizard's overall progress bar).
func TestSplit_StatusPlaneAggregateProgressAdvances(t *testing.T) {
	const repo = "/repo/backend"
	wantSlug := filepath.Base(repo)

	var mu sync.Mutex
	calls := 0
	read := func(string) (*statusfile.File, bool) {
		mu.Lock()
		calls++
		c := calls
		mu.Unlock()
		if c <= 3 {
			return &statusfile.File{Indexing: true, Entities: 100}, true
		}
		return &statusfile.File{Indexing: false, GraphFBMtime: 1000, Entities: 500}, true
	}
	probe := newStatusPlaneProbeWith([]string{repo}, "/root", read, aliveLiveness, pendingUntil(8))

	var sseCh <-chan sseEvent
	evCh := make(chan progress.Event, 64)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := splitPollConfig{interval: time.Millisecond, startupWindow: 0, timeout: 45 * time.Minute}
	o := runSplitIndexCore(ctx, cancel, noTrigger, sseCh, evCh, probe, &fakeSplitClock{}, cfg, true, nil)
	if o.err != nil {
		t.Fatalf("unexpected error: %v", o.err)
	}

	rows := map[string]wiztui.Row{}
	sawPositiveBeforeDone := false
	n := len(evCh)
	for i := 0; i < n; i++ {
		e := <-evCh
		rows = wiztui.Fold(rows, e)
		if rows[wantSlug].Phase != progress.PhaseDone && wiztui.AggregateProgress(rows, 1) > 0 {
			sawPositiveBeforeDone = true
		}
	}
	if !sawPositiveBeforeDone {
		t.Fatal("AggregateProgress stayed at 0 until the terminal Done event — the overall bar would look hung")
	}
}

// LIVE-3. Monotonic merge: a status-plane event (coarse PhaseIndexing) must
// NOT regress a row that a real, finer-grained SSE event already advanced
// past it, and it must fold into the SAME row (no duplicate) rather than
// creating a second entry keyed differently.
func TestSplit_StatusPlaneEventMergesMonotonicWithFinerSSEPhase(t *testing.T) {
	rows := map[string]wiztui.Row{}
	// A real SSE tick already reported a finer, more-advanced phase.
	rows = wiztui.Fold(rows, progress.Event{RepoSlug: "backend", Phase: progress.PhaseResolveRefs, TS: 1})

	// A later status-plane poll observes the repo still Indexing and
	// synthesizes the coarse PhaseIndexing event.
	evCh := make(chan progress.Event, 4)
	emitStatusPlaneRowEvents(evCh, []repoStatusSnapshot{{Slug: "backend", Indexing: true, Entities: 77}})
	if len(evCh) != 1 {
		t.Fatalf("emitStatusPlaneRowEvents sent %d events, want 1", len(evCh))
	}
	statusEvent := <-evCh
	statusEvent.TS = 2 // later than the SSE event, still must not regress the phase
	rows = wiztui.Fold(rows, statusEvent)

	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 (status-plane event must merge into the existing row, not duplicate it)", len(rows))
	}
	got := rows["backend"]
	if got.Phase != progress.PhaseResolveRefs {
		t.Fatalf("row phase regressed to %q; want it to stay at the finer SSE phase %q", got.Phase, progress.PhaseResolveRefs)
	}
}

// LIVE-4. Monorepo gate: for a monorepo (perRepoRows=false), the completion
// poll must emit ZERO status-plane row events — the status plane has ONE
// entry for the whole repo root while the display is per-MODULE, so emitting
// a repo-level event here would resurrect the spurious repo-level row #5773
// removed and double-count against the per-module rows.
func TestSplit_Monorepo_NoStatusPlaneRowEmitted(t *testing.T) {
	const repo = "/repo/monorepo-root"
	store := newFakeStatusStore()
	// Repo actively indexing, then advances (flushed right before the ack,
	// same pattern as pendingUntilThen elsewhere in this file) — exactly the
	// sequence that would fire row events for a flat repo.
	store.set(repo, &statusfile.File{Indexing: true})
	flush := func() {
		store.set(repo, &statusfile.File{Indexing: false, GraphFBMtime: 1000, Entities: 999})
	}
	probe := newStatusPlaneProbeWith([]string{repo}, "/root", store.read, aliveLiveness, pendingUntilThen(4, flush))

	var sseCh <-chan sseEvent
	evCh := make(chan progress.Event, 64)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := splitPollConfig{interval: time.Millisecond, startupWindow: 0, timeout: 45 * time.Minute}
	o := runSplitIndexCore(ctx, cancel, noTrigger, sseCh, evCh, probe, &fakeSplitClock{}, cfg, false /* monorepo: perRepoRows=false */, nil)
	if o.err != nil {
		t.Fatalf("unexpected error: %v", o.err)
	}
	if got := len(evCh); got != 0 {
		t.Fatalf("evCh got %d status-plane row event(s) for a monorepo; want 0 (perRepoRows=false must suppress them)", got)
	}
}
