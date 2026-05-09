package just_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/just"
	"github.com/cajasmota/archigraph/internal/types"
)

// ---- helpers ----------------------------------------------------------------

func extractJust(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("just")
	if !ok {
		t.Fatal("just extractor not registered")
	}
	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "just",
	})
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	return got
}

func justRelsByKind(entities []types.EntityRecord, kind string) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for _, e := range entities {
		for _, r := range e.Relationships {
			if r.Kind == kind {
				out = append(out, r)
			}
		}
	}
	return out
}

func findJustByName(entities []types.EntityRecord, name string) *types.EntityRecord {
	for i := range entities {
		if entities[i].Name == name {
			return &entities[i]
		}
	}
	return nil
}

func hasJustRel(entities []types.EntityRecord, kind, toContains string) bool {
	for _, e := range entities {
		for _, r := range e.Relationships {
			if r.Kind == kind && strings.Contains(r.ToID, toContains) {
				return true
			}
		}
	}
	return false
}

// ---- IMPORTS ---------------------------------------------------------------

func TestJustRelationships_Imports_QuotedDouble(t *testing.T) {
	src := `import "path/to/other.justfile"

build:
    echo hi
`
	entities := extractJust(t, "justfile", src)
	rels := justRelsByKind(entities, "IMPORTS")
	if len(rels) != 1 {
		t.Fatalf("IMPORTS count = %d, want 1", len(rels))
	}
	if rels[0].ToID != "path/to/other.justfile" {
		t.Errorf("IMPORTS ToID = %q, want path/to/other.justfile", rels[0].ToID)
	}
	if rels[0].FromID != "justfile" {
		t.Errorf("IMPORTS FromID = %q, want justfile", rels[0].FromID)
	}
	if rels[0].Properties["import_kind"] != "import" {
		t.Errorf("import_kind = %q, want import", rels[0].Properties["import_kind"])
	}
	if rels[0].Properties["language"] != "just" {
		t.Errorf("language = %q, want just", rels[0].Properties["language"])
	}
}

func TestJustRelationships_Imports_QuotedSingle(t *testing.T) {
	src := `import 'helpers.just'
`
	entities := extractJust(t, "justfile", src)
	rels := justRelsByKind(entities, "IMPORTS")
	if len(rels) != 1 {
		t.Fatalf("IMPORTS count = %d, want 1", len(rels))
	}
	if rels[0].ToID != "helpers.just" {
		t.Errorf("ToID = %q, want helpers.just", rels[0].ToID)
	}
}

func TestJustRelationships_Imports_Optional(t *testing.T) {
	src := `import? "optional.just"
`
	entities := extractJust(t, "justfile", src)
	rels := justRelsByKind(entities, "IMPORTS")
	if len(rels) != 1 {
		t.Fatalf("IMPORTS count = %d, want 1", len(rels))
	}
	if rels[0].ToID != "optional.just" {
		t.Errorf("ToID = %q, want optional.just", rels[0].ToID)
	}
}

func TestJustRelationships_Imports_DedupesIdentical(t *testing.T) {
	src := `import "dup.just"
import "dup.just"
`
	entities := extractJust(t, "justfile", src)
	rels := justRelsByKind(entities, "IMPORTS")
	if len(rels) != 1 {
		t.Errorf("IMPORTS dedup count = %d, want 1", len(rels))
	}
}

func TestJustRelationships_Imports_NoneWhenAbsent(t *testing.T) {
	src := `build:
    echo hi
`
	entities := extractJust(t, "justfile", src)
	rels := justRelsByKind(entities, "IMPORTS")
	if len(rels) != 0 {
		t.Errorf("IMPORTS count = %d, want 0", len(rels))
	}
}

func TestJustRelationships_Imports_SkipsCommented(t *testing.T) {
	src := `# import "commented.just"
import "real.just"
`
	entities := extractJust(t, "justfile", src)
	rels := justRelsByKind(entities, "IMPORTS")
	if len(rels) != 1 {
		t.Fatalf("IMPORTS count = %d, want 1", len(rels))
	}
	if rels[0].ToID != "real.just" {
		t.Errorf("ToID = %q, want real.just", rels[0].ToID)
	}
}

// ---- CALLS -----------------------------------------------------------------

func TestJustRelationships_Calls_FromDependencies(t *testing.T) {
	src := `build:
    go build

test: build
    go test
`
	entities := extractJust(t, "justfile", src)
	test := findJustByName(entities, "test")
	if test == nil {
		t.Fatal("test recipe not found")
	}
	var callees []string
	for _, r := range test.Relationships {
		if r.Kind == "CALLS" {
			callees = append(callees, r.ToID)
		}
	}
	found := false
	for _, c := range callees {
		if c == "build" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected CALLS to build from test (got %v)", callees)
	}
}

