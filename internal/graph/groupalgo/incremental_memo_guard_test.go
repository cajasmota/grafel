package groupalgo

// incremental_memo_guard_test.go — group-level in-memory compute-once guard.
//
// The disk overlay (input-hash keyed) is the FAST reuse path for the group-scope
// Pass-4 sweep, but it only helps when the overlay can be PERSISTED. If
// WriteOverlayFromResult fails (read-only ~/.grafel/groups, disk-full, EPERM) or
// the overlay is otherwise absent, RunGroupAlgorithmsIncremental had nothing to
// fall back on and re-ran the full RunAlgorithms (Louvain + PageRank + O(V·E)
// betweenness over the whole ~32k-node group union) on EVERY trigger — the
// group-scope analog of the per-repo #50 compute→evict CPU spin.
//
// These tests pin the fix: an in-memory guard keyed on the group-version
// (community input hash) makes the heavy pass run at most ONCE per version in a
// process, regardless of whether the overlay ever reached disk. A genuine
// structural change bumps the hash and triggers exactly one recompute.
//
// The group is modeled as ≥2 repos (setupIncrGroup: svc + web) so the combined
// union path is exercised, matching the real corpus symptom.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// TestIncremental_MemoGuard_ComputesOncePerVersion proves the guard: with the
// overlay never persisted (simulating a persist failure — the sidecar can't be
// written), repeated group-graph loads must reuse the first in-process compute
// instead of re-running the full sweep each time.
func TestIncremental_MemoGuard_ComputesOncePerVersion(t *testing.T) {
	group, _, _, _ := setupIncrGroup(t)

	first, err := RunGroupAlgorithmsIncremental(group)
	if err != nil {
		t.Fatalf("first incremental run: %v", err)
	}
	if first.Skipped {
		t.Fatal("first run must NOT skip (nothing computed yet)")
	}

	// Deliberately do NOT WriteOverlayFromResult — models a persist failure /
	// absent overlay. Every subsequent load hits the same graph.fb version.
	for i := 0; i < 3; i++ {
		got, err := RunGroupAlgorithmsIncremental(group)
		if err != nil {
			t.Fatalf("reload %d: %v", i, err)
		}
		if !got.Skipped {
			t.Fatalf("reload %d recomputed the full group Pass-4 (Skipped=false) — the compute-once-per-version guard is missing; this is the ~32k-node betweenness CPU spin", i)
		}
		if got.InputHash != first.InputHash {
			t.Fatalf("reload %d input hash drifted: %s != %s", i, got.InputHash, first.InputHash)
		}
	}

	// Parity: the reused result equals a from-scratch full recompute.
	full, err := RunGroupAlgorithms(group)
	if err != nil {
		t.Fatalf("full reference: %v", err)
	}
	reused, err := RunGroupAlgorithmsIncremental(group)
	if err != nil {
		t.Fatalf("reused run: %v", err)
	}
	resultsEqual(t, full.Results, reused.Results)
}

// TestIncremental_MemoGuard_PersistFailureDoesNotRecomputeForever mirrors the
// per-repo TestSchedulePendingAlgo_PersistFailureDoesNotRecomputeForever at
// group scope: when the overlay directory is read-only so WriteOverlayFromResult
// genuinely fails, the next incremental run must still be served from the
// in-memory guard (Skipped=true), never a fresh full sweep.
func TestIncremental_MemoGuard_PersistFailureDoesNotRecomputeForever(t *testing.T) {
	group, _, _, _ := setupIncrGroup(t)

	first, err := RunGroupAlgorithmsIncremental(group)
	if err != nil {
		t.Fatalf("first incremental run: %v", err)
	}
	if first.Skipped {
		t.Fatal("first run must NOT skip")
	}

	// Force the overlay write to fail: make the groups/ directory read-only so
	// the temp-file create inside WriteOverlayTo returns EPERM (best-effort,
	// swallowed by the caller in prod).
	path, perr := OverlayPath(group)
	if perr != nil {
		t.Fatalf("overlay path: %v", perr)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir groups: %v", err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod groups ro: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	if werr := WriteOverlayFromResult(first); werr == nil {
		t.Skip("overlay write unexpectedly succeeded on a read-only dir (running as root?) — cannot exercise the persist-failure path")
	}
	// Precondition: no overlay on disk, so the disk-skip path cannot engage.
	if _, ok := ReadOverlay(path, nil); ok {
		t.Fatal("test precondition broken: overlay present despite failed write")
	}

	got, err := RunGroupAlgorithmsIncremental(group)
	if err != nil {
		t.Fatalf("post-persist-failure run: %v", err)
	}
	if !got.Skipped {
		t.Fatal("full group Pass-4 recomputed after a persist failure — the in-memory guard does NOT bound the recompute (CPU spin)")
	}
}

// TestIncremental_MemoGuard_RecomputesOnVersionChange proves correctness is
// preserved: a structural change (new tightly-connected cluster) bumps the
// group-version hash, so the guard misses and exactly one recompute runs; the
// next load for the NEW version is then reused.
func TestIncremental_MemoGuard_RecomputesOnVersionChange(t *testing.T) {
	group, pathA, _, serviceID := setupIncrGroup(t)

	first, err := RunGroupAlgorithmsIncremental(group)
	if err != nil {
		t.Fatalf("first incremental run: %v", err)
	}

	// Structural change in repo-A (new cluster + edge into Service) — a real
	// re-index. This bumps graph.fb and, crucially, the community input hash.
	repoA, _, _ := fixtureGraphs()
	mkEnt := func(name string) graph.Entity {
		return graph.Entity{ID: "svc:" + name, Name: name, Kind: "function", SourceFile: "svc/" + name + ".go", Language: "go"}
	}
	mkRel := func(from, to string) graph.Relationship {
		return graph.Relationship{ID: from + "->" + to, FromID: from, ToID: to, Kind: "CALLS"}
	}
	cluster := []string{"g1", "g2", "g3", "g4", "g5", "g6"}
	for _, n := range cluster {
		repoA.Entities = append(repoA.Entities, mkEnt(n))
	}
	for i := 0; i < len(cluster); i++ {
		for j := i + 1; j < len(cluster); j++ {
			repoA.Relationships = append(repoA.Relationships, mkRel("svc:"+cluster[i], "svc:"+cluster[j]))
		}
	}
	repoA.Relationships = append(repoA.Relationships, mkRel("svc:g1", serviceID))
	writeFixtureRepo(t, "svc", pathA, repoA)

	changed, err := RunGroupAlgorithmsIncremental(group)
	if err != nil {
		t.Fatalf("post-change run: %v", err)
	}
	if changed.Skipped {
		t.Fatal("structural change must NOT be skipped (correctness: a re-index must recompute)")
	}
	if changed.InputHash == first.InputHash {
		t.Fatal("input hash unchanged after a structural re-index")
	}

	// The NEW version is now memoized: a subsequent load reuses it.
	reused, err := RunGroupAlgorithmsIncremental(group)
	if err != nil {
		t.Fatalf("reused post-change run: %v", err)
	}
	if !reused.Skipped {
		t.Fatal("second load of the new version recomputed instead of reusing the guard")
	}
	if reused.InputHash != changed.InputHash {
		t.Fatalf("reused hash %s != changed hash %s", reused.InputHash, changed.InputHash)
	}
}
