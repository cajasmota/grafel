package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/indexer/diff"
)

// ---------------------------------------------------------------------------
// Fixture helpers — a state dir is just a directory holding graph.<gen>.fb, a
// `current` pointer naming it, and the incremental diff manifest. None of the
// seeding machinery decodes the graph, so opaque bytes are a faithful stand-in
// and every test in this file runs in microseconds (no index, no git).
// ---------------------------------------------------------------------------

func writeParentStateDir(t *testing.T, dir string, gen uint64, body string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir parent state dir: %v", err)
	}
	name := graph.GenFileName(gen)
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write gen file: %v", err)
	}
	if err := graph.WriteCurrentPointer(dir, name); err != nil {
		t.Fatalf("write current pointer: %v", err)
	}
	m := diff.LoadManifest(dir) // empty manifest, correct Version
	m.Files["a.go"] = diff.FileEntry{SHA256: "deadbeef", Size: 3}
	if err := diff.SaveManifest(dir, "/does/not/matter", m); err != nil {
		t.Fatalf("save manifest: %v", err)
	}
	return name
}

func seedReq(t *testing.T, parentStateDir, childStateDir string) SeedRequest {
	t.Helper()
	return SeedRequest{
		ParentPath:     filepath.Join(t.TempDir(), "parent-repo"),
		ParentRef:      "release/v9",
		ParentStateDir: parentStateDir,
		ChildPath:      filepath.Join(t.TempDir(), "child-wt"),
		ChildRef:       "feat/x",
		ChildStateDir:  childStateDir,
		RepoTag:        "myservice",
	}
}

// ---------------------------------------------------------------------------
// Happy path
// ---------------------------------------------------------------------------

func TestSeedWorktreeGraph_CopiesGenGraphManifestAndStamp(t *testing.T) {
	root := t.TempDir()
	parentSD := filepath.Join(root, "parent-state")
	childSD := filepath.Join(root, "child-state")
	genName := writeParentStateDir(t, parentSD, 7, "GRAPH-BYTES-7")

	out := SeedWorktreeGraph(seedReq(t, parentSD, childSD))
	if !out.Seeded {
		t.Fatalf("Seeded=false reason=%q detail=%q", out.Reason, out.Detail)
	}
	if out.Reason != "" {
		t.Errorf("Reason=%q want empty on a successful seed", out.Reason)
	}

	// Graph generation file copied byte-for-byte.
	got, err := os.ReadFile(filepath.Join(childSD, genName))
	if err != nil {
		t.Fatalf("read seeded gen file: %v", err)
	}
	if string(got) != "GRAPH-BYTES-7" {
		t.Errorf("seeded gen body=%q want GRAPH-BYTES-7", got)
	}
	// The `current` pointer resolves the seeded graph.
	desc, err := graph.CurrentGraphDescriptor(childSD)
	if err != nil {
		t.Fatalf("descriptor: %v", err)
	}
	if desc.Kind != graph.GraphSingleFile || filepath.Base(desc.Path) != genName {
		t.Errorf("child descriptor kind=%v path=%q want single-file %s", desc.Kind, desc.Path, genName)
	}
	// The diff manifest came across — without it the incremental pass has no
	// baseline and every file reads as changed.
	if _, err := os.Stat(filepath.Join(childSD, diff.ManifestFileName)); err != nil {
		t.Errorf("diff manifest not seeded: %v", err)
	}
	// Provenance stamp is on disk and names the parent generation.
	st, err := ReadSeedStamp(childSD)
	if err != nil {
		t.Fatalf("ReadSeedStamp: %v", err)
	}
	if st.ParentPointer != genName {
		t.Errorf("stamp ParentPointer=%q want %q", st.ParentPointer, genName)
	}
	if st.ParentRef != "release/v9" {
		t.Errorf("stamp ParentRef=%q want release/v9", st.ParentRef)
	}
	if st.RepoTag != "myservice" {
		t.Errorf("stamp RepoTag=%q want myservice", st.RepoTag)
	}
	if len(st.Artifacts) < 2 {
		t.Errorf("stamp Artifacts=%v want at least the gen file + manifest", st.Artifacts)
	}
}