func TestJustRelationships_Calls_MultipleDependencies(t *testing.T) {
	src := `build:
    echo build

test:
    echo test

lint:
    echo lint

ci: build test lint
    echo done
`
	entities := extractJust(t, "justfile", src)
	ci := findJustByName(entities, "ci")
	if ci == nil {
		t.Fatal("ci recipe not found")
	}
	wants := map[string]bool{"build": false, "test": false, "lint": false}
	for _, r := range ci.Relationships {
		if r.Kind == "CALLS" {
			if _, ok := wants[r.ToID]; ok {
				wants[r.ToID] = true
			}
		}
	}
	for k, seen := range wants {
		if !seen {
			t.Errorf("missing CALLS to %q", k)
		}
	}
}

func TestJustRelationships_Calls_StripsParenthesisedArgs(t *testing.T) {
	// Parenthesised dep groups carry recipe arguments; the existing
	// normalizeDeps drops them, so CALLS edges are emitted only for the
	// top-level dep names.
	src := `test:
    echo test

release: test (lint "strict")
    echo ok
`
	entities := extractJust(t, "justfile", src)
	rel := findJustByName(entities, "release")
	if rel == nil {
		t.Fatal("release recipe not found")
	}
	var callees []string
	for _, r := range rel.Relationships {
		if r.Kind == "CALLS" {
			callees = append(callees, r.ToID)
		}
	}
	if len(callees) != 1 || callees[0] != "test" {
		t.Errorf("CALLS = %v, want [test]", callees)
	}
}

func TestJustRelationships_Calls_DedupedPerRecipe(t *testing.T) {
	src := `build:
    echo build

ci: build build build
    echo done
`
	entities := extractJust(t, "justfile", src)
	ci := findJustByName(entities, "ci")
	if ci == nil {
		t.Fatal("ci not found")
	}
	count := 0
	for _, r := range ci.Relationships {
		if r.Kind == "CALLS" && r.ToID == "build" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("CALLS to build count = %d, want 1 (deduped)", count)
	}
}

func TestJustRelationships_Calls_NoneWithoutDeps(t *testing.T) {
	src := `build:
    echo build
`
	entities := extractJust(t, "justfile", src)
	rels := justRelsByKind(entities, "CALLS")
	if len(rels) != 0 {
		t.Errorf("CALLS count = %d, want 0", len(rels))
	}
}

// ---- CONTAINS --------------------------------------------------------------

func TestJustRelationships_Contains_FileToRecipe(t *testing.T) {
	src := `build:
    echo hi
`
	entities := extractJust(t, "justfile", src)
	wantRef := extractor.BuildOperationStructuralRef("just", "justfile", "build")
	if !hasJustRel(entities, "CONTAINS", wantRef) {
		t.Errorf("expected CONTAINS to %q", wantRef)
	}
}

func TestJustRelationships_Contains_MultipleRecipes(t *testing.T) {
	src := `a:
    echo a

b:
    echo b

c:
    echo c
`
	entities := extractJust(t, "justfile", src)
	count := 0
	for _, e := range entities {
		for _, r := range e.Relationships {
			if r.Kind == "CONTAINS" {
				count++
			}
		}
	}
	if count < 3 {
		t.Errorf("CONTAINS count = %d, want >= 3", count)
	}
}

func TestJustRelationships_Contains_UsesStructuralRef(t *testing.T) {
	src := `deploy:
    echo deploy
`
	entities := extractJust(t, "ops/justfile", src)
	if !hasJustRel(entities, "CONTAINS", "scope:operation:method:just:ops/justfile:deploy") {
		t.Errorf("expected CONTAINS structural-ref")
	}
}

// ---- Combined --------------------------------------------------------------

func TestJustRelationships_Combined_AllThreeKinds(t *testing.T) {
	src := `import "shared.just"

build:
    echo build

test: build
    echo test
`
	entities := extractJust(t, "justfile", src)
	if len(justRelsByKind(entities, "IMPORTS")) < 1 {
		t.Errorf("expected >= 1 IMPORTS")
	}
	if len(justRelsByKind(entities, "CALLS")) < 1 {
		t.Errorf("expected >= 1 CALLS")
	}
	if len(justRelsByKind(entities, "CONTAINS")) < 1 {
		t.Errorf("expected >= 1 CONTAINS")
	}
}
