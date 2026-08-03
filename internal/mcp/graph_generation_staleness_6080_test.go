// graph_generation_staleness_6080_test.go — issue #6080.
//
// #6060 made the reload gate re-run discovery when the repo was being served
// from a NON-current REF. That is correct at ref granularity and blind one level
// down: a plain same-branch reindex publishes graph.<gen+1>.fb, flips the
// `current` pointer, and — by design (graph.GCStaleGens keeps the current gen
// AND the one before it) — LEAVES graph.<gen>.fb on disk. The pinned lr.GraphFile
// still stats fine with an unchanged mtime, so the fast path is taken and the
// daemon keeps answering from generation N-1 with a success response.
//
// This is the ordinary edit-and-reindex event, and there is no git activity at
// all between the two reloads below: HEAD, refs/heads/main and the ref state
// directory are byte-for-byte identical. No gitmeta-keyed gate of ANY
// granularity could observe this — which is also the evidence that #6080 and
// #6079 are independent defects rather than one root cause.
package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// genDocWithMarker returns a document whose only entity carries the given id, so
// "which generation is being served" is answerable from the resident read path
// rather than from a path string.
func genDocWithMarker(marker string) *graph.Document {
	pr := 0.5
	return &graph.Document{
		Entities: []graph.Entity{
			{ID: marker, Name: marker, Kind: "Function", SourceFile: marker + ".go", StartLine: 1, PageRank: &pr},
		},
	}
}

// headStateFingerprint captures everything a git-derived cache key could
// possibly observe about the repo's HEAD: the HEAD pointer's bytes, the branch
// tip's bytes, and the resolved per-ref state directory. Used to PROVE the
// reproduction below involves no git movement whatsoever.
func headStateFingerprint(t *testing.T, repoDir string) string {
	t.Helper()
	head, err := os.ReadFile(filepath.Join(repoDir, ".git", "HEAD"))
	if err != nil {
		t.Fatalf("read HEAD: %v", err)
	}
	tip, err := os.ReadFile(filepath.Join(repoDir, ".git", "refs", "heads", "main"))
	if err != nil {
		t.Fatalf("read refs/heads/main: %v", err)
	}
	return string(head) + "\x00" + string(tip) + "\x00" + daemon.StateDirForRepo(repoDir)
}

