// idle_group_evict_5872_pr2_test.go — tests for the idle-group LRU eviction
// POLICY (memory epic #5850, issue #5872 PR2): State.SweepIdleGroups (the
// production wiring of the EvictGroup primitive), its active-group PIN, the
// keepReader knob, the disabled=no-op contract, and its composition with the
// BM25 idle sweep. Companion to evict_group_5872_test.go (the PR1 primitive).
package mcp

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// seedGroupsOnDisk writes one repo per named group to disk (graph.fb under the
// repo's daemon state dir, mtime/hash primed so reload is a no-op skip) and
// returns a State with every group resident. Mirrors seedRepoOnDisk for the
// multi-group case so an evict-then-revive round-trip can reconstruct from disk.
func seedGroupsOnDisk(t *testing.T, docs map[string]*graph.Document) *State {
	t.Helper()
	regGroups := map[string]RegistryGroup{}
	loaded := map[string]*LoadedGroup{}
	for gname, doc := range docs {
		repoDir := t.TempDir()
		stateDir := daemon.StateDirForRepo(repoDir)
		if err := os.MkdirAll(stateDir, 0o755); err != nil {
			t.Fatalf("mkdir state dir: %v", err)
		}
		fbPath := filepath.Join(stateDir, "graph.fb")
		if err := fbwriter.WriteAtomic(fbPath, doc); err != nil {
			t.Fatalf("write fb: %v", err)
		}
		fi, err := os.Stat(fbPath)
		if err != nil {
			t.Fatalf("stat fb: %v", err)
		}
		hash, err := hashGraphFile(fbPath)
		if err != nil {
			t.Fatalf("hash fb: %v", err)
		}
		regGroups[gname] = RegistryGroup{Repos: map[string]RegistryRepo{"r": {Path: repoDir}}}
		lr := &LoadedRepo{
			Repo:        "r",
			Path:        repoDir,
			GraphFile:   fbPath,
			Doc:         doc,
			LabelIndex:  BuildLabelIndex(doc),
			mtime:       fi.ModTime(),
			contentHash: hash,
		}
		loaded[gname] = &LoadedGroup{Name: gname, Repos: map[string]*LoadedRepo{"r": lr}}
	}
	st := NewState(&Registry{Groups: regGroups})
	st.mu.Lock()
	for gname, lg := range loaded {
		st.groups[gname] = lg
	}
	st.mu.Unlock()
	t.Cleanup(st.Close)
	return st
}

// setLastAccess stamps a group's lastAccess under s.mu (the same lock the sweep
// reads it under) so tests never race a torn timestamp.
func setLastAccess(st *State, name string, at time.Time) {
	st.mu.Lock()
	st.groups[name].lastAccess = at
	st.mu.Unlock()
}

// Criterion 1: a group idle past the window IS evicted by SweepIdleGroups; a
// group accessed within the window is NOT.
func TestSweepIdleGroups_EvictsIdleKeepsRecent(t *testing.T) {
	st := seedGroupsOnDisk(t, map[string]*graph.Document{
		"active": lazyTestDoc(),
		"idle":   lazyTestDoc(),
	})
	window := 5 * time.Minute
	now := time.Now()
	// active: touched 1min ago (within window) AND the most recent → doubly safe.
	setLastAccess(st, "active", now.Add(-1*time.Minute))
	// idle: touched 10min ago (past window) and not the most recent → evictable.
	setLastAccess(st, "idle", now.Add(-10*time.Minute))

	n := st.SweepIdleGroups(window, true)
	if n != 1 {
		t.Fatalf("expected exactly 1 group evicted, got %d", n)
	}
	if !st.groupResident("active") {
		t.Error("within-window group was evicted — must be kept")
	}
	if st.groupResident("idle") {
		t.Error("past-window group was NOT evicted")
	}
	if !st.gated("idle") {
		t.Error("evicted group not recorded in the cold-gate")
	}
	// Revive round-trip: an explicit access brings the idle group back.
	if grp := st.Group("idle"); grp == nil {
		t.Fatal("evicted group did not revive on access")
	}
}

