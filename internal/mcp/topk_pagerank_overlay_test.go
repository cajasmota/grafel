package mcp

// topk_pagerank_overlay_test.go — overlay-aware regression test for the
// getTopKPageRank live bug introduced by ADR-0027 Cutover PR1 (#5865).
//
// Per-repo Pass-4 (the code that used to compute + persist real per-entity
// PageRank into graph.fb) was removed when the group-scope algo pass
// (A1-A3, #5349) replaced it. graph.fb's PageRank/CommunityID/Centrality/
// god/articulation fields are now PERMANENT SENTINELS — never populated by
// the indexer. The one authoritative source of real PageRank is the
// <group>-algo.json overlay, which applyGroupAlgoOverlay (state.go) stamps
// onto lr.Doc.Entities[i] IN PLACE at load. It never touches the mmap'd
// graph.fb bytes the Reader serves.
//
// PR1 re-sourced getTopKPageRank to read lr.Reader's Pagerank() scalar
// whenever a Reader is present — which is always true for a live-indexed
// repo. Since that scalar is a permanent sentinel (0), top-K collapsed to
// id order, silently handing pickFallback the wrong entity. The Document-
// sourced parity test PR1 shipped with (TestTopKPageRankReaderParity_PR1)
// missed this because its fixture bakes real PageRank directly into
// graph.fb via fbwriter — bypassing the sentinel entirely, so both sides
// read the same (non-zero) values and parity trivially holds.
//
// This test instead reproduces the REAL production shape: a graph.fb with
// sentinel (zero) PageRank for every entity, PLUS a Doc-only overlay stamp
// (the applyGroupAlgoOverlay data path) that sets distinct real PageRank
// values on lr.Doc.Entities. It asserts getTopKPageRank returns entities in
// overlay-PageRank order — which is only possible when the Doc, not the
// Reader, is the source of truth.

import (
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbreader"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// overlayAwareTopKDoc returns a Document whose entities all carry a nil
// PageRank (graph.fb's sentinel shape post-Pass-4-removal): none of them are
// algo-annotated, mirroring what a freshly-indexed repo looks like today.
func overlayAwareTopKDoc() *graph.Document {
	mk := func(id, name string) graph.Entity {
		return graph.Entity{
			ID: id, Name: name, QualifiedName: "pkg." + name, Kind: "FUNCTION",
			SourceFile: "pkg/" + name + ".go", Language: "go", PageRank: nil,
		}
	}
	return &graph.Document{
		Entities: []graph.Entity{
			mk("id::alpha", "Alpha"),
			mk("id::bravo", "Bravo"),
			mk("id::charlie", "Charlie"),
			mk("id::delta", "Delta"),
			mk("id::echo", "Echo"),
		},
	}
}

// buildOverlayAwareLoadedRepo writes overlayAwareTopKDoc() to a real graph.fb
// (sentinel PageRank throughout, exactly as a live-indexed repo's fb would be
// post-Pass-4-removal), loads it back into a Document AND opens an mmap
// Reader over the same bytes (both sentinel), then applies the
// applyGroupAlgoOverlay DATA PATH — the exact field-stamp
// (`ents[i].PageRank = &pr`) that function performs — directly onto the
// loaded Doc's entities with distinct, non-sentinel values. The Reader is
// left untouched (as production always leaves it): it keeps serving the
// sentinel graph.fb bytes.
func buildOverlayAwareLoadedRepo(t *testing.T) *LoadedRepo {
	t.Helper()
	dir := t.TempDir()
	fbPath := filepath.Join(dir, "graph.fb")
	if err := fbwriter.WriteAtomic(fbPath, overlayAwareTopKDoc()); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}
	doc, err := graph.LoadGraphFromDir(dir)
	if err != nil {
		t.Fatalf("LoadGraphFromDir: %v", err)
	}
	// Sanity: confirm the freshly loaded Doc really does carry the sentinel
	// shape (PageRank nil for every entity) before we overlay-stamp it — this
	// pins the precondition the whole test depends on.
	for i := range doc.Entities {
		if doc.Entities[i].PageRank != nil {
			t.Fatalf("precondition: entity %s has non-nil PageRank before overlay stamp: %v",
				doc.Entities[i].ID, *doc.Entities[i].PageRank)
		}
	}
	r, err := fbreader.Open(fbPath)
	if err != nil {
		t.Fatalf("fbreader.Open: %v", err)
	}
	t.Cleanup(func() { r.Close() })

	// applyGroupAlgoOverlay's data path: stamp distinct PageRank pointers onto
	// lr.Doc.Entities[i] by ID, exactly as the <group>-algo.json overlay apply
	// loop does (state.go applyGroupAlgoOverlay). Deliberately distinct,
	// strictly descending values so top-K order is unambiguous. "id::echo" is
	// left un-stamped (stays at the graph.fb sentinel) to mirror an entity the
	// group-algo pass never covered.
	overlay := map[string]float64{
		"id::charlie": 0.9,
		"id::alpha":   0.5,
		"id::delta":   0.2,
		"id::bravo":   0.05,
	}
	for i := range doc.Entities {
		pr, ok := overlay[doc.Entities[i].ID]
		if !ok {
			continue
		}
		v := pr
		doc.Entities[i].PageRank = &v
	}

	return &LoadedRepo{Repo: "repo", Doc: doc, Reader: r}
}

