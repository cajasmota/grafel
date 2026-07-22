// view_materialize_5870_pr7a_test.go — deretain-flip PR7a (#5870).
//
// These tests are the byte-identity gate for the raw-slice → Reader-seam
// migration. The core simulation mirrors TestLabelIndexAt_ReaderSourcedBound_PR6:
// a flag-ON repo whose lr.Doc has been EMPTIED (the future slice-drop) must
// still produce the SAME result a full-Doc flag-OFF repo produces, sourcing the
// data from the mmap Reader instead of the now-empty raw slices. A helper or
// tool that still reads lr.Doc.Entities / lr.Doc.Relationships directly returns
// EMPTY here and fails — that is exactly the silent-empty regression this slice
// removes. A separate retired-Reader case proves the existing readRetired→Doc
// fallback still works unchanged.
package mcp

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbreader"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

func prPtrPR7a(f float64) *float64 { return &f }

// pr7aFixtureDoc builds one rich Document exercising every migrated tool:
//   - an IMPORTS cycle m1→m2→m3→m1 (FindImportCycles)
//   - Kind="Module" containers with member functions (RunModuleAlgorithms)
//   - production http_endpoint_definition + a test function + TESTS edge (coverage)
//   - an auth_policy entity + TAGGED_AS edge (auth coverage)
//   - CALLS/EXTENDS/DISCRIMINATES_ON edges carrying properties (relationshipAt)
func pr7aFixtureDoc() *graph.Document {
	ents := []graph.Entity{
		{ID: "m1", Name: "modA", Kind: "Module", SourceFile: "a/mod.go", Language: "go", PageRank: prPtrPR7a(0.9)},
		{ID: "m2", Name: "modB", Kind: "Module", SourceFile: "b/mod.go", Language: "go", PageRank: prPtrPR7a(0.5)},
		{ID: "m3", Name: "modC", Kind: "Module", SourceFile: "c/mod.go", Language: "go", PageRank: prPtrPR7a(0.2)},
		{ID: "fa", Name: "FnA", QualifiedName: "a.FnA", Kind: "SCOPE.Operation", SourceFile: "a/a.go", Language: "go", StartLine: 10, EndLine: 20, PageRank: prPtrPR7a(0.7)},
		{ID: "fb", Name: "FnB", QualifiedName: "b.FnB", Kind: "SCOPE.Operation", SourceFile: "b/b.go", Language: "go", StartLine: 5, EndLine: 15, PageRank: prPtrPR7a(0.3)},
		{ID: "ep", Name: "GetThing", Kind: "http_endpoint_definition", SourceFile: "a/api.go", Language: "go", StartLine: 30, EndLine: 40},
		{ID: "tf", Name: "TestFnA", Kind: "SCOPE.Operation", SourceFile: "a/a_test.go", Language: "go", StartLine: 1, EndLine: 8},
	}
	// module membership + auth policy props
	ents[3].PropSet("module", "modA")
	ents[4].PropSet("module", "modB")
	auth := graph.Entity{ID: "authp", Name: "RequireAuth", Kind: "SCOPE.Config", Subtype: "auth_policy", SourceFile: "a/api.go", Language: "go", StartLine: 28, EndLine: 28}
	// DRF class-auth + repo default-policy props (exercises buildDRFClassAuthByFile
	// + repoDRFDefaultPolicy). Kind SCOPE.Config is not a coverage/module/test
	// kind, so these props do not perturb the other tools' fixtures.
	auth.PropSet("has_permission_classes", "true")
	auth.PropSet("permission_classes", "IsAuthenticated")
	auth.PropSet("drf_default_permission_present", "true")
	auth.PropSet("drf_default_permission_classes", "IsAuthenticated")
	ents = append(ents, auth)

	mkRel := func(from, to, kind string, props map[string]string) graph.Relationship {
		r := graph.Relationship{FromID: from, ToID: to, Kind: kind}
		if props != nil {
			r.PropsReplace(props)
		}
		return r
	}
	rels := []graph.Relationship{
		mkRel("m1", "m2", "IMPORTS", nil),
		mkRel("m2", "m3", "IMPORTS", nil),
		mkRel("m3", "m1", "IMPORTS", nil),
		mkRel("fa", "fb", "CALLS", map[string]string{"line": "12", "resolved_by": "agent-repair", "resolved_by_agent": "bot"}),
		mkRel("fb", "fa", "CALLS", map[string]string{"line": "7"}),
		mkRel("tf", "ep", "TESTS", nil),
		mkRel("tf", "fa", "CALLS", nil),
		mkRel("ep", "authp", "TAGGED_AS", nil),
		mkRel("fa", "fb", "EXTENDS", map[string]string{"base_name": "b.FnB"}),
		mkRel("fa", "fb", "DISCRIMINATES_ON", map[string]string{"line": "12", "literal": "x"}),
	}
	return &graph.Document{Entities: ents, Relationships: rels}
}

