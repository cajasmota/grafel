// ADR-0027 Cutover PR1: the exported per-row materializers
// (MaterializeEntity / MaterializeRelationship) must produce a graph.Entity /
// graph.Relationship byte-identical to the row loadFBDocument materializes for
// the same graph.fb. This is the contract later PRs (loader flip) rely on when
// they source rows from the mmap Reader on demand instead of the Document.
package graph_test

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbreader"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

func f64(v float64) *float64 { return &v }

// materializeDoc exercises the fields the materializers copy: interned
// id/kind/subtype/module/source_file/language, high-cardinality
// name/qualified_name/signature, multi/empty/zero property sets, and the
// Pass-4 pagerank scalar (present and absent).
func materializeDoc() *graph.Document {
	e0 := graph.Entity{
		ID: "r::pkg.Foo", Name: "Foo", QualifiedName: "pkg.Foo", Kind: "type",
		Subtype: "struct", SourceFile: "pkg/foo.go", Language: "go",
		Signature: "type Foo struct{}", StartLine: 10, PageRank: f64(0.42),
	}
	e0.PropsReplace(map[string]string{"module": "pkg", "visibility": "public", "empty": ""})

	// Empty signature/subtype, no module property, no pagerank (nil pointer).
	e1 := graph.Entity{
		ID: "r::pkg.bar", Name: "bar", QualifiedName: "pkg.bar", Kind: "func",
		SourceFile: "pkg/bar.go", Language: "go",
	}

	// No properties at all.
	e2 := graph.Entity{
		ID: "r::pkg.Baz", Name: "Baz", QualifiedName: "pkg.Baz", Kind: "const",
		SourceFile: "pkg/baz.go", Language: "go", PageRank: f64(0.01),
	}

	r0 := graph.Relationship{FromID: "r::pkg.Foo", ToID: "r::pkg.bar", Kind: "CALLS"}
	r0.PropsReplace(map[string]string{"id": "edge-0001", "count": "3", "line": "12"})

	// Relationship without an id property.
	r1 := graph.Relationship{FromID: "r::pkg.bar", ToID: "r::pkg.Baz", Kind: "references"}

	return &graph.Document{
		Entities:      []graph.Entity{e0, e1, e2},
		Relationships: []graph.Relationship{r0, r1},
	}
}

// TestMaterializeEntityByteEqualsDocument_PR1 proves MaterializeEntity(r, i) is
// reflect.DeepEqual to the loader's Document.Entities[i] for every row.
func TestMaterializeEntityByteEqualsDocument_PR1(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fbPath := filepath.Join(dir, "graph.fb")
	if err := fbwriter.WriteAtomic(fbPath, materializeDoc()); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}
	doc, err := graph.LoadGraphFromDir(dir)
	if err != nil {
		t.Fatalf("LoadGraphFromDir: %v", err)
	}
	r, err := fbreader.Open(fbPath)
	if err != nil {
		t.Fatalf("fbreader.Open: %v", err)
	}
	defer r.Close()

	if got, want := r.EntityCount(), len(doc.Entities); got != want {
		t.Fatalf("EntityCount %d != len(doc.Entities) %d", got, want)
	}
	for i := range doc.Entities {
		got := graph.MaterializeEntity(r, i)
		if !reflect.DeepEqual(got, doc.Entities[i]) {
			t.Errorf("entity %d: MaterializeEntity != doc.Entities[i]\n got=%#v\nwant=%#v", i, got, doc.Entities[i])
		}
	}
}

// TestMaterializeRelationshipByteEqualsDocument_PR1 proves
// MaterializeRelationship(r, i) is reflect.DeepEqual to
// Document.Relationships[i] for every row.
func TestMaterializeRelationshipByteEqualsDocument_PR1(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fbPath := filepath.Join(dir, "graph.fb")
	if err := fbwriter.WriteAtomic(fbPath, materializeDoc()); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}
	doc, err := graph.LoadGraphFromDir(dir)
	if err != nil {
		t.Fatalf("LoadGraphFromDir: %v", err)
	}
	r, err := fbreader.Open(fbPath)
	if err != nil {
		t.Fatalf("fbreader.Open: %v", err)
	}
	defer r.Close()

	if got, want := r.RelationshipCount(), len(doc.Relationships); got != want {
		t.Fatalf("RelationshipCount %d != len(doc.Relationships) %d", got, want)
	}
	for i := range doc.Relationships {
		got := graph.MaterializeRelationship(r, i)
		if !reflect.DeepEqual(got, doc.Relationships[i]) {
			t.Errorf("relationship %d: MaterializeRelationship != doc.Relationships[i]\n got=%#v\nwant=%#v", i, got, doc.Relationships[i])
		}
	}
}

// TestMaterializeOutOfRange_PR1 documents the nil-reader / out-of-range
// contract: the zero value, never a panic.
func TestMaterializeOutOfRange_PR1(t *testing.T) {
	t.Parallel()
	if got := graph.MaterializeEntity(nil, 0); !reflect.DeepEqual(got, graph.Entity{}) {
		t.Errorf("MaterializeEntity(nil,0) = %#v, want zero", got)
	}
	if got := graph.MaterializeRelationship(nil, 0); !reflect.DeepEqual(got, graph.Relationship{}) {
		t.Errorf("MaterializeRelationship(nil,0) = %#v, want zero", got)
	}

	dir := t.TempDir()
	fbPath := filepath.Join(dir, "graph.fb")
	if err := fbwriter.WriteAtomic(fbPath, materializeDoc()); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}
	r, err := fbreader.Open(fbPath)
	if err != nil {
		t.Fatalf("fbreader.Open: %v", err)
	}
	defer r.Close()
	if got := graph.MaterializeEntity(r, 9999); !reflect.DeepEqual(got, graph.Entity{}) {
		t.Errorf("MaterializeEntity(r, oob) = %#v, want zero", got)
	}
	if got := graph.MaterializeRelationship(r, 9999); !reflect.DeepEqual(got, graph.Relationship{}) {
		t.Errorf("MaterializeRelationship(r, oob) = %#v, want zero", got)
	}
}