// TestReload_PicksUpNewGenerationOnSameBranchReindex is the #6080 regression.
//
// It drives a REAL same-branch reindex (fbwriter.WriteGraphGen — the production
// publish path: write graph.<gen>.fb, flip `current`, GC older gens) through the
// REAL reload gate, and asserts the served graph is the new generation.
//
// Non-vacuity is asserted explicitly, because the two ways this test could
// silently prove nothing are (a) the generation never actually advancing and
// (b) the ref moving, which the #6060 gate would already have caught:
//   - the generation genuinely advanced (`current` names a different file);
//   - generation N-1 is genuinely still on disk (otherwise the old fast-path
//     stat would have failed and masked the defect);
//   - the ref directory genuinely did NOT change;
//   - HEAD and refs/heads/main are byte-identical across the reindex.
func TestReload_PicksUpNewGenerationOnSameBranchReindex(t *testing.T) {
	root := t.TempDir()
	t.Setenv(daemon.EnvRoot, root)
	// #6101: never let this test read the developer's real state directory.
	t.Setenv("GRAFEL_HOME", t.TempDir())

	repoDir := gitRepoForDiscovery(t)
	mainDir := daemon.StateDirForRepoRef(repoDir, "main")
	if got := daemon.StateDirForRepo(repoDir); got != mainDir {
		t.Fatalf("fixture degenerate: HEAD ref dir %s != refs/main dir %s", got, mainDir)
	}
	fpBefore := headStateFingerprint(t, repoDir)

	// Generation 1 — the repo as first indexed.
	gen1Path, err := fbwriter.WriteGraphGen(mainDir, genDocWithMarker("gen1only"))
	if err != nil {
		t.Fatalf("publish gen 1: %v", err)
	}

	reg := &Registry{Groups: map[string]RegistryGroup{
		"test": {Repos: map[string]RegistryRepo{"r": {Path: repoDir}}},
	}}
	st := NewState(reg)
	t.Cleanup(st.Close)
	if _, _, err := st.reloadLocked(); err != nil {
		t.Fatalf("initial reload: %v", err)
	}

	lr := st.Group("test").Repos["r"]
	if lr == nil {
		t.Fatal("repo not loaded")
	}
	if lr.GraphFile != gen1Path {
		t.Fatalf("initial discovery resolved %s, want %s", lr.GraphFile, gen1Path)
	}
	if _, ok := lr.getByIDOne("gen1only"); !ok {
		t.Fatal("fixture degenerate: generation 1's entity is not served after the initial reload")
	}

	// ---- A plain same-branch reindex. No git activity of any kind. ----
	gen2Path, err := fbwriter.WriteGraphGen(mainDir, genDocWithMarker("gen2only"))
	if err != nil {
		t.Fatalf("publish gen 2: %v", err)
	}

	// Non-vacuity guard 1: the generation genuinely advanced.
	if gen2Path == gen1Path {
		t.Fatalf("vacuous: reindex reused the same generation file %s", gen1Path)
	}
	if got := graph.CurrentGraphPath(mainDir); got != gen2Path {
		t.Fatalf("vacuous: `current` resolves to %s, want the new generation %s", got, gen2Path)
	}
	// Non-vacuity guard 2: generation N-1 is RETAINED (graph.GCStaleGens keeps
	// current and current-1). If it had been GC'd, the old fast path's os.Stat
	// would fail and fall through to discovery on its own — the defect would be
	// invisible and this test would pass for the wrong reason.
	if _, err := os.Stat(gen1Path); err != nil {
		t.Fatalf("vacuous: generation N-1 (%s) was GC'd, so the stale stat could not have succeeded: %v", gen1Path, err)
	}
	// Non-vacuity guard 3: the REF did not move. This is what makes the defect
	// distinct from #6060/#6078 rather than a re-run of them.
	if got := daemon.StateDirForRepo(repoDir); got != mainDir {
		t.Fatalf("vacuous: ref dir moved to %s — the #6060 gate would catch that already", got)
	}
	if fpAfter := headStateFingerprint(t, repoDir); fpAfter != fpBefore {
		t.Fatalf("vacuous: git HEAD state changed across the reindex:\n before=%q\n after =%q", fpBefore, fpAfter)
	}

	// ---- The reload the daemon performs on its next poll. ----
	if _, _, err := st.reloadLocked(); err != nil {
		t.Fatalf("reload after same-branch reindex: %v", err)
	}

	if lr.GraphFile != gen2Path {
		t.Errorf("reload kept the superseded generation pinned:\n  lr.GraphFile: %s\n  want:         %s",
			lr.GraphFile, gen2Path)
	}
	if _, ok := lr.getByIDOne("gen2only"); !ok {
		t.Errorf("daemon is serving generation N-1: the new generation's entity %q is not visible\n"+
			"after a same-branch reindex — a query for it returns 'not found' with a success response", "gen2only")
	}
	if _, ok := lr.getByIDOne("gen1only"); ok {
		t.Errorf("daemon is serving generation N-1: the superseded generation's entity %q is still visible", "gen1only")
	}
}

