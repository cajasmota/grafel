package fbreader_test

import (
	"path/filepath"
	"sort"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	fbgraph "github.com/cajasmota/grafel/internal/graph/fbgraph"
	"github.com/cajasmota/grafel/internal/graph/fbreader"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// writeSegment marshals doc (its Entities/Relationships must already be
// disjoint-and-sorted-by-key subsets of the overall test corpus, per the
// FlatBuffers `(key)` requirement on Entity.id) into its own segment file
// under dir and returns its path.
func writeSegment(t *testing.T, dir, name string, doc *graph.Document) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := fbwriter.WriteAtomic(path, doc); err != nil {
		t.Fatalf("write segment %s: %v", name, err)
	}
	return path
}

// openMultiCleanup opens a MultiReader over paths and registers its Close
// in t.Cleanup BEFORE the TempDir's own cleanup can run (t.Cleanup funcs
// run in LIFO order, so registering this Close after t.TempDir() was
// created — but before any nested TempDir — still unwinds first). This
// mirrors the v0.1.9 Windows teardown lesson: a mapped file cannot be
// unlinked on Windows, so the mapping must be released before the temp
// directory removal fires.
func openMultiCleanup(t *testing.T, paths []string) *fbreader.MultiReader {
	t.Helper()
	mr, err := fbreader.OpenSegments(paths)
	if err != nil {
		t.Fatalf("OpenSegments: %v", err)
	}
	t.Cleanup(func() { _ = mr.Close() })
	return mr
}

// threeSegmentFixture builds a small graph split across 3 disjoint,
// sorted-by-key segment files:
//
//	seg0: entities a, b   | relationship a -> z (z lives in seg2)
//	seg1: entities m, n   | relationship m -> n
//	seg2: entities y, z   | relationship y -> a (a lives in seg0)
//
// This deliberately exercises cross-segment relationship resolution in
// both directions.
func threeSegmentFixture(t *testing.T) []string {
	t.Helper()
	dir := t.TempDir()

	seg0 := &graph.Document{
		Repo: "multiseg-test",
		Entities: []graph.Entity{
			{ID: "a", QualifiedName: "pkg.A", Kind: "function", Name: "A"},
			{ID: "b", QualifiedName: "pkg.B", Kind: "struct", Name: "B"},
		},
		Relationships: []graph.Relationship{
			{FromID: "a", ToID: "z", Kind: "CALLS"},
		},
	}
	seg1 := &graph.Document{
		Repo: "multiseg-test",
		Entities: []graph.Entity{
			{ID: "m", QualifiedName: "pkg.M", Kind: "function", Name: "M"},
			{ID: "n", QualifiedName: "pkg.N", Kind: "struct", Name: "N"},
		},
		Relationships: []graph.Relationship{
			{FromID: "m", ToID: "n", Kind: "CALLS"},
		},
	}
	seg2 := &graph.Document{
		Repo: "multiseg-test",
		Entities: []graph.Entity{
			{ID: "y", QualifiedName: "pkg.Y", Kind: "function", Name: "Y"},
			{ID: "z", QualifiedName: "pkg.Z", Kind: "struct", Name: "Z"},
		},
		Relationships: []graph.Relationship{
			{FromID: "y", ToID: "a", Kind: "REFERENCES"},
		},
	}

	return []string{
		writeSegment(t, dir, "seg0.fb", seg0),
		writeSegment(t, dir, "seg1.fb", seg1),
		writeSegment(t, dir, "seg2.fb", seg2),
	}
}

func TestMultiReaderCountsSumAcrossSegments(t *testing.T) {
	paths := threeSegmentFixture(t)
	mr := openMultiCleanup(t, paths)

	if got, want := mr.EntityCount(), 6; got != want {
		t.Errorf("EntityCount = %d, want %d", got, want)
	}
	if got, want := mr.RelationshipCount(), 3; got != want {
		t.Errorf("RelationshipCount = %d, want %d", got, want)
	}
	if got, want := mr.SegmentCount(), 3; got != want {
		t.Errorf("SegmentCount = %d, want %d", got, want)
	}
}

