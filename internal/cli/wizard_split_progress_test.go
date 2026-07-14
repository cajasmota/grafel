package cli

// wizard_split_progress_test.go — TDD coverage for split-mode wizard completion
// detection. All fakes; no real daemon, no real index, no real sleeps.

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/cli/wiztui"
	"github.com/cajasmota/grafel/internal/progress"
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

// fakeProbe returns a scripted sequence of snapshots. snapFn is called on every
// Snapshot() with the (1-based) call count and returns the reading.
type fakeProbe struct {
	mu    sync.Mutex
	calls int
	fn    func(call int) splitSnapshot
}

func (p *fakeProbe) Snapshot() (splitSnapshot, error) {
	p.mu.Lock()
	p.calls++
	n := p.calls
	fn := p.fn
	p.mu.Unlock()
	return fn(n), nil
}

func (p *fakeProbe) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func testPollCfg() splitPollConfig {
	return splitPollConfig{interval: 10 * time.Millisecond, timeout: 5 * time.Minute}
}

func mkSSE(t *testing.T, e progress.Event) sseEvent {
	t.Helper()
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	return sseEvent{name: "progress", data: string(b)}
}

// 1. SPLIT: completion fires only AFTER the status transition, never at the
// instant Rebuild returns.
func TestSplit_CompletionWaitsForStatusTransition(t *testing.T) {
	const wantEntities, wantRels = int64(4321), int64(8765)
	// Not indexed (but engine alive) for the first 3 polls; indexed on the 4th.
	probe := &fakeProbe{fn: func(call int) splitSnapshot {
		if call < 4 {
			return splitSnapshot{Indexed: false, EngineAlive: true}
		}
		return splitSnapshot{Indexed: true, EngineAlive: true, Entities: wantEntities, Rels: wantRels}
	}}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sseCh := make(chan sseEvent) // never delivers
	evCh := make(chan progress.Event, 8)
	rebuildCalled := false

	o := runSplitIndexCore(ctx, cancel, func() { rebuildCalled = true }, sseCh, evCh, probe, &fakeSplitClock{}, testPollCfg())

	if !rebuildCalled {
		t.Fatal("triggerRebuild was never called")
	}
	if o.err != nil {
		t.Fatalf("unexpected error: %v", o.err)
	}
	if probe.count() < 4 {
		t.Fatalf("completed after %d polls; want >=4 (must wait for the status transition, not the enqueue return)", probe.count())
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

	// Complete only once all n events have been forwarded into evCh's buffer.
	probe := &fakeProbe{fn: func(int) splitSnapshot {
		if len(evCh) >= n {
			return splitSnapshot{Indexed: true, EngineAlive: true, Entities: 1, Rels: 1}
		}
		return splitSnapshot{Indexed: false, EngineAlive: true}
	}}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	o := runSplitIndexCore(ctx, cancel, func() {}, sseCh, evCh, probe, &fakeSplitClock{}, testPollCfg())
	if o.err != nil {
		t.Fatalf("unexpected error: %v", o.err)
	}
	if got := len(evCh); got != n {
		t.Fatalf("forwarded %d events; want %d (per-module bars must render throughout the window)", got, n)
	}
	// Sanity: the forwarded events parse back to the per-module payloads.
	first := <-evCh
	if first.RepoSlug != "backend" || first.Phase != "scanning" {
		t.Fatalf("first forwarded event = %+v; want backend/scanning", first)
	}
}

// 3. SPLIT: the final outcome carries the real stats sourced from the status
// signal (entities=E, rels=R), not empty.
func TestSplit_OutcomeCarriesStatusStats(t *testing.T) {
	const E, R = int64(12345), int64(67890)
	probe := &fakeProbe{fn: func(call int) splitSnapshot {
		if call < 2 {
			return splitSnapshot{Indexed: false, EngineAlive: true}
		}
		return splitSnapshot{Indexed: true, EngineAlive: true, Entities: E, Rels: R, ElapsedSec: 7}
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	o := runSplitIndexCore(ctx, cancel, func() {}, make(chan sseEvent), make(chan progress.Event, 4), probe, &fakeSplitClock{}, testPollCfg())
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
// been alive and the group never completes → the outcome is a real ERROR, never
// a fake Done.
func TestSplit_EngineDiesSurfacesError(t *testing.T) {
	probe := &fakeProbe{fn: func(call int) splitSnapshot {
		if call <= 2 {
			return splitSnapshot{Indexed: false, EngineAlive: true} // alive, working
		}
		return splitSnapshot{Indexed: false, EngineAlive: false} // engine died
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	o := runSplitIndexCore(ctx, cancel, func() {}, make(chan sseEvent), make(chan progress.Event, 4), probe, &fakeSplitClock{}, testPollCfg())
	if o.err == nil {
		t.Fatal("engine died but outcome carried no error (fake Done)")
	}
	if o.entities != 0 {
		t.Fatalf("failed outcome should carry no stats, got entities=%d", o.entities)
	}
}

// 4b. SPLIT + never completes while alive: bounded timeout → real error.
func TestSplit_TimeoutSurfacesError(t *testing.T) {
	probe := &fakeProbe{fn: func(int) splitSnapshot {
		return splitSnapshot{Indexed: false, EngineAlive: true} // alive forever, never done
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := splitPollConfig{interval: time.Second, timeout: 3 * time.Second}
	o := runSplitIndexCore(ctx, cancel, func() {}, make(chan sseEvent), make(chan progress.Event, 4), probe, &fakeSplitClock{}, cfg)
	if o.err == nil {
		t.Fatal("index never completed but no timeout error surfaced")
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