// TestReload_SameBranchReindexIsInvisibleToGitState is the independence
// evidence for the "#6080 and #6079 are one defect" hypothesis, stated as an
// executable assertion rather than an argument: across a full publish of a new
// generation, EVERY git-observable input to gitmeta's cache key is unchanged.
//
// gitmeta.CaptureCached (#6079) keys on the HEAD pointer; a hypothetical
// per-commit-correct key would additionally observe refs/heads/<branch>. Neither
// moves here. Widening the gitmeta key — the #6079 fix — therefore cannot
// detect a generation flip, and the two issues are independent.
func TestReload_SameBranchReindexIsInvisibleToGitState(t *testing.T) {
	root := t.TempDir()
	t.Setenv(daemon.EnvRoot, root)
	t.Setenv("GRAFEL_HOME", t.TempDir())

	repoDir := gitRepoForDiscovery(t)
	mainDir := daemon.StateDirForRepoRef(repoDir, "main")

	stat := func(rel string) (int64, int64) {
		t.Helper()
		fi, err := os.Stat(filepath.Join(repoDir, ".git", rel))
		if err != nil {
			t.Fatalf("stat .git/%s: %v", rel, err)
		}
		return fi.ModTime().UnixNano(), fi.Size()
	}

	gen1Path, err := fbwriter.WriteGraphGen(mainDir, genDocWithMarker("g1"))
	if err != nil {
		t.Fatalf("publish gen 1: %v", err)
	}
	headMt, headSz := stat("HEAD")
	tipMt, tipSz := stat(filepath.Join("refs", "heads", "main"))

	gen2Path, err := fbwriter.WriteGraphGen(mainDir, genDocWithMarker("g2"))
	if err != nil {
		t.Fatalf("publish gen 2: %v", err)
	}
	// This test CARRIES the independence argument, so it must not lean on a
	// sibling test to establish that a publish actually happened. If the second
	// WriteGraphGen were a no-op, "git state did not change" would be trivially
	// true and would prove nothing.
	if gen2Path == gen1Path {
		t.Fatalf("vacuous: no new generation was published (both %s)", gen1Path)
	}
	if got := graph.CurrentGraphPath(mainDir); got != gen2Path {
		t.Fatalf("vacuous: `current` resolves to %s, want the new generation %s", got, gen2Path)
	}

	if mt, sz := stat("HEAD"); mt != headMt || sz != headSz {
		t.Errorf("HEAD pointer moved across a reindex (%d/%d -> %d/%d)", headMt, headSz, mt, sz)
	}
	if mt, sz := stat(filepath.Join("refs", "heads", "main")); mt != tipMt || sz != tipSz {
		t.Errorf("branch tip moved across a reindex (%d/%d -> %d/%d)", tipMt, tipSz, mt, sz)
	}
}

