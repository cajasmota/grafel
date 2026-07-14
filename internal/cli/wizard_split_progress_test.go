package cli

// wizard_split_progress_test.go — TDD coverage for split-mode wizard completion
// detection. All fakes; no real daemon, no real index, no real sleeps.

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/cli/wiztui"
	"github.com/cajasmota/grafel/internal/progress"
	"github.com/cajasmota/grafel/internal/statusfile"
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

	o := runSplitIndexCore(ctx, cancel, func() error { rebuildCalled = true; return nil }, sseCh, evCh, probe, &fakeSplitClock{}, testPollCfg())

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
	o := runSplitIndexCore(ctx, cancel, noTrigger, sseCh, evCh, probe, &fakeSplitClock{}, testPollCfg())
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
	o := runSplitIndexCore(ctx, cancel, noTrigger, make(chan sseEvent), make(chan progress.Event, 4), probe, &fakeSplitClock{}, testPollCfg())
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
	o := runSplitIndexCore(ctx, cancel, noTrigger, make(chan sseEvent), make(chan progress.Event, 4), probe, &fakeSplitClock{}, testPollCfg())
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
	o := runSplitIndexCore(ctx, cancel, noTrigger, make(chan sseEvent), make(chan progress.Event, 4), probe, &fakeSplitClock{}, cfg)
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
	o := runSplitIndexCore(ctx, cancel, noTrigger, make(chan sseEvent), make(chan progress.Event, 4), probe, &fakeSplitClock{}, cfg)
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
	o := runSplitIndexCore(ctx, cancel, noTrigger, make(chan sseEvent), make(chan progress.Event, 4), probe, &fakeSplitClock{}, cfg)

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
	o := runSplitIndexCore(ctx, cancel, noTrigger, make(chan sseEvent), make(chan progress.Event, 4), probe, &fakeSplitClock{}, cfg)
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
	o := runSplitIndexCore(ctx, cancel, noTrigger, sseCh, make(chan progress.Event, 4), probe, &fakeSplitClock{}, testPollCfg())
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
	o := runSplitIndexCore(ctx, cancel, noTrigger, make(chan sseEvent), make(chan progress.Event, 4), probe, &fakeSplitClock{}, testPollCfg())
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
	o := runSplitIndexCore(ctx, cancel, noTrigger, make(chan sseEvent), make(chan progress.Event, 4), probe, &fakeSplitClock{}, testPollCfg())
	if o.err != nil {
		t.Fatalf("fresh status flushed before ack should classify OK, got: %v", o.err)
	}
	if o.entities != 42 || o.rels != 84 {
		t.Fatalf("stats = (%d,%d); want (42,84) from the freshly-flushed status", o.entities, o.rels)
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
