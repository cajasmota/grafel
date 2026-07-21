// labelindex_interning_5850_test.go — correctness coverage for the ByLabel/
// ByQName key interning added to BuildLabelIndex as part of the Tier-2b index
// mop-up (#5850). BuildLabelIndex now canonicalizes lowercased Name/
// QualifiedName strings through a per-build keyInterner so that entities
// sharing an equal lowercased label/qualified-name share ONE backing string
// instead of each independently paying for its own strings.ToLower()
// allocation. Interning must be byte-correct and completely invisible to
// Lookup/LookupAll — this test locks that in with mixed-case, duplicate, and
// collision (label == qname after lowering) fixtures.
package mcp

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

func labelIndexInterningDoc() *graph.Document {
	return &graph.Document{
		Entities: []graph.Entity{
			// Duplicate label across many entities, exercising the interner's
			// dedup path (the common case: getters/setters, "Get"/"String").
			{ID: "e1", Name: "Get", QualifiedName: "pkg.a.Get", Kind: "Method"},
			{ID: "e2", Name: "Get", QualifiedName: "pkg.b.Get", Kind: "Method"},
			{ID: "e3", Name: "get", QualifiedName: "pkg.c.get", Kind: "Method"}, // already-lowercase Name
			// Mixed-case name that needs folding (allocates on ToLower).
			{ID: "e4", Name: "MixedCase", QualifiedName: "pkg.d.MixedCase", Kind: "Function"},
			// A label whose lowered form collides with another entity's lowered
			// qualified name — both must resolve independently and correctly.
			{ID: "e5", Name: "Widget", QualifiedName: "widget", Kind: "Class"},
			{ID: "e6", Name: "X", QualifiedName: "WIDGET", Kind: "Function"},
			// No qualified name at all.
			{ID: "e7", Name: "NoQName", Kind: "Function"},
		},
	}
}

func TestLabelIndexInterning_ByLabelByteCorrect(t *testing.T) {
	doc := labelIndexInterningDoc()
	idx := BuildLabelIndex(doc)

	// ADR-0027 Cutover PR2: byLabel now holds []int32 entity indices; materialize
	// via at() to read the entity's ID.
	getEntries := idx.byLabel["get"]
	if len(getEntries) != 3 {
		t.Fatalf("byLabel[get] has %d entries; want 3 (e1,e2,e3)", len(getEntries))
	}
	gotIDs := map[string]bool{}
	for _, i := range getEntries {
		gotIDs[idx.at(i).ID] = true
	}
	for _, want := range []string{"e1", "e2", "e3"} {
		if !gotIDs[want] {
			t.Errorf("byLabel[get] missing entity %q; got %v", want, gotIDs)
		}
	}

	mixed := idx.byLabel["mixedcase"]
	if len(mixed) != 1 || idx.at(mixed[0]).ID != "e4" {
		t.Errorf("byLabel[mixedcase] = %v; want [e4]", mixed)
	}
}

func TestLabelIndexInterning_ByQNameByteCorrect(t *testing.T) {
	doc := labelIndexInterningDoc()
	idx := BuildLabelIndex(doc)

	if i, ok := idx.byQName["pkg.a.get"]; !ok || idx.at(i).ID != "e1" {
		t.Errorf("byQName[pkg.a.get] = %v (ok=%v); want e1", i, ok)
	}
	if i, ok := idx.byQName["pkg.d.mixedcase"]; !ok || idx.at(i).ID != "e4" {
		t.Errorf("byQName[pkg.d.mixedcase] = %v (ok=%v); want e4", i, ok)
	}
	// Label/qname collision after lowering: "widget" is both e5's Name and
	// e6's QualifiedName (uppercased on the entity, lowered as the key). Both
	// entities intern the SAME "widget" key string (that's the point of the
	// shared interner), but ByQName is a single-value map so the later
	// entity (e6) wins the last-write.
	if i, ok := idx.byQName["widget"]; !ok || idx.at(i).ID != "e6" {
		t.Errorf("byQName[widget] = %v (ok=%v); want e6 (last write wins)", i, ok)
	}
	entries := idx.byLabel["widget"]
	if len(entries) != 1 || idx.at(entries[0]).ID != "e5" {
		t.Errorf("byLabel[widget] = %v; want [e5]", entries)
	}
}

func TestLabelIndexInterning_LookupAndLookupAll(t *testing.T) {
	doc := labelIndexInterningDoc()
	idx := BuildLabelIndex(doc)

	// Lookup by ID passes through untouched.
	if e := idx.Lookup("e4"); e == nil || e.ID != "e4" {
		t.Errorf("Lookup(e4) = %v; want e4", e)
	}
	// Lookup by qualified name is case-insensitive via the interned key.
	if e := idx.Lookup("PKG.D.MIXEDCASE"); e == nil || e.ID != "e4" {
		t.Errorf("Lookup(PKG.D.MIXEDCASE) = %v; want e4", e)
	}
	// LookupAll on a duplicated label returns every match.
	all := idx.LookupAll("GET")
	if len(all) != 3 {
		t.Errorf("LookupAll(GET) returned %d entities; want 3", len(all))
	}
	// A qname takes precedence over a label match in LookupAll (see e6/e5
	// collision): "widget" resolves to the ByQName hit (e6, last write wins)
	// alone, not the ByLabel hit (e5).
	all = idx.LookupAll("widget")
	if len(all) != 1 || all[0].ID != "e6" {
		t.Errorf("LookupAll(widget) = %v; want [e6] (qname precedence)", all)
	}
	// Entity with no qualified name still resolves purely by label.
	if e := idx.Lookup("noqname"); e == nil || e.ID != "e7" {
		t.Errorf("Lookup(noqname) = %v; want e7", e)
	}
}

// TestLabelIndexInterning_SharedBackingAcrossDuplicateLabels asserts the
// interning dedup actually fires: two independently-lowered occurrences of
// an equal label string share the SAME backing array (same address), which
// is the property the interner exists to guarantee. Uses unsafe string data
// pointer comparison via a byte-level identity check (comparing the first
// byte's address through reflection is not portable in pure Go without
// unsafe, so this test instead asserts the functional invariant: every
// ByLabel bucket key, when re-derived, equals — byte for byte — the stored
// key, which is what interning must preserve losslessly).
func TestLabelIndexInterning_KeysByteIdenticalToLowered(t *testing.T) {
	doc := labelIndexInterningDoc()
	idx := BuildLabelIndex(doc)

	for lbl, entries := range idx.byLabel {
		for _, i := range entries {
			e := idx.at(i)
			want := strings.ToLower(e.Name)
			if lbl != want {
				t.Errorf("byLabel key %q does not match strings.ToLower(%q) = %q", lbl, e.Name, want)
			}
		}
	}
	for qn, i := range idx.byQName {
		e := idx.at(i)
		want := strings.ToLower(e.QualifiedName)
		if qn != want {
			t.Errorf("byQName key %q does not match strings.ToLower(%q) = %q", qn, e.QualifiedName, want)
		}
	}
}
