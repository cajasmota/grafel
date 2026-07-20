package python_test

// meta_2018_2020_2021_test.go — regression coverage for the Wave 9 meta-fix
// bundle.
//
//   #2018 — NeighbourBrief.Properties surfacing (docgen-side; covered by
//           internal/docgen tests; here we cover the producer side by
//           confirming the Properties stamped by Bundle C round-trip onto
//           the file entity's IMPORTS edges).
//   #2020 — Module entities are no longer phantom nodes: every Module
//           emitted by emitPackageModuleEntity carries a CONTAINS edge
//           pointing at the parallel SCOPE.Component (subtype="file")
//           entity for the same source file.
//   #2021 — Bundle C edge Properties round-trip through graph.fb
//           persistence: re_export / dead_import / live annotations on
//           IMPORTS edges survive Save → Load → re-read.
//
// Fixture: client-fixture-X, a minimal __init__.py + sibling module pair
// mirroring the production W9R1 shape (acme-core) without leaking any
// client name (standing scrub rule).

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	"github.com/cajasmota/grafel/internal/types"
)

// runPythonExtractor is a small wrapper that feeds a virtual file through
// the registered Python extractor and returns the records. Mirrors the
// helper used by the other bundle tests so this file stays self-contained.
func runPythonExtractor(t *testing.T, path, content string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("python")
	if !ok {
		t.Fatalf("python extractor not registered")
	}
	recs, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:    path,
		Content: []byte(content),
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return recs
}

// findIMPORTSEdges returns every IMPORTS RelationshipRecord embedded on
// the supplied records, paired with the owning entity's name for error
// messages.
func findIMPORTSEdges(records []types.EntityRecord) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for _, e := range records {
		for _, r := range e.Relationships {
			if r.Kind == "IMPORTS" {
				out = append(out, r)
			}
		}
	}
	return out
}

// findEntityByKind returns the first record matching kind+subtype (and optional
// Name) or nil.
func findEntityByKind(records []types.EntityRecord, kind, subtype string) *types.EntityRecord {
	for i := range records {
		e := &records[i]
		if e.Kind == kind && (subtype == "" || e.Subtype == subtype) {
			return e
		}
	}
	return nil
}

// -----------------------------------------------------------------------
// #2020 — Module entity carries CONTAINS edge to parallel file entity.
// -----------------------------------------------------------------------

func TestIssue2020_ModuleContainsFileEntity(t *testing.T) {
	t.Parallel()

	// client-fixture-X __init__.py — re-exports two symbols from sibling
	// modules. The base extractor emits a SCOPE.Component(file) entity
	// for __init__.py + a Module entity for the package boundary. The
	// fix wires a CONTAINS edge from the Module to the file entity.
	src := `from .models import User
from .celery import app as celery_app

__all__ = ("User", "celery_app")
`
	records := runPythonExtractor(t, "client_fixture_x/__init__.py", src)

	module := findEntityByKind(records, "Module", "package")
	if module == nil {
		t.Fatalf("no Module entity emitted; records=%d", len(records))
	}

	// The CONTAINS edge points at the file SCOPE.Component via a Format A
	// structural ref. We assert by suffix to stay independent of
	// extractor.BuildFileComponentStructuralRef's exact encoding.
	wantSuffix := "client_fixture_x/__init__.py"
	var found bool
	for _, r := range module.Relationships {
		if r.Kind == "CONTAINS" && strings.HasPrefix(r.ToID, "scope:component:file:python:") &&
			strings.HasSuffix(r.ToID, wantSuffix) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Module entity missing CONTAINS → file edge; relationships=%v", module.Relationships)
	}

	// Sanity: the per-source-file SCOPE.Component still exists and is
	// the IMPORTS carrier (so the docgen-side traversal in
	// loadEntityContext will actually find IMPORTS when it walks the
	// contained file entity).
	fileEntity := findEntityByKind(records, "SCOPE.Component", "file")
	if fileEntity == nil {
		t.Fatalf("no per-file SCOPE.Component entity emitted")
	}
	var importsOnFile int
	for _, r := range fileEntity.Relationships {
		if r.Kind == "IMPORTS" {
			importsOnFile++
		}
	}
	if importsOnFile < 2 {
		t.Errorf("expected ≥2 IMPORTS edges on file entity, got %d (relationships=%v)",
			importsOnFile, fileEntity.Relationships)
	}
}

