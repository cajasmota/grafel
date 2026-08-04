// group_lock_scope_6060_test.go — issue #6060. State.Group used to hold the
// State-global s.mu across the whole cold-wake revive, so a revive of group A
// blocked every call to group B — head-of-line blocking across the fleet, on a
// server whose consumers are concurrent agents.
//
// Three properties are pinned here, and each one is what makes the NEXT one
// safe to have:
//
//   - TestGroup_ColdWakeDoesNotBlockOtherGroups — the revive really is outside
//     the State-global lock.
//   - TestGroup_ConcurrentRevivesMaterializeOnce — dropping that lock did not
//     let two goroutines rebuild the same group (the per-group gate).
//   - TestGroup_NoCallerObservesHalfRevivedGroup — nor let anyone SEE the group
//     before it was finished. This is the one that matters most: a half-revived
//     group is a wrong-results bug, not a crash, so it would not announce
//     itself.
//
// Each test documents the concrete edit that makes it fail; all three were
// confirmed to fail against that edit rather than assumed to.
package mcp

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

func TestGroup_ColdWakeDoesNotBlockOtherGroups(t *testing.T) {
	st := seedTwoGroups(t)

	// Both groups warm, then evict A only.
	if st.Group("a") == nil || st.Group("b") == nil {
		t.Fatal("warm-up returned nil group")
	}
	if !st.EvictGroup("a", true) {
		t.Fatal("EvictGroup(a) returned false")
	}

	parked := make(chan struct{})
	release := make(chan struct{})
	reviveMaterializeHook = func(name string) {
		if name != "a" {
			return
		}
		close(parked)
		<-release
	}
	t.Cleanup(func() { reviveMaterializeHook = nil })

	reviveDone := make(chan struct{})
	go func() {
		defer close(reviveDone)
		if st.Group("a") == nil {
			t.Error("revive of a returned nil")
		}
	}()

	select {
	case <-parked:
	case <-time.After(10 * time.Second):
		close(release)
		t.Fatal("revive of a never reached the materialization window")
	}

	// A is parked mid-revive. A call to the untouched, resident group b must not
	// wait on it.
	bDone := make(chan struct{})
	go func() {
		defer close(bDone)
		if st.Group("b") == nil {
			t.Error("Group(b) returned nil")
		}
	}()

	select {
	case <-bDone:
	case <-time.After(5 * time.Second):
		close(release)
		<-reviveDone
		t.Fatal("call to group b blocked behind group a's revive — State.Group is holding the State-global lock across the revive")
	}

	close(release)
	<-reviveDone
	if !st.groupResident("a") {
		t.Error("group a not resident after revive")
	}
}

// Singleflight pin. Concurrent revives of the SAME group must materialize the
// dropped heap exactly once and every caller must observe the same, fully
// materialized group — never a half-revived one. The per-group gate is what
// replaces "everything under one global lock" here.
func TestGroup_ConcurrentRevivesMaterializeOnce(t *testing.T) {
	doc := lazyTestDoc()
	st, _, _ := seedRepoOnDisk(t, doc)
	if st.Group("test") == nil {
		t.Fatal("warm Group returned nil")
	}
	if !st.EvictGroup("test", true) {
		t.Fatal("EvictGroup returned false")
	}

	var materializations atomic.Int64
	reviveMaterializeHook = func(string) {
		materializations.Add(1)
		// Widen the window so a missing gate is actually observed.
		time.Sleep(20 * time.Millisecond)
	}
	t.Cleanup(func() { reviveMaterializeHook = nil })

	const goroutines = 8
	var wg sync.WaitGroup
	got := make([]*LoadedGroup, goroutines)
	start := make(chan struct{})
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			got[i] = st.Group("test")
		}(i)
	}
	close(start)
	wg.Wait()

	if n := materializations.Load(); n != 1 {
		t.Errorf("revive materialized %d times under %d concurrent callers; want exactly 1", n, goroutines)
	}
	for i, g := range got {
		if g == nil {
			t.Fatalf("goroutine %d got a nil group", i)
		}
		// #6114: State.Group returns an immutable per-call VIEW, so the group
		// POINTER is expected to differ per caller and proves nothing. The
		// single-flight property is about the MATERIALIZED instance behind the
		// view, so compare the per-repo record — which the view shares by
		// pointer and which the revive is what creates.
		if g.Repos["r"] != got[0].Repos["r"] {
			t.Errorf("goroutine %d observed a different materialized repo instance — revive was not single-flighted", i)
		}
		// Fully materialized, not half: the LabelIndex dropped by the eviction is back.
		lr := g.Repos["r"]
		if lr == nil || lr.LabelIndex == nil {
			t.Fatalf("goroutine %d observed a half-revived group (nil LabelIndex)", i)
		}
	}
}

