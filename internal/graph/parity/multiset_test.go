package parity

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// These cases pin #6037: the comparator must compare MULTISETS, not sets. A set
// comparison collapses duplicate rows into one map slot and is structurally
// incapable of seeing a duplication defect (#6094 persists duplicate entity and
// relationship rows into graph.fb on the incremental path).
//
// Every case asserts BOTH directions — a one-directional comparison reports a
// subset as parity.

func rel(from, to, kind string, props map[string]string) graph.Relationship {
	r := graph.Relationship{FromID: from, ToID: to, Kind: kind}
	if props != nil {
		r = r.WithProperties(props)
	}
	return r
}

// TestCompare_DetectsDuplicateRelationshipRow_Invented is the #6094 shape: the
// incremental graph carries the SAME edge twice where a full rebuild carries it
// once.
func TestCompare_DetectsDuplicateRelationshipRow_Invented(t *testing.T) {
	a := &graph.Document{Relationships: []graph.Relationship{
		rel("x", "y", "CALLS", nil),
	}}
	b := &graph.Document{Relationships: []graph.Relationship{
		rel("x", "y", "CALLS", nil),
		rel("x", "y", "CALLS", nil), // duplicate row
	}}
	rep := Compare(a, b)
	if rep.Equivalent {
		t.Fatal("a duplicated relationship row must NOT be reported as parity")
	}
	if len(rep.RelMultiplicityDiffs) != 1 {
		t.Fatalf("RelMultiplicityDiffs=%+v want 1", rep.RelMultiplicityDiffs)
	}
	d := rep.RelMultiplicityDiffs[0]
	if !strings.Contains(d.Key, "x→y:CALLS") {
		t.Errorf("diff should name the edge, got %q", d.Key)
	}
	if !strings.Contains(d.Detail, "1") || !strings.Contains(d.Detail, "2") {
		t.Errorf("diff should carry both counts, got %q", d.Detail)
	}
	if !strings.Contains(rep.String(), "x→y:CALLS") {
		t.Errorf("String() must render the multiplicity diff:\n%s", rep.String())
	}
}

// TestCompare_DetectsDuplicateRelationshipRow_Lost is the other direction: a
// LOST duplicate row (the reference has two, the candidate one). A comparator
// that only checks A→B presence reports this subset as parity.
func TestCompare_DetectsDuplicateRelationshipRow_Lost(t *testing.T) {
	a := &graph.Document{Relationships: []graph.Relationship{
		rel("x", "y", "CALLS", nil),
		rel("x", "y", "CALLS", nil),
	}}
	b := &graph.Document{Relationships: []graph.Relationship{
		rel("x", "y", "CALLS", nil),
	}}
	rep := Compare(a, b)
	if rep.Equivalent {
		t.Fatal("a LOST duplicate relationship row must NOT be reported as parity")
	}
	if len(rep.RelMultiplicityDiffs) != 1 {
		t.Fatalf("RelMultiplicityDiffs=%+v want 1", rep.RelMultiplicityDiffs)
	}
}

// TestCompare_EqualRelationshipMultiplicityIsEquivalent guards the false-positive
// side the issue calls out: the FULL path legitimately emits distinct
// relationships sharing a RelationshipID (extractor.go's `?mv` multi-value
// edges). The correct assertion is "multiplicities are EQUAL", never "every
// multiplicity is 1".
func TestCompare_EqualRelationshipMultiplicityIsEquivalent(t *testing.T) {
	mk := func() []graph.Relationship {
		return []graph.Relationship{
			rel("run", "handler", "CALLS", map[string]string{"line": "8"}),
			rel("run", "handler", "CALLS", map[string]string{"line": "9", "via_value": "true"}),
		}
	}
	a := &graph.Document{Relationships: mk()}
	// b lists the same two rows in the OPPOSITE order — the comparator must be
	// order-independent, not last-write-wins.
	rb := mk()
	rb[0], rb[1] = rb[1], rb[0]
	b := &graph.Document{Relationships: rb}
	if rep := Compare(a, b); !rep.Equivalent {
		t.Fatalf("equal multiplicities with equal property sets must be equivalent:\n%s", rep.String())
	}
}

