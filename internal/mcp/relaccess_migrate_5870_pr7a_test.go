// relaccess_migrate_5870_pr7a_test.go — deretain-flip PR7a (#5870).
//
// Result-equivalence for the relationship random-access migrations: the
// adjacency-relIdx property lookups (inspect discriminators/semantic edges,
// mro extendsBases, effective-contract relPropsFor) now source the backing
// relationship via lr.relationshipAt instead of the raw lr.Doc.Relationships
// slice. Each asserts the migrated function on a flag-ON emptied-Doc repo
// (Reader-sourced) is byte-identical to the flag-OFF full-Doc result.
package mcp

import (
	"reflect"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

func entByID(doc *graph.Document, id string) *graph.Entity {
	for i := range doc.Entities {
		if doc.Entities[i].ID == id {
			return &doc.Entities[i]
		}
	}
	return nil
}

func TestExtendsBases_ReaderParity_PR7a(t *testing.T) {
	doc, r := loadPR7aFixture(t)
	fa := entByID(doc, "fa")

	withServeFromMMap(t, false)
	want := extendsBases(docFullRepo(doc), fa)
	if len(want) == 0 {
		t.Fatal("fixture must have an EXTENDS base for fa")
	}

	withServeFromMMap(t, true)
	got := extendsBases(readerEmptiedRepo(t, doc, r), fa)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("extendsBases flag-ON(emptied Doc) != flag-OFF\n got=%#v\nwant=%#v", got, want)
	}
	if fb := extendsBases(readerFullRepoRetired(t, doc, r), fa); !reflect.DeepEqual(fb, want) {
		t.Fatalf("extendsBases retired-Reader fallback != flag-OFF")
	}
}

func TestRelPropsFor_ReaderParity_PR7a(t *testing.T) {
	doc, r := loadPR7aFixture(t)

	withServeFromMMap(t, true)
	lrOn := readerEmptiedRepo(t, doc, r)
	withServeFromMMap(t, false)
	lrOff := docFullRepo(doc)

	for i := range doc.Relationships {
		withServeFromMMap(t, false)
		want := relPropsFor(lrOff, i)
		withServeFromMMap(t, true)
		got := relPropsFor(lrOn, i)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("relPropsFor(%d) flag-ON(emptied Doc) != flag-OFF\n got=%#v\nwant=%#v", i, got, want)
		}
	}
	// Synthetic index -> nil on both paths.
	withServeFromMMap(t, true)
	if relPropsFor(lrOn, -1) != nil {
		t.Fatal("relPropsFor(-1) must be nil")
	}
}

func TestInspectDiscriminators_ReaderParity_PR7a(t *testing.T) {
	doc, r := loadPR7aFixture(t)
	fa := entByID(doc, "fa")

	withServeFromMMap(t, false)
	want := inspectDiscriminators(docFullRepo(doc), fa, true)

	withServeFromMMap(t, true)
	got := inspectDiscriminators(readerEmptiedRepo(t, doc, r), fa, true)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("inspectDiscriminators flag-ON(emptied Doc) != flag-OFF\n got=%#v\nwant=%#v", got, want)
	}
	if fb := inspectDiscriminators(readerFullRepoRetired(t, doc, r), fa, true); !reflect.DeepEqual(fb, want) {
		t.Fatalf("inspectDiscriminators retired-Reader fallback != flag-OFF")
	}
}

func TestInspectSemanticEdges_ReaderParity_PR7a(t *testing.T) {
	doc, r := loadPR7aFixture(t)
	fa := entByID(doc, "fa")

	withServeFromMMap(t, false)
	want := inspectSemanticEdges(docFullRepo(doc), fa, true, isSemanticEdgeKind)

	withServeFromMMap(t, true)
	got := inspectSemanticEdges(readerEmptiedRepo(t, doc, r), fa, true, isSemanticEdgeKind)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("inspectSemanticEdges flag-ON(emptied Doc) != flag-OFF\n got=%#v\nwant=%#v", got, want)
	}
	if fb := inspectSemanticEdges(readerFullRepoRetired(t, doc, r), fa, true, isSemanticEdgeKind); !reflect.DeepEqual(fb, want) {
		t.Fatalf("inspectSemanticEdges retired-Reader fallback != flag-OFF")
	}
}