// loadPR7aFixture writes the fixture and returns the loader-materialized Document
// plus an open Reader over the SAME file (so Reader-materialized rows are
// reflect.DeepEqual to the Document rows).
func loadPR7aFixture(t *testing.T) (*graph.Document, *fbreader.Reader) {
	t.Helper()
	dir := t.TempDir()
	fbPath := filepath.Join(dir, "graph.fb")
	if err := fbwriter.WriteAtomic(fbPath, pr7aFixtureDoc()); err != nil {
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
	t.Cleanup(func() { r.Close() })
	return doc, r
}

// docFullRepo builds a flag-OFF-style repo holding the full Doc plus its
// Doc-sourced LabelIndex (production flag-OFF repos always have one).
func docFullRepo(doc *graph.Document) *LoadedRepo {
	return &LoadedRepo{Repo: "corpus", Path: "corpus", Doc: doc, LabelIndex: BuildLabelIndex(doc)}
}

// readerEmptiedRepo builds a flag-ON-style repo: a live Reader + LabelIndex, but
// an EMPTIED Doc — the future slice-drop the migration must survive.
func readerEmptiedRepo(t *testing.T, doc *graph.Document, r *fbreader.Reader) *LoadedRepo {
	t.Helper()
	lr := &LoadedRepo{Repo: "corpus", Path: "corpus", Doc: doc, Reader: r}
	li := BuildLabelIndexFromReader(r, doc)
	li.readerMu = &lr.readerMu
	lr.LabelIndex = li
	lr.Doc = &graph.Document{} // emptied skeleton — Entities/Relationships nil
	return lr
}

// readerFullRepoRetired builds a flag-ON repo whose Reader is RETIRED (mapping
// unmapped) and whose Doc is FULL — the readRetired→Doc fallback must still work.
func readerFullRepoRetired(t *testing.T, doc *graph.Document, r *fbreader.Reader) *LoadedRepo {
	t.Helper()
	lr := &LoadedRepo{Repo: "corpus", Path: "corpus", Doc: doc, Reader: r}
	li := BuildLabelIndexFromReader(r, doc)
	li.readerMu = &lr.readerMu
	lr.LabelIndex = li
	lr.handle = &MapHandle{readRetired: true}
	return lr
}

func TestMaterializeAllEntities_ReaderSourced_PR7a(t *testing.T) {
	doc, r := loadPR7aFixture(t)

	withServeFromMMap(t, false)
	wantEnts := materializeAllEntities(docFullRepo(doc))
	if !reflect.DeepEqual(wantEnts, doc.Entities) {
		t.Fatalf("flag-OFF materializeAllEntities != raw Doc.Entities")
	}

	withServeFromMMap(t, true)
	lr := readerEmptiedRepo(t, doc, r)
	got := materializeAllEntities(lr)
	if len(got) != len(doc.Entities) {
		t.Fatalf("flag-ON emptied-Doc materializeAllEntities len=%d, want %d (raw-slice read would be 0)", len(got), len(doc.Entities))
	}
	if !reflect.DeepEqual(got, doc.Entities) {
		t.Fatalf("flag-ON emptied-Doc materializeAllEntities != raw Doc.Entities\n got=%#v\nwant=%#v", got, doc.Entities)
	}
}

func TestMaterializeAllRelationships_ReaderSourced_PR7a(t *testing.T) {
	doc, r := loadPR7aFixture(t)

	withServeFromMMap(t, false)
	if got := materializeAllRelationships(docFullRepo(doc)); !reflect.DeepEqual(got, doc.Relationships) {
		t.Fatalf("flag-OFF materializeAllRelationships != raw Doc.Relationships")
	}

	withServeFromMMap(t, true)
	lr := readerEmptiedRepo(t, doc, r)
	got := materializeAllRelationships(lr)
	if !reflect.DeepEqual(got, doc.Relationships) {
		t.Fatalf("flag-ON emptied-Doc materializeAllRelationships != raw Doc.Relationships (len got=%d want=%d)", len(got), len(doc.Relationships))
	}
}

func TestRelationshipAt_ReaderSourced_PR7a(t *testing.T) {
	doc, r := loadPR7aFixture(t)

	withServeFromMMap(t, true)
	lr := readerEmptiedRepo(t, doc, r)
	for i := range doc.Relationships {
		got := lr.relationshipAt(i)
		if got == nil {
			t.Fatalf("relationshipAt(%d) nil on emptied-Doc flag-ON repo (raw-slice read would be nil)", i)
		}
		want := &doc.Relationships[i]
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("relationshipAt(%d) mismatch\n got=%#v\nwant=%#v", i, got, want)
		}
	}
	// Out-of-range and synthetic indices → nil.
	if lr.relationshipAt(len(doc.Relationships)) != nil {
		t.Fatal("relationshipAt(len) should be nil")
	}
	if lr.relationshipAt(-1) != nil {
		t.Fatal("relationshipAt(-1) should be nil")
	}
}

