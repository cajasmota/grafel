package javascript_test

import (
	"context"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	tsjavascript "github.com/smacker/go-tree-sitter/javascript"
	tstypescript "github.com/smacker/go-tree-sitter/typescript/typescript"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
	// Blank import to trigger init() registration.
	_ "github.com/cajasmota/archigraph/internal/extractors/javascript"
)

// parseJSRel parses JS source for relationship tests.
func parseJSRel(t *testing.T, src []byte) *sitter.Tree {
	t.Helper()
	p := sitter.NewParser()
	p.SetLanguage(tsjavascript.GetLanguage())
	tree, err := p.ParseCtx(context.Background(), nil, src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return tree
}

// parseTSRel parses TS source for relationship tests.
func parseTSRel(t *testing.T, src []byte) *sitter.Tree {
	t.Helper()
	p := sitter.NewParser()
	p.SetLanguage(tstypescript.GetLanguage())
	tree, err := p.ParseCtx(context.Background(), nil, src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return tree
}

func runJS(t *testing.T, src string, language string, tree *sitter.Tree) []types.EntityRecord {
	t.Helper()
	ext, _ := extractor.Get(language)
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "test." + extOf(language),
		Content:  []byte(src),
		Language: language,
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

func extOf(language string) string {
	if language == "typescript" {
		return "ts"
	}
	return "js"
}

func findByNameRel(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}

func hasRelEdge(ents []types.EntityRecord, fromName, kind, toID string) bool {
	src := findByNameRel(ents, fromName)
	if src == nil {
		return false
	}
	for _, r := range src.Relationships {
		if r.Kind == kind && r.ToID == toID {
			return true
		}
	}
	return false
}

func countRelByKind(ents []types.EntityRecord, fromName, kind string) int {
	src := findByNameRel(ents, fromName)
	if src == nil {
		return 0
	}
	n := 0
	for _, r := range src.Relationships {
		if r.Kind == kind {
			n++
		}
	}
	return n
}

// TestExtract_ContainsClassMethods (#41) — class with N methods produces N
// CONTAINS edges from the class to each method.
func TestExtract_ContainsClassMethods(t *testing.T) {
	src := `class Foo {
  a() {}
  b() {}
  c() {}
}
`
	tree := parseJSRel(t, []byte(src))
	ents := runJS(t, src, "javascript", tree)

	if c := countRelByKind(ents, "Foo", "CONTAINS"); c != 3 {
		t.Errorf("expected 3 CONTAINS edges, got %d", c)
	}
	// Issue #144 — CONTAINS targets are structural-ref stubs (Format A)
	// keyed on the source file so the resolver disambiguates by location.
	for _, m := range []string{"a", "b", "c"} {
		want := "scope:operation:method:javascript:test.js:" + m
		if !hasRelEdge(ents, "Foo", "CONTAINS", want) {
			t.Errorf("expected CONTAINS Foo→%s", want)
		}
	}
}

// TestExtract_CallsBareName (#41) — function calling another function emits
// a CALLS edge with stub to_id; duplicate call sites collapse to one edge.
func TestExtract_CallsBareName(t *testing.T) {
	src := `function helper() { return 1; }
function caller() {
  helper();
  helper();
  console.log("x");
}
`
	tree := parseJSRel(t, []byte(src))
	ents := runJS(t, src, "javascript", tree)

	if !hasRelEdge(ents, "caller", "CALLS", "helper") {
		t.Errorf("expected CALLS caller→helper")
	}
	if !hasRelEdge(ents, "caller", "CALLS", "log") {
		t.Errorf("expected CALLS caller→log (member trailing)")
	}
	n := 0
	for _, r := range findByNameRel(ents, "caller").Relationships {
		if r.Kind == "CALLS" && r.ToID == "helper" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("expected 1 CALLS caller→helper after dedup, got %d", n)
	}
}

// TestExtract_ImportsES6 (#41) — file with M import statements emits M
// IMPORTS relationships on module entities.
func TestExtract_ImportsES6(t *testing.T) {
	src := `import { Foo } from "./foo";
import bar from "bar";
const lodash = require("lodash");
`
	tree := parseJSRel(t, []byte(src))
	ents := runJS(t, src, "javascript", tree)

	want := map[string]bool{"./foo": false, "bar": false, "lodash": false}
	for _, e := range ents {
		if e.Subtype != "import" {
			continue
		}
		if _, ok := want[e.Name]; ok {
			want[e.Name] = true
		}
		if len(e.Relationships) != 1 || e.Relationships[0].Kind != "IMPORTS" {
			t.Errorf("import entity %q missing IMPORTS edge: %+v", e.Name, e.Relationships)
		}
	}
	for k, ok := range want {
		if !ok {
			t.Errorf("expected import entity for %q", k)
		}
	}
}

// TestExtract_TypeScript covers the same shape against the TS grammar to
// guarantee parity (single extractor, two languages).
func TestExtract_TypeScript(t *testing.T) {
	src := `import { X } from "./x";
class A {
  foo() { this.bar(); helper(); }
  bar() { return 1; }
}
function helper() {}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	if c := countRelByKind(ents, "A", "CONTAINS"); c != 2 {
		t.Errorf("expected 2 CONTAINS from A, got %d", c)
	}
	// Issue #144 — TS goes through the same JS extractor; CONTAINS targets
	// must be structural-ref stubs prefixed with the "typescript" segment.
	for _, m := range []string{"foo", "bar"} {
		want := "scope:operation:method:typescript:test.ts:" + m
		if !hasRelEdge(ents, "A", "CONTAINS", want) {
			t.Errorf("expected CONTAINS A→%s", want)
		}
	}
	if !hasRelEdge(ents, "foo", "CALLS", "bar") {
		t.Errorf("expected CALLS foo→bar")
	}
	if !hasRelEdge(ents, "foo", "CALLS", "helper") {
		t.Errorf("expected CALLS foo→helper")
	}
	importFound := false
	for _, e := range ents {
		if e.Subtype == "import" && e.Name == "./x" && len(e.Relationships) == 1 {
			importFound = true
		}
	}
	if !importFound {
		t.Errorf("expected ./x import entity with IMPORTS relationship")
	}
}
