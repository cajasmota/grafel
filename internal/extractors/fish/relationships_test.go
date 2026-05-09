package fish_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/fish"
	"github.com/cajasmota/archigraph/internal/types"
)

// ---- helpers ----------------------------------------------------------------

func extractFish(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("fish")
	if !ok {
		t.Fatal("fish extractor not registered")
	}
	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "fish",
	})
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	return got
}

func relsByKind(entities []types.EntityRecord, kind string) []types.RelationshipRecord {
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

func findFishByName(entities []types.EntityRecord, name string) *types.EntityRecord {
	for i := range entities {
		if entities[i].Name == name {
			return &entities[i]
		}
	}
	return nil
}

func hasFishRel(entities []types.EntityRecord, kind, toContains string) bool {
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

func TestRelationships_Imports_SourceLong(t *testing.T) {
	src := `source path/to/lib.fish

function hello
    echo hi
end
`
	entities := extractFish(t, "config.fish", src)
	rels := relsByKind(entities, "IMPORTS")
	if len(rels) != 1 {
		t.Fatalf("IMPORTS count = %d, want 1", len(rels))
	}
	if rels[0].ToID != "path/to/lib.fish" {
		t.Errorf("IMPORTS ToID = %q, want path/to/lib.fish", rels[0].ToID)
	}
	if rels[0].FromID != "config.fish" {
		t.Errorf("IMPORTS FromID = %q, want config.fish", rels[0].FromID)
	}
	if rels[0].Properties["import_kind"] != "source" {
		t.Errorf("import_kind = %q, want source", rels[0].Properties["import_kind"])
	}
	if rels[0].Properties["language"] != "fish" {
		t.Errorf("language = %q, want fish", rels[0].Properties["language"])
	}
}

func TestRelationships_Imports_DotSource(t *testing.T) {
	src := `. helpers.fish
. another/file.fish
`
	entities := extractFish(t, "config.fish", src)
	rels := relsByKind(entities, "IMPORTS")
	if len(rels) != 2 {
		t.Fatalf("IMPORTS count = %d, want 2", len(rels))
	}
	wants := map[string]bool{"helpers.fish": false, "another/file.fish": false}
	for _, r := range rels {
		if _, ok := wants[r.ToID]; ok {
			wants[r.ToID] = true
		}
	}
	for k, seen := range wants {
		if !seen {
			t.Errorf("missing IMPORTS to %q", k)
		}
	}
}

func TestRelationships_Imports_NoneWhenAbsent(t *testing.T) {
	src := `function hello
    echo hi
end
`
	entities := extractFish(t, "x.fish", src)
	rels := relsByKind(entities, "IMPORTS")
	if len(rels) != 0 {
		t.Errorf("IMPORTS count = %d, want 0", len(rels))
	}
}

func TestRelationships_Imports_DedupesIdentical(t *testing.T) {
	src := `source dup.fish
source dup.fish
`
	entities := extractFish(t, "x.fish", src)
	rels := relsByKind(entities, "IMPORTS")
	if len(rels) != 1 {
		t.Errorf("IMPORTS dedup count = %d, want 1", len(rels))
	}
}

func TestRelationships_Imports_QuotedPath(t *testing.T) {
	src := `source "quoted/path.fish"
source 'single/quoted.fish'
`
	entities := extractFish(t, "x.fish", src)
	rels := relsByKind(entities, "IMPORTS")
	if len(rels) != 2 {
		t.Fatalf("IMPORTS count = %d, want 2", len(rels))
	}
	wants := map[string]bool{"quoted/path.fish": false, "single/quoted.fish": false}
	for _, r := range rels {
		if _, ok := wants[r.ToID]; ok {
			wants[r.ToID] = true
		}
	}
	for k, seen := range wants {
		if !seen {
			t.Errorf("missing IMPORTS to %q", k)
		}
	}
}

func TestRelationships_Imports_SkipsCommented(t *testing.T) {
	src := `# source commented.fish
source real.fish
`
	entities := extractFish(t, "x.fish", src)
	rels := relsByKind(entities, "IMPORTS")
	if len(rels) != 1 {
		t.Fatalf("IMPORTS count = %d, want 1", len(rels))
	}
	if rels[0].ToID != "real.fish" {
		t.Errorf("ToID = %q, want real.fish", rels[0].ToID)
	}
}

// ---- CALLS -----------------------------------------------------------------

func TestRelationships_Calls_FromFunctionBody(t *testing.T) {
	src := `function ll
    ls -lAh $argv
    grep foo
end
`
	entities := extractFish(t, "x.fish", src)
	h := findFishByName(entities, "ll")
	if h == nil {
		t.Fatal("ll function not found")
	}
	var calls []string
	for _, r := range h.Relationships {
		if r.Kind == "CALLS" {
			calls = append(calls, r.ToID)
		}
	}
	wants := map[string]bool{"ls": false, "grep": false}
	for _, c := range calls {
		if _, ok := wants[c]; ok {
			wants[c] = true
		}
	}
	for k, seen := range wants {
		if !seen {
			t.Errorf("missing CALLS to %q (got %v)", k, calls)
		}
	}
}

func TestRelationships_Calls_DedupedPerFunction(t *testing.T) {
	src := `function runner
    foo
    foo
    foo
end
`
	entities := extractFish(t, "x.fish", src)
	h := findFishByName(entities, "runner")
	if h == nil {
		t.Fatal("runner not found")
	}
	count := 0
	for _, r := range h.Relationships {
		if r.Kind == "CALLS" && r.ToID == "foo" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("CALLS to foo count = %d, want 1 (deduped)", count)
	}
}

func TestRelationships_Calls_SkipsKeywords(t *testing.T) {
	src := `function r
    if test -n "$x"
        echo hi
    else
        echo bye
    end
    for i in 1 2 3
        echo $i
    end
    while true
        break
    end
    switch $argv[1]
        case foo
            echo foo
    end
    return 0
end
`
	entities := extractFish(t, "x.fish", src)
	h := findFishByName(entities, "r")
	if h == nil {
		t.Fatal("r not found")
	}
	for _, rel := range h.Relationships {
		if rel.Kind != "CALLS" {
			continue
		}
		switch rel.ToID {
		case "if", "else", "end", "for", "while", "switch", "case", "return",
			"break", "continue", "and", "or", "not", "begin", "function":
			t.Errorf("CALLS to keyword %q should not be emitted", rel.ToID)
		}
	}
}

func TestRelationships_Calls_SkipsSelfRecursion(t *testing.T) {
	src := `function fact
    fact $argv
end
`
	entities := extractFish(t, "x.fish", src)
	h := findFishByName(entities, "fact")
	if h == nil {
		t.Fatal("fact not found")
	}
	for _, r := range h.Relationships {
		if r.Kind == "CALLS" && r.ToID == "fact" {
			t.Error("self-recursion CALLS should be filtered")
		}
	}
}

func TestRelationships_Calls_NoneOutsideFunction(t *testing.T) {
	// Top-level commands are not extracted as functions; CALLS only emit
	// from within function bodies.
	src := `set -gx EDITOR nvim
echo "hi"
`
	entities := extractFish(t, "x.fish", src)
	rels := relsByKind(entities, "CALLS")
	if len(rels) != 0 {
		t.Errorf("CALLS count = %d, want 0 (no functions)", len(rels))
	}
}

// ---- CONTAINS --------------------------------------------------------------

func TestRelationships_Contains_FileToFunction(t *testing.T) {
	src := `function hello
    echo hi
end
`
	entities := extractFish(t, "config.fish", src)
	wantRef := extractor.BuildOperationStructuralRef("fish", "config.fish", "hello")
	if !hasFishRel(entities, "CONTAINS", wantRef) {
		t.Errorf("expected CONTAINS to %q\ngot: %+v", wantRef, entities)
	}
}

func TestRelationships_Contains_MultipleFunctions(t *testing.T) {
	src := `function a
    echo a
end

function b
    echo b
end

function c
    echo c
end
`
	entities := extractFish(t, "x.fish", src)
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

func TestRelationships_Contains_UsesStructuralRef(t *testing.T) {
	src := `function onClick
end
`
	entities := extractFish(t, "pages/foo.fish", src)
	if !hasFishRel(entities, "CONTAINS", "scope:operation:method:fish:pages/foo.fish:onClick") {
		t.Errorf("expected CONTAINS structural-ref")
	}
}

// ---- Combined --------------------------------------------------------------

func TestRelationships_Combined_AllThreeKinds(t *testing.T) {
	src := `source helpers.fish

function greet
    echo "hi"
    helper
end
`
	entities := extractFish(t, "config.fish", src)
	if len(relsByKind(entities, "IMPORTS")) < 1 {
		t.Errorf("expected >= 1 IMPORTS")
	}
	if len(relsByKind(entities, "CALLS")) < 1 {
		t.Errorf("expected >= 1 CALLS")
	}
	if len(relsByKind(entities, "CONTAINS")) < 1 {
		t.Errorf("expected >= 1 CONTAINS")
	}
}
