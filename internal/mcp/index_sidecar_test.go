package mcp

// index_sidecar_test.go — correctness tests for the persisted derived-index
// sidecar (#3368). The contract under test:
//
//   1. A sidecar written from a Document, then loaded against that resident
//      Document, reconstructs indexes equal to the build-from-Doc path.
//   2. A sidecar whose identity hash no longer matches the on-disk graph.fb is
//      rejected (treated as a miss) — a stale index is NEVER served.
//   3. The lazy getters PREFER a valid sidecar: they reconstruct instead of
//      rebuilding, asserted via the idxBuiltFromDoc build-counter.

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/graph/fbwriter"
)

// sidecarFixtureDoc returns a small but structurally-complete document touching
// every persisted index: in/out adjacency, CALLS, STEP_IN_PROCESS, and PageRank.
func sidecarFixtureDoc() *graph.Document {
	return &graph.Document{
		Repo: "fix",
		Entities: []graph.Entity{
			{ID: "a", Name: "A", Kind: "Function", SourceFile: "a.go", PageRank: pr(0.9)},
			{ID: "b", Name: "B", Kind: "Function", SourceFile: "b.go", PageRank: pr(0.5)},
			{ID: "c", Name: "C", Kind: "Function", SourceFile: "c.go", PageRank: pr(0.1)},
			{ID: "p", Name: "P", Kind: "process_flow", SourceFile: "p.go", PageRank: pr(0.3)},
		},
		Relationships: []graph.Relationship{
			{ID: "r1", FromID: "a", ToID: "b", Kind: "CALLS", Properties: map[string]string{"count": "3"}},
			{ID: "r2", FromID: "a", ToID: "c", Kind: "CALLS"},
			{ID: "r3", FromID: "b", ToID: "c", Kind: "IMPORTS"},
			{ID: "r4", FromID: "p", ToID: "a", Kind: "STEP_IN_PROCESS", Properties: map[string]string{"step_index": "0"}},
			{ID: "r5", FromID: "p", ToID: "b", Kind: "STEP_IN_PROCESS", Properties: map[string]string{"step_index": "1"}},
		},
	}
}

// withSidecarConsume toggles the package-level sidecar-consume gate for one test
// and restores it on cleanup. The gate defaults OFF (build-from-Doc is faster);
// the consume-path tests flip it ON to exercise the deserialize path.
func withSidecarConsume(t *testing.T, on bool) {
	t.Helper()
	prev := sidecarConsumeEnabled
	sidecarConsumeEnabled = on
	t.Cleanup(func() { sidecarConsumeEnabled = prev })
}

// writeFBFixture writes graph.fb into a fresh temp state dir and returns the dir.
func writeFBFixture(t *testing.T, doc *graph.Document) string {
	t.Helper()
	dir := t.TempDir()
	fbPath := filepath.Join(dir, "graph.fb")
	if err := fbwriter.WriteAtomic(fbPath, doc); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}
	return dir
}

// adjEqual compares two adjacency indexes for out/in equality.
func adjEqual(t *testing.T, label string, want, got *adjacency) {
	t.Helper()
	if !reflect.DeepEqual(want.out, got.out) {
		t.Errorf("%s adjacency.out mismatch:\nwant %#v\ngot  %#v", label, want.out, got.out)
	}
	if !reflect.DeepEqual(want.in, got.in) {
		t.Errorf("%s adjacency.in mismatch:\nwant %#v\ngot  %#v", label, want.in, got.in)
	}
}

// TestSidecar_RoundTripEqualsBuild asserts a written→loaded sidecar reconstructs
// indexes equal to the build-from-Doc path (GOAL-1 correctness invariant).
func TestSidecar_RoundTripEqualsBuild(t *testing.T) {
	doc := sidecarFixtureDoc()
	dir := writeFBFixture(t, doc)

	if err := WriteIndexSidecar(dir, "fix", doc); err != nil {
		t.Fatalf("WriteIndexSidecar: %v", err)
	}

	sc, err := loadValidSidecar(dir, doc)
	if err != nil {
		t.Fatalf("loadValidSidecar: %v", err)
	}
	if sc == nil {
		t.Fatal("expected a valid sidecar, got nil")
	}

	adjEqual(t, "roundtrip", buildAdjacency(doc, "fix"), sc.adj)

	if !reflect.DeepEqual(buildCallsAdjacency(doc), sc.callsAdj) {
		t.Errorf("callsAdj mismatch:\nwant %#v\ngot  %#v", buildCallsAdjacency(doc), sc.callsAdj)
	}
	if !reflect.DeepEqual(buildStepAdjacency(doc), sc.stepAdj) {
		t.Errorf("stepAdj mismatch:\nwant %#v\ngot  %#v", buildStepAdjacency(doc), sc.stepAdj)
	}
	if !reflect.DeepEqual(buildTopKPageRank(doc, 64), sc.topKPageRank) {
		t.Errorf("topKPageRank mismatch:\nwant %v\ngot  %v", buildTopKPageRank(doc, 64), sc.topKPageRank)
	}
}

