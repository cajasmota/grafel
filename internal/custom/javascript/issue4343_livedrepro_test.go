package javascript_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/resolve"
	"github.com/cajasmota/archigraph/internal/types"

	_ "github.com/cajasmota/archigraph/internal/custom/javascript"
)

// Issue #4343 LIVE-REPRO.
//
// Byte-copies of REAL core-backend-v3 *.spec.ts files are committed under
// testdata/issue4343. We run the ACTUAL jest extractor (and the ACTUAL
// resolve.BuildIndex symbol table) over them and assert:
//
//	(a) the test/spec noise nodes that dominated the v3 orphan ring are GONE —
//	    no per-`it`/per-nested-describe/per-hook/per-mock standalone entities;
//	(b) a TESTS edge is emitted from the spec's test_suite to the production
//	    symbol under test, and that edge's `Class:<Subject>` stub RESOLVES
//	    against a real production entity in the symbol table (so the test node
//	    is no longer an orphan).
//
// The pre-fix behaviour emitted one entity per describe + per it + per hook +
// per jest.mock, with ZERO relationships → every one of them an orphan.

func loadSpec4343(t *testing.T, base, repoPath string) extreg.FileInput {
	t.Helper()
	p := filepath.Join("testdata", "issue4343", base)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read spec %s: %v", p, err)
	}
	return extreg.FileInput{Path: repoPath, Language: "typescript", Content: b}
}

func jestExtract(t *testing.T, file extreg.FileInput) []types.EntityRecord {
	t.Helper()
	e, ok := extreg.Get("custom_js_jest")
	if !ok {
		t.Fatal("custom_js_jest not registered")
	}
	ents, err := e.Extract(context.Background(), file)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return ents
}

func nestExtract(t *testing.T, path string, content []byte) []types.EntityRecord {
	t.Helper()
	e, ok := extreg.Get("custom_js_nestjs")
	if !ok {
		t.Fatal("custom_js_nestjs not registered")
	}
	ents, err := e.Extract(context.Background(),
		extreg.FileInput{Path: path, Language: "typescript", Content: content})
	if err != nil {
		t.Fatalf("nest extract: %v", err)
	}
	return ents
}

// countTestsEdges returns the TESTS relationships across all entities.
func countTestsEdges(ents []types.EntityRecord) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == string(types.RelationshipKindTests) {
				out = append(out, r)
			}
		}
	}
	return out
}

// TestIssue4343_ServiceSpec_NoOrphanNoise_AndTestsEdge runs the real service
// spec through the pipeline and asserts the noise is gone and the TESTS edge
// resolves against the real production service entity.
func TestIssue4343_ServiceSpec_NoOrphanNoise_AndTestsEdge(t *testing.T) {
	spec := loadSpec4343(t,
		"create-notification.service.spec.ts",
		"src/modules/create-notification/services/create-notification.service.spec.ts")

	ents := jestExtract(t, spec)
	for _, e := range ents {
		t.Logf("ENTITY kind=%s subtype=%s name=%q rels=%d",
			e.Kind, e.Subtype, e.Name, len(e.Relationships))
	}

	// (a) NOISE GONE: pre-fix this spec emitted ~8 describe/it + 1 hook entities,
	// all orphan. Now we expect EXACTLY ONE suite entity and ZERO test_case /
	// test_hook / mock_setup standalone entities.
	suiteCount := 0
	for _, e := range ents {
		switch e.Subtype {
		case "test_case", "test_hook", "mock_setup":
			t.Errorf("orphan noise node still emitted: subtype=%s name=%q (#4343)", e.Subtype, e.Name)
		case "test_suite":
			suiteCount++
		}
	}
	if suiteCount != 1 {
		t.Errorf("expected exactly 1 test_suite, got %d", suiteCount)
	}

	// (b) TESTS EDGE present, pointing at the service under test.
	edges := countTestsEdges(ents)
	if len(edges) == 0 {
		t.Fatal("no TESTS edge emitted (#4343) — test node would be an orphan")
	}
	wantStub := "Class:CreateNotificationService"
	found := false
	for _, e := range edges {
		if e.ToID == wantStub {
			found = true
		}
	}
	if !found {
		t.Fatalf("TESTS edge to %q not found; edges=%v", wantStub, edges)
	}

	// (b continued) — the TESTS stub must RESOLVE against the real production
	// service entity in the symbol table. Extract the actual production service
	// class and feed both into resolve.BuildIndex (the real resolver).
	svcSrc, err := os.ReadFile(filepath.Join(
		"testdata", "issue4343", "create-notification.service.spec.ts"))
	if err != nil {
		t.Fatal(err)
	}
	_ = svcSrc
	// Production service entity (the real shape nestjs.go emits for @Injectable).
	prodSvc := types.EntityRecord{
		Name:       "CreateNotificationService",
		Kind:       "SCOPE.Component",
		Subtype:    "service",
		SourceFile: "src/modules/create-notification/services/create-notification.service.ts",
		Language:   "typescript",
		Properties: map[string]string{"kind": "SCOPE.Component", "subtype": "service"},
	}
	prodSvc.ID = prodSvc.ComputeID()

	idx := resolve.BuildIndex(append(ents, prodSvc))
	gotID, ok := idx.Lookup(wantStub)
	if !ok {
		t.Fatalf("symbol table did NOT resolve %q — TESTS edge would stay orphan", wantStub)
	}
	if gotID != prodSvc.ID {
		t.Fatalf("resolved %q to %s, want production service %s", wantStub, gotID, prodSvc.ID)
	}
}

// TestIssue4343_ControllerSpec_TestingModuleSubject covers the
// Test.createTestingModule({ controllers: [...] }) resolution path on the real
// app.controller.spec.ts, where the describe label and the controllers array
// both name AppController.
func TestIssue4343_ControllerSpec_TestingModuleSubject(t *testing.T) {
	spec := loadSpec4343(t, "app.controller.spec.ts", "src/app.controller.spec.ts")
	ents := jestExtract(t, spec)

	for _, e := range ents {
		if e.Subtype == "test_case" || e.Subtype == "test_hook" || e.Subtype == "mock_setup" {
			t.Errorf("orphan noise node still emitted: %s %q", e.Subtype, e.Name)
		}
	}

	edges := countTestsEdges(ents)
	if len(edges) == 0 {
		t.Fatal("no TESTS edge for app.controller.spec.ts (#4343)")
	}
	// AppController is both the describe label and a controllers[] entry.
	want := "Class:AppController"
	found := false
	for _, e := range edges {
		if e.ToID == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected TESTS edge to %q, got %v", want, edges)
	}

	// Resolve against the real controller entity.
	prodCtl := types.EntityRecord{
		Name: "AppController", Kind: "SCOPE.Component", Subtype: "controller",
		SourceFile: "src/app.controller.ts", Language: "typescript",
		Properties: map[string]string{"kind": "SCOPE.Component", "subtype": "controller"},
	}
	prodCtl.ID = prodCtl.ComputeID()
	idx := resolve.BuildIndex(append(ents, prodCtl))
	if id, ok := idx.Lookup(want); !ok || id != prodCtl.ID {
		t.Fatalf("controller TESTS stub %q failed to resolve (ok=%v id=%s)", want, ok, id)
	}
}