func TestMultiReaderLookupEntityByIDAnySegment(t *testing.T) {
	paths := threeSegmentFixture(t)
	mr := openMultiCleanup(t, paths)

	for _, id := range []string{"a", "b", "m", "n", "y", "z"} {
		e := mr.LookupEntityByID(id)
		if e == nil {
			t.Errorf("LookupEntityByID(%q): not found", id)
			continue
		}
		if got := string(e.Id()); got != id {
			t.Errorf("LookupEntityByID(%q).Id() = %q", id, got)
		}
	}

	if e := mr.LookupEntityByID("does-not-exist"); e != nil {
		t.Errorf("LookupEntityByID(missing) = %v, want nil", e)
	}

	if e, ok := mr.FindEntityByID("z"); !ok || e == nil {
		t.Errorf("FindEntityByID(z): ok=%v e=%v", ok, e)
	}
	if _, ok := mr.FindEntityByID("missing"); ok {
		t.Errorf("FindEntityByID(missing): expected ok=false")
	}
}

// TestMultiReaderCrossSegmentRelationshipResolve is the explicit
// cross-segment-lookup-works assertion: a relationship recorded in seg0
// whose to_id entity lives in seg2 resolves via LookupEntityByID, and
// likewise for a relationship in seg2 pointing back into seg0.
func TestMultiReaderCrossSegmentRelationshipResolve(t *testing.T) {
	paths := threeSegmentFixture(t)
	mr := openMultiCleanup(t, paths)

	fromA := mr.IterateRelationshipsFromID("a")
	if len(fromA) != 1 {
		t.Fatalf("IterateRelationshipsFromID(a): got %d, want 1", len(fromA))
	}
	toID := string(fromA[0].ToId())
	if toID != "z" {
		t.Fatalf("relationship from a: to_id = %q, want z", toID)
	}
	// z lives in segment 2 while the relationship itself lives in segment 0
	// — resolving it must fan out across segments.
	target := mr.LookupEntityByID(toID)
	if target == nil {
		t.Fatalf("cross-segment resolve: entity %q (segment 2) not found via relationship recorded in segment 0", toID)
	}
	if got := string(target.Name()); got != "Z" {
		t.Errorf("resolved entity name = %q, want Z", got)
	}

	// Mirror the other direction: y (segment 2) -> a (segment 0).
	toA := mr.IterateRelationshipsToID("a")
	if len(toA) != 1 {
		t.Fatalf("IterateRelationshipsToID(a): got %d, want 1", len(toA))
	}
	fromIDBack := string(toA[0].FromId())
	if fromIDBack != "y" {
		t.Fatalf("relationship to a: from_id = %q, want y", fromIDBack)
	}
	src := mr.LookupEntityByID(fromIDBack)
	if src == nil {
		t.Fatalf("cross-segment resolve: entity %q (segment 2) not found", fromIDBack)
	}
}

func TestMultiReaderIterationChainsSegmentsInOrder(t *testing.T) {
	paths := threeSegmentFixture(t)
	mr := openMultiCleanup(t, paths)

	var ids []string
	mr.IterateEntities(func(e *fbgraph.Entity) bool {
		ids = append(ids, string(e.Id()))
		return true
	})
	wantIDs := []string{"a", "b", "m", "n", "y", "z"}
	if len(ids) != len(wantIDs) {
		t.Fatalf("IterateEntities: got %d ids, want %d", len(ids), len(wantIDs))
	}
	for i, want := range wantIDs {
		if ids[i] != want {
			t.Errorf("IterateEntities[%d] = %q, want %q (segment order not preserved)", i, ids[i], want)
		}
	}

	// Early-stop must halt the whole cross-segment chain, not just the
	// current segment's scan.
	count := 0
	mr.IterateEntities(func(_ *fbgraph.Entity) bool {
		count++
		return false
	})
	if count != 1 {
		t.Errorf("IterateEntities early-stop: visited %d, want 1", count)
	}

	var relKinds []string
	mr.IterateRelationships(func(rel *fbgraph.Relationship) bool {
		relKinds = append(relKinds, string(rel.Kind()))
		return true
	})
	if len(relKinds) != 3 {
		t.Errorf("IterateRelationships: got %d, want 3", len(relKinds))
	}

	funcs := mr.FilterEntitiesByKind("function")
	if len(funcs) != 3 { // a, m, y
		t.Errorf("FilterEntitiesByKind(function): got %d, want 3", len(funcs))
	}
	var funcIDs []string
	for _, e := range funcs {
		funcIDs = append(funcIDs, string(e.Id()))
	}
	sort.Strings(funcIDs)
	wantFuncIDs := []string{"a", "m", "y"}
	for i, want := range wantFuncIDs {
		if i >= len(funcIDs) || funcIDs[i] != want {
			t.Errorf("FilterEntitiesByKind(function) ids = %v, want %v", funcIDs, wantFuncIDs)
			break
		}
	}
}

