// Package extractors — emission_order_test.go
//
// Regression tests for issue #5974: the incremental reindex path emitted
// entities in a non-ID order, while fbreader.Reader.LookupEntityByID is a bare
// FlatBuffers `(key)` binary search with no linear fallback. Any graph written
// by the incremental producer therefore lost lookups for entities that were
// demonstrably present in the vector.
//
// These live in-package (not extractors_test) so they can drive the producer's
// own sortGraphDocumentForEmission directly.
package extractors

import (
	"fmt"
	"os"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	fb "github.com/cajasmota/grafel/internal/graph/fbgraph"
	"github.com/cajasmota/grafel/internal/graph/fbreader"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// emissionFixture builds a document whose natural (SourceFile, Kind, Name)
// order is the exact reverse of its ID order. Under the pre-#5974 comparator
// the emitted entity vector came out ID-descending, which is precisely what
// EntitiesByKey's binary search cannot cope with.
func emissionFixture(n int) *graph.Document {
	doc := &graph.Document{Repo: "fixture"}
	for i := 0; i < n; i++ {
		doc.Entities = append(doc.Entities, graph.Entity{
			ID:            fmt.Sprintf("ent-%03d", i),
			Name:          fmt.Sprintf("Sym%03d", i),
			QualifiedName: fmt.Sprintf("pkg.Sym%03d", i),
			Kind:          "function",
			SourceFile:    fmt.Sprintf("src/f%03d.go", n-i),
			StartLine:     n - i,
		})
	}
	for i := 0; i+1 < n; i++ {
		from, to := doc.Entities[i].ID, doc.Entities[i+1].ID
		doc.Relationships = append(doc.Relationships, graph.Relationship{
			ID:     graph.RelationshipID(from, to, "CALLS"),
			FromID: from,
			ToID:   to,
			Kind:   "CALLS",
		})
	}
	return doc
}

// writeFlatAndOpen runs the incremental producer's emission sort, writes the
// document through the flat gen writer (the flag-OFF default path) and opens
// the result with the real reader.
func writeFlatAndOpen(t *testing.T, doc *graph.Document) *fbreader.Reader {
	t.Helper()
	sortGraphDocumentForEmission(doc)
	stateDir := t.TempDir()
	genPath, err := fbwriter.WriteGraphGen(stateDir, doc)
	if err != nil {
		t.Fatalf("WriteGraphGen: %v", err)
	}
	if fi, err := os.Stat(genPath); err != nil || fi.IsDir() {
		t.Fatalf("expected a flat gen file at %s (err=%v)", genPath, err)
	}
	r, err := fbreader.Open(genPath)
	if err != nil {
		t.Fatalf("fbreader.Open: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

// TestIncrementalEmission_EveryEntityIsLookupable is the test that would have
// caught #5974: every entity reachable via IterateEntities must also be
// reachable via LookupEntityByID.
func TestIncrementalEmission_EveryEntityIsLookupable(t *testing.T) {
	doc := emissionFixture(64)
	want := len(doc.Entities)
	r := writeFlatAndOpen(t, doc)

	var seen int
	var missing []string
	r.IterateEntities(func(e *fb.Entity) bool {
		seen++
		id := string(e.Id())
		if r.LookupEntityByID(id) == nil {
			missing = append(missing, id)
		}
		return true
	})
	if seen != want {
		t.Fatalf("IterateEntities visited %d entities, want %d", seen, want)
	}
	if len(missing) > 0 {
		t.Fatalf("LookupEntityByID missed %d/%d entities present in the vector (first few: %v)",
			len(missing), seen, missing[:min(5, len(missing))])
	}
}

// TestIncrementalEmission_EntityVectorIsIDSorted asserts the on-disk invariant
// the binary search depends on, directly.
func TestIncrementalEmission_EntityVectorIsIDSorted(t *testing.T) {
	doc := emissionFixture(64)
	r := writeFlatAndOpen(t, doc)

	prev := ""
	first := true
	r.IterateEntities(func(e *fb.Entity) bool {
		id := string(e.Id())
		if !first && id < prev {
			t.Errorf("entity vector not ID-sorted: %q follows %q", id, prev)
		}
		prev, first = id, false
		return true
	})
}

// TestIncrementalEmission_ComparatorSortsEntitiesByID pins the comparator
// itself, independent of the writer. The gen writers also canonicalise
// defensively, so without this assertion a regression in the producer's own
// sort would be masked at the reader level.
func TestIncrementalEmission_ComparatorSortsEntitiesByID(t *testing.T) {
	doc := emissionFixture(64)
	sortGraphDocumentForEmission(doc)
	for i := 1; i < len(doc.Entities); i++ {
		if doc.Entities[i-1].ID > doc.Entities[i].ID {
			t.Fatalf("entities not ID-sorted at %d: %q then %q",
				i, doc.Entities[i-1].ID, doc.Entities[i].ID)
		}
	}
}

// TestIncrementalEmission_RelationshipsCanonicallyOrdered pins the
// relationship order to (FromID, ToID, Kind, ID) — the same tuple the
// full-index path uses.
func TestIncrementalEmission_RelationshipsCanonicallyOrdered(t *testing.T) {
	doc := emissionFixture(32)
	sortGraphDocumentForEmission(doc)
	for i := 1; i < len(doc.Relationships); i++ {
		a, b := doc.Relationships[i-1], doc.Relationships[i]
		if a.FromID > b.FromID ||
			(a.FromID == b.FromID && a.ToID > b.ToID) ||
			(a.FromID == b.FromID && a.ToID == b.ToID && a.Kind > b.Kind) ||
			(a.FromID == b.FromID && a.ToID == b.ToID && a.Kind == b.Kind && a.ID > b.ID) {
			t.Fatalf("relationships out of canonical order at %d: %+v then %+v", i, a, b)
		}
	}
}