// TestSidecar_StaleRejected asserts a sidecar is rejected once graph.fb changes
// (identity hash mismatch) — never serving a stale index.
func TestSidecar_StaleRejected(t *testing.T) {
	doc := sidecarFixtureDoc()
	dir := writeFBFixture(t, doc)
	if err := WriteIndexSidecar(dir, "fix", doc); err != nil {
		t.Fatalf("WriteIndexSidecar: %v", err)
	}
	if sc, _ := loadValidSidecar(dir, doc); sc == nil {
		t.Fatal("precondition: sidecar should load before graph.fb changes")
	}

	// Mutate graph.fb (add an entity + edge) WITHOUT rewriting the sidecar.
	doc2 := sidecarFixtureDoc()
	doc2.Entities = append(doc2.Entities, graph.Entity{ID: "z", Name: "Z", Kind: "Function", SourceFile: "z.go"})
	doc2.Relationships = append(doc2.Relationships, graph.Relationship{ID: "rz", FromID: "a", ToID: "z", Kind: "CALLS"})
	if err := fbwriter.WriteAtomic(filepath.Join(dir, "graph.fb"), doc2); err != nil {
		t.Fatalf("rewrite graph.fb: %v", err)
	}

	sc, err := loadValidSidecar(dir, doc2)
	if err != nil {
		t.Fatalf("loadValidSidecar after change: unexpected error %v", err)
	}
	if sc != nil {
		t.Fatal("stale sidecar was served after graph.fb changed; expected nil (miss)")
	}
}

// TestSidecar_MissingIsCleanMiss asserts a missing sidecar is a quiet miss.
func TestSidecar_MissingIsCleanMiss(t *testing.T) {
	doc := sidecarFixtureDoc()
	dir := writeFBFixture(t, doc)
	sc, err := loadValidSidecar(dir, doc)
	if err != nil {
		t.Fatalf("missing sidecar should not error, got: %v", err)
	}
	if sc != nil {
		t.Fatal("missing sidecar should yield nil")
	}
}

// TestSidecar_CorruptIsCleanMiss asserts a garbage sidecar is rejected, not
// mis-decoded, and falls back cleanly.
func TestSidecar_CorruptIsCleanMiss(t *testing.T) {
	doc := sidecarFixtureDoc()
	dir := writeFBFixture(t, doc)
	if err := os.WriteFile(indexSidecarPath(dir), []byte("not a valid sidecar stream at all"), 0o644); err != nil {
		t.Fatalf("write corrupt sidecar: %v", err)
	}
	sc, _ := loadValidSidecar(dir, doc)
	if sc != nil {
		t.Fatal("corrupt sidecar should yield nil (miss)")
	}
}

// TestSidecar_DocShapeMismatchMiss asserts that a sidecar valid by hash but
// loaded against a Doc with a different entity count is rejected (defense in
// depth — the hash already catches this, but the count guard is cheap).
func TestSidecar_DocShapeMismatchMiss(t *testing.T) {
	doc := sidecarFixtureDoc()
	dir := writeFBFixture(t, doc)
	if err := WriteIndexSidecar(dir, "fix", doc); err != nil {
		t.Fatalf("WriteIndexSidecar: %v", err)
	}
	shrunk := sidecarFixtureDoc()
	shrunk.Entities = shrunk.Entities[:len(shrunk.Entities)-1]
	sc, _ := loadValidSidecar(dir, shrunk)
	if sc != nil {
		t.Fatal("sidecar should be rejected when Doc entity count diverges")
	}
}

// TestSidecar_GettersPreferSidecarNoRebuild asserts the lazy getters reconstruct
// from a valid sidecar and do NOT rebuild from Doc (idxBuiltFromDoc stays 0).
func TestSidecar_GettersPreferSidecarNoRebuild(t *testing.T) {
	withSidecarConsume(t, true)
	doc := sidecarFixtureDoc()
	dir := writeFBFixture(t, doc)
	if err := WriteIndexSidecar(dir, "fix", doc); err != nil {
		t.Fatalf("WriteIndexSidecar: %v", err)
	}

	lr := &LoadedRepo{Repo: "fix", Doc: doc, GraphFile: filepath.Join(dir, "graph.fb")}

	_ = lr.getAdjacency()
	_ = lr.getCallsAdj()
	_ = lr.getStepAdj()
	_ = lr.getTopKPageRank()

	if lr.idxBuiltFromDoc != 0 {
		t.Fatalf("expected 0 build-from-Doc with a valid sidecar, got %d", lr.idxBuiltFromDoc)
	}

	adjEqual(t, "getter", buildAdjacency(doc, "fix"), lr.getAdjacency())
	if !reflect.DeepEqual(buildCallsAdjacency(doc), lr.getCallsAdj()) {
		t.Error("getCallsAdj from sidecar diverged from build")
	}
	if !reflect.DeepEqual(buildStepAdjacency(doc), lr.getStepAdj()) {
		t.Error("getStepAdj from sidecar diverged from build")
	}
	if !reflect.DeepEqual(buildTopKPageRank(doc, 64), lr.getTopKPageRank()) {
		t.Error("getTopKPageRank from sidecar diverged from build")
	}
}

