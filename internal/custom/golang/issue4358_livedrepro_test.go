package golang_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/resolve"
	"github.com/cajasmota/archigraph/internal/types"

	_ "github.com/cajasmota/archigraph/internal/custom/golang"
)

// Issue #4358 LIVE-REPRO.
//
// Byte-copies of a representative Go production package (order_service.go) and
// its testify+stdlib *_test.go (order_service_test.go) are committed under
// testdata/issue4358. We run the ACTUAL Go test-framework extractor (and the
// ACTUAL resolve.BuildIndex symbol table) over them and assert:
//
//	(a) the per-suite-struct / per-case / per-assertion / per-suite-run noise
//	    nodes that dominate the Go orphan ring are GONE — exactly ONE collapsed
//	    test_suite entity is emitted for the file;
//	(b) a TESTS edge is emitted from the suite to the production symbol under
//	    test, and that edge's `Class:OrderService` stub RESOLVES against the
//	    real production OrderService entity in the symbol table (so the test
//	    node is no longer an orphan).
//
// The pre-fix behaviour emitted one entity per suite struct + per suite.Run +
// per suite method + per assert/require call, with ZERO relationships → every
// one of them an orphan, and NO TESTS edge at all.

func loadGoTest4358(t *testing.T, base, repoPath string) extreg.FileInput {
	t.Helper()
	p := filepath.Join("testdata", "issue4358", base)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return extreg.FileInput{Path: repoPath, Language: "go", Content: b}
}

func testifyExtract4358(t *testing.T, file extreg.FileInput) []types.EntityRecord {
	t.Helper()
	e, ok := extreg.Get("custom_go_testify")
	if !ok {
		t.Fatal("custom_go_testify not registered")
	}
	ents, err := e.Extract(context.Background(), file)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return ents
}

func testsEdges4358(ents []types.EntityRecord) []types.RelationshipRecord {
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

func TestIssue4358_TestifySuite_NoOrphanNoise_AndTestsEdge(t *testing.T) {
	tf := loadGoTest4358(t,
		"order_service_test.go",
		"internal/order/order_service_test.go")

	ents := testifyExtract4358(t, tf)
	for _, e := range ents {
		t.Logf("ENTITY kind=%s subtype=%s name=%q rels=%d props=%v",
			e.Kind, e.Subtype, e.Name, len(e.Relationships), e.Properties)
	}

	// (a) NOISE GONE: pre-fix this file emitted 1 suite struct + 1 suite.Run +
	// 2 suite methods + many assert/require nodes, all orphan. Now we expect
	// EXACTLY ONE test_suite entity and ZERO standalone case/assertion/run nodes.
	suiteCount := 0
	for _, e := range ents {
		switch e.Subtype {
		case "test_case", "assertion", "suite_run":
			t.Errorf("orphan noise node still emitted: subtype=%s name=%q (#4358)", e.Subtype, e.Name)
		case "test_suite":
			suiteCount++
		}
	}
	if suiteCount != 1 {
		t.Fatalf("expected exactly 1 test_suite, got %d", suiteCount)
	}

	// (b) TESTS EDGE present, pointing at OrderService.
	edges := testsEdges4358(ents)
	if len(edges) == 0 {
		t.Fatal("no TESTS edge emitted (#4358) — test node would be an orphan")
	}
	wantStub := "Class:OrderService"
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
	// OrderService entity. Build the production struct entity (the shape the
	// base Go extractor emits for a struct) and feed both into the real
	// resolve.BuildIndex symbol table.
	prod := types.EntityRecord{
		Name:       "OrderService",
		Kind:       "SCOPE.Class",
		Subtype:    "struct",
		SourceFile: "internal/order/order_service.go",
		Language:   "go",
		Properties: map[string]string{"kind": "SCOPE.Class", "subtype": "struct"},
	}
	prod.ID = prod.ComputeID()

	idx := resolve.BuildIndex(append(ents, prod))
	gotID, ok := idx.Lookup(wantStub)
	if !ok {
		t.Fatalf("symbol table did NOT resolve %q — TESTS edge would stay orphan", wantStub)
	}
	if gotID != prod.ID {
		t.Fatalf("resolved %q to %s, want production OrderService %s", wantStub, gotID, prod.ID)
	}
}
