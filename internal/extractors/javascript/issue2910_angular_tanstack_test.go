// Package javascript — issue #2910 cross-framework TanStack Query (Angular).
//
// The TanStack Query Angular adapter (@tanstack/angular-query-experimental)
// uses injectQuery/injectMutation/injectInfiniteQuery — the Angular equivalent
// of React's useQuery family. Angular components are .ts files parsed by this
// extractor; angularTanstackQuery decorates each inject* call site inside an
// Angular class as a SCOPE.Operation subtype="tanstack_query" with a CONTAINS
// edge from the component. Proven with the hand-written
// testdata/angular_internals/tanstack_query.ts fixture.
package javascript_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	tssmacker "github.com/cajasmota/grafel/internal/treesitter/ts/smacker"
	tstsx "github.com/smacker/go-tree-sitter/typescript/tsx"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func extractAngularTanstack(t *testing.T) []types.EntityRecord {
	t.Helper()
	path := filepath.Join("testdata", "angular_internals", "tanstack_query.ts")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	parser, err := tssmacker.New().NewParser(tssmacker.WrapLanguage(tstsx.GetLanguage()))
	if err != nil {
		t.Fatalf("parser init: %v", err)
	}
	defer parser.Close()
	tree, err := parser.Parse(content)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ext, _ := extreg.Get("typescript")
	ents, err := ext.Extract(context.Background(), extreg.FileInput{
		Path: path, Content: content, Language: "typescript", TSTree: tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

func TestIssue2910_AngularTanstackQuery(t *testing.T) {
	ents := extractAngularTanstack(t)

	tsq := bySubtype(ents, "SCOPE.Operation", "tanstack_query")
	if len(tsq) != 3 {
		t.Fatalf("expected 3 tanstack_query operations (injectQuery/Mutation/InfiniteQuery); got %d: %s", len(tsq), dumpKinds(ents))
	}
	gotKinds := map[string]bool{}
	for i := range tsq {
		e := &tsq[i]
		if e.Properties["via"] != "tanstack_query" {
			t.Errorf("%s via = %q, want tanstack_query", e.Name, e.Properties["via"])
		}
		if e.Properties["framework"] != "angular" {
			t.Errorf("%s framework = %q, want angular", e.Name, e.Properties["framework"])
		}
		gotKinds[e.Properties["query_kind"]] = true
	}
	for _, want := range []string{"query", "mutation", "infinite_query"} {
		if !gotKinds[want] {
			t.Errorf("missing tanstack_query of kind %q; got %v", want, gotKinds)
		}
	}

	// CONTAINS edge from the TodosComponent class to each injected query.
	var comp *types.EntityRecord
	for i := range ents {
		if ents[i].Kind == "SCOPE.Component" && ents[i].Name == "TodosComponent" {
			comp = &ents[i]
			break
		}
	}
	if comp == nil {
		t.Fatalf("missing TodosComponent entity")
	}
	contains := 0
	for _, r := range comp.Relationships {
		if r.Kind == "CONTAINS" && r.Properties["subtype"] == "tanstack_query" {
			contains++
		}
	}
	if contains != 3 {
		t.Errorf("TodosComponent CONTAINS tanstack_query edges = %d, want 3", contains)
	}
}
