package mcp

// ADR-0027 mmap-cutover PR7 (memory epic #5850): the opt-in flip.
//
// These tests prove that a HEADER-ONLY loaded Document (Entities/Relationships
// left empty) paired with a resident fbreader.Reader serves reads END-TO-END
// byte-identically to the full-Doc (flag-OFF) path — i.e. the whole read
// surface (getByIDOne / forEachEntity / LabelIndex ByID+Lookup / adjacency /
// BM25 / topK PageRank / overlay field) routes through the Reader and never
// silently collapses to the (now empty) Doc slices. This is the correctness
// proof behind the flip; the perf follow-up (by-Kind indexed scanners) is a
// SEPARATE PR.

import (
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbreader"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// writeSyntheticFB writes a synthetic graph.fb (buildSyntheticDoc) to a temp
// dir and returns the dir and the graph.fb path.
func writeSyntheticFB(t *testing.T, nEnt int) (dir, fbPath string) {
	t.Helper()
	dir = t.TempDir()
	fbPath = filepath.Join(dir, "graph.fb")
	if err := fbwriter.WriteAtomic(fbPath, buildSyntheticDoc(nEnt)); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}
	return dir, fbPath
}

// TestLoadGraphHeaderOnlyFromDir_ShapeAndCounts pins the header-only contract:
// the returned Document is non-nil (the "loaded" sentinel), its Entities and
// Relationships slices are EMPTY, yet Stats carries the true record counts read
// off the graph.fb header — and every other field matches a full load.
func TestLoadGraphHeaderOnlyFromDir_ShapeAndCounts(t *testing.T) {
	dir, _ := writeSyntheticFB(t, 500)

	full, err := graph.LoadGraphFromDir(dir)
	if err != nil {
		t.Fatalf("LoadGraphFromDir: %v", err)
	}
	hdr, err := graph.LoadGraphHeaderOnlyFromDir(dir)
	if err != nil {
		t.Fatalf("LoadGraphHeaderOnlyFromDir: %v", err)
	}

	if hdr == nil {
		t.Fatal("header-only Doc is nil — it MUST stay non-nil (the loaded sentinel)")
	}
	if len(hdr.Entities) != 0 {
		t.Fatalf("header-only Entities len=%d, want 0 (empty slice)", len(hdr.Entities))
	}
	if len(hdr.Relationships) != 0 {
		t.Fatalf("header-only Relationships len=%d, want 0 (empty slice)", len(hdr.Relationships))
	}
	// Stats carries the TRUE counts (sourced from the header), matching a full load.
	if hdr.Stats.Entities != full.Stats.Entities {
		t.Errorf("header-only Stats.Entities=%d, want %d (full)", hdr.Stats.Entities, full.Stats.Entities)
	}
	if hdr.Stats.Relationships != full.Stats.Relationships {
		t.Errorf("header-only Stats.Relationships=%d, want %d (full)", hdr.Stats.Relationships, full.Stats.Relationships)
	}
	if hdr.Stats.Entities != len(full.Entities) {
		t.Errorf("header-only Stats.Entities=%d, want full materialized count %d", hdr.Stats.Entities, len(full.Entities))
	}
	// Meta/version parity.
	if hdr.Version != full.Version || hdr.Repo != full.Repo || !hdr.GeneratedAt.Equal(full.GeneratedAt) {
		t.Errorf("header-only meta mismatch: version %d/%d repo %q/%q genAt %v/%v",
			hdr.Version, full.Version, hdr.Repo, full.Repo, hdr.GeneratedAt, full.GeneratedAt)
	}
}

// newHeaderOnlyReaderRepo builds the flag-ON served-repo shape: a HEADER-ONLY
// Document (empty Entities/Relationships) paired with a resident Reader, its
// Reader-sourced LabelIndex, and a published MapHandle — exactly how
// reloadLocked wires a repo when serveFromMMap() is on.
func newHeaderOnlyReaderRepo(t *testing.T, dir, fbPath string) *LoadedRepo {
	t.Helper()
	hdr, err := graph.LoadGraphHeaderOnlyFromDir(dir)
	if err != nil {
		t.Fatalf("LoadGraphHeaderOnlyFromDir: %v", err)
	}
	if len(hdr.Entities) != 0 {
		t.Fatalf("header-only Doc unexpectedly materialized %d entities", len(hdr.Entities))
	}
	r, err := fbreader.Open(fbPath)
	if err != nil {
		t.Fatalf("fbreader.Open: %v", err)
	}
	t.Cleanup(func() { r.Close() })

	lr := &LoadedRepo{Repo: "corpus", Doc: hdr, GraphFile: fbPath, Reader: r}
	li := BuildLabelIndexFromReader(r, hdr)
	li.readerMu = &lr.readerMu
	h := newMapHandle(r)
	li.handle = h
	lr.LabelIndex = li
	lr.publishHandle(h)
	return lr
}