// Plain .py file (non-__init__) — the Module still wires the CONTAINS
// → file edge so a docgen page seeded on the module surfaces the file's
// IMPORTS / REFERENCES.
func TestIssue2020_PlainModuleContainsFileEntity(t *testing.T) {
	t.Parallel()

	src := `import json

def helper():
    return json.dumps({})
`
	records := runPythonExtractor(t, "client_fixture_x/helpers.py", src)

	module := findEntityByKind(records, "Module", "package")
	if module == nil {
		t.Fatalf("no Module entity emitted; records=%d", len(records))
	}

	var found bool
	for _, r := range module.Relationships {
		if r.Kind == "CONTAINS" &&
			strings.HasPrefix(r.ToID, "scope:component:file:python:") &&
			strings.HasSuffix(r.ToID, "client_fixture_x/helpers.py") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("plain-module CONTAINS → file edge missing; relationships=%v", module.Relationships)
	}
}

// -----------------------------------------------------------------------
// #2021 — Bundle C edge Properties (re_export, dead_import, live)
//         persist through the graph.fb round-trip.
// -----------------------------------------------------------------------

func TestIssue2021_BundleCPropertiesRoundTripThroughFB(t *testing.T) {
	t.Parallel()

	// __init__.py with one re-export (User) listed in __all__ as
	// public. Bundle C's applyReExports + applyDeadImports stamp the
	// re_export / package_init / public properties on the file
	// entity's IMPORTS edge.
	src := `from .models import User

__all__ = ("User",)
`
	records := runPythonExtractor(t, "client_fixture_x/__init__.py", src)

	// Build a minimal graph.Document directly from the extracted records
	// so we exercise the same fbwriter / fbreader paths used by the
	// daemon's index pipeline (no need to spin up the full Indexer for
	// a property-persistence test).
	var rels []graph.Relationship
	for _, e := range records {
		for _, r := range e.Relationships {
			if r.Kind != "IMPORTS" {
				continue
			}
			rels = append(rels, graph.Relationship{
				ID:     graph.RelationshipID(r.FromID, r.ToID, r.Kind),
				FromID: r.FromID,
				ToID:   r.ToID,
				Kind:   r.Kind,
			}.WithProperties(r.Properties))
		}
	}
	if len(rels) == 0 {
		t.Fatalf("no IMPORTS edges produced by extractor; cannot test graph.fb round-trip")
	}

	// Pre-condition: at least one IMPORTS edge carries re_export=true on
	// the in-memory records. If this fails the regression is upstream of
	// graph.fb persistence (in the Bundle C extractor passes themselves).
	var preReExport bool
	for _, r := range rels {
		if r.PropGet("re_export") == "true" {
			preReExport = true
			break
		}
	}
	if !preReExport {
		t.Fatalf("in-memory IMPORTS edge missing re_export=true property — extractor regression (Bundle C #1991 / #2006)")
	}

	doc := &graph.Document{
		Version:       graph.SchemaVersion,
		Repo:          "client-fixture-x",
		Entities:      nil, // entities not needed for IMPORTS-only round-trip
		Relationships: rels,
	}

	// Save to graph.fb in a tempdir, then load via the canonical
	// LoadGraphFromDir path the daemon + docgen use in production.
	tmp := t.TempDir()
	fbPath := filepath.Join(tmp, "graph.fb")
	if err := fbwriter.WriteAtomic(fbPath, doc); err != nil {
		t.Fatalf("fbwriter.WriteAtomic: %v", err)
	}
	loaded, err := graph.LoadGraphFromDir(tmp)
	if err != nil {
		t.Fatalf("graph.LoadGraphFromDir: %v", err)
	}

	// Post-condition: round-tripped IMPORTS edge still carries re_export=true.
	// This is the exact assertion #2021 requested as a smoke test.
	var postReExport bool
	for _, r := range loaded.Relationships {
		if r.Kind != "IMPORTS" {
			continue
		}
		if r.PropGet("re_export") == "true" {
			postReExport = true
			break
		}
	}
	if !postReExport {
		t.Fatalf("re_export=true property LOST after graph.fb round-trip — properties stripped by fbwriter/fbreader path")
	}

	// Also assert package_init (every IMPORTS on an __init__.py) and
	// public (User is in __all__) survive — these are the other
	// annotations Bundle C surfaces and that #2021's full-graph scan
	// reported as missing.
	var sawPackageInit, sawPublic bool
	for _, r := range loaded.Relationships {
		if r.Kind != "IMPORTS" {
			continue
		}
		if r.PropGet("package_init") == "true" {
			sawPackageInit = true
		}
		if r.PropGet("public") == "true" {
			sawPublic = true
		}
	}
	if !sawPackageInit {
		t.Errorf("package_init=true LOST after graph.fb round-trip")
	}
	if !sawPublic {
		t.Errorf("public=true LOST after graph.fb round-trip")
	}
}