// A same-branch reindex must not be masked by the mtime memo either: when the
// resolved graph path CHANGES, lr.mtime describes a DIFFERENT file and is not a
// valid freshness signal for the new one. Coarse filesystem timestamps can make
// two generations written milliseconds apart compare equal, in which case the
// `fileMtime.Equal(lr.mtime)` skip would decline to reparse and the daemon would
// serve generation N-1 even with discovery corrected.
//
// The fixture forces the collision by stamping both generations with an
// identical mtime, which is exactly the state a coarse-granularity filesystem
// produces on its own.
func TestReload_NewGenerationWithIdenticalMtimeStillReparses(t *testing.T) {
	root := t.TempDir()
	t.Setenv(daemon.EnvRoot, root)
	t.Setenv("GRAFEL_HOME", t.TempDir())

	repoDir := gitRepoForDiscovery(t)
	mainDir := daemon.StateDirForRepoRef(repoDir, "main")

	gen1Path, err := fbwriter.WriteGraphGen(mainDir, genDocWithMarker("gen1only"))
	if err != nil {
		t.Fatalf("publish gen 1: %v", err)
	}
	fi1, err := os.Stat(gen1Path)
	if err != nil {
		t.Fatalf("stat gen 1: %v", err)
	}

	reg := &Registry{Groups: map[string]RegistryGroup{
		"test": {Repos: map[string]RegistryRepo{"r": {Path: repoDir}}},
	}}
	st := NewState(reg)
	t.Cleanup(st.Close)
	if _, _, err := st.reloadLocked(); err != nil {
		t.Fatalf("initial reload: %v", err)
	}
	lr := st.Group("test").Repos["r"]
	if lr == nil {
		t.Fatal("repo not loaded")
	}

	gen2Path, err := fbwriter.WriteGraphGen(mainDir, genDocWithMarker("gen2only"))
	if err != nil {
		t.Fatalf("publish gen 2: %v", err)
	}
	// Simulate a coarse-granularity filesystem: both generations share an mtime.
	if err := os.Chtimes(gen2Path, fi1.ModTime(), fi1.ModTime()); err != nil {
		t.Fatalf("chtimes gen 2: %v", err)
	}
	fi2, err := os.Stat(gen2Path)
	if err != nil {
		t.Fatalf("stat gen 2: %v", err)
	}
	if !fi2.ModTime().Equal(fi1.ModTime()) {
		t.Fatalf("fixture degenerate: mtimes differ (%v vs %v)", fi1.ModTime(), fi2.ModTime())
	}
	if gen2Path == gen1Path {
		t.Fatal("vacuous: reindex reused the same generation file")
	}

	if _, _, err := st.reloadLocked(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if _, ok := lr.getByIDOne("gen2only"); !ok {
		t.Errorf("mtime memo suppressed the reparse of a NEW generation file: %q not served", "gen2only")
	}
}

// TestReload_PicksUpNewSegmentSetGenerationOnSameBranchReindex is the headline
// #6080 scenario on the MULTI-SEGMENT layout, where a generation is a
// graph.<gen>/ DIRECTORY rather than a graph.<gen>.fb file.
//
// This shape exercises two paths the single-file tests never reach:
// findGraphFileInDir's segment-set branch (which resolves the gen dir and takes
// its freshness from manifest.json), and hashGraphFile applied to a directory on
// the pathChanged reparse. Cheap to cover because the flush threshold is
// env-tunable, so a tiny document still produces a real segment set.
func TestReload_PicksUpNewSegmentSetGenerationOnSameBranchReindex(t *testing.T) {
	root := t.TempDir()
	t.Setenv(daemon.EnvRoot, root)
	t.Setenv("GRAFEL_HOME", t.TempDir())
	t.Setenv("GRAFEL_STREAM_SEGMENTS", "1")
	t.Setenv("GRAFEL_SEGMENT_BYTES", "1") // force segmentation of even a tiny doc

	repoDir := gitRepoForDiscovery(t)
	mainDir := daemon.StateDirForRepoRef(repoDir, "main")

	gen1Path, err := fbwriter.WriteGraphGen(mainDir, genDocWithMarker("gen1only"))
	if err != nil {
		t.Fatalf("publish gen 1: %v", err)
	}
	// Non-vacuity: this test only means something if the layout really is a
	// segment set. If the writer took its single-file fast path, the single-file
	// tests already cover what remains.
	desc, dErr := graph.CurrentGraphDescriptor(mainDir)
	if dErr != nil {
		t.Fatalf("descriptor: %v", dErr)
	}
	if desc.Kind != graph.GraphSegmentSet {
		t.Skipf("writer produced %v, not a segment set — cannot exercise the segment path here", desc.Kind)
	}

	reg := &Registry{Groups: map[string]RegistryGroup{
		"test": {Repos: map[string]RegistryRepo{"r": {Path: repoDir}}},
	}}
	st := NewState(reg)
	t.Cleanup(st.Close)
	if _, _, err := st.reloadLocked(); err != nil {
		t.Fatalf("initial reload: %v", err)
	}
	lr := st.Group("test").Repos["r"]
	if lr == nil || lr.Doc == nil {
		t.Fatalf("segment-set graph not loaded (loadErr=%q)", func() string {
			if lr == nil {
				return "<no repo>"
			}
			return lr.loadErr
		}())
	}
	if _, ok := lr.getByIDOne("gen1only"); !ok {
		t.Fatal("fixture degenerate: generation 1's entity is not served after the initial reload")
	}

	// Same-branch reindex, still segmented.
	gen2Path, err := fbwriter.WriteGraphGen(mainDir, genDocWithMarker("gen2only"))
	if err != nil {
		t.Fatalf("publish gen 2: %v", err)
	}
	if gen2Path == gen1Path {
		t.Fatal("vacuous: no new generation was published")
	}
	if _, err := os.Stat(gen1Path); err != nil {
		t.Fatalf("vacuous: gen N-1 was GC'd, so the stale stat could not have succeeded: %v", err)
	}
	if got := daemon.StateDirForRepo(repoDir); got != mainDir {
		t.Fatalf("vacuous: ref dir moved to %s", got)
	}

	if _, _, err := st.reloadLocked(); err != nil {
		t.Fatalf("reload after same-branch reindex: %v", err)
	}
	if lr.GraphFile != gen2Path {
		t.Errorf("reload kept the superseded segment-set generation pinned:\n  lr.GraphFile: %s\n  want:         %s",
			lr.GraphFile, gen2Path)
	}
	if _, ok := lr.getByIDOne("gen2only"); !ok {
		t.Error("daemon is serving generation N-1 on the segment-set layout: \"gen2only\" is not visible")
	}
	if _, ok := lr.getByIDOne("gen1only"); ok {
		t.Error("daemon is serving generation N-1 on the segment-set layout: \"gen1only\" is still visible")
	}
}

// TestReload_UnreadablePointerFallsThroughAndFlagsTheRepo pins the LIMIT of the
// #6080 gate, so the contract in state.go is pinned by execution rather than by
// prose. Read this before strengthening that comment again.
//
// When the `current` pointer cannot be read, the gate declines the fast path and
// falls through to full discovery — that much is real, and this test kills the
// mutant that serves the pinned generation instead. But the DESTINATION is not
// safe: when full discovery also comes up empty, reloadAllLocked sets
// lr.loadErr and `continue`s WITHOUT clearing lr.Doc, so the previously-resident
// generation keeps being served and the newer one on disk stays invisible.
//
// That is NOT a regression — the pre-#6080 gate stat'd the pinned file and
// served exactly the same stale graph on the same trigger — and it is NOT fixed
// here. Clearing lr.Doc is the correct destination (tools.go's `unavailable`
// list keys on Doc==nil plus loadErr, which is precisely the "report
// unavailable" shape this wants) but there is no `lr.Doc = nil` anywhere in this
// package today, and introducing one is unsafe while State.Group() hands out the
// group and drops s.mu: readers nil-check r.Doc and dereference it later, so a
// concurrent nil would convert a stale answer into a nil-deref panic across the
// whole read surface. The locking hole has to close first.
func TestReload_UnreadablePointerFallsThroughAndFlagsTheRepo(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: chmod 000 does not prevent reads")
	}
	root := t.TempDir()
	t.Setenv(daemon.EnvRoot, root)
	t.Setenv("GRAFEL_HOME", t.TempDir())

	repoDir := gitRepoForDiscovery(t)
	mainDir := daemon.StateDirForRepoRef(repoDir, "main")

	gen1Path, err := fbwriter.WriteGraphGen(mainDir, genDocWithMarker("gen1only"))
	if err != nil {
		t.Fatalf("publish gen 1: %v", err)
	}

	reg := &Registry{Groups: map[string]RegistryGroup{
		"test": {Repos: map[string]RegistryRepo{"r": {Path: repoDir}}},
	}}
	st := NewState(reg)
	t.Cleanup(st.Close)
	if _, _, err := st.reloadLocked(); err != nil {
		t.Fatalf("initial reload: %v", err)
	}
	lr := st.Group("test").Repos["r"]
	if lr == nil || lr.Doc == nil {
		t.Fatal("fixture degenerate: repo not loaded")
	}
	if lr.loadErr != "" {
		t.Fatalf("fixture degenerate: repo already flagged before the fault (%q)", lr.loadErr)
	}

	// A newer generation is published, then the pointer becomes unreadable.
	gen2Path, err := fbwriter.WriteGraphGen(mainDir, genDocWithMarker("gen2only"))
	if err != nil {
		t.Fatalf("publish gen 2: %v", err)
	}
	if gen2Path == gen1Path {
		t.Fatal("vacuous: no new generation was published")
	}
	// Non-vacuity: the pinned generation must still exist, or the old gate's
	// os.Stat would fail on its own and the mutant would be unkillable.
	if _, err := os.Stat(gen1Path); err != nil {
		t.Fatalf("vacuous: gen N-1 is gone, so the pinned-stat mutant cannot be distinguished: %v", err)
	}
	pointer := filepath.Join(mainDir, "current")
	if err := os.Chmod(pointer, 0o000); err != nil {
		t.Fatalf("chmod current: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(pointer, 0o644) })
	if _, err := os.ReadFile(pointer); err == nil {
		t.Skip("current is still readable after chmod 000; cannot stage the fault here")
	}

	if _, _, err := st.reloadLocked(); err != nil {
		t.Fatalf("reload with unreadable pointer: %v", err)
	}

	// THE ASSERTION THAT MATTERS: the gate did NOT take the fast path on an
	// unverifiable pointer. A mutant that serves the pinned generation leaves
	// loadErr empty and this fails.
	if lr.loadErr == "" {
		t.Error("gate served the pinned generation on an unverifiable `current` pointer: " +
			"the repo was not flagged, so nothing anywhere records that freshness is unknown")
	}

	// CHARACTERIZATION of the known gap, asserted so it cannot change silently.
	// If a later change clears lr.Doc / marks the repo unservable, this block is
	// the one to delete — and the comment in state.go must be strengthened with it.
	if lr.Doc == nil {
		t.Error("lr.Doc was cleared — the known gap this test documents has been closed; " +
			"update the #6080 contract comment in state.go and delete this block")
	} else if _, ok := lr.getByIDOne("gen1only"); !ok {
		t.Error("expected the superseded generation to still be resident (documented gap)")
	}
}

// The freshness gate must not regress the #6060 ref-staleness fix, and must not
// break the legacy flat graph.fb layout (no `current` pointer at all).
func TestReload_FlatLegacyGraphStillServed(t *testing.T) {
	root := t.TempDir()
	t.Setenv(daemon.EnvRoot, root)
	t.Setenv("GRAFEL_HOME", t.TempDir())

	repoDir := gitRepoForDiscovery(t)
	mainDir := daemon.StateDirForRepoRef(repoDir, "main")
	writeGraphInto(t, mainDir, genDocWithMarker("flat"))
	if _, err := os.Stat(filepath.Join(mainDir, graph.CurrentPointerName)); err == nil {
		t.Fatal("fixture degenerate: legacy flat layout must have no `current` pointer")
	}

	reg := &Registry{Groups: map[string]RegistryGroup{
		"test": {Repos: map[string]RegistryRepo{"r": {Path: repoDir}}},
	}}
	st := NewState(reg)
	t.Cleanup(st.Close)
	if _, _, err := st.reloadLocked(); err != nil {
		t.Fatalf("initial reload: %v", err)
	}
	lr := st.Group("test").Repos["r"]
	if lr == nil || lr.Doc == nil {
		t.Fatalf("legacy flat graph.fb not loaded (loadErr=%q)", func() string {
			if lr == nil {
				return "<no repo>"
			}
			return lr.loadErr
		}())
	}
	// A second reload must keep serving it (the gate must not evict a layout it
	// cannot find a generation pointer for).
	if _, _, err := st.reloadLocked(); err != nil {
		t.Fatalf("second reload: %v", err)
	}
	if _, ok := lr.getByIDOne("flat"); !ok {
		t.Error("legacy flat graph.fb stopped being served after a second reload")
	}
}
