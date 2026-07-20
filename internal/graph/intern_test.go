// Tests for LOADER-SIDE string interning during graph.Document
// materialization (Tier-1b of grafel's resident-graph memory program).
//
// Tier-1a (fbwriter CreateSharedString, see fbwriter/sharedstring_test.go)
// shrinks the ON-DISK graph.fb by sharing identical string offsets at write
// time. That optimization is invisible to the LOADER: loadFBDocument calls
// string(fbBytes) once per field per record, which always allocates a fresh
// backing array — so N on-disk-shared occurrences of the same string still
// materialize as N independent heap allocations once loaded into a
// graph.Document. This test proves the loader itself dedupes high-duplication
// string fields (entity id/kind/subtype/module/source_file/language/property
// keys, relationship from_id/to_id/kind) through a per-load interner so that
// equal-valued strings share ONE backing array in the resident *Document.
package graph_test

import (
	"path/filepath"
	"testing"
	"unsafe"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// samePointer reports whether two strings share the same backing array by
// comparing their unsafe.StringData pointers. Empty strings are excluded by
// callers since Go may point all zero-length strings at a shared runtime
// symbol regardless of interning.
func samePointer(a, b string) bool {
	return unsafe.StringData(a) == unsafe.StringData(b)
}

// buildInternFixture builds a Document with many relationships that all
// share the same two from_id/to_id endpoints and many entities that share
// common kind/source_file/language/module values — the maximally-duplicated
// shape the loader interner should collapse (mirroring the ~8.7x average
// endpoint degree observed on the real corpus).
func buildInternFixture(nRels, nEnts int) *graph.Document {
	doc := &graph.Document{
		Repo: "intern-fixture",
	}
	for i := 0; i < nEnts; i++ {
		id := "entShared0000000A"
		if i%2 == 1 {
			id = "entShared0000000B"
		}
		doc.Entities = append(doc.Entities, graph.Entity{
			ID:            id + itoaSuffix(i),
			Name:          "Entity" + itoaSuffix(i), // high-cardinality, must NOT dedupe entities themselves
			QualifiedName: "pkg.Entity" + itoaSuffix(i),
			Kind:          "FUNCTION",
			Subtype:       "method",
			SourceFile:    "shared/pkg/file.go",
			Language:      "go",
			Properties:    map[string]string{"module": "pkg/shared"},
		})
	}
	// Two "anchor" entities that every relationship in this fixture points at.
	doc.Entities = append(doc.Entities,
		graph.Entity{ID: "anchorA00000000001", Name: "AnchorA", Kind: "FUNCTION", SourceFile: "anchor.go", Language: "go"},
		graph.Entity{ID: "anchorB00000000002", Name: "AnchorB", Kind: "FUNCTION", SourceFile: "anchor.go", Language: "go"},
	)
	for i := 0; i < nRels; i++ {
		doc.Relationships = append(doc.Relationships, graph.Relationship{
			FromID: "anchorA00000000001",
			ToID:   "anchorB00000000002",
			Kind:   "CALLS",
		})
	}
	doc.Stats.Entities = len(doc.Entities)
	doc.Stats.Relationships = len(doc.Relationships)
	return doc
}

// itoaSuffix avoids importing strconv just for a handful of test fixture IDs.
func itoaSuffix(i int) string {
	digits := "0123456789"
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{digits[i%10]}, b...)
		i /= 10
	}
	return string(b)
}

// TestLoaderIntern_RoundtripCorrectness asserts that interning never drops or
// corrupts a field: every entity and relationship must read back with its
// exact original field values after passing through the loader interner.
func TestLoaderIntern_RoundtripCorrectness(t *testing.T) {
	doc := buildInternFixture(20, 10)
	dir := t.TempDir()
	if err := fbwriter.WriteAtomic(filepath.Join(dir, "graph.fb"), doc); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}
	got, err := graph.LoadGraphFromDir(dir)
	if err != nil {
		t.Fatalf("LoadGraphFromDir: %v", err)
	}
	if len(got.Entities) != len(doc.Entities) {
		t.Fatalf("entity count: got %d want %d", len(got.Entities), len(doc.Entities))
	}
	if len(got.Relationships) != len(doc.Relationships) {
		t.Fatalf("relationship count: got %d want %d", len(got.Relationships), len(doc.Relationships))
	}

	byID := make(map[string]graph.Entity, len(got.Entities))
	for _, e := range got.Entities {
		byID[e.ID] = e
	}
	for _, want := range doc.Entities {
		gotEnt, ok := byID[want.ID]
		if !ok {
			t.Fatalf("entity %q missing after load", want.ID)
		}
		if gotEnt.Name != want.Name {
			t.Errorf("entity %q Name: got %q want %q", want.ID, gotEnt.Name, want.Name)
		}
		if gotEnt.QualifiedName != want.QualifiedName {
			t.Errorf("entity %q QualifiedName: got %q want %q", want.ID, gotEnt.QualifiedName, want.QualifiedName)
		}
		if gotEnt.Kind != want.Kind {
			t.Errorf("entity %q Kind: got %q want %q", want.ID, gotEnt.Kind, want.Kind)
		}
		if gotEnt.Subtype != want.Subtype {
			t.Errorf("entity %q Subtype: got %q want %q", want.ID, gotEnt.Subtype, want.Subtype)
		}
		if gotEnt.SourceFile != want.SourceFile {
			t.Errorf("entity %q SourceFile: got %q want %q", want.ID, gotEnt.SourceFile, want.SourceFile)
		}
		if gotEnt.Language != want.Language {
			t.Errorf("entity %q Language: got %q want %q", want.ID, gotEnt.Language, want.Language)
		}
		for k, v := range want.Properties {
			if gotEnt.Properties[k] != v {
				t.Errorf("entity %q Properties[%q]: got %q want %q", want.ID, k, gotEnt.Properties[k], v)
			}
		}
	}

	for i, want := range doc.Relationships {
		gotRel := got.Relationships[i]
		if gotRel.FromID != want.FromID || gotRel.ToID != want.ToID || gotRel.Kind != want.Kind {
			t.Errorf("relationship %d: got {%q %q %q} want {%q %q %q}",
				i, gotRel.FromID, gotRel.ToID, gotRel.Kind, want.FromID, want.ToID, want.Kind)
		}
	}
}

