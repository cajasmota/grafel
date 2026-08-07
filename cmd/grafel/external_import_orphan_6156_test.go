// Package main — external_import_orphan_6156_test.go
//
// Issue #6156. On the FULL rebuild path a THIRD-PARTY import's IMPORTS edge is
// left pointing at the hex id of the import placeholder that
// resolve.PruneImportPlaceholders has already removed — a dangling endpoint —
// even though the `SCOPE.External` entity that edge should bind to exists in
// the very same graph (external.Synthesize runs later and creates it).
//
// The #6131 repoint cannot cover this shape: it re-points an incoming edge at
// the entity the pipeline had ALREADY resolved the same (importer file,
// source_module) import to, and for a third-party module nothing is resolved at
// prune time.
//
// Everything below is asserted against the graph READ BACK FROM DISK
// (graph.LoadGraphFromDir), which is how #6156's measurements were taken.
//
// Refs #6156, #6131, #642, #6129.
package main

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// oiIndex builds a two-file Python corpus and returns a full rebuild of it,
// loaded from disk.
//
// The corpus is deliberately minimal and reaches BOTH halves of the prune's
// repoint decision in one graph:
//
//	`import falcon`         — a package with no in-repo definition. Nothing
//	                          resolves it at prune time; external.Synthesize
//	                          creates `ext:falcon` afterwards. This is #6156.
//	`import oi_lib_static`  — a module that DOES exist in the repo. The import
//	                          resolver binds it before the prune runs, so the
//	                          #6131 repoint has a target. It is the control:
//	                          the #6156 repair must not disturb it.
func oiIndex(t *testing.T) *graph.Document {
	t.Helper()
	repo := t.TempDir()
	dvWriteFile(t, repo, "oi_lib_static.py", `def oi_target(x):
    return x + 1
`)
	dvWriteFile(t, repo, "oi_main.py", `import falcon
import oi_lib_static


def oi_handle(x):
    app = falcon.App()
    return app, oi_lib_static.oi_target(x)
`)
	return dvFullRebuild(t, repo, t.TempDir())
}

// oiLiveIDs returns the set of entity ids present in the document.
func oiLiveIDs(doc *graph.Document) map[string]bool {
	live := make(map[string]bool, len(doc.Entities))
	for i := range doc.Entities {
		live[doc.Entities[i].ID] = true
	}
	return live
}

// oiFileComponentID returns the id of the file-level SCOPE.Component carrier
// for path, failing the test when there is none — the IMPORTS edges under test
// hang off it, so its absence would make every assertion below vacuous.
func oiFileComponentID(t *testing.T, doc *graph.Document, path string) string {
	t.Helper()
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if e.Kind == "SCOPE.Component" && e.Subtype == "file" && e.SourceFile == path {
			return e.ID
		}
	}
	t.Fatalf("no file-level SCOPE.Component carrier for %s in the persisted graph — "+
		"the fixture never reached the shape under test", path)
	return ""
}

// oiWholeModuleImport returns the single IMPORTS edge emitted by fromID whose
// source_module is module and whose imported_name is the module itself (the
// whole-module form `import <module>`), failing when there is not exactly one.
func oiWholeModuleImport(t *testing.T, doc *graph.Document, fromID, module string) *graph.Relationship {
	t.Helper()
	var hits []*graph.Relationship
	for i := range doc.Relationships {
		r := &doc.Relationships[i]
		if r.Kind != "IMPORTS" || r.FromID != fromID {
			continue
		}
		if r.PropGet("source_module") != module {
			continue
		}
		if r.PropGet("imported_name") != module {
			continue
		}
		hits = append(hits, r)
	}
	if len(hits) != 1 {
		t.Fatalf("want exactly 1 whole-module IMPORTS edge for %q from %s, got %d — "+
			"the fixture no longer reaches the shape under test", module, fromID, len(hits))
	}
	return hits[0]
}

