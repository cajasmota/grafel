package graph

import (
	"testing"
)

// relImport constructs an IMPORTS relationship.
func relImport(from, to string) Relationship {
	return Relationship{ID: from + "-imports-" + to, FromID: from, ToID: to, Kind: "IMPORTS"}
}

// relCalls constructs a CALLS relationship (should be ignored by cycle detector).
func relCalls2(from, to string) Relationship {
	return Relationship{ID: from + "-calls-" + to, FromID: from, ToID: to, Kind: "CALLS"}
}

// TestFindImportCycles_SimpleCycle — A imports B, B imports A. Must detect one cycle.
func TestFindImportCycles_SimpleCycle(t *testing.T) {
	ents := makeEntities("A", "B")
	rels := []Relationship{
		relImport("A", "B"),
		relImport("B", "A"),
	}
	cycles := FindImportCycles(ents, rels, nil)
	if len(cycles) != 1 {
		t.Fatalf("expected 1 cycle, got %d: %v", len(cycles), cycles)
	}
	c := cycles[0]
	if c.Size != 2 {
		t.Errorf("cycle size = %d, want 2", c.Size)
	}
	if len(c.Edges) != 2 {
		t.Errorf("edge count = %d, want 2", len(c.Edges))
	}
	// Members must be sorted.
	if c.Members[0] != "A" || c.Members[1] != "B" {
		t.Errorf("members not sorted: %v", c.Members)
	}
}

// TestFindImportCycles_ThreeNodeCycle — A→B→C→A. Must detect one 3-member cycle.
func TestFindImportCycles_ThreeNodeCycle(t *testing.T) {
	ents := makeEntities("A", "B", "C")
	rels := []Relationship{
		relImport("A", "B"),
		relImport("B", "C"),
		relImport("C", "A"),
	}
	cycles := FindImportCycles(ents, rels, nil)
	if len(cycles) != 1 {
		t.Fatalf("expected 1 cycle, got %d", len(cycles))
	}
	if cycles[0].Size != 3 {
		t.Errorf("want 3-member cycle, got %d members", cycles[0].Size)
	}
}

// TestFindImportCycles_Acyclic — A→B, B→C. No cycles.
func TestFindImportCycles_Acyclic(t *testing.T) {
	ents := makeEntities("A", "B", "C")
	rels := []Relationship{
		relImport("A", "B"),
		relImport("B", "C"),
	}
	cycles := FindImportCycles(ents, rels, nil)
	if len(cycles) != 0 {
		t.Errorf("acyclic graph should have no cycles, got %d", len(cycles))
	}
}

// TestFindImportCycles_NoImportEdges — Only CALLS edges; cycle detector must ignore them.
func TestFindImportCycles_NoImportEdges(t *testing.T) {
	ents := makeEntities("A", "B")
	rels := []Relationship{
		relCalls2("A", "B"),
		relCalls2("B", "A"),
	}
	cycles := FindImportCycles(ents, rels, nil)
	if len(cycles) != 0 {
		t.Errorf("CALLS edges should not produce import cycles, got %d", len(cycles))
	}
}

// TestFindImportCycles_TwoCycles — Two independent 2-cycles.
func TestFindImportCycles_TwoCycles(t *testing.T) {
	ents := makeEntities("A", "B", "C", "D")
	rels := []Relationship{
		relImport("A", "B"),
		relImport("B", "A"),
		relImport("C", "D"),
		relImport("D", "C"),
	}
	cycles := FindImportCycles(ents, rels, nil)
	if len(cycles) != 2 {
		t.Fatalf("expected 2 cycles, got %d", len(cycles))
	}
}

// TestFindImportCycles_SortedBySize — larger cycle listed first.
func TestFindImportCycles_SortedBySize(t *testing.T) {
	ents := makeEntities("A", "B", "C", "D", "E")
	rels := []Relationship{
		// 3-cycle: C→D→E→C
		relImport("C", "D"),
		relImport("D", "E"),
		relImport("E", "C"),
		// 2-cycle: A→B→A
		relImport("A", "B"),
		relImport("B", "A"),
	}
	cycles := FindImportCycles(ents, rels, nil)
	if len(cycles) < 2 {
		t.Fatalf("expected at least 2 cycles, got %d", len(cycles))
	}
	if cycles[0].Size < cycles[1].Size {
		t.Errorf("cycles not sorted by descending size: %d < %d", cycles[0].Size, cycles[1].Size)
	}
}