// TestCompare_DuplicateIDGroupPropertyDriftIsCaught closes the last-write-wins
// hole: two rows share a key on both sides, but one side's property set differs.
// The old map-collapsing comparator kept whichever row landed last, so this was
// detected or missed depending purely on slice order.
func TestCompare_DuplicateIDGroupPropertyDriftIsCaught(t *testing.T) {
	a := &graph.Document{Relationships: []graph.Relationship{
		rel("run", "handler", "CALLS", map[string]string{"line": "8"}),
		rel("run", "handler", "CALLS", map[string]string{"line": "9"}),
	}}
	b := &graph.Document{Relationships: []graph.Relationship{
		rel("run", "handler", "CALLS", map[string]string{"line": "8"}),
		rel("run", "handler", "CALLS", map[string]string{"line": "10"}), // drifted
	}}
	rep := Compare(a, b)
	if rep.Equivalent {
		t.Fatal("property drift inside a duplicate-key group must be caught")
	}
	if len(rep.RelPropDiffs) != 1 {
		t.Fatalf("RelPropDiffs=%+v want 1", rep.RelPropDiffs)
	}
	// And with the drifted row FIRST — the result must be identical, not order-dependent.
	b.Relationships[0], b.Relationships[1] = b.Relationships[1], b.Relationships[0]
	if rep2 := Compare(a, b); rep2.Equivalent || len(rep2.RelPropDiffs) != 1 {
		t.Fatalf("order-dependent result: equivalent=%v diffs=%+v", rep2.Equivalent, rep2.RelPropDiffs)
	}
}

// TestCompare_DetectsDuplicateEntityRow — #6094 persists duplicate ENTITY rows
// too, and compareEntities collapses them the same way.
func TestCompare_DetectsDuplicateEntityRow(t *testing.T) {
	e := ent("SCOPE.Operation", "Alpha", "a.go")
	a := &graph.Document{Entities: []graph.Entity{e}}
	b := &graph.Document{Entities: []graph.Entity{e, e}}

	rep := Compare(a, b)
	if rep.Equivalent {
		t.Fatal("a duplicated entity row must NOT be reported as parity")
	}
	if len(rep.EntityMultiplicityDiffs) != 1 {
		t.Fatalf("EntityMultiplicityDiffs=%+v want 1", rep.EntityMultiplicityDiffs)
	}
	if !strings.Contains(rep.EntityMultiplicityDiffs[0].Key, "Alpha") {
		t.Errorf("diff should name the entity, got %q", rep.EntityMultiplicityDiffs[0].Key)
	}

	// Reverse direction.
	rep = Compare(b, a)
	if rep.Equivalent || len(rep.EntityMultiplicityDiffs) != 1 {
		t.Fatalf("LOST duplicate entity row not reported: equivalent=%v diffs=%+v", rep.Equivalent, rep.EntityMultiplicityDiffs)
	}
}

// TestCompare_OnlyInListsCarryMultiplicity — when a key is absent from one side
// entirely, the count on the present side is still load-bearing information
// (three invented copies is not the same defect as one).
func TestCompare_OnlyInListsCarryMultiplicity(t *testing.T) {
	a := &graph.Document{}
	b := &graph.Document{Relationships: []graph.Relationship{
		rel("x", "y", "CALLS", nil),
		rel("x", "y", "CALLS", nil),
		rel("x", "y", "CALLS", nil),
	}}
	rep := Compare(a, b)
	if len(rep.RelsOnlyInB) != 1 {
		t.Fatalf("RelsOnlyInB=%v want 1 entry", rep.RelsOnlyInB)
	}
	if !strings.Contains(rep.RelsOnlyInB[0], "×3") {
		t.Errorf("only-in entry should carry the count, got %q", rep.RelsOnlyInB[0])
	}
}