// TestLoaderIntern_SharesBackingArray is the RED/GREEN proof: it verifies
// that repeated occurrences of the same string VALUE in the loaded Document
// share ONE backing array (same unsafe.StringData pointer), rather than each
// occurrence carrying its own independently-allocated copy. This is the
// resident-memory win a loader-side interner delivers on top of Tier-1a's
// on-disk CreateSharedString (which only shrinks graph.fb, not the RAM
// footprint of the materialized Document — the loader deep-copies every
// string out of the mmap'd bytes regardless of on-disk sharing).
func TestLoaderIntern_SharesBackingArray(t *testing.T) {
	const nRels = 200
	const nEnts = 50
	doc := buildInternFixture(nRels, nEnts)
	dir := t.TempDir()
	if err := fbwriter.WriteAtomic(filepath.Join(dir, "graph.fb"), doc); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}
	got, err := graph.LoadGraphFromDir(dir)
	if err != nil {
		t.Fatalf("LoadGraphFromDir: %v", err)
	}
	if len(got.Relationships) != nRels {
		t.Fatalf("relationship count: got %d want %d", len(got.Relationships), nRels)
	}

	// (1) Every relationship's FromID must share backing storage with every
	// other relationship's FromID (all equal to "anchorA00000000001") — the
	// single biggest resident win: N endpoint references collapse to one
	// backing array instead of N independent copies.
	first := got.Relationships[0]
	for i := 1; i < len(got.Relationships); i++ {
		rel := got.Relationships[i]
		if !samePointer(first.FromID, rel.FromID) {
			t.Fatalf("relationship[%d].FromID does not share backing storage with relationship[0].FromID — from_id values are not interned", i)
		}
		if !samePointer(first.ToID, rel.ToID) {
			t.Fatalf("relationship[%d].ToID does not share backing storage with relationship[0].ToID — to_id values are not interned", i)
		}
		if !samePointer(first.Kind, rel.Kind) {
			t.Fatalf("relationship[%d].Kind does not share backing storage with relationship[0].Kind — relationship kind values are not interned", i)
		}
	}

	// (2) Critical case: a relationship endpoint (FromID/ToID) MUST share
	// backing storage with the corresponding entity's ID field — proving the
	// interner is a single shared map across entity and relationship
	// construction within one load, not two independent per-type interners.
	var anchorA, anchorB *graph.Entity
	for i := range got.Entities {
		switch got.Entities[i].ID {
		case "anchorA00000000001":
			anchorA = &got.Entities[i]
		case "anchorB00000000002":
			anchorB = &got.Entities[i]
		}
	}
	if anchorA == nil || anchorB == nil {
		t.Fatalf("anchor entities missing after load")
	}
	if !samePointer(anchorA.ID, first.FromID) {
		t.Error("entity anchorA.ID does not share backing storage with relationship FromID referencing it — entity id and relationship endpoint strings are not interned through the same map")
	}
	if !samePointer(anchorB.ID, first.ToID) {
		t.Error("entity anchorB.ID does not share backing storage with relationship ToID referencing it — entity id and relationship endpoint strings are not interned through the same map")
	}

	// (3) Entity-side high-duplication fields (Kind/SourceFile/Language and
	// property keys) must also share backing storage across entities.
	var sample []graph.Entity
	for _, e := range got.Entities {
		if e.SourceFile == "shared/pkg/file.go" {
			sample = append(sample, e)
		}
	}
	if len(sample) < 2 {
		t.Fatalf("expected at least 2 fixture entities with the shared source file, got %d", len(sample))
	}
	for i := 1; i < len(sample); i++ {
		if !samePointer(sample[0].Kind, sample[i].Kind) {
			t.Errorf("entity[%d].Kind does not share backing storage with entity[0].Kind", i)
		}
		if !samePointer(sample[0].SourceFile, sample[i].SourceFile) {
			t.Errorf("entity[%d].SourceFile does not share backing storage with entity[0].SourceFile", i)
		}
		if !samePointer(sample[0].Language, sample[i].Language) {
			t.Errorf("entity[%d].Language does not share backing storage with entity[0].Language", i)
		}
		// Name is genuinely unique per entity in this fixture and must NOT be
		// forced to share storage — the interner must not touch high-cardinality
		// fields (this would just bloat the interner map for no benefit).
		if samePointer(sample[0].Name, sample[i].Name) {
			t.Errorf("entity[%d].Name unexpectedly shares backing storage with entity[0].Name — Name is high-cardinality and must not be interned", i)
		}
	}
}
