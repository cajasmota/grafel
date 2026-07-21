// mmap_gating_safety_test.go — ADR-0027 cutover safety guard.
//
// ALL handler-path mmap reads (LabelIndex.at / getByID / getAdjacency /
// getCallsAdj / getStepAdj) are gated behind GRAFEL_SERVE_FROM_MMAP (default
// OFF). With the flag OFF, none of them may dereference lr.Reader's mmap — the
// handler path must read only the GC-safe Document, so a concurrent reload can
// retire()+munmap the Reader without a read-after-unmap SIGBUS (the latent PR1
// #5865 hazard).
//
// This test proves that observationally and deterministically (no race repro):
// the LoadedRepo's Reader is loaded from a graph.fb whose content DIVERGES from
// its Document (different entity ids, no relationships). With the flag OFF every
// read path must return the DOCUMENT's content — if any of them sourced from the
// Reader instead, the result would carry the Reader's divergent content and the
// assertion fails. The Reader is a valid open mapping throughout, so the test
// itself can build the Reader-sourced structures for the negative comparison.
package mcp

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbreader"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// TestServeFromMMapOff_HandlerPathsNeverDereferenceReader is the cutover safety
// guard: flag OFF ⇒ at/getByID/getAdjacency/getCallsAdj/getStepAdj are all
// Document-sourced and never touch lr.Reader's mmap.
func TestServeFromMMapOff_HandlerPathsNeverDereferenceReader(t *testing.T) {
	forceServeFromMMap(t, false) // production default

	// Document: entities A,B with a CALLS and a STEP_IN_PROCESS edge A->B.
	doc := &graph.Document{Version: 1, Repo: "svc"}
	doc.Entities = []graph.Entity{
		{ID: "A", Name: "A", Kind: "function", SourceFile: "a.go", Language: "go"},
		{ID: "B", Name: "B", Kind: "function", SourceFile: "b.go", Language: "go"},
	}
	doc.Relationships = []graph.Relationship{
		{FromID: "A", ToID: "B", Kind: "CALLS"},
		{FromID: "A", ToID: "B", Kind: "STEP_IN_PROCESS"},
	}

	// DIVERGENT graph.fb backing the Reader: entities X,Y, NO relationships. If a
	// flag-off read path touched the Reader, it would surface X/Y / empty edges.
	rdrDoc := &graph.Document{Version: 1, Repo: "svc"}
	rdrDoc.Entities = []graph.Entity{
		{ID: "X", Name: "X", Kind: "function", SourceFile: "x.go", Language: "go"},
		{ID: "Y", Name: "Y", Kind: "function", SourceFile: "y.go", Language: "go"},
	}
	fbPath := filepath.Join(t.TempDir(), "graph.fb")
	if err := fbwriter.WriteAtomic(fbPath, rdrDoc); err != nil {
		t.Fatalf("write divergent graph.fb: %v", err)
	}
	rdr, err := fbreader.Open(fbPath)
	if err != nil {
		t.Fatalf("open reader: %v", err)
	}
	t.Cleanup(func() { _ = rdr.Close() })

	lr := &LoadedRepo{Repo: "svc", Doc: doc, Reader: rdr, LabelIndex: BuildLabelIndexFromReader(rdr, doc)}

	// --- LabelIndex.at / getByID: must surface the Document's ids, not the
	// Reader's divergent X/Y.
	if a := lr.LabelIndex.at(0); a == nil || a.ID != "A" {
		t.Fatalf("flag-off at(0) sourced from Reader (got %v); must be Document entity A", a)
	}
	byID := lr.getByID()
	if _, ok := byID["A"]; !ok {
		t.Fatalf("flag-off getByID missing Document id A (keys=%v)", entityMapKeys(byID))
	}
	if _, ok := byID["X"]; ok {
		t.Fatalf("flag-off getByID surfaced Reader id X — it dereferenced the mmap")
	}

	// --- adjacency ×3: flag-off must equal the Document build and differ from the
	// Reader build (the two diverge, so equality to Doc proves Doc-sourcing).
	wantAdj := buildAdjacency(doc, "svc")
	rdrAdj := buildAdjacencyFromReader(rdr, "svc")
	gotAdj := lr.getAdjacency()
	if !reflect.DeepEqual(gotAdj, wantAdj) {
		t.Fatalf("flag-off getAdjacency != Document build (Reader-sourced?)")
	}
	if reflect.DeepEqual(gotAdj, rdrAdj) {
		t.Fatalf("flag-off getAdjacency == Reader build — it dereferenced the mmap")
	}

	wantCalls := buildCallsAdjacency(doc)
	rdrCalls := buildCallsAdjacencyFromReader(rdr)
	gotCalls := lr.getCallsAdj()
	if !reflect.DeepEqual(gotCalls, wantCalls) {
		t.Fatalf("flag-off getCallsAdj != Document build (Reader-sourced?)")
	}
	if reflect.DeepEqual(gotCalls, rdrCalls) {
		t.Fatalf("flag-off getCallsAdj == Reader build — it dereferenced the mmap")
	}

	wantStep := buildStepAdjacency(doc)
	rdrStep := buildStepAdjacencyFromReader(rdr)
	gotStep := lr.getStepAdj()
	if !reflect.DeepEqual(gotStep, wantStep) {
		t.Fatalf("flag-off getStepAdj != Document build (Reader-sourced?)")
	}
	if reflect.DeepEqual(gotStep, rdrStep) {
		t.Fatalf("flag-off getStepAdj == Reader build — it dereferenced the mmap")
	}
}

func entityMapKeys(m map[string]*graph.Entity) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
