package main

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// TestIncrementalMergeDropsCarriedForwardHierarchySelfLoop_6805 grades the
// ONE path the extraction-quality gate structurally cannot reach: the
// incremental splice.
//
// The gate only ever runs full indexes, so it observes buildDocument's filter
// and nothing else. `rebuild --incremental` reads the PREVIOUS graph back and
// carries forward every relationship whose endpoints both still exist — a
// check on each endpoint separately, never on the two against each other. A
// graph written by a pre-#6805 binary therefore keeps its hierarchy
// self-loops for every file that did not change, indefinitely.
//
// This test asserts exactly that one behaviour of the merge and nothing
// broader: a carried-forward IMPLEMENTS self-loop is gone afterwards, while a
// carried-forward CALLS self-loop (direct recursion) and a normal
// carried-forward hierarchy edge both survive. It does NOT re-grade
// DropHierarchySelfLoops' own kind/case/order decisions — those are pinned in
// internal/graph.
func TestIncrementalMergeDropsCarriedForwardHierarchySelfLoop_6805(t *testing.T) {
	ent := func(id, file string) graph.Entity {
		return graph.Entity{ID: id, Name: id, Kind: "SCOPE.Component", SourceFile: file}
	}
	rel := func(id, from, to, kind string) graph.Relationship {
		return graph.Relationship{ID: id, FromID: from, ToID: to, Kind: kind}
	}

	// "old.py" is UNCHANGED, so everything anchored in it is carried forward
	// verbatim from the previous graph — including the self-loop a pre-fix
	// binary wrote there.
	prev := &graph.Document{
		Entities: []graph.Entity{ent("A", "old.py"), ent("B", "old.py")},
		Relationships: []graph.Relationship{
			rel("SELF", "A", "A", "IMPLEMENTS"),
			rel("REAL", "A", "B", "IMPLEMENTS"),
			rel("RECUR", "B", "B", "CALLS"),
		},
	}
	doc := &graph.Document{}

	mergeIncrementalPrevDoc(doc, prev, map[string]bool{"changed.py": true})

	got := map[string]bool{}
	for _, r := range doc.Relationships {
		got[r.ID] = true
	}
	if got["SELF"] {
		for _, r := range doc.Relationships {
			t.Logf("  id=%s %s->%s %s", r.ID, r.FromID, r.ToID, r.Kind)
		}
		t.Fatalf("#6805: the carried-forward IMPLEMENTS self-loop survived the incremental merge — " +
			"a graph written by a pre-fix binary keeps its self-loops across every incremental rebuild")
	}
	// Positive controls: the drop must be the narrow one, not "the merge
	// stopped carrying edges forward", which would satisfy the assertion above
	// for entirely the wrong reason.
	if !got["REAL"] {
		t.Fatalf("#6805: the normal IMPLEMENTS edge A->B was also dropped — the filter is over-broad")
	}
	if !got["RECUR"] {
		t.Fatalf("#6805: the CALLS self-loop (direct recursion) was dropped — CALLS is deliberately excluded")
	}
	if n := len(doc.Relationships); n != 2 {
		t.Fatalf("#6805: merged relationship count = %d, want 2 (REAL + RECUR)", n)
	}
}