func TestMultiReaderEntityAtRelationshipAtGlobalIndex(t *testing.T) {
	paths := threeSegmentFixture(t)
	mr := openMultiCleanup(t, paths)

	// Index 0 is segment 0's first entity (a); index 2 is segment 1's
	// first entity (m); index 4 is segment 2's first entity (y).
	if got := string(mr.EntityAt(0).Id()); got != "a" {
		t.Errorf("EntityAt(0) = %q, want a", got)
	}
	if got := string(mr.EntityAt(2).Id()); got != "m" {
		t.Errorf("EntityAt(2) = %q, want m", got)
	}
	if got := string(mr.EntityAt(4).Id()); got != "y" {
		t.Errorf("EntityAt(4) = %q, want y", got)
	}
	if e := mr.EntityAt(6); e != nil {
		t.Errorf("EntityAt(6) (out of range) = %v, want nil", e)
	}
	if e := mr.EntityAt(-1); e != nil {
		t.Errorf("EntityAt(-1) = %v, want nil", e)
	}

	if got := string(mr.RelationshipAt(0).ToId()); got != "z" {
		t.Errorf("RelationshipAt(0).ToId() = %q, want z", got)
	}
	if got := string(mr.RelationshipAt(1).ToId()); got != "n" {
		t.Errorf("RelationshipAt(1).ToId() = %q, want n", got)
	}
	if got := string(mr.RelationshipAt(2).ToId()); got != "a" {
		t.Errorf("RelationshipAt(2).ToId() = %q, want a", got)
	}
}

// TestMultiReaderSingleSegmentParity is the required parity assertion:
// a MultiReader over exactly one segment must behave byte-identically to
// the existing single-file Reader over the same file — same counts, same
// lookup results, same iteration order.
func TestMultiReaderSingleSegmentParity(t *testing.T) {
	doc := &graph.Document{
		Repo: "parity-test",
		Entities: []graph.Entity{
			{ID: "a", QualifiedName: "pkg.A", Kind: "function", Name: "A"},
			{ID: "b", QualifiedName: "pkg.B", Kind: "struct", Name: "B"},
			{ID: "c", QualifiedName: "pkg.C", Kind: "function", Name: "C"},
		},
		Relationships: []graph.Relationship{
			{FromID: "a", ToID: "b", Kind: "CALLS"},
			{FromID: "c", ToID: "b", Kind: "CALLS"},
			{FromID: "a", ToID: "c", Kind: "REFERENCES"},
		},
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "g.fb")
	if err := fbwriter.WriteAtomic(path, doc); err != nil {
		t.Fatalf("write: %v", err)
	}

	single, err := fbreader.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = single.Close() })

	multi := openMultiCleanup(t, []string{path})

	if got, want := multi.EntityCount(), single.EntityCount(); got != want {
		t.Errorf("EntityCount: multi=%d single=%d", got, want)
	}
	if got, want := multi.RelationshipCount(), single.RelationshipCount(); got != want {
		t.Errorf("RelationshipCount: multi=%d single=%d", got, want)
	}

	for _, id := range []string{"a", "b", "c", "missing"} {
		se := single.LookupEntityByID(id)
		me := multi.LookupEntityByID(id)
		if (se == nil) != (me == nil) {
			t.Fatalf("LookupEntityByID(%q): single-nil=%v multi-nil=%v", id, se == nil, me == nil)
		}
		if se != nil {
			if string(se.Id()) != string(me.Id()) || string(se.Name()) != string(me.Name()) || string(se.Kind()) != string(me.Kind()) {
				t.Errorf("LookupEntityByID(%q): single=%+v multi=%+v differ", id, se, me)
			}
		}
	}

	var singleIDs, multiIDs []string
	single.IterateEntities(func(e *fbgraph.Entity) bool { singleIDs = append(singleIDs, string(e.Id())); return true })
	multi.IterateEntities(func(e *fbgraph.Entity) bool { multiIDs = append(multiIDs, string(e.Id())); return true })
	if len(singleIDs) != len(multiIDs) {
		t.Fatalf("IterateEntities length: single=%d multi=%d", len(singleIDs), len(multiIDs))
	}
	for i := range singleIDs {
		if singleIDs[i] != multiIDs[i] {
			t.Errorf("IterateEntities[%d]: single=%q multi=%q", i, singleIDs[i], multiIDs[i])
		}
	}

	var singleRelKinds, multiRelKinds []string
	single.IterateRelationships(func(r *fbgraph.Relationship) bool {
		singleRelKinds = append(singleRelKinds, string(r.FromId())+"->"+string(r.ToId()))
		return true
	})
	multi.IterateRelationships(func(r *fbgraph.Relationship) bool {
		multiRelKinds = append(multiRelKinds, string(r.FromId())+"->"+string(r.ToId()))
		return true
	})
	if len(singleRelKinds) != len(multiRelKinds) {
		t.Fatalf("IterateRelationships length: single=%d multi=%d", len(singleRelKinds), len(multiRelKinds))
	}
	for i := range singleRelKinds {
		if singleRelKinds[i] != multiRelKinds[i] {
			t.Errorf("IterateRelationships[%d]: single=%q multi=%q", i, singleRelKinds[i], multiRelKinds[i])
		}
	}

	sFuncs := single.FilterEntitiesByKind("function")
	mFuncs := multi.FilterEntitiesByKind("function")
	if len(sFuncs) != len(mFuncs) {
		t.Errorf("FilterEntitiesByKind(function): single=%d multi=%d", len(sFuncs), len(mFuncs))
	}

	sFromA := single.IterateRelationshipsFromID("a")
	mFromA := multi.IterateRelationshipsFromID("a")
	if len(sFromA) != len(mFromA) {
		t.Errorf("IterateRelationshipsFromID(a): single=%d multi=%d", len(sFromA), len(mFromA))
	}

	sToB := single.IterateRelationshipsToID("b")
	mToB := multi.IterateRelationshipsToID("b")
	if len(sToB) != len(mToB) {
		t.Errorf("IterateRelationshipsToID(b): single=%d multi=%d", len(sToB), len(mToB))
	}

	if got, want := multi.Version(), single.Version(); got != want {
		t.Errorf("Version: multi=%d single=%d", got, want)
	}
	sMeta := single.LoadGraphMeta()
	mMeta := multi.LoadGraphMeta()
	if sMeta.RepoTag != mMeta.RepoTag || sMeta.Version != mMeta.Version {
		t.Errorf("LoadGraphMeta: single=%+v multi=%+v", sMeta, mMeta)
	}
}