// Criterion 2 (the CRUX): the active group — the max-lastAccess group the fleet
// is servicing RIGHT NOW — is NEVER evicted even when it is itself past the idle
// window and the sweep runs. Proven both at the SweepIdleGroups level and end to
// end through the real reloadBeforeCall hook.
func TestSweepIdleGroups_PinsActiveGroupPastWindow(t *testing.T) {
	t.Run("direct sweep pins the max-lastAccess group", func(t *testing.T) {
		st := seedGroupsOnDisk(t, map[string]*graph.Document{
			"A": lazyTestDoc(),
			"B": lazyTestDoc(),
		})
		window := 5 * time.Minute
		now := time.Now()
		// A is the ACTIVE group (most recent access) but is itself PAST the window.
		setLastAccess(st, "A", now.Add(-8*time.Minute))
		// B is even older — a genuinely idle group.
		setLastAccess(st, "B", now.Add(-20*time.Minute))

		n := st.SweepIdleGroups(window, true)
		if n != 1 {
			t.Fatalf("expected exactly 1 eviction (B), got %d", n)
		}
		if !st.groupResident("A") {
			t.Fatal("PIN VIOLATED: the active (max-lastAccess) group A was evicted")
		}
		if st.groupResident("B") {
			t.Error("idle group B was not evicted")
		}
	})

	t.Run("reloadBeforeCall→sweep never evicts the just-routed group", func(t *testing.T) {
		st := seedGroupsOnDisk(t, map[string]*graph.Document{
			"A": lazyTestDoc(),
			"B": lazyTestDoc(),
		})
		srv := &Server{
			State:             st,
			Tel:               NewTelemetry(0),
			groupIdleEviction: 5 * time.Minute,
			groupKeepReader:   true,
			reloadDebounce:    0, // force the slow path (and thus the sweep) every call
		}
		now := time.Now()
		// Simulate: B was queried a while ago, then A was queried last (A is active)
		// — both stamps are now past the window (the session paused between calls).
		setLastAccess(st, "B", now.Add(-30*time.Minute))
		setLastAccess(st, "A", now.Add(-8*time.Minute))

		// A new tool call for A fires reloadBeforeCall, which runs the sweep BEFORE
		// the handler re-routes to A. A is the active group and must survive.
		srv.reloadBeforeCall()

		if !st.groupResident("A") {
			t.Fatal("PIN VIOLATED via reloadBeforeCall: active group A evicted mid-call")
		}
		if st.groupResident("B") {
			t.Error("idle group B was not evicted by the wired sweep")
		}
	})
}

// Criterion 3: the keepReader knob is honored — true keeps the mmap Reader mapped
// on a cold shell (no munmap), false fully munmaps exactly once.
func TestSweepIdleGroups_KeepReaderKnob(t *testing.T) {
	// build makes a 2-group State: target (idle, evictable) holds a counting
	// closer; pin (more recent) keeps target from being the max-lastAccess pin.
	build := func(cc readerCloser) *State {
		st := NewState(&Registry{Groups: map[string]RegistryGroup{
			"target": {Repos: map[string]RegistryRepo{"r": {}}},
			"pin":    {Repos: map[string]RegistryRepo{"r": {}}},
		}})
		lr := &LoadedRepo{Repo: "r"}
		st.mu.Lock()
		st.groups["target"] = &LoadedGroup{Name: "target", Repos: map[string]*LoadedRepo{"r": lr}}
		st.groups["pin"] = &LoadedGroup{Name: "pin", Repos: map[string]*LoadedRepo{"r": {Repo: "r"}}}
		lr.publishHandle(&MapHandle{closer: cc})
		st.mu.Unlock()
		now := time.Now()
		setLastAccess(st, "target", now.Add(-30*time.Minute)) // idle, non-pin
		setLastAccess(st, "pin", now.Add(-6*time.Minute))     // more recent → pin
		return st
	}

	t.Run("keepReader=true keeps it mapped", func(t *testing.T) {
		cc := &countingCloser{}
		st := build(cc)
		if n := st.SweepIdleGroups(5*time.Minute, true); n != 1 {
			t.Fatalf("expected 1 eviction, got %d", n)
		}
		if got := cc.n.Load(); got != 0 {
			t.Fatalf("keepReader=true munmapped the reader: count=%d, want 0", got)
		}
		st.mu.Lock()
		shell := st.evicted["target"]
		st.mu.Unlock()
		if shell == nil || shell.Repos["r"] == nil || shell.Repos["r"].handle == nil {
			t.Fatal("keepReader=true did not retain a cold shell with the handle")
		}
	})

	t.Run("keepReader=false munmaps once", func(t *testing.T) {
		cc := &countingCloser{}
		st := build(cc)
		if n := st.SweepIdleGroups(5*time.Minute, false); n != 1 {
			t.Fatalf("expected 1 eviction, got %d", n)
		}
		if got := cc.n.Load(); got != 1 {
			t.Fatalf("keepReader=false munmap count = %d, want 1", got)
		}
		st.mu.Lock()
		shell, gated := st.evicted["target"]
		st.mu.Unlock()
		if !gated {
			t.Fatal("full evict did not record the cold-gate")
		}
		if shell != nil {
			t.Fatal("full evict retained a shell; want nil (fully cold)")
		}
	})
}

