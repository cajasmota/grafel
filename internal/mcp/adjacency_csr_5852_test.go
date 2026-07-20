// adjacency_csr_5852_test.go — golden equivalence coverage for the CSR
// (compressed-sparse-row) refactor of the resident adjacency index (#5852).
//
// The map-based adjacency (map[string][]edge for out/in) is being replaced
// internally with a CSR layout over a dense int32 node index, to cut the
// ~290 MB resident cost at corpus scale. The external contract —
// Outgoing(id)/Incoming(id) returning []edge in the SAME order with the
// SAME target/kind/weight/relIdx values — must not change. This test
// builds a reference []edge per node independently of buildAdjacency (a
// straight append-in-relationship-order walk, mirroring the pre-refactor
// map-based semantics) and asserts buildAdjacency's Outgoing/Incoming match
// exactly, for every node that appears as a From/To id in the fixture.
//
// This test is written to hold on BOTH sides of the CSR refactor: it
// passed against the original map-based buildAdjacency (proving the
// reference is a faithful re-derivation of the old behaviour) and must
// keep passing after the CSR swap (proving equivalence).
package mcp

import (
	"reflect"
	"sort"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

func adjacencyGoldenDoc() *graph.Document {
	mk := func(id string) graph.Entity {
		return graph.Entity{ID: id, Name: id, QualifiedName: "pkg." + id, Kind: "Function", SourceFile: id + ".go", StartLine: 1}
	}
	return &graph.Document{
		Entities: []graph.Entity{
			mk("a"), mk("b"), mk("c"), mk("d"), mk("e"), mk("hub"),
		},
		Relationships: []graph.Relationship{
			// Multiple out-edges from "a", in original append order, several kinds.
			{FromID: "a", ToID: "b", Kind: "CALLS"},
			{FromID: "a", ToID: "c", Kind: "REFERENCES"},
			{FromID: "a", ToID: "b", Kind: "IMPORTS"}, // duplicate target, different kind
			// Weighted edges via Properties["count"] and Properties["weight"].
			{FromID: "a", ToID: "d", Kind: "CALLS", Properties: map[string]string{"count": "7"}},
			{FromID: "b", ToID: "d", Kind: "CALLS", Properties: map[string]string{"weight": "2.5"}},
			// Zero/invalid weight properties fall back to 1.0.
			{FromID: "c", ToID: "d", Kind: "CALLS", Properties: map[string]string{"count": "0"}},
			{FromID: "c", ToID: "e", Kind: "CALLS", Properties: map[string]string{"count": "not-a-number"}},
			// A hub with many in-edges to exercise a larger CSR row.
			{FromID: "a", ToID: "hub", Kind: "CALLS"},
			{FromID: "b", ToID: "hub", Kind: "CALLS"},
			{FromID: "c", ToID: "hub", Kind: "CALLS"},
			{FromID: "d", ToID: "hub", Kind: "CALLS"},
			{FromID: "e", ToID: "hub", Kind: "CALLS"},
			// Self-loop edge case.
			{FromID: "e", ToID: "e", Kind: "CALLS"},
		},
	}
}

// referenceAdjacency reproduces the pre-refactor map[string][]edge semantics
// directly against doc.Relationships, independent of buildAdjacency's
// internals, to serve as the golden oracle.
func referenceAdjacency(doc *graph.Document) (out, in map[string][]edge) {
	out = map[string][]edge{}
	in = map[string][]edge{}
	for i := range doc.Relationships {
		r := &doc.Relationships[i]
		w := edgeWeight(r)
		out[r.FromID] = append(out[r.FromID], edge{target: r.ToID, kind: r.Kind, weight: w, relIdx: i})
		in[r.ToID] = append(in[r.ToID], edge{target: r.FromID, kind: r.Kind, weight: w, relIdx: i})
	}
	return out, in
}

func adjacencyGoldenNodeIDs(doc *graph.Document) []string {
	seen := map[string]bool{}
	var ids []string
	for _, e := range doc.Entities {
		if !seen[e.ID] {
			seen[e.ID] = true
			ids = append(ids, e.ID)
		}
	}
	for _, r := range doc.Relationships {
		for _, id := range []string{r.FromID, r.ToID} {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	// "nonexistent" probes the not-found path (no From/To reference at all).
	ids = append(ids, "nonexistent")
	sort.Strings(ids)
	return ids
}

func TestCSR5852_OutgoingIncomingMatchReference(t *testing.T) {
	doc := adjacencyGoldenDoc()
	refOut, refIn := referenceAdjacency(doc)
	a := buildAdjacency(doc, "repo1")

	for _, id := range adjacencyGoldenNodeIDs(doc) {
		gotOut := a.Outgoing(id)
		wantOut := refOut[id] // nil for ids with no out-edges
		if !edgesEqual(gotOut, wantOut) {
			t.Errorf("Outgoing(%q) = %+v; want %+v", id, gotOut, wantOut)
		}
		gotIn := a.Incoming(id)
		wantIn := refIn[id]
		if !edgesEqual(gotIn, wantIn) {
			t.Errorf("Incoming(%q) = %+v; want %+v", id, gotIn, wantIn)
		}
	}
}

// edgesEqual treats nil and empty slice as equivalent (both callers' and the
// old map-based lookup miss return a nil slice; the CSR path must too, but we
// don't want a spurious failure if an implementation legitimately returns an
// empty non-nil slice for a present-but-degree-0 case).
func edgesEqual(a, b []edge) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return reflect.DeepEqual(a, b)
}

// TestCSR5852_WeightFallbackSemantics locks in edgeWeight's precedence and
// fallback behaviour (count > weight > 1.0; non-positive/unparseable -> 1.0)
// as observed through the adjacency accessor, since the CSR path stores
// weight as float32 — this also guards against precision loss regressions.
func TestCSR5852_WeightFallbackSemantics(t *testing.T) {
	doc := adjacencyGoldenDoc()
	a := buildAdjacency(doc, "repo1")

	find := func(edges []edge, target, kind string) (edge, bool) {
		for _, e := range edges {
			if e.target == target && e.kind == kind {
				return e, true
			}
		}
		return edge{}, false
	}

	out := a.Outgoing("a")
	e, ok := find(out, "d", "CALLS")
	if !ok || e.weight != 7.0 {
		t.Errorf("a->d CALLS weight = %+v (ok=%v); want 7.0", e, ok)
	}

	out = a.Outgoing("b")
	e, ok = find(out, "d", "CALLS")
	if !ok || e.weight != 2.5 {
		t.Errorf("b->d CALLS weight = %+v (ok=%v); want 2.5", e, ok)
	}

	out = a.Outgoing("c")
	e, ok = find(out, "d", "CALLS")
	if !ok || e.weight != 1.0 {
		t.Errorf("c->d CALLS weight (count=0, falls back) = %+v (ok=%v); want 1.0", e, ok)
	}
	e, ok = find(out, "e", "CALLS")
	if !ok || e.weight != 1.0 {
		t.Errorf("c->e CALLS weight (unparseable count, falls back) = %+v (ok=%v); want 1.0", e, ok)
	}
}

// TestCSR5852_EdgeOrderPreserved locks in relationship-append order per node,
// which several BFS/traversal call sites rely on implicitly via stable
// tie-break ordering downstream.
func TestCSR5852_EdgeOrderPreserved(t *testing.T) {
	doc := adjacencyGoldenDoc()
	a := buildAdjacency(doc, "repo1")

	out := a.Outgoing("a")
	var gotTargets []string
	var gotKinds []string
	for _, e := range out {
		gotTargets = append(gotTargets, e.target)
		gotKinds = append(gotKinds, e.kind)
	}
	wantTargets := []string{"b", "c", "b", "d", "hub"}
	wantKinds := []string{"CALLS", "REFERENCES", "IMPORTS", "CALLS", "CALLS"}
	if !reflect.DeepEqual(gotTargets, wantTargets) || !reflect.DeepEqual(gotKinds, wantKinds) {
		t.Errorf("Outgoing(a) order = targets=%v kinds=%v; want targets=%v kinds=%v",
			gotTargets, gotKinds, wantTargets, wantKinds)
	}

	in := a.Incoming("hub")
	var gotIn []string
	for _, e := range in {
		gotIn = append(gotIn, e.target)
	}
	wantIn := []string{"a", "b", "c", "d", "e"}
	if !reflect.DeepEqual(gotIn, wantIn) {
		t.Errorf("Incoming(hub) order = %v; want %v", gotIn, wantIn)
	}
}

// TestCSR5852_RelIdxRoundTrips confirms relIdx still points back into
// doc.Relationships for both directions after the CSR swap.
func TestCSR5852_RelIdxRoundTrips(t *testing.T) {
	doc := adjacencyGoldenDoc()
	a := buildAdjacency(doc, "repo1")

	for _, e := range a.Outgoing("a") {
		if e.relIdx < 0 || e.relIdx >= len(doc.Relationships) {
			t.Fatalf("Outgoing(a) relIdx out of range: %+v", e)
		}
		r := doc.Relationships[e.relIdx]
		if r.FromID != "a" || r.ToID != e.target || r.Kind != e.kind {
			t.Errorf("relIdx %d does not point back to matching relationship: edge=%+v rel=%+v", e.relIdx, e, r)
		}
	}
	for _, e := range a.Incoming("hub") {
		if e.relIdx < 0 || e.relIdx >= len(doc.Relationships) {
			t.Fatalf("Incoming(hub) relIdx out of range: %+v", e)
		}
		r := doc.Relationships[e.relIdx]
		if r.ToID != "hub" || r.FromID != e.target || r.Kind != e.kind {
			t.Errorf("relIdx %d does not point back to matching relationship: edge=%+v rel=%+v", e.relIdx, e, r)
		}
	}
}

// TestCSR5852_SelfLoop exercises the degenerate self-loop case (FromID==ToID)
// in both directions.
func TestCSR5852_SelfLoop(t *testing.T) {
	doc := adjacencyGoldenDoc()
	a := buildAdjacency(doc, "repo1")

	foundOut, foundIn := false, false
	for _, e := range a.Outgoing("e") {
		if e.target == "e" && e.kind == "CALLS" {
			foundOut = true
		}
	}
	for _, e := range a.Incoming("e") {
		if e.target == "e" && e.kind == "CALLS" {
			foundIn = true
		}
	}
	if !foundOut {
		t.Error("expected self-loop e->e in Outgoing(e)")
	}
	if !foundIn {
		t.Error("expected self-loop e->e in Incoming(e)")
	}
}

// TestCSR5852_NilReceiverAndMissingID guards the nil-receiver contract and
// the not-found path (id absent from both directions).
func TestCSR5852_NilReceiverAndMissingID(t *testing.T) {
	var na *adjacency
	if got := na.Outgoing("a"); got != nil {
		t.Errorf("nil.Outgoing = %+v; want nil", got)
	}
	if got := na.Incoming("a"); got != nil {
		t.Errorf("nil.Incoming = %+v; want nil", got)
	}

	doc := adjacencyGoldenDoc()
	a := buildAdjacency(doc, "repo1")
	if got := a.Outgoing("nonexistent"); got != nil {
		t.Errorf("Outgoing(nonexistent) = %+v; want nil", got)
	}
	if got := a.Incoming("nonexistent"); got != nil {
		t.Errorf("Incoming(nonexistent) = %+v; want nil", got)
	}
}