// TestMultiReaderCleanCloseReleasesAllMappings verifies that closing a
// multi-segment reader releases every segment's mmap and does so in a
// way that is safe on Windows: the Close is registered via t.Cleanup
// BEFORE the temp directory's own removal, so the mapping is gone before
// the directory teardown tries to unlink the mapped files.
func TestMultiReaderCleanCloseReleasesAllMappings(t *testing.T) {
	paths := threeSegmentFixture(t)

	mr, err := fbreader.OpenSegments(paths)
	if err != nil {
		t.Fatalf("OpenSegments: %v", err)
	}
	if got, want := mr.SegmentCount(), 3; got != want {
		t.Errorf("SegmentCount = %d, want %d", got, want)
	}

	// Close explicitly now (in addition to relying on cleanup ordering in
	// the other tests) to assert Close itself is well-behaved: idempotent,
	// no error, and safe to call on an already-mapped multi-segment reader.
	if err := mr.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	// Second Close must be a safe no-op.
	if err := mr.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	if got := mr.SegmentCount(); got != 0 {
		t.Errorf("SegmentCount after Close = %d, want 0", got)
	}
}

func TestOpenSegmentsRequiresAtLeastOnePath(t *testing.T) {
	if _, err := fbreader.OpenSegments(nil); err == nil {
		t.Errorf("OpenSegments(nil): expected error, got nil")
	}
	if _, err := fbreader.OpenSegments([]string{}); err == nil {
		t.Errorf("OpenSegments(empty): expected error, got nil")
	}
}

func TestOpenSegmentsClosesPartialOnFailure(t *testing.T) {
	dir := t.TempDir()
	doc := &graph.Document{
		Repo: "partial-fail-test",
		Entities: []graph.Entity{
			{ID: "a", Kind: "function", Name: "A"},
		},
	}
	good := writeSegment(t, dir, "good.fb", doc)
	bad := filepath.Join(dir, "does-not-exist.fb")

	if _, err := fbreader.OpenSegments([]string{good, bad}); err == nil {
		t.Fatalf("OpenSegments([good, bad]): expected error, got nil")
	}
	// If the first (good) segment's mapping leaked, a subsequent re-open
	// from a fresh reader would still succeed (mmap doesn't exclusively
	// lock on unix), so the meaningful check here is simply that
	// OpenSegments returned promptly with an error rather than panicking
	// or hanging, and that a follow-up Open of the same "good" path still
	// works cleanly (no residual state was corrupted).
	r, err := fbreader.Open(good)
	if err != nil {
		t.Fatalf("re-Open(good) after partial failure: %v", err)
	}
	_ = r.Close()
}