// TestGetTopKPageRank_OverlayOrder_NotReaderSentinel is the regression test
// for the ADR-0027 Cutover PR1 live bug (#5865): getTopKPageRank must return
// entities in the OVERLAY-stamped PageRank order (Doc-sourced), not
// id/sentinel order (which is what a Reader-sourced build produces because
// the FB Pagerank() scalar is a permanent sentinel).
//
// This test FAILS against the pre-fix getTopKPageRank (which prefers
// buildTopKPageRankFromReader whenever lr.Reader != nil): every entity's FB
// Pagerank() scalar is 0, so the Reader-sourced top-K collapses to
// insertion/id order (alpha, bravo, charlie, delta, echo) instead of the
// overlay order (charlie, alpha, delta, bravo, ...).
func TestGetTopKPageRank_OverlayOrder_NotReaderSentinel(t *testing.T) {
	lr := buildOverlayAwareLoadedRepo(t)

	got := lr.getTopKPageRank()

	want := []string{"id::charlie", "id::alpha", "id::delta", "id::bravo"}
	if len(got) < len(want) {
		t.Fatalf("getTopKPageRank returned too few entities: got %v, want at least %v", got, want)
	}
	for i, id := range want {
		if got[i] != id {
			t.Fatalf("getTopKPageRank()[%d] = %q, want %q (overlay-PageRank order)\nfull got=%v",
				i, got[i], id, got)
		}
	}

	// The un-stamped entity (id::echo, still at the graph.fb sentinel) must
	// rank below every overlay-stamped entity.
	echoPos, wantPos := -1, -1
	for i, id := range got {
		if id == "id::echo" {
			echoPos = i
		}
		if id == "id::delta" {
			wantPos = i
		}
	}
	if echoPos == -1 || wantPos == -1 || echoPos < wantPos {
		t.Fatalf("expected id::echo (un-stamped sentinel) to rank below id::delta (overlay-stamped); got=%v", got)
	}
}

// TestGetTopKPageRank_OverlayOrder_ReaderSourcedWouldBeWrong documents (via a
// direct comparison, not a getTopKPageRank call) exactly why
// buildTopKPageRankFromReader must not be used here: reading the same
// graph.fb's Pagerank() scalar sees only the sentinel and cannot reproduce
// the overlay order the Doc-sourced build correctly returns.
func TestGetTopKPageRank_OverlayOrder_ReaderSourcedWouldBeWrong(t *testing.T) {
	lr := buildOverlayAwareLoadedRepo(t)

	docOrder := buildTopKPageRank(lr.Doc, 64)
	readerOrder := buildTopKPageRankFromReader(lr.Reader, 64)

	if len(docOrder) == 0 || len(readerOrder) == 0 {
		t.Fatalf("expected non-empty top-K on both sides: doc=%v reader=%v", docOrder, readerOrder)
	}
	if docOrder[0] != "id::charlie" {
		t.Fatalf("Doc-sourced top-1 = %q, want id::charlie (overlay PageRank 0.9)", docOrder[0])
	}
	if readerOrder[0] == "id::charlie" {
		t.Fatalf("Reader-sourced top-1 unexpectedly matched the overlay winner (id::charlie); " +
			"this fixture is supposed to prove the Reader CANNOT see the overlay (sentinel-only fb)")
	}
}