func TestEntityRelCount_ReaderSourced_PR7a(t *testing.T) {
	doc, r := loadPR7aFixture(t)

	withServeFromMMap(t, false)
	if got := docFullRepo(doc).entityCount(); got != len(doc.Entities) {
		t.Fatalf("flag-OFF entityCount=%d want %d", got, len(doc.Entities))
	}
	if got := docFullRepo(doc).relCount(); got != len(doc.Relationships) {
		t.Fatalf("flag-OFF relCount=%d want %d", got, len(doc.Relationships))
	}

	withServeFromMMap(t, true)
	lr := readerEmptiedRepo(t, doc, r)
	if got := lr.entityCount(); got != len(doc.Entities) {
		t.Fatalf("flag-ON emptied-Doc entityCount=%d want %d (len(Doc.Entities) would be 0)", got, len(doc.Entities))
	}
	if got := lr.relCount(); got != len(doc.Relationships) {
		t.Fatalf("flag-ON emptied-Doc relCount=%d want %d (len(Doc.Relationships) would be 0)", got, len(doc.Relationships))
	}
}

// TestReaderSeams_RetiredFallback_PR7a proves the existing readRetired→Doc
// fallback is untouched: with a retired Reader and a FULL Doc, every seam falls
// back to the Doc and produces the full result.
func TestReaderSeams_RetiredFallback_PR7a(t *testing.T) {
	doc, r := loadPR7aFixture(t)
	withServeFromMMap(t, true)
	lr := readerFullRepoRetired(t, doc, r)

	if got := materializeAllEntities(lr); !reflect.DeepEqual(got, doc.Entities) {
		t.Fatalf("retired-Reader materializeAllEntities did not fall back to Doc (len got=%d want=%d)", len(got), len(doc.Entities))
	}
	if got := materializeAllRelationships(lr); !reflect.DeepEqual(got, doc.Relationships) {
		t.Fatalf("retired-Reader materializeAllRelationships did not fall back to Doc")
	}
	if got := lr.entityCount(); got != len(doc.Entities) {
		t.Fatalf("retired-Reader entityCount=%d want %d (must use Doc len)", got, len(doc.Entities))
	}
	if got := lr.relCount(); got != len(doc.Relationships) {
		t.Fatalf("retired-Reader relCount=%d want %d", got, len(doc.Relationships))
	}
	for i := range doc.Relationships {
		if got := lr.relationshipAt(i); !reflect.DeepEqual(got, &doc.Relationships[i]) {
			t.Fatalf("retired-Reader relationshipAt(%d) did not fall back to Doc", i)
		}
	}
}