func TestSeedWorktreeGraph_PinsRepoTagSoEntityIDsAgreeWithTheParent(t *testing.T) {
	// graph.EntityID hashes the repo tag FIRST. A worktree indexed with the
	// default tag (its own directory basename) mints entity IDs that disagree
	// with the parent's for byte-identical files, which would make a seeded
	// graph a hybrid of two ID spaces. The seed therefore records the tag the
	// index MUST be pinned to.
	root := t.TempDir()
	parentSD := filepath.Join(root, "parent-state")
	childSD := filepath.Join(root, "child-state")
	writeParentStateDir(t, parentSD, 1, "G")

	out := SeedWorktreeGraph(seedReq(t, parentSD, childSD))
	if !out.Seeded {
		t.Fatalf("seed failed: %s %s", out.Reason, out.Detail)
	}
	if got := ReadRepoTagPin(childSD); got != "myservice" {
		t.Errorf("ReadRepoTagPin=%q want myservice", got)
	}
	parentID := graph.EntityID("myservice", "SCOPE.Operation", "Handle", "svc/a.go")
	pinnedID := graph.EntityID(ReadRepoTagPin(childSD), "SCOPE.Operation", "Handle", "svc/a.go")
	if parentID != pinnedID {
		t.Errorf("pinned tag yields %s, parent yields %s — seeded graph would be a hybrid", pinnedID, parentID)
	}
	unpinnedID := graph.EntityID("child-wt", "SCOPE.Operation", "Handle", "svc/a.go")
	if unpinnedID == parentID {
		t.Fatal("fixture is inert: the unpinned tag must produce a DIFFERENT id, else this test proves nothing")
	}
}

func TestWriteRepoTagPin_RoundTripsAndIsIndependentOfTheSeed(t *testing.T) {
	dir := t.TempDir()
	if got := ReadRepoTagPin(dir); got != "" {
		t.Errorf("unpinned dir returned %q want empty", got)
	}
	if err := WriteRepoTagPin(dir, "svc-a"); err != nil {
		t.Fatalf("WriteRepoTagPin: %v", err)
	}
	if got := ReadRepoTagPin(dir); got != "svc-a" {
		t.Errorf("ReadRepoTagPin=%q want svc-a", got)
	}
	// A full (unseeded) index of the same worktree must use the SAME tag, or
	// the seeded and from-scratch graphs would not be comparable at all.
	if err := DiscardSeed(dir); err != nil {
		t.Fatalf("DiscardSeed: %v", err)
	}
	if got := ReadRepoTagPin(dir); got != "svc-a" {
		t.Errorf("after DiscardSeed ReadRepoTagPin=%q want svc-a (the pin outlives the seed)", got)
	}
}

// ---------------------------------------------------------------------------
// Fallback path 1 — parent never indexed
// ---------------------------------------------------------------------------

func TestSeedWorktreeGraph_FallsBackWhenParentNotIndexed(t *testing.T) {
	root := t.TempDir()
	parentSD := filepath.Join(root, "parent-state") // empty: never indexed
	childSD := filepath.Join(root, "child-state")
	if err := os.MkdirAll(parentSD, 0o755); err != nil {
		t.Fatal(err)
	}

	out := SeedWorktreeGraph(seedReq(t, parentSD, childSD))
	if out.Seeded {
		t.Fatal("Seeded=true for an unindexed parent")
	}
	if out.Reason != SeedFallbackParentNotIndexed {
		t.Errorf("Reason=%q want %q", out.Reason, SeedFallbackParentNotIndexed)
	}
	// A silent fallback is the failure mode this feature is designed against.
	if out.Detail == "" {
		t.Error("Detail is empty — the fallback must name what it found")
	}
	if desc, _ := graph.CurrentGraphDescriptor(childSD); desc.Kind != graph.GraphAbsent {
		t.Error("child state dir must be left with no graph so a full index runs")
	}
}

