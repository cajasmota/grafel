// Package resolve — unit parity tests for the compact kindIDs type (#5954).
//
// kindIDs replaces the innermost map[string]string (kind -> entity_id) buckets
// in the resolver Index's four dominant inner maps (nameKinds, nameKindsReal,
// byLocationKind, byLocationKindReal). It MUST be byte-for-byte behaviourally
// identical to the map it replaces under the resolver's exact write/read rules:
//
//   - first-writer-wins per (kind);
//   - a SECOND writer with a different id blanks the entry to "" (the
//     ambiguous-within-kind sentinel);
//   - a present-but-blank ("") entry is DISTINCT from an absent entry:
//     get returns ("", true) for blank, ("", false) for absent.
//
// These tests pin that contract against a reference map oracle.
package resolve

import (
	"reflect"
	"sort"
	"testing"
)

// mapOracle replays the EXACT write rule the resolver uses today on a real
// map[string]string, so we can assert kindIDs matches it entry-for-entry.
func mapOracleSet(m map[string]string, kind, id string) {
	if existing, ok := m[kind]; ok && existing != id {
		m[kind] = ""
	} else {
		m[kind] = id
	}
}

// applyWrites runs the same write sequence through both a kindIDs and the map
// oracle and asserts get/len/each agree for every kind touched.
func applyWrites(t *testing.T, writes [][2]string) {
	t.Helper()
	var kid kindIDs
	oracle := map[string]string{}
	for _, w := range writes {
		kid.set(w[0], w[1])
		mapOracleSet(oracle, w[0], w[1])
	}
	// len parity.
	if kid.len() != len(oracle) {
		t.Fatalf("len mismatch: kindIDs=%d oracle=%d (writes=%v)", kid.len(), len(oracle), writes)
	}
	// get parity for every kind in the oracle (present, possibly blank).
	for k, want := range oracle {
		gotID, gotPresent := kid.get(k)
		if !gotPresent {
			t.Fatalf("get(%q): kindIDs reports ABSENT but oracle has it (=%q); writes=%v", k, want, writes)
		}
		if gotID != want {
			t.Fatalf("get(%q): kindIDs=%q oracle=%q; writes=%v", k, gotID, want, writes)
		}
	}
	// each() must enumerate the identical (kind,id) set.
	eachSet := map[string]string{}
	kid.each(func(kind, id string) { eachSet[kind] = id })
	if !reflect.DeepEqual(eachSet, oracle) {
		t.Fatalf("each() set mismatch:\n each=%v\noracle=%v\nwrites=%v", eachSet, oracle, writes)
	}
}

func TestKindIDs_EmptyIsAbsent(t *testing.T) {
	var kid kindIDs
	if kid.len() != 0 {
		t.Fatalf("empty len = %d, want 0", kid.len())
	}
	if id, present := kid.get("Function"); present || id != "" {
		t.Fatalf("empty get = (%q,%v), want (\"\",false)", id, present)
	}
	// each on empty must not call the callback.
	kid.each(func(kind, id string) { t.Fatalf("each on empty called with (%q,%q)", kind, id) })
}

func TestKindIDs_SingleEntry(t *testing.T) {
	applyWrites(t, [][2]string{{"Function", "aaaa"}})
}

func TestKindIDs_TwoDistinctKinds(t *testing.T) {
	applyWrites(t, [][2]string{{"Function", "aaaa"}, {"Method", "bbbb"}})
}

func TestKindIDs_ThreeDistinctKinds(t *testing.T) {
	applyWrites(t, [][2]string{
		{"SCOPE.Component", "aaaa"},
		{"Component", "aaaa"},
		{"SCOPE.Operation", "bbbb"},
	})
}

func TestKindIDs_FirstWriterWins_SameID_NoBlank(t *testing.T) {
	// Re-writing the SAME id must NOT blank (mirrors the map's else-branch).
	var kid kindIDs
	kid.set("Function", "aaaa")
	kid.set("Function", "aaaa")
	id, present := kid.get("Function")
	if !present || id != "aaaa" {
		t.Fatalf("get after same-id rewrite = (%q,%v), want (aaaa,true)", id, present)
	}
	applyWrites(t, [][2]string{{"Function", "aaaa"}, {"Function", "aaaa"}})
}

func TestKindIDs_CollisionBlanks(t *testing.T) {
	// A second, DIFFERENT id blanks the entry to "" — present but ambiguous.
	var kid kindIDs
	kid.set("Function", "aaaa")
	kid.set("Function", "bbbb")
	id, present := kid.get("Function")
	if !present {
		t.Fatalf("blanked entry must remain PRESENT; got absent")
	}
	if id != "" {
		t.Fatalf("blanked entry id = %q, want \"\"", id)
	}
	applyWrites(t, [][2]string{{"Function", "aaaa"}, {"Function", "bbbb"}})
}

func TestKindIDs_CollisionInSpill(t *testing.T) {
	// Collision on a spilled (rest) entry, not just the inline slot.
	applyWrites(t, [][2]string{
		{"Function", "aaaa"},
		{"Method", "bbbb"},
		{"Method", "cccc"}, // collision on the spilled key -> blank
	})
}

func TestKindIDs_BlankThenSameKindStaysBlank(t *testing.T) {
	// Once blanked, a further different writer keeps it blank (map: existing=""
	// != newID -> stays "").
	applyWrites(t, [][2]string{
		{"Function", "aaaa"},
		{"Function", "bbbb"}, // -> ""
		{"Function", "cccc"}, // existing "" != cccc -> stays ""
	})
}

// TestKindIDs_ExhaustiveVsOracle fuzzes many write sequences drawn from a small
// alphabet of kinds and ids and asserts full parity with the map oracle.
func TestKindIDs_ExhaustiveVsOracle(t *testing.T) {
	kindsAlpha := []string{"K1", "K2", "K3"}
	idsAlpha := []string{"x", "y", "z"}
	// Enumerate all sequences of length up to 3.
	var gen func(prefix [][2]string, depth int)
	gen = func(prefix [][2]string, depth int) {
		applyWrites(t, prefix)
		if depth == 0 {
			return
		}
		for _, k := range kindsAlpha {
			for _, id := range idsAlpha {
				gen(append(append([][2]string(nil), prefix...), [2]string{k, id}), depth-1)
			}
		}
	}
	gen(nil, 3)
}

// TestKindIDs_EachDeterministicOrder pins that each()/insertion is inline-first
// then spill order — mirrors the insertion order the resolver replays so the M5
// parity harness (reflect.DeepEqual) stays valid.
func TestKindIDs_EachDeterministicOrder(t *testing.T) {
	var kid kindIDs
	kid.set("first", "1")
	kid.set("second", "2")
	kid.set("third", "3")
	var order []string
	kid.each(func(kind, id string) { order = append(order, kind) })
	want := []string{"first", "second", "third"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("each order = %v, want %v", order, want)
	}
	// sanity: sorted set identity independent of order
	sort.Strings(order)
	sort.Strings(want)
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("each key set = %v, want %v", order, want)
	}
}

// TestKindIDs_SingleEntryZeroAlloc proves the ~1-cardinality common case never
// touches the heap — the entire point of the compaction.
func TestKindIDs_SingleEntryZeroAlloc(t *testing.T) {
	allocs := testing.AllocsPerRun(1000, func() {
		var kid kindIDs
		kid.set("Function", "aaaaaaaaaaaaaaaa")
		if id, ok := kid.get("Function"); !ok || id == "" {
			t.Fatalf("unexpected get result")
		}
	})
	if allocs != 0 {
		t.Fatalf("single-entry set+get did %v heap allocs, want 0", allocs)
	}
}