// Publication-ordering pin (#6060). Dropping s.mu for the materialization window
// is only safe because the group stays parked in s.evicted — invisible to
// lookups — until it is FULLY materialized. This test parks a revive at the top
// of that window and requires that a second caller arriving mid-window never
// observes the group in s.groups carrying the LabelIndex the eviction dropped.
//
// Proof the fixture can fail: publishing `s.groups[name] = cold` BEFORE the
// rematerializeFromReader loop (instead of after) makes the second caller return
// immediately with a nil LabelIndex and this test fails with "observed a
// half-revived group".
func TestGroup_NoCallerObservesHalfRevivedGroup(t *testing.T) {
	doc := lazyTestDoc()
	st, _, _ := seedRepoOnDisk(t, doc)
	if st.Group("test") == nil {
		t.Fatal("warm Group returned nil")
	}
	if !st.EvictGroup("test", true) {
		t.Fatal("EvictGroup returned false")
	}
	// Sanity: the eviction really did drop the heap the observer checks for, so a
	// half-revived observation is distinguishable from a complete one.
	st.mu.Lock()
	shell := st.evicted["test"]
	st.mu.Unlock()
	if shell == nil || shell.Repos["r"] == nil {
		t.Fatal("no cold shell after keepReader evict")
	}
	if shell.Repos["r"].LabelIndex != nil {
		t.Fatal("cold shell kept its LabelIndex — this fixture cannot detect a half-revive")
	}

	parked := make(chan struct{})
	release := make(chan struct{})
	reviveMaterializeHook = func(string) {
		close(parked)
		<-release
	}
	t.Cleanup(func() { reviveMaterializeHook = nil })

	reviveDone := make(chan struct{})
	go func() {
		defer close(reviveDone)
		_ = st.Group("test")
	}()
	select {
	case <-parked:
	case <-time.After(10 * time.Second):
		close(release)
		t.Fatal("revive never reached the materialization window")
	}

	// A second caller arriving mid-window. Either it blocks on the per-group gate
	// (correct) or it observes whatever is in s.groups (the regression).
	observed := make(chan bool, 1) // true == LabelIndex present
	go func() {
		g := st.Group("test")
		if g == nil {
			observed <- false
			return
		}
		lr := g.Repos["r"]
		lr.rmu().Lock()
		ok := lr.LabelIndex != nil
		lr.rmu().Unlock()
		observed <- ok
	}()

	select {
	case complete := <-observed:
		// Returned while the revive is still parked ⇒ it read s.groups mid-revive.
		close(release)
		<-reviveDone
		if !complete {
			t.Fatal("second caller observed a half-revived group (nil LabelIndex) published before materialization completed")
		}
		t.Fatal("second caller returned while the revive was still parked — the group was published before it was materialized")
	case <-time.After(500 * time.Millisecond):
		// Correctly blocked on the per-group gate.
	}

	close(release)
	<-reviveDone
	if complete := <-observed; !complete {
		t.Error("second caller observed a group with no LabelIndex after the revive completed")
	}
}

// seedTwoGroups builds a State with two independent single-repo groups ("a",
// "b"), each with a real graph.fb on disk, both resident.
func seedTwoGroups(t *testing.T) *State {
	t.Helper()
	reg := &Registry{Groups: map[string]RegistryGroup{}}
	type seeded struct {
		lr *LoadedRepo
	}
	seeds := map[string]seeded{}
	for _, name := range []string{"a", "b"} {
		repoDir := t.TempDir()
		stateDir := daemon.StateDirForRepo(repoDir)
		if err := os.MkdirAll(stateDir, 0o755); err != nil {
			t.Fatalf("mkdir state dir: %v", err)
		}
		fbPath := filepath.Join(stateDir, "graph.fb")
		doc := lazyTestDoc()
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
		reg.Groups[name] = RegistryGroup{Repos: map[string]RegistryRepo{"r": {Path: repoDir}}}
		seeds[name] = seeded{lr: &LoadedRepo{
			Repo:        "r",
			Path:        repoDir,
			GraphFile:   fbPath,
			Doc:         doc,
			LabelIndex:  BuildLabelIndex(doc),
			mtime:       fi.ModTime(),
			contentHash: hash,
		}}
	}
	st := NewState(reg)
	st.mu.Lock()
	for name, s := range seeds {
		st.groups[name] = &LoadedGroup{Name: name, Repos: map[string]*LoadedRepo{"r": s.lr}}
	}
	st.mu.Unlock()
	t.Cleanup(st.Close)
	return st
}