// Companion assertion for the dead-import side (#1985): an unused
// import is stamped with live=false / dead_import=true and those
// annotations also survive the graph.fb round-trip.
func TestIssue2021_DeadImportPropertiesRoundTripThroughFB(t *testing.T) {
	t.Parallel()

	// `Unused` is imported but never referenced in the body — Bundle C
	// should mark it live=false / dead_import=true. `Used` is referenced
	// by helper() so it stays live. Module-level `import x` is excluded
	// from dead detection (side-effect imports), so we use the
	// `from X import Y` shape that Bundle C does flag.
	src := `from .models import Used
from .models import Unused

def helper():
    return Used()
`
	records := runPythonExtractor(t, "client_fixture_x/helpers.py", src)

	var rels []graph.Relationship
	for _, e := range records {
		for _, r := range e.Relationships {
			if r.Kind != "IMPORTS" {
				continue
			}
			rels = append(rels, graph.Relationship{
				ID:     graph.RelationshipID(r.FromID, r.ToID, r.Kind),
				FromID: r.FromID,
				ToID:   r.ToID,
				Kind:   r.Kind,
			}.WithProperties(r.Properties))
		}
	}
	if len(rels) < 2 {
		t.Fatalf("expected ≥2 IMPORTS edges (json + os), got %d", len(rels))
	}

	doc := &graph.Document{
		Version:       graph.SchemaVersion,
		Repo:          "client-fixture-x",
		Relationships: rels,
	}
	tmp := t.TempDir()
	fbPath := filepath.Join(tmp, "graph.fb")
	if err := fbwriter.WriteAtomic(fbPath, doc); err != nil {
		t.Fatalf("fbwriter.WriteAtomic: %v", err)
	}
	loaded, err := graph.LoadGraphFromDir(tmp)
	if err != nil {
		t.Fatalf("graph.LoadGraphFromDir: %v", err)
	}

	var sawDead, sawLiveFalse bool
	for _, r := range loaded.Relationships {
		if r.Kind != "IMPORTS" {
			continue
		}
		// dead_import marker stamped on the `os` edge.
		if r.PropGet("dead_import") == "true" {
			sawDead = true
		}
		if r.PropGet("live") == "false" {
			sawLiveFalse = true
		}
	}
	if !sawDead {
		t.Errorf("dead_import=true property LOST after graph.fb round-trip")
	}
	if !sawLiveFalse {
		t.Errorf("live=false property LOST after graph.fb round-trip")
	}
}

// -----------------------------------------------------------------------
// #2018 — Edge Properties stamped by Bundle C are visible on the
//         extractor output (producer side). Docgen-side surfacing in
//         NeighbourBrief is covered in internal/docgen/llm_bundle_test.go.
// -----------------------------------------------------------------------

func TestIssue2018_ExtractorStampsEdgePropertiesOnFileEntity(t *testing.T) {
	t.Parallel()

	// __init__.py — exercises the re_export + alias annotations.
	// Every IMPORTS edge on an __init__.py is marked re_export=true by
	// applyReExports, and Bundle C never flags __init__.py imports dead
	// (they're treated as the package's public surface). So we test the
	// dead_import annotation against a plain module below.
	initSrc := `from .models import User as PublicUser

__all__ = ("PublicUser",)
`
	initRecords := runPythonExtractor(t, "client_fixture_x/__init__.py", initSrc)
	initEdges := findIMPORTSEdges(initRecords)
	if len(initEdges) == 0 {
		t.Fatalf("expected ≥1 IMPORTS edge on __init__.py, got 0")
	}
	var sawReExport, sawAlias bool
	for _, r := range initEdges {
		if r.Properties["re_export"] == "true" {
			sawReExport = true
		}
		if r.Properties["alias"] == "PublicUser" {
			sawAlias = true
		}
	}
	if !sawReExport {
		t.Errorf("expected at least one IMPORTS edge with re_export=true on __init__.py")
	}
	if !sawAlias {
		t.Errorf("expected at least one IMPORTS edge with alias=PublicUser on __init__.py")
	}

	// Plain module — exercises the dead-import annotation. Bundle C
	// only flags from-import shapes (module-level `import x` is treated
	// as a side-effect import and never flagged dead).
	modSrc := `from .models import Used
from .models import Unused

def helper():
    return Used()
`
	modRecords := runPythonExtractor(t, "client_fixture_x/helpers.py", modSrc)
	modEdges := findIMPORTSEdges(modRecords)
	if len(modEdges) < 2 {
		t.Fatalf("expected ≥2 IMPORTS edges on plain module, got %d", len(modEdges))
	}
	var sawDead bool
	for _, r := range modEdges {
		if r.Properties["dead_import"] == "true" {
			sawDead = true
		}
	}
	if !sawDead {
		t.Errorf("expected at least one IMPORTS edge with dead_import=true (unused `Unused` import)")
	}
}
