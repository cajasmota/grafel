package shell_test

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/shell"
	"github.com/cajasmota/archigraph/internal/types"
)

// findEntity returns the first entity matching the predicate.
func findEntity(entities []types.EntityRecord, pred func(types.EntityRecord) bool) (types.EntityRecord, bool) {
	for _, e := range entities {
		if pred(e) {
			return e, true
		}
	}
	return types.EntityRecord{}, false
}

func collectImports(entities []types.EntityRecord) []string {
	var paths []string
	for _, e := range entities {
		for _, r := range e.Relationships {
			if r.Kind == "IMPORTS" {
				paths = append(paths, r.ToID)
			}
		}
	}
	return paths
}

func collectCallsFor(entities []types.EntityRecord, fnName string) []string {
	var targets []string
	for _, e := range entities {
		if e.Name != fnName || e.Kind != "SCOPE.Operation" {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == "CALLS" {
				targets = append(targets, r.ToID)
			}
		}
	}
	return targets
}

func TestShell_Imports_SourceCommand(t *testing.T) {
	src := `#!/bin/bash
source ./lib.sh
. /etc/foo.sh

main() {
    echo hi
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("shell")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:    "test.sh",
		Content: []byte(src),
		Tree:    tree,
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	imps := collectImports(entities)
	want := map[string]bool{"./lib.sh": false, "/etc/foo.sh": false}
	for _, p := range imps {
		if _, ok := want[p]; ok {
			want[p] = true
		}
	}
	for p, seen := range want {
		if !seen {
			t.Errorf("missing IMPORTS for %q. got %v", p, imps)
		}
	}
}

func TestShell_Imports_HaveProperties(t *testing.T) {
	src := `source ./lib.sh
foo() { :; }
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("shell")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:    "test.sh",
		Content: []byte(src),
		Tree:    tree,
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	stub, ok := findEntity(entities, func(e types.EntityRecord) bool {
		return e.Name == "./lib.sh"
	})
	if !ok {
		t.Fatalf("expected import stub for ./lib.sh, entities=%+v", entities)
	}
	if len(stub.Relationships) == 0 {
		t.Fatal("import stub has no relationships")
	}
	r := stub.Relationships[0]
	if r.Kind != "IMPORTS" {
		t.Errorf("expected IMPORTS, got %q", r.Kind)
	}
	if r.FromID != "test.sh" {
		t.Errorf("expected FromID=test.sh, got %q", r.FromID)
	}
	if r.ToID != "./lib.sh" {
		t.Errorf("expected ToID=./lib.sh, got %q", r.ToID)
	}
	if r.Properties["import_kind"] != "source" {
		t.Errorf("expected import_kind=source, got %q", r.Properties["import_kind"])
	}
	if r.Properties["language"] != "shell" {
		t.Errorf("expected language=shell, got %q", r.Properties["language"])
	}
}

func TestShell_Calls_BetweenLocalFunctions(t *testing.T) {
	src := `helper() {
    echo hi
}

main() {
    helper
    other_thing
    helper arg1 arg2
}

other_thing() {
    :
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("shell")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:    "test.sh",
		Content: []byte(src),
		Tree:    tree,
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	calls := collectCallsFor(entities, "main")
	want := map[string]bool{"helper": false, "other_thing": false}
	for _, c := range calls {
		if _, ok := want[c]; ok {
			want[c] = true
		}
	}
	for k, v := range want {
		if !v {
			t.Errorf("expected CALLS to %q from main, got %v", k, calls)
		}
	}
}

func TestShell_Calls_FilterExternalCommands(t *testing.T) {
	src := `helper() {
    :
}

main() {
    docker build .
    rm -rf /tmp
    helper
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("shell")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:    "test.sh",
		Content: []byte(src),
		Tree:    tree,
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	calls := collectCallsFor(entities, "main")
	for _, c := range calls {
		if c == "docker" || c == "rm" {
			t.Errorf("did not expect external command %q in CALLS, got %v", c, calls)
		}
	}
	found := false
	for _, c := range calls {
		if c == "helper" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected CALLS to helper from main, got %v", calls)
	}
}

func TestShell_Calls_DropSelfRecursion(t *testing.T) {
	src := `recurse() {
    recurse
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("shell")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:    "test.sh",
		Content: []byte(src),
		Tree:    tree,
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	calls := collectCallsFor(entities, "recurse")
	if len(calls) != 0 {
		t.Errorf("expected self-recursion to be filtered, got %v", calls)
	}
}

func TestShell_Calls_Dedup(t *testing.T) {
	src := `helper() { :; }

main() {
    helper
    helper
    helper
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("shell")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:    "test.sh",
		Content: []byte(src),
		Tree:    tree,
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	calls := collectCallsFor(entities, "main")
	count := 0
	for _, c := range calls {
		if c == "helper" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected deduped CALLS to helper, got count=%d", count)
	}
}

func TestShell_Contains_FileToFunctions(t *testing.T) {
	src := `helper() { :; }
main() { :; }
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("shell")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:    "scripts/run.sh",
		Content: []byte(src),
		Tree:    tree,
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	script, ok := findEntity(entities, func(e types.EntityRecord) bool {
		return e.Kind == "SCOPE.Component" && e.Subtype == "script"
	})
	if !ok {
		t.Fatalf("expected SCOPE.Component subtype=script entity, got %+v", entities)
	}
	wantHelper := extractor.BuildOperationStructuralRef("shell", "scripts/run.sh", "helper")
	wantMain := extractor.BuildOperationStructuralRef("shell", "scripts/run.sh", "main")
	got := map[string]bool{}
	for _, r := range script.Relationships {
		if r.Kind == "CONTAINS" {
			got[r.ToID] = true
		}
	}
	if !got[wantHelper] {
		t.Errorf("missing CONTAINS to %q. got %+v", wantHelper, got)
	}
	if !got[wantMain] {
		t.Errorf("missing CONTAINS to %q. got %+v", wantMain, got)
	}
}

func TestShell_Relationships_LanguageTagged(t *testing.T) {
	src := `source ./lib.sh
helper() { :; }
main() { helper; }
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("shell")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:    "x.sh",
		Content: []byte(src),
		Tree:    tree,
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	for _, e := range entities {
		for _, r := range e.Relationships {
			if r.Properties == nil || r.Properties["language"] != "shell" {
				t.Errorf("relationship %s→%s missing language=shell tag, props=%+v",
					r.Kind, r.ToID, r.Properties)
			}
		}
	}
}