// newFullDocRepo builds the flag-OFF reference: a fully materialized Document
// with a Doc-sourced LabelIndex.
func newFullDocRepo(t *testing.T, dir string) *LoadedRepo {
	t.Helper()
	doc, err := graph.LoadGraphFromDir(dir)
	if err != nil {
		t.Fatalf("LoadGraphFromDir: %v", err)
	}
	lr := &LoadedRepo{Repo: "corpus", Doc: doc}
	lr.LabelIndex = BuildLabelIndex(doc)
	return lr
}

// TestMMapCutoverFlip_ReadParity_PR7 is the end-to-end correctness proof: with
// GRAFEL_SERVE_FROM_MMAP ON and a header-only Doc + resident Reader, every read
// primitive returns the SAME answer as the flag-OFF full-Doc path over the same
// graph.fb. If any read silently fell back to the (empty) header-only Doc, its
// flag-ON result would be empty/nil and diverge here.
func TestMMapCutoverFlip_ReadParity_PR7(t *testing.T) {
	const nEnt = 600
	dir, fbPath := writeSyntheticFB(t, nEnt)

	// --- Flag-OFF reference (full Doc). ---
	withServeFromMMap(t, false)
	off := newFullDocRepo(t, dir)

	// --- Flag-ON subject (header-only Doc + resident Reader). ---
	withServeFromMMap(t, true)
	on := newHeaderOnlyReaderRepo(t, dir, fbPath)

	wantCount := int(on.Reader.EntityCount())
	if wantCount != len(off.Doc.Entities) {
		t.Fatalf("fixture sanity: reader EntityCount=%d != full doc entity count=%d", wantCount, len(off.Doc.Entities))
	}

	// (1) getByIDOne — single-materialize accessor.
	for _, id := range []string{"ent-000000", "ent-000123", "ent-000599"} {
		onE, onOK := on.getByIDOne(id)
		offE, offOK := off.getByIDOne(id)
		if onOK != offOK {
			t.Fatalf("getByIDOne(%q) ok mismatch: on=%v off=%v", id, onOK, offOK)
		}
		if !onOK {
			continue
		}
		if onE.ID != offE.ID || onE.Name != offE.Name || onE.QualifiedName != offE.QualifiedName ||
			onE.Kind != offE.Kind || onE.SourceFile != offE.SourceFile {
			t.Errorf("getByIDOne(%q) mismatch:\n on = %+v\n off= %+v", id, onE, offE)
		}
	}
	if _, ok := on.getByIDOne("does-not-exist"); ok {
		t.Error("getByIDOne(missing) flag-ON returned ok=true")
	}

	// (2) forEachEntity count == reader EntityCount().
	onN := 0
	on.forEachEntity(func(*graph.Entity) bool { onN++; return true })
	if onN != wantCount {
		t.Errorf("flag-ON forEachEntity counted %d, want EntityCount=%d", onN, wantCount)
	}
	offN := 0
	off.forEachEntity(func(*graph.Entity) bool { offN++; return true })
	if onN != offN {
		t.Errorf("forEachEntity count mismatch: on=%d off=%d", onN, offN)
	}

	// (3) LabelIndex ByID + Lookup.
	for _, id := range []string{"ent-000010", "ent-000400"} {
		onE := on.LabelIndex.ByID(id)
		offE := off.LabelIndex.ByID(id)
		if onE == nil || offE == nil {
			t.Fatalf("LabelIndex.ByID(%q): on=%v off=%v", id, onE, offE)
		}
		if onE.ID != offE.ID || onE.Name != offE.Name {
			t.Errorf("LabelIndex.ByID(%q) mismatch: on=%+v off=%+v", id, onE, offE)
		}
		// Lookup by (unique) qualified name resolves to the same entity.
		onL := on.LabelIndex.Lookup(offE.QualifiedName)
		if onL == nil || onL.ID != offE.ID {
			t.Errorf("LabelIndex.Lookup(%q): on=%v want id=%q", offE.QualifiedName, onL, offE.ID)
		}
	}

	// (4) adjacency / expand — outgoing neighbor sets must match.
	onAdj := on.getAdjacency()
	offAdj := off.getAdjacency()
	for _, id := range []string{"ent-000000", "ent-000050", "ent-000300"} {
		onTargets := sortedEdgeTargets(onAdj.Outgoing(id))
		offTargets := sortedEdgeTargets(offAdj.Outgoing(id))
		if !reflect.DeepEqual(onTargets, offTargets) {
			t.Errorf("adjacency Outgoing(%q) mismatch:\n on = %v\n off= %v", id, onTargets, offTargets)
		}
	}

	// (5) BM25 search — same ranked entity IDs.
	query := "handleOrderRequest customer processor kafka fulfilment pipeline"
	onHits := hitIDs(on.getBM25().Search(query, 10))
	offHits := hitIDs(off.getBM25().Search(query, 10))
	if !reflect.DeepEqual(onHits, offHits) {
		t.Errorf("BM25 search mismatch:\n on = %v\n off= %v", onHits, offHits)
	}
	if len(onHits) == 0 {
		t.Error("BM25 search returned no hits flag-ON — index likely built from the empty Doc")
	}
	// The flag-ON BM25 index must have been built FROM the Reader (one slot per row).
	if on.BM25 == nil || on.BM25.resolve == nil {
		t.Error("flag-ON getBM25 produced no resolver — not built from the Reader")
	} else if len(on.BM25.entities) != wantCount {
		t.Errorf("flag-ON BM25 entities len=%d, want reader EntityCount=%d", len(on.BM25.entities), wantCount)
	}

	// (6) getTopKPageRank — same ordered id list (identical input order + algo).
	onTopK := on.getTopKPageRank()
	offTopK := off.getTopKPageRank()
	if !reflect.DeepEqual(onTopK, offTopK) {
		t.Errorf("getTopKPageRank mismatch:\n on = %v\n off= %v", onTopK, offTopK)
	}
	if len(onTopK) == 0 {
		t.Error("getTopKPageRank returned empty flag-ON")
	}
}