// TestSidecar_GettersFallBackWhenMissing asserts that with no sidecar the getters
// rebuild from Doc (idxBuiltFromDoc increments) and still serve correct data.
func TestSidecar_GettersFallBackWhenMissing(t *testing.T) {
	doc := sidecarFixtureDoc()
	dir := writeFBFixture(t, doc) // graph.fb present, but NO sidecar written

	lr := &LoadedRepo{Repo: "fix", Doc: doc, GraphFile: filepath.Join(dir, "graph.fb")}

	_ = lr.getAdjacency()
	_ = lr.getCallsAdj()
	_ = lr.getStepAdj()
	_ = lr.getTopKPageRank()

	if lr.idxBuiltFromDoc != 4 {
		t.Fatalf("expected 4 build-from-Doc (no sidecar), got %d", lr.idxBuiltFromDoc)
	}
	if !reflect.DeepEqual(buildStepAdjacency(doc), lr.getStepAdj()) {
		t.Error("fallback getStepAdj diverged from build")
	}
}

// TestSidecar_ResetRearms asserts resetIndexes re-arms the sidecar load so a
// cached sidecar cannot survive a reload.
func TestSidecar_ResetRearms(t *testing.T) {
	withSidecarConsume(t, true)
	doc := sidecarFixtureDoc()
	dir := writeFBFixture(t, doc)
	if err := WriteIndexSidecar(dir, "fix", doc); err != nil {
		t.Fatalf("WriteIndexSidecar: %v", err)
	}
	lr := &LoadedRepo{Repo: "fix", Doc: doc, GraphFile: filepath.Join(dir, "graph.fb")}
	_ = lr.getAdjacency()
	if lr.sidecar == nil {
		t.Fatal("expected cached sidecar after first access")
	}
	lr.resetIndexes()
	if lr.sidecar != nil {
		t.Fatal("resetIndexes must clear the cached sidecar")
	}
	if lr.idxBuiltFromDoc != 0 {
		t.Fatal("resetIndexes must zero the build counter")
	}
}

// --- timing benchmarks (before/after first-use) ----------------------------

// largeFixtureDoc builds a synthetic doc with n entities and ~2n relationships
// (CALLS + IMPORTS) plus PageRank, to size the build-vs-load first-use cost.
func largeFixtureDoc(n int) *graph.Document {
	doc := &graph.Document{Repo: "big", Entities: make([]graph.Entity, n)}
	for i := 0; i < n; i++ {
		id := "e" + itoaTest(i)
		doc.Entities[i] = graph.Entity{ID: id, Name: id, Kind: "Function", SourceFile: id + ".go", PageRank: pr(float64(i % 97))}
	}
	for i := 0; i < n; i++ {
		from := "e" + itoaTest(i)
		to := "e" + itoaTest((i*7+1)%n)
		to2 := "e" + itoaTest((i*13+3)%n)
		doc.Relationships = append(doc.Relationships,
			graph.Relationship{ID: "c" + itoaTest(i), FromID: from, ToID: to, Kind: "CALLS"},
			graph.Relationship{ID: "i" + itoaTest(i), FromID: from, ToID: to2, Kind: "IMPORTS"},
		)
	}
	return doc
}

func itoaTest(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	return string(b[p:])
}

// BenchmarkFirstUse_BuildFromDoc measures the first-use cost the lazy getters
// pay WITHOUT a sidecar: full build-from-Doc (the pre-#3368 residual cost).
func BenchmarkFirstUse_BuildFromDoc(b *testing.B) {
	doc := largeFixtureDoc(20000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = buildAdjacency(doc, "big")
		_ = buildCallsAdjacency(doc)
		_ = buildStepAdjacency(doc)
		_ = buildTopKPageRank(doc, 64)
	}
}

// BenchmarkFirstUse_LoadSidecar measures the first-use cost WITH a sidecar:
// validate identity + reconstruct all four indexes from the int-indexed blob,
// reusing the resident Doc's strings.
func BenchmarkFirstUse_LoadSidecar(b *testing.B) {
	doc := largeFixtureDoc(20000)
	dir := b.TempDir()
	if err := fbwriter.WriteAtomic(filepath.Join(dir, "graph.fb"), doc); err != nil {
		b.Fatalf("write graph.fb: %v", err)
	}
	if err := WriteIndexSidecar(dir, "big", doc); err != nil {
		b.Fatalf("WriteIndexSidecar: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sc, err := loadValidSidecar(dir, doc)
		if err != nil || sc == nil {
			b.Fatalf("loadValidSidecar: sc=%v err=%v", sc, err)
		}
	}
}