// TestFindImportCycles_WeakestLink — the entity with the lowest PageRank
// should be the weakest-link source.
func TestFindImportCycles_WeakestLink(t *testing.T) {
	ents := makeEntities("A", "B", "C")
	rels := []Relationship{
		relImport("A", "B"),
		relImport("B", "C"),
		relImport("C", "A"),
	}
	// Assign C the lowest PageRank.
	pr := map[string]float64{
		"A": 0.5,
		"B": 0.4,
		"C": 0.1,
	}
	cycles := FindImportCycles(ents, rels, pr)
	if len(cycles) != 1 {
		t.Fatalf("expected 1 cycle, got %d", len(cycles))
	}
	if cycles[0].WeakestLinkFromID != "C" {
		t.Errorf("weakest link source = %q, want %q", cycles[0].WeakestLinkFromID, "C")
	}
	if cycles[0].WeakestLinkToID != "A" {
		t.Errorf("weakest link dest = %q, want %q", cycles[0].WeakestLinkToID, "A")
	}
}

// TestFindImportCycles_SuggestedExtraction — the entity with the highest
// PageRank should be the suggested extraction target.
func TestFindImportCycles_SuggestedExtraction(t *testing.T) {
	ents := makeEntities("A", "B", "C")
	rels := []Relationship{
		relImport("A", "B"),
		relImport("B", "C"),
		relImport("C", "A"),
	}
	pr := map[string]float64{
		"A": 0.5,
		"B": 0.9,
		"C": 0.1,
	}
	cycles := FindImportCycles(ents, rels, pr)
	if len(cycles) != 1 {
		t.Fatalf("expected 1 cycle, got %d", len(cycles))
	}
	if cycles[0].SuggestedExtractionID != "B" {
		t.Errorf("suggested extraction = %q, want %q", cycles[0].SuggestedExtractionID, "B")
	}
}

// TestFindImportCycles_SelfImport — self-import should not count as a cycle.
func TestFindImportCycles_SelfImport(t *testing.T) {
	ents := makeEntities("A")
	rels := []Relationship{
		relImport("A", "A"),
	}
	cycles := FindImportCycles(ents, rels, nil)
	if len(cycles) != 0 {
		t.Errorf("self-import should not produce a cycle, got %d", len(cycles))
	}
}

// TestFindImportCycles_DanglingEdge — edge referencing an entity not in the
// entity list should be silently skipped (no panic, no false positive).
func TestFindImportCycles_DanglingEdge(t *testing.T) {
	ents := makeEntities("A", "B")
	rels := []Relationship{
		relImport("A", "B"),
		relImport("B", "UNKNOWN"), // dangling
		relImport("UNKNOWN", "A"), // dangling
	}
	// Without the UNKNOWN node present, there is no complete cycle.
	cycles := FindImportCycles(ents, rels, nil)
	if len(cycles) != 0 {
		t.Errorf("dangling edges should not produce cycles, got %d", len(cycles))
	}
}

// TestFindImportCycles_Empty — empty input must not panic.
func TestFindImportCycles_Empty(t *testing.T) {
	cycles := FindImportCycles(nil, nil, nil)
	if len(cycles) != 0 {
		t.Errorf("empty input: expected 0 cycles, got %d", len(cycles))
	}
}

// TestFindImportCycles_Determinism — multiple calls with same input produce
// identical output (members and edges sorted, cycles in stable order).
func TestFindImportCycles_Determinism(t *testing.T) {
	ents := makeEntities("A", "B", "C", "D", "E", "F")
	rels := []Relationship{
		relImport("A", "B"), relImport("B", "C"), relImport("C", "A"),
		relImport("D", "E"), relImport("E", "F"), relImport("F", "D"),
	}
	var prev []ImportCycle
	for i := 0; i < 5; i++ {
		got := FindImportCycles(ents, rels, nil)
		if prev == nil {
			prev = got
			continue
		}
		if len(got) != len(prev) {
			t.Fatalf("run %d: cycle count changed %d→%d", i, len(prev), len(got))
		}
		for j := range got {
			if got[j].Size != prev[j].Size {
				t.Errorf("run %d cycle %d: size %d→%d", i, j, prev[j].Size, got[j].Size)
			}
			if len(got[j].Members) != len(prev[j].Members) {
				t.Errorf("run %d cycle %d: member count changed", i, j)
				continue
			}
			for k := range got[j].Members {
				if got[j].Members[k] != prev[j].Members[k] {
					t.Errorf("run %d cycle %d member %d: %q→%q",
						i, j, k, prev[j].Members[k], got[j].Members[k])
				}
			}
		}
		prev = got
	}
}