// TestMMapCutoverFlip_OverlayFieldRead_PR7 proves an overlay field (PageRank/
// CommunityID) stamped into the flag-ON LabelIndex side-table is read back
// through the Reader-materialized accessors (getByIDOne / forEachEntity),
// byte-identical to the same overlay stamped onto a full-Doc row flag-OFF.
func TestMMapCutoverFlip_OverlayFieldRead_PR7(t *testing.T) {
	const nEnt = 200
	dir, fbPath := writeSyntheticFB(t, nEnt)
	const targetIdx = 42
	targetID := "ent-000042"
	wantPR := 0.875
	wantCID := 7

	// --- Flag-OFF: overlay stamped directly onto the Doc row. ---
	withServeFromMMap(t, false)
	off := newFullDocRepo(t, dir)
	for i := range off.Doc.Entities {
		if off.Doc.Entities[i].ID == targetID {
			off.Doc.Entities[i].PageRank = &wantPR
			off.Doc.Entities[i].CommunityID = &wantCID
		}
	}

	// --- Flag-ON: overlay carried in the LabelIndex side-table. ---
	withServeFromMMap(t, true)
	on := newHeaderOnlyReaderRepo(t, dir, fbPath)
	on.LabelIndex.overlay = map[int32]entityOverlay{
		targetIdx: {PageRank: &wantPR, CommunityID: &wantCID},
	}

	onE, ok := on.getByIDOne(targetID)
	if !ok {
		t.Fatalf("getByIDOne(%q) flag-ON not found", targetID)
	}
	offE, ok := off.getByIDOne(targetID)
	if !ok {
		t.Fatalf("getByIDOne(%q) flag-OFF not found", targetID)
	}
	if onE.PageRank == nil || offE.PageRank == nil || *onE.PageRank != *offE.PageRank {
		t.Errorf("overlay PageRank mismatch: on=%v off=%v", onE.PageRank, offE.PageRank)
	}
	if onE.CommunityID == nil || offE.CommunityID == nil || *onE.CommunityID != *offE.CommunityID {
		t.Errorf("overlay CommunityID mismatch: on=%v off=%v", onE.CommunityID, offE.CommunityID)
	}

	// The same overlay field must also surface through a forEachEntity scan.
	var scanned *graph.Entity
	on.forEachEntity(func(e *graph.Entity) bool {
		if e.ID == targetID {
			cp := *e
			scanned = &cp
			return false
		}
		return true
	})
	if scanned == nil || scanned.PageRank == nil || *scanned.PageRank != wantPR {
		t.Errorf("forEachEntity overlay PageRank read: got %v, want %v", scanned, wantPR)
	}
}

func sortedEdgeTargets(edges []edge) []string {
	out := make([]string, 0, len(edges))
	for _, e := range edges {
		out = append(out, e.target)
	}
	sort.Strings(out)
	return out
}