func TestSeedWorktreeGraph_FallsBackWhenParentHasNoDiffManifest(t *testing.T) {
	root := t.TempDir()
	parentSD := filepath.Join(root, "parent-state")
	childSD := filepath.Join(root, "child-state")
	writeParentStateDir(t, parentSD, 3, "G")
	if err := os.Remove(filepath.Join(parentSD, diff.ManifestFileName)); err != nil {
		t.Fatal(err)
	}

	out := SeedWorktreeGraph(seedReq(t, parentSD, childSD))
	if out.Seeded {
		t.Fatal("Seeded=true with no parent diff manifest")
	}
	if out.Reason != SeedFallbackParentManifestAbsent {
		t.Errorf("Reason=%q want %q", out.Reason, SeedFallbackParentManifestAbsent)
	}
	if desc, _ := graph.CurrentGraphDescriptor(childSD); desc.Kind != graph.GraphAbsent {
		t.Error("no graph must be left behind")
	}
}

// ---------------------------------------------------------------------------
// Fallback path 2 — the parent generation moved under the copy
// ---------------------------------------------------------------------------

func TestSeedWorktreeGraph_FallsBackWhenParentGenerationMovesMidCopy(t *testing.T) {
	root := t.TempDir()
	parentSD := filepath.Join(root, "parent-state")
	childSD := filepath.Join(root, "child-state")
	writeParentStateDir(t, parentSD, 4, "GEN-4")

	req := seedReq(t, parentSD, childSD)
	// Simulate the parent finishing a reindex while the copy is in flight:
	// a new generation lands and `current` flips.
	req.afterCopyHook = func() {
		if err := os.WriteFile(filepath.Join(parentSD, graph.GenFileName(5)), []byte("GEN-5"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := graph.WriteCurrentPointer(parentSD, graph.GenFileName(5)); err != nil {
			t.Fatal(err)
		}
	}

	out := SeedWorktreeGraph(req)
	if out.Seeded {
		t.Fatal("Seeded=true although the parent generation moved mid-copy — the graph and the diff manifest could be from different generations")
	}
	if out.Reason != SeedFallbackGenerationMoved {
		t.Errorf("Reason=%q want %q", out.Reason, SeedFallbackGenerationMoved)
	}
	if desc, _ := graph.CurrentGraphDescriptor(childSD); desc.Kind != graph.GraphAbsent {
		t.Error("a torn seed must not be left resolvable — it would be silently wrong")
	}
	if _, err := ReadSeedStamp(childSD); err == nil {
		t.Error("a stamp must not be written for a torn seed")
	}
}

// ---------------------------------------------------------------------------
// Fallback path 3 — copy failure
// ---------------------------------------------------------------------------

func TestSeedWorktreeGraph_FallsBackWhenCopyFails(t *testing.T) {
	root := t.TempDir()
	parentSD := filepath.Join(root, "parent-state")
	writeParentStateDir(t, parentSD, 2, "G")
	// Make the child state dir un-creatable: its parent path is a FILE.
	blocker := filepath.Join(root, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	childSD := filepath.Join(blocker, "child-state")

	out := SeedWorktreeGraph(seedReq(t, parentSD, childSD))
	if out.Seeded {
		t.Fatal("Seeded=true despite an unwritable destination")
	}
	if out.Reason != SeedFallbackCopyFailed {
		t.Errorf("Reason=%q want %q", out.Reason, SeedFallbackCopyFailed)
	}
	if out.Detail == "" {
		t.Error("copy failure must carry the underlying error in Detail")
	}
}

// ---------------------------------------------------------------------------
// Fallback path 4 — the child already has a graph
// ---------------------------------------------------------------------------

func TestSeedWorktreeGraph_DoesNotClobberAnAlreadyIndexedChild(t *testing.T) {
	root := t.TempDir()
	parentSD := filepath.Join(root, "parent-state")
	childSD := filepath.Join(root, "child-state")
	writeParentStateDir(t, parentSD, 9, "PARENT")
	writeParentStateDir(t, childSD, 1, "CHILD-OWN-GRAPH")

	out := SeedWorktreeGraph(seedReq(t, parentSD, childSD))
	if out.Seeded {
		t.Fatal("Seeded=true over an already-indexed child — that would discard the child's own newer graph")
	}
	if out.Reason != SeedFallbackChildAlreadyIndexed {
		t.Errorf("Reason=%q want %q", out.Reason, SeedFallbackChildAlreadyIndexed)
	}
	desc, _ := graph.CurrentGraphDescriptor(childSD)
	body, _ := os.ReadFile(desc.Path)
	if string(body) != "CHILD-OWN-GRAPH" {
		t.Errorf("child graph body=%q — the child's own graph was clobbered", body)
	}
}

// ---------------------------------------------------------------------------
// Stamp verification + the mutation that must fail
// ---------------------------------------------------------------------------

func TestVerifySeededGraph_AcceptsAnIntactSeed(t *testing.T) {
	root := t.TempDir()
	parentSD := filepath.Join(root, "parent-state")
	childSD := filepath.Join(root, "child-state")
	writeParentStateDir(t, parentSD, 11, "G")
	if out := SeedWorktreeGraph(seedReq(t, parentSD, childSD)); !out.Seeded {
		t.Fatalf("seed failed: %s", out.Reason)
	}

	st, reason, err := VerifySeededGraph(childSD)
	if err != nil {
		t.Fatalf("VerifySeededGraph: %v", err)
	}
	if reason != "" {
		t.Fatalf("reason=%q want empty for an intact seed", reason)
	}
	if st == nil || st.ParentPointer != graph.GenFileName(11) {
		t.Fatalf("stamp=%+v want ParentPointer=%s", st, graph.GenFileName(11))
	}
}

// MUTATION GUARD: a seed whose recorded generation does not match what is on
// disk must be REJECTED. If VerifySeededGraph is weakened to trust the stamp
// without recomputing the digest, this test fails.
func TestVerifySeededGraph_RejectsAStampWhoseGraphContentChanged(t *testing.T) {
	root := t.TempDir()
	parentSD := filepath.Join(root, "parent-state")
	childSD := filepath.Join(root, "child-state")
	writeParentStateDir(t, parentSD, 11, "TRUSTED")
	if out := SeedWorktreeGraph(seedReq(t, parentSD, childSD)); !out.Seeded {
		t.Fatalf("seed failed: %s", out.Reason)
	}
	// Swap the seeded graph body for content the stamp never covered — the
	// exact shape of "the child's graph contains entities from code not in
	// the child's tree".
	if err := os.WriteFile(filepath.Join(childSD, graph.GenFileName(11)), []byte("TAMPERED-LONGER"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, reason, err := VerifySeededGraph(childSD)
	if err != nil {
		t.Fatalf("VerifySeededGraph returned err: %v", err)
	}
	if reason != SeedFallbackStampMismatch {
		t.Fatalf("reason=%q want %q — a seed whose content does not match its stamp was ACCEPTED", reason, SeedFallbackStampMismatch)
	}
}

func TestVerifySeededGraph_RejectsWhenTheDiffManifestWasSwappedIndependently(t *testing.T) {
	// The graph and the diff manifest must come from the SAME parent
	// generation. If the manifest is newer than the graph, files that really
	// differ read as unchanged and their stale entities survive — silently.
	root := t.TempDir()
	parentSD := filepath.Join(root, "parent-state")
	childSD := filepath.Join(root, "child-state")
	writeParentStateDir(t, parentSD, 6, "G")
	if out := SeedWorktreeGraph(seedReq(t, parentSD, childSD)); !out.Seeded {
		t.Fatalf("seed failed: %s", out.Reason)
	}
	m := diff.LoadManifest(childSD)
	m.Files["b.go"] = diff.FileEntry{SHA256: "cafebabe", Size: 9}
	if err := diff.SaveManifest(childSD, "/x", m); err != nil {
		t.Fatal(err)
	}

	_, reason, _ := VerifySeededGraph(childSD)
	if reason != SeedFallbackStampMismatch {
		t.Fatalf("reason=%q want %q", reason, SeedFallbackStampMismatch)
	}
}

func TestVerifySeededGraph_RejectsWhenTheCurrentPointerMovedToAnUnrelatedGeneration(t *testing.T) {
	// A pointer that moved BACKWARD (or sideways) is not the child having
	// built its own graph — generations are minted monotonically per state
	// dir — so it cannot be classified as superseded and must be rejected.
	root := t.TempDir()
	parentSD := filepath.Join(root, "parent-state")
	childSD := filepath.Join(root, "child-state")
	writeParentStateDir(t, parentSD, 6, "G")
	if out := SeedWorktreeGraph(seedReq(t, parentSD, childSD)); !out.Seeded {
		t.Fatalf("seed failed: %s", out.Reason)
	}
	if err := os.WriteFile(filepath.Join(childSD, graph.GenFileName(2)), []byte("OTHER"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := graph.WriteCurrentPointer(childSD, graph.GenFileName(2)); err != nil {
		t.Fatal(err)
	}

	_, reason, _ := VerifySeededGraph(childSD)
	if reason != SeedFallbackGenerationMoved {
		t.Fatalf("reason=%q want %q", reason, SeedFallbackGenerationMoved)
	}
	if SeedVerdictIsBenign(reason) {
		t.Error("a backward pointer move must not be treated as benign")
	}
}

func TestVerifySeededGraph_NoStampIsNotAFailure(t *testing.T) {
	// An ordinary, never-seeded state dir must verify clean — otherwise every
	// non-worktree repo would be forced down the fallback path forever.
	dir := t.TempDir()
	writeParentStateDir(t, dir, 1, "G")
	st, reason, err := VerifySeededGraph(dir)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if st != nil || reason != "" {
		t.Fatalf("st=%+v reason=%q want nil/empty for an unseeded dir", st, reason)
	}
}

// ---------------------------------------------------------------------------
// DiscardSeed
// ---------------------------------------------------------------------------

func TestDiscardSeed_RemovesEverySeededArtifactAndThePointer(t *testing.T) {
	root := t.TempDir()
	parentSD := filepath.Join(root, "parent-state")
	childSD := filepath.Join(root, "child-state")
	writeParentStateDir(t, parentSD, 8, "G")
	if out := SeedWorktreeGraph(seedReq(t, parentSD, childSD)); !out.Seeded {
		t.Fatalf("seed failed: %s", out.Reason)
	}

	if err := DiscardSeed(childSD); err != nil {
		t.Fatalf("DiscardSeed: %v", err)
	}
	if desc, _ := graph.CurrentGraphDescriptor(childSD); desc.Kind != graph.GraphAbsent {
		t.Error("graph still resolvable after DiscardSeed → a full index would merge into an untrusted seed")
	}
	if _, err := os.Stat(filepath.Join(childSD, diff.ManifestFileName)); !os.IsNotExist(err) {
		t.Error("diff manifest survived DiscardSeed → the next pass would use an untrusted baseline")
	}
	if _, err := ReadSeedStamp(childSD); err == nil {
		t.Error("stamp survived DiscardSeed")
	}
}

// ---------------------------------------------------------------------------
// A stale stamp must never destroy the child's own graph.
//
// ConsumeSeedStamp is only reachable on the paths that go looking for a stamp.
// The scheduler skips cfg.Incremental entirely when incremental reindexing is
// disabled (GRAFEL_INCREMENTAL_REINDEX=0, a documented, supported setting), so
// a full index can run over a seeded dir and leave the stamp behind. If the
// next verification then read that stale stamp as "generation moved" and
// DiscardSeed removed the pointer, a full corpus reindex the child legitimately
// paid for would be thrown away — the exact cost this feature exists to
// eliminate.
// ---------------------------------------------------------------------------

func TestVerifySeededGraph_TreatsAPointerMovedFORWARDAsASupersededSeed(t *testing.T) {
	root := t.TempDir()
	parentSD := filepath.Join(root, "parent-state")
	childSD := filepath.Join(root, "child-state")
	writeParentStateDir(t, parentSD, 1, "PARENT")
	if out := SeedWorktreeGraph(seedReq(t, parentSD, childSD)); !out.Seeded {
		t.Fatalf("seed failed: %s", out.Reason)
	}
	// The child runs a FULL index over the seeded dir (the path that never
	// consumes the stamp) and writes its own, newer generation.
	if err := os.WriteFile(filepath.Join(childSD, graph.GenFileName(2)), []byte("CHILD-OWN-GRAPH"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := graph.WriteCurrentPointer(childSD, graph.GenFileName(2)); err != nil {
		t.Fatal(err)
	}

	stamp, reason, err := VerifySeededGraph(childSD)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if reason != SeedFallbackSuperseded {
		t.Fatalf("reason=%q want %q — a stale stamp over the child's OWN newer graph was reported as an untrusted seed", reason, SeedFallbackSuperseded)
	}
	if stamp == nil {
		t.Fatal("stamp must still be returned so the caller can consume it")
	}
	if !SeedVerdictIsBenign(reason) {
		t.Error("a superseded seed must be classified benign — the caller consumes the stamp, it does not fall back")
	}
	if SeedVerdictIsBenign(SeedFallbackGenerationMoved) || SeedVerdictIsBenign(SeedFallbackStampMismatch) {
		t.Error("genuinely untrusted verdicts must NOT be classified benign")
	}
}

func TestDiscardSeed_RefusesToRemoveAGraphNEWERThanTheStamp(t *testing.T) {
	// Belt and braces: even if a caller ignores the superseded verdict and
	// calls DiscardSeed anyway, the child's own graph must survive. This is
	// the mechanism that makes the data loss structurally impossible rather
	// than merely unreached.
	root := t.TempDir()
	parentSD := filepath.Join(root, "parent-state")
	childSD := filepath.Join(root, "child-state")
	writeParentStateDir(t, parentSD, 1, "PARENT")
	if out := SeedWorktreeGraph(seedReq(t, parentSD, childSD)); !out.Seeded {
		t.Fatalf("seed failed: %s", out.Reason)
	}
	if err := os.WriteFile(filepath.Join(childSD, graph.GenFileName(2)), []byte("CHILD-OWN-GRAPH"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := graph.WriteCurrentPointer(childSD, graph.GenFileName(2)); err != nil {
		t.Fatal(err)
	}

	if err := DiscardSeed(childSD); err != nil {
		t.Fatalf("DiscardSeed: %v", err)
	}

	desc, _ := graph.CurrentGraphDescriptor(childSD)
	if desc.Kind == graph.GraphAbsent {
		t.Fatal("DATA LOSS: DiscardSeed made the child's own legitimately built graph unresolvable")
	}
	body, _ := os.ReadFile(desc.Path)
	if string(body) != "CHILD-OWN-GRAPH" {
		t.Errorf("resolved body=%q want CHILD-OWN-GRAPH", body)
	}
	// The stale stamp itself must be gone, so the dir stops re-triggering this.
	if _, err := ReadSeedStamp(childSD); err == nil {
		t.Error("the stale stamp survived DiscardSeed")
	}
}

func TestDiscardSeed_StillRemovesAGraphThatIsNOTNewerThanTheStamp(t *testing.T) {
	// The refusal above must not become a blanket refusal: a seed that is
	// genuinely untrusted (same generation, tampered content) must still be
	// made unresolvable so a full index runs.
	root := t.TempDir()
	parentSD := filepath.Join(root, "parent-state")
	childSD := filepath.Join(root, "child-state")
	writeParentStateDir(t, parentSD, 3, "PARENT")
	if out := SeedWorktreeGraph(seedReq(t, parentSD, childSD)); !out.Seeded {
		t.Fatalf("seed failed: %s", out.Reason)
	}
	if err := os.WriteFile(filepath.Join(childSD, graph.GenFileName(3)), []byte("TAMPERED"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := DiscardSeed(childSD); err != nil {
		t.Fatalf("DiscardSeed: %v", err)
	}
	if desc, _ := graph.CurrentGraphDescriptor(childSD); desc.Kind != graph.GraphAbsent {
		t.Fatal("an untrusted, non-superseded seed stayed resolvable")
	}
}

func TestConsumeSeedStamp_LeavesTheGraphAndStopsLaterVerificationFromWipingIt(t *testing.T) {
	// After the consuming pass writes its own generation, an un-consumed
	// stamp would make the next VerifySeededGraph report generation_moved and
	// DiscardSeed would delete a graph the child legitimately built.
	root := t.TempDir()
	parentSD := filepath.Join(root, "parent-state")
	childSD := filepath.Join(root, "child-state")
	writeParentStateDir(t, parentSD, 4, "G")
	if out := SeedWorktreeGraph(seedReq(t, parentSD, childSD)); !out.Seeded {
		t.Fatalf("seed failed: %s", out.Reason)
	}

	if err := ConsumeSeedStamp(childSD); err != nil {
		t.Fatalf("ConsumeSeedStamp: %v", err)
	}
	if desc, _ := graph.CurrentGraphDescriptor(childSD); desc.Kind == graph.GraphAbsent {
		t.Fatal("ConsumeSeedStamp removed the graph")
	}
	if _, err := os.Stat(filepath.Join(childSD, diff.ManifestFileName)); err != nil {
		t.Fatalf("ConsumeSeedStamp removed the diff manifest: %v", err)
	}

	// The child now writes its own generation, as a real pass would.
	if err := os.WriteFile(filepath.Join(childSD, graph.GenFileName(12)), []byte("CHILD-OWN"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := graph.WriteCurrentPointer(childSD, graph.GenFileName(12)); err != nil {
		t.Fatal(err)
	}
	st, reason, err := VerifySeededGraph(childSD)
	if err != nil || st != nil || reason != "" {
		t.Fatalf("VerifySeededGraph after consume = (%+v, %q, %v) want (nil, \"\", nil)", st, reason, err)
	}
	if err := DiscardSeed(childSD); err != nil {
		t.Fatal(err)
	}
	if desc, _ := graph.CurrentGraphDescriptor(childSD); desc.Kind == graph.GraphAbsent {
		t.Fatal("DiscardSeed deleted the child's OWN graph after the stamp was consumed")
	}
}

func TestConsumeSeedStamp_IsANoOpWithoutAStamp(t *testing.T) {
	if err := ConsumeSeedStamp(t.TempDir()); err != nil {
		t.Fatalf("ConsumeSeedStamp: %v", err)
	}
}

func TestDiscardSeed_IsANoOpOnADirItNeverSeeded(t *testing.T) {
	dir := t.TempDir()
	writeParentStateDir(t, dir, 1, "REAL-GRAPH")
	if err := DiscardSeed(dir); err != nil {
		t.Fatalf("DiscardSeed: %v", err)
	}
	desc, _ := graph.CurrentGraphDescriptor(dir)
	if desc.Kind == graph.GraphAbsent {
		t.Fatal("DiscardSeed deleted a real, unseeded graph")
	}
	body, _ := os.ReadFile(desc.Path)
	if string(body) != "REAL-GRAPH" {
		t.Errorf("body=%q want REAL-GRAPH", body)
	}
}

// ---------------------------------------------------------------------------
// Input validation
// ---------------------------------------------------------------------------

func TestSeedWorktreeGraph_RefusesWithoutAPinnedRepoTag(t *testing.T) {
	root := t.TempDir()
	parentSD := filepath.Join(root, "parent-state")
	childSD := filepath.Join(root, "child-state")
	writeParentStateDir(t, parentSD, 1, "G")
	req := seedReq(t, parentSD, childSD)
	req.RepoTag = ""

	out := SeedWorktreeGraph(req)
	if out.Seeded {
		t.Fatal("Seeded=true with no repo tag — entity IDs would not agree with the parent's")
	}
	if out.Reason != SeedFallbackRepoTagUnresolved {
		t.Errorf("Reason=%q want %q", out.Reason, SeedFallbackRepoTagUnresolved)
	}
}

func TestSeedWorktreeGraph_RefusesWithoutAParentPath(t *testing.T) {
	root := t.TempDir()
	childSD := filepath.Join(root, "child-state")
	req := seedReq(t, "", childSD)
	req.ParentPath = ""
	req.ParentStateDir = ""

	out := SeedWorktreeGraph(req)
	if out.Seeded {
		t.Fatal("Seeded=true with no parent path")
	}
	if out.Reason != SeedFallbackParentPathUnresolved {
		t.Errorf("Reason=%q want %q", out.Reason, SeedFallbackParentPathUnresolved)
	}
}

// #3652 REGRESSION GUARD. The first clone-from-parent attempt resolved the
// parent's graph with a HARDCODED "main" ref, so it always read an empty dir
// for any parent not on main. SeedWorktreeGraph must resolve the parent's
// state dir from the parent's ACTUAL ref.
func TestSeedWorktreeGraph_UsesTheParentsActualRefNotMain(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GRAFEL_DAEMON_ROOT", filepath.Join(root, "daemonroot"))
	parentRepo := filepath.Join(root, "parent-repo")
	childRepo := filepath.Join(root, "child-wt")
	for _, d := range []string{parentRepo, childRepo} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	const actualParentRef = "release/2026-07"
	realSD := StateDirForRepoRef(parentRepo, actualParentRef)
	mainSD := StateDirForRepoRef(parentRepo, "main")
	if realSD == mainSD {
		t.Fatal("fixture is inert: the two ref dirs must differ")
	}
	// Only the parent's ACTUAL ref has a graph. "main" is deliberately empty —
	// the #3652 bug, reproduced.
	writeParentStateDir(t, realSD, 3, "PARENT-ON-RELEASE-BRANCH")
	if err := os.MkdirAll(mainSD, 0o755); err != nil {
		t.Fatal(err)
	}

	childSD := StateDirForRepoRef(childRepo, "feat/x")
	out := SeedWorktreeGraph(SeedRequest{
		ParentPath: parentRepo,
		ParentRef:  actualParentRef,
		ChildPath:  childRepo,
		ChildRef:   "feat/x",
		RepoTag:    "myservice",
	})
	if !out.Seeded {
		t.Fatalf("Seeded=false (%s: %s) — the parent's actual ref was not used; this is the #3652 regression", out.Reason, out.Detail)
	}
	desc, _ := graph.CurrentGraphDescriptor(childSD)
	body, _ := os.ReadFile(desc.Path)
	if string(body) != "PARENT-ON-RELEASE-BRANCH" {
		t.Errorf("seeded body=%q want the release-branch graph", body)
	}
	st, err := ReadSeedStamp(childSD)
	if err != nil {
		t.Fatalf("ReadSeedStamp: %v", err)
	}
	if st.ParentRef != actualParentRef {
		t.Errorf("stamp ParentRef=%q want %q — a hardcoded ref would be recorded here", st.ParentRef, actualParentRef)
	}
	if strings.Contains(st.ParentStateDir, string(filepath.Separator)+"main"+string(filepath.Separator)) {
		t.Errorf("stamp ParentStateDir=%q resolved through a 'main' segment", st.ParentStateDir)
	}
}