// Criterion 4: disabled (idle <= 0) → SweepIdleGroups no-ops, zero evictions,
// behaviour 100% unchanged. Also covers the resolver defaults (unset env = OFF).
func TestSweepIdleGroups_DisabledIsNoOp(t *testing.T) {
	st := seedGroupsOnDisk(t, map[string]*graph.Document{
		"a": lazyTestDoc(),
		"b": lazyTestDoc(),
	})
	// Both groups are ancient — under any positive window they would evict.
	old := time.Now().Add(-24 * time.Hour)
	setLastAccess(st, "a", old)
	setLastAccess(st, "b", old)

	if n := st.SweepIdleGroups(0, true); n != 0 {
		t.Fatalf("idle=0 must evict nothing, got %d", n)
	}
	if n := st.SweepIdleGroups(-time.Minute, true); n != 0 {
		t.Fatalf("negative idle must evict nothing, got %d", n)
	}
	if !st.groupResident("a") || !st.groupResident("b") {
		t.Fatal("disabled sweep changed residency — must be byte-identical no-op")
	}

	// Resolver contract: unset env → OFF (0); malformed → OFF; positive-but-tiny →
	// clamped up to the BM25 floor; keepReader default ON.
	t.Setenv("GRAFEL_MCP_GROUP_IDLE_MS", "")
	if got := resolveGroupIdleEviction(); got != 0 {
		t.Errorf("unset GRAFEL_MCP_GROUP_IDLE_MS: want 0 (OFF), got %v", got)
	}
	t.Setenv("GRAFEL_MCP_GROUP_IDLE_MS", "garbage")
	if got := resolveGroupIdleEviction(); got != 0 {
		t.Errorf("malformed window: want 0 (OFF), got %v", got)
	}
	t.Setenv("GRAFEL_MCP_GROUP_IDLE_MS", "1000") // below the ~5min BM25 floor
	floor := time.Duration(defaultBM25IdleEvictionMS) * time.Millisecond
	if got := resolveGroupIdleEviction(); got != floor {
		t.Errorf("sub-floor window not clamped: want %v, got %v", floor, got)
	}
	t.Setenv("GRAFEL_MCP_GROUP_KEEP_READER", "")
	if !resolveGroupKeepReader() {
		t.Error("keepReader default should be ON")
	}
	t.Setenv("GRAFEL_MCP_GROUP_KEEP_READER", "false")
	if resolveGroupKeepReader() {
		t.Error("GRAFEL_MCP_GROUP_KEEP_READER=false should disable keepReader")
	}
}

// Criterion 5 (-race): a query to group A concurrent with a sweep that evicts
// idle group B — no fault, A unaffected. Run under `go test -race`.
func TestSweepIdleGroups_ConcurrentQueryDuringSweep(t *testing.T) {
	st := seedGroupsOnDisk(t, map[string]*graph.Document{
		"A": lazyTestDoc(),
		"B": lazyTestDoc(),
	})
	window := 5 * time.Minute
	// B starts idle-past-window; A is queried continuously below (so it is always
	// the freshly-stamped max-lastAccess pin).
	setLastAccess(st, "B", time.Now().Add(-30*time.Minute))

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Continuous queries to A (each State.Group stamps A's lastAccess).
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				if grp := st.Group("A"); grp == nil {
					t.Error("query to active group A returned nil during sweep")
					return
				}
			}
		}
	}()

	// Repeated sweeps racing the queries.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 2000; i++ {
			st.SweepIdleGroups(window, true)
		}
		close(stop)
	}()

	wg.Wait()
	if !st.groupResident("A") {
		t.Fatal("active group A was evicted by a concurrent sweep")
	}
}

// Criterion 6: a long-idle group that ALREADY lost its BM25 (short-window sweep)
// evicts cleanly at the whole-group window — the two idle sweeps compose.
func TestSweepIdleGroups_ComposesWithBM25Eviction(t *testing.T) {
	st := seedGroupsOnDisk(t, map[string]*graph.Document{
		"active": lazyTestDoc(),
		"idle":   lazyTestDoc(),
	})
	now := time.Now()
	setLastAccess(st, "active", now.Add(-1*time.Minute))
	setLastAccess(st, "idle", now.Add(-20*time.Minute))

	// Warm then BM25-evict the idle group first (the short 5-min window), so the
	// whole-group sweep later meets a group whose BM25 heap is already gone.
	st.mu.Lock()
	idleRepo := st.groups["idle"].Repos["r"]
	st.mu.Unlock()
	_ = idleRepo.getBM25() // build it
	idleRepo.bm25LastUse = now.Add(-20 * time.Minute)
	if evicted := st.SweepIdleBM25(5 * time.Minute); evicted == 0 {
		t.Fatal("precondition: BM25 idle sweep evicted nothing")
	}
	if idleRepo.BM25 != nil {
		t.Fatal("precondition: idle repo's BM25 was not dropped")
	}

	// Now the whole-group sweep at the longer window must evict the idle group
	// cleanly despite its BM25 already being nil.
	if n := st.SweepIdleGroups(10*time.Minute, true); n != 1 {
		t.Fatalf("expected the BM25-already-evicted idle group to evict cleanly, got %d", n)
	}
	if st.groupResident("idle") {
		t.Error("BM25-already-evicted idle group was not whole-group evicted")
	}
	if !st.groupResident("active") {
		t.Error("active group must survive the composed sweep")
	}
	// Revive still reconstructs a working search index from disk.
	grp := st.Group("idle")
	if grp == nil {
		t.Fatal("composed-evicted group did not revive")
	}
	if hits := grp.Repos["r"].getBM25().Search("Proc", 10); len(hits) == 0 {
		t.Error("revived group's BM25 search returned no hits — index not rebuilt")
	}
}