// TestFullRebuild_ThirdPartyImportBindsToExternal_6156 is the #6156 gate.
//
// It asserts its own PREMISES before the behaviour, because each of them is a
// way this test could pass while measuring nothing:
//
//	P1 `falcon` is genuinely third-party — no in-repo entity defines it, so the
//	   import resolver has nothing to bind and the #6131 repoint has no target.
//	P2 the import placeholder for it was genuinely PRUNED — no surviving entity
//	   is a `SCOPE.Component` subtype="import" for it. If prune had kept the
//	   placeholder, the edge would bind trivially and the defect would be absent
//	   for the wrong reason.
//	P3 the `SCOPE.External` node genuinely EXISTS in the same graph. The defect
//	   is not "no external node was created"; it is that the edge does not bind
//	   to the one that was.
func TestFullRebuild_ThirdPartyImportBindsToExternal_6156(t *testing.T) {
	doc := oiIndex(t)
	if len(doc.Relationships) == 0 {
		t.Fatalf("persisted graph carries no relationships — the comparison is inert")
	}
	live := oiLiveIDs(doc)
	mainID := oiFileComponentID(t, doc, "oi_main.py")

	// P1 — falcon is third-party: nothing in the repo defines it.
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if e.Kind == "SCOPE.External" {
			continue
		}
		if e.Name == "falcon" || e.QualifiedName == "falcon" {
			t.Fatalf("premise broken: in-repo entity %s/%s@%s claims the name falcon, "+
				"so the import is not third-party and this test measures the #6131 path instead",
				e.Kind, e.Name, e.SourceFile)
		}
	}
	// The control import, by contrast, MUST have an in-repo definition —
	// otherwise the two arms of the test are the same arm.
	oiFileComponentID(t, doc, "oi_lib_static.py")

	// P2 — the import placeholder was pruned.
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if e.Kind == "SCOPE.Component" && e.Subtype == "import" {
			t.Fatalf("premise broken: import placeholder %s@%s survived the prune; "+
				"the dangling-endpoint shape #6156 describes cannot arise when the "+
				"placeholder is still live", e.Name, e.SourceFile)
		}
	}

	// P3 — the SCOPE.External node exists in this same graph.
	extID := ""
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if e.Kind == "SCOPE.External" && e.Name == "falcon" {
			extID = e.ID
			break
		}
	}
	if extID == "" {
		t.Fatalf("premise broken: no SCOPE.External entity for falcon in the persisted graph — " +
			"#6156 is about an edge failing to bind to a node that EXISTS, so without it " +
			"this test would be measuring a different defect")
	}

	// Behaviour — the third-party import binds to that node rather than
	// dangling on the pruned placeholder's hex id.
	rel := oiWholeModuleImport(t, doc, mainID, "falcon")
	if !live[rel.ToID] {
		t.Errorf("#6156: IMPORTS edge for `import falcon` dangles — ToID %q resolves to no "+
			"entity in the persisted graph, though SCOPE.External falcon (%s) is right there",
			rel.ToID, extID)
	}
	if rel.ToID != extID {
		t.Errorf("#6156: IMPORTS edge for `import falcon` binds to %q; want the SCOPE.External "+
			"node %q", rel.ToID, extID)
	}

	// Control — the in-repo import is untouched by the repair and still binds
	// to a live in-repo entity (this is the #6131 case; a fix that hijacked it
	// would turn a first-party dependency into a third-party one).
	ctrl := oiWholeModuleImport(t, doc, mainID, "oi_lib_static")
	if !live[ctrl.ToID] {
		t.Errorf("control: IMPORTS edge for `import oi_lib_static` dangles on ToID %q", ctrl.ToID)
	}
	if ctrl.ToID == extID {
		t.Errorf("control: the in-repo import bound to the falcon external node")
	}
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if e.ID != ctrl.ToID {
			continue
		}
		if e.Kind == "SCOPE.External" {
			t.Errorf("control: `import oi_lib_static` bound to SCOPE.External %s — the repo "+
				"defines that module, so a third-party bind is a mis-bind", e.Name)
		}
	}
}
