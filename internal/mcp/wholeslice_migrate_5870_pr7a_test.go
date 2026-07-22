// wholeslice_migrate_5870_pr7a_test.go — deretain-flip PR7a (#5870).
//
// Result-equivalence for the whole-slice analytical migrations (import cycles,
// module algorithms, orientation, coverage, doc-semantics). Each asserts the
// migrated computation on a flag-ON repo with an EMPTIED Doc (Reader-sourced,
// simulating the future slice-drop) is byte-identical to the flag-OFF full-Doc
// result — and non-trivial (the fixture exercises the tool). A raw-slice
// regression returns EMPTY on the emptied-Doc repo and fails.
package mcp

import (
	"reflect"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// TestImportCycles_ReaderParity_PR7a exercises the cycles_tools.go migration:
// FindImportCycles + buildPageRankMap fed the materialize seam.
func TestImportCycles_ReaderParity_PR7a(t *testing.T) {
	doc, r := loadPR7aFixture(t)

	compute := func(lr *LoadedRepo) []graph.ImportCycle {
		ents := materializeAllEntities(lr)
		rels := materializeAllRelationships(lr)
		return graph.FindImportCycles(ents, rels, buildPageRankMap(ents))
	}

	withServeFromMMap(t, false)
	want := compute(docFullRepo(doc))
	if len(want) == 0 {
		t.Fatal("fixture must contain an import cycle")
	}

	withServeFromMMap(t, true)
	got := compute(readerEmptiedRepo(t, doc, r))
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("import cycles flag-ON(emptied Doc) != flag-OFF\n got=%#v\nwant=%#v", got, want)
	}

	// Retired-Reader fallback: same result via the Doc.
	if fb := compute(readerFullRepoRetired(t, doc, r)); !reflect.DeepEqual(fb, want) {
		t.Fatalf("import cycles retired-Reader fallback != flag-OFF")
	}
}

// TestModuleAlgorithms_ReaderParity_PR7a exercises computeModuleAnalysis (the
// real handler seam) across both flag paths.
func TestModuleAlgorithms_ReaderParity_PR7a(t *testing.T) {
	doc, r := loadPR7aFixture(t)

	withServeFromMMap(t, false)
	want := computeModuleAnalysis([]*LoadedRepo{docFullRepo(doc)})
	if len(want) == 0 || want[0].Res == nil || len(want[0].Res.ModuleIDs) == 0 {
		t.Fatal("fixture must contain modules")
	}

	withServeFromMMap(t, true)
	got := computeModuleAnalysis([]*LoadedRepo{readerEmptiedRepo(t, doc, r)})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("module algorithms flag-ON(emptied Doc) != flag-OFF\n got=%#v\nwant=%#v", got, want)
	}
	if fb := computeModuleAnalysis([]*LoadedRepo{readerFullRepoRetired(t, doc, r)}); !reflect.DeepEqual(fb, want) {
		t.Fatalf("module algorithms retired-Reader fallback != flag-OFF")
	}
}

// TestOrientation_ReaderParity_PR7a exercises the tools.go AnalyzeOrientation
// migration.
func TestOrientation_ReaderParity_PR7a(t *testing.T) {
	doc, r := loadPR7aFixture(t)
	opts := graph.OrientationOptions{}

	compute := func(lr *LoadedRepo) graph.OrientationResult {
		return graph.AnalyzeOrientation(materializeAllEntities(lr), materializeAllRelationships(lr), opts)
	}

	withServeFromMMap(t, false)
	want := compute(docFullRepo(doc))

	withServeFromMMap(t, true)
	got := compute(readerEmptiedRepo(t, doc, r))
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("orientation flag-ON(emptied Doc) != flag-OFF")
	}
	if len(want.KeyEntities) == 0 {
		t.Fatal("fixture must produce orientation key entities")
	}
	if fb := compute(readerFullRepoRetired(t, doc, r)); !reflect.DeepEqual(fb, want) {
		t.Fatalf("orientation retired-Reader fallback != flag-OFF")
	}
}

// TestCoverage_ReaderParity_PR7a exercises the coverage_tools.go migration via
// the materializedDoc adapter (ComputeCoverage + ComputeEntityCoverage).
func TestCoverage_ReaderParity_PR7a(t *testing.T) {
	doc, r := loadPR7aFixture(t)

	withServeFromMMap(t, false)
	want := graph.ComputeCoverage(materializedDoc(docFullRepo(doc)))
	if want.TotalProduction == 0 {
		t.Fatal("fixture must contain production entities for coverage")
	}

	withServeFromMMap(t, true)
	got := graph.ComputeCoverage(materializedDoc(readerEmptiedRepo(t, doc, r)))
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("coverage flag-ON(emptied Doc) != flag-OFF\n got=%#v\nwant=%#v", got, want)
	}
	if fb := graph.ComputeCoverage(materializedDoc(readerFullRepoRetired(t, doc, r))); !reflect.DeepEqual(fb, want) {
		t.Fatalf("coverage retired-Reader fallback != flag-OFF")
	}

	// Entity coverage on the tested production endpoint.
	withServeFromMMap(t, false)
	wantEC, wantFound := graph.ComputeEntityCoverage(materializedDoc(docFullRepo(doc)), "ep")
	withServeFromMMap(t, true)
	gotEC, gotFound := graph.ComputeEntityCoverage(materializedDoc(readerEmptiedRepo(t, doc, r)), "ep")
	if gotFound != wantFound || !reflect.DeepEqual(gotEC, wantEC) {
		t.Fatalf("entity coverage flag-ON(emptied Doc) != flag-OFF")
	}
}

// TestDocSemanticsEntities_ReaderParity_PR7a exercises doc_semantics_tools.go:
// the codeEntities set used for target-existence validation.
func TestDocSemanticsEntities_ReaderParity_PR7a(t *testing.T) {
	doc, r := loadPR7aFixture(t)

	withServeFromMMap(t, true)
	got := materializeAllEntities(readerEmptiedRepo(t, doc, r))
	if !reflect.DeepEqual(got, doc.Entities) {
		t.Fatalf("doc-semantics codeEntities flag-ON(emptied Doc) != raw Doc.Entities")
	}
}
