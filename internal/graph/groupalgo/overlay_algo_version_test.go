package groupalgo

import (
	"testing"
)

// ALGORITHM-VERSION INVALIDATION.
//
// Every skip decision in this package is justified by DETERMINISM: "the same
// input hash implies a recompute would reproduce this overlay byte-for-byte."
// That premise holds only while the algorithm is fixed. Replacing the community
// detector — a different but equally good partition — silently breaks it: the
// union is unchanged, so the input hash matches, so the memo hits, so grafel
// keeps serving the OLD partition forever. The user upgrades and gets none of
// the benefit, and the stored overlay disagrees with what the running code
// would produce.
//
// So the skip/apply decision is gated on an explicit OverlayAlgoVersion in
// addition to the input hash. These tests pin all three decision points.

// The version must be stamped on every overlay this build writes, or the gates
// below would immediately invalidate everything they read.
func TestBuildOverlay_StampsAlgoVersion(t *testing.T) {
	ov := BuildOverlay(sampleResult())
	if ov == nil {
		t.Fatal("BuildOverlay returned nil for a non-nil result")
	}
	if ov.AlgoVersion != OverlayAlgoVersion {
		t.Fatalf("AlgoVersion = %d, want %d (every overlay this build writes must carry the current version)", ov.AlgoVersion, OverlayAlgoVersion)
	}
}

// A legacy overlay (written before the field existed, so version 0) must not be
// APPLIED — applying it would mix a stale partition into a build that would
// compute a different one.
func TestReadOverlay_RejectsForeignAlgoVersion(t *testing.T) {
	group, _, _, _ := setupIncrGroup(t)
	writeFreshOverlay(t, group)
	path, err := OverlayPath(group)
	if err != nil {
		t.Fatalf("OverlayPath: %v", err)
	}
	cur, err := CurrentSourceMtimes(group)
	if err != nil {
		t.Fatalf("CurrentSourceMtimes: %v", err)
	}
	if _, ok := ReadOverlay(path, cur); !ok {
		t.Fatal("setup: a freshly written overlay must be readable")
	}

	for _, stored := range []int{0, OverlayAlgoVersion - 1, OverlayAlgoVersion + 1} {
		ov := readOverlayUnconditional(path)
		if ov == nil {
			t.Fatal("readOverlayUnconditional returned nil for a present overlay")
		}
		ov.AlgoVersion = stored
		if err := WriteOverlayTo(path, ov); err != nil {
			t.Fatalf("WriteOverlayTo: %v", err)
		}
		if _, ok := ReadOverlay(path, cur); ok {
			t.Fatalf("an overlay stamped algo_version=%d must NOT be applied by a build at version %d (downgrade must skip and recompute, never mix partitions)", stored, OverlayAlgoVersion)
		}
	}
}

// The sweep predicate must force a recompute after a version change even when
// the mtimes and the input hash are both perfectly current — that is exactly
// the upgrade case, where nothing about the graph moved.
func TestOverlayNeedsRecompute_ForcedByAlgoVersionChange(t *testing.T) {
	group, _, _, _ := setupIncrGroup(t)
	writeFreshOverlay(t, group)
	path, err := OverlayPath(group)
	if err != nil {
		t.Fatalf("OverlayPath: %v", err)
	}

	if OverlayNeedsRecompute(group) {
		t.Fatal("setup: a freshly computed overlay must not need recompute")
	}

	ov := readOverlayUnconditional(path)
	ov.AlgoVersion = OverlayAlgoVersion - 1
	if err := WriteOverlayTo(path, ov); err != nil {
		t.Fatalf("WriteOverlayTo: %v", err)
	}
	if !OverlayNeedsRecompute(group) {
		t.Fatal("an overlay from a different algorithm version must force a recompute even with matching mtimes and input hash — otherwise an upgrade silently keeps serving the old partition forever")
	}
}

// The incremental entrypoint's disk-overlay skip is the memo the coordinator
// flagged: same union, same hash, different algorithm. It must NOT skip.
func TestRunGroupAlgorithmsIncremental_DoesNotSkipAcrossAlgoVersions(t *testing.T) {
	group, _, _, _ := setupIncrGroup(t)
	writeFreshOverlay(t, group)
	path, err := OverlayPath(group)
	if err != nil {
		t.Fatalf("OverlayPath: %v", err)
	}

	// Same-version overlay: the skip is expected (this is the property the
	// version gate must not break).
	resetGroupAlgoMemo()
	same, err := RunGroupAlgorithmsIncrementalOneShot(group)
	if err != nil {
		t.Fatalf("RunGroupAlgorithmsIncrementalOneShot: %v", err)
	}
	if !same.Skipped {
		t.Fatal("an unchanged union with a current-version overlay must still take the cheap skip path")
	}

	// Foreign-version overlay: the skip must NOT fire.
	ov := readOverlayUnconditional(path)
	ov.AlgoVersion = OverlayAlgoVersion - 1
	if err := WriteOverlayTo(path, ov); err != nil {
		t.Fatalf("WriteOverlayTo: %v", err)
	}
	resetGroupAlgoMemo()
	changed, err := RunGroupAlgorithmsIncrementalOneShot(group)
	if err != nil {
		t.Fatalf("RunGroupAlgorithmsIncrementalOneShot: %v", err)
	}
	if changed.Skipped {
		t.Fatal("the input-hash memo must not hit across an algorithm-version change — that is how an upgrade ends up serving the old gonum partition indefinitely")
	}
	// And the recompute must re-stamp the CURRENT version, so the next run can
	// legitimately skip again.
	if err := WriteOverlayFromResult(changed); err != nil {
		t.Fatalf("WriteOverlayFromResult: %v", err)
	}
	if got := readOverlayUnconditional(path).AlgoVersion; got != OverlayAlgoVersion {
		t.Fatalf("recomputed overlay stamped algo_version=%d, want %d", got, OverlayAlgoVersion)
	}
}

// Drift guard. OverlayAlgoVersion is the ONLY thing standing between an
// algorithm change and an indefinitely stale partition, and it is invisible at
// the call site that would need to bump it (internal/graph's Louvain/PageRank
// implementation). This test exists to fail loudly on the value, so that
// changing the partitioning without bumping the version cannot pass review
// silently: whoever changes the algorithm has to come here, read why, and
// update both numbers together.
func TestOverlayAlgoVersion_MustBeBumpedDeliberately(t *testing.T) {
	// EXPECTED VALUE — bump this IN THE SAME COMMIT as OverlayAlgoVersion, and
	// only when the change alters the partition/ranking a pass produces.
	const expected = 1
	if OverlayAlgoVersion != expected {
		t.Fatalf("OverlayAlgoVersion = %d but this test expects %d.\n"+
			"If you changed the community/centrality algorithm, bump BOTH — every stored overlay is then invalidated and recomputed.\n"+
			"If you did not, revert the constant: lowering or reusing a version makes upgraded daemons keep serving a partition the current code would not produce.",
			OverlayAlgoVersion, expected)
	}
	if OverlayAlgoVersion <= 0 {
		t.Fatal("OverlayAlgoVersion must be >= 1: 0 is reserved for pre-versioning overlays, which are always invalidated")
	}
}
