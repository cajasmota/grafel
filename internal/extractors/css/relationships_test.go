package css_test

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/css"
	"github.com/cajasmota/archigraph/internal/types"
)

// findImportRel returns the IMPORTS RelationshipRecord whose ToID matches
// module, or nil. Searches all entities' embedded Relationships.
func findImportRel(entities []types.EntityRecord, module string) *types.RelationshipRecord {
	for i := range entities {
		for j := range entities[i].Relationships {
			r := &entities[i].Relationships[j]
			if r.Kind == "IMPORTS" && r.ToID == module {
				return r
			}
		}
	}
	return nil
}

func assertImportContract(t *testing.T, r *types.RelationshipRecord, fromPath, module, wantLocal, wantLang string) {
	t.Helper()
	if r == nil {
		t.Fatalf("expected IMPORTS edge for module %q, found none", module)
	}
	if r.FromID != fromPath {
		t.Errorf("FromID: want %q, got %q", fromPath, r.FromID)
	}
	if r.ToID != module {
		t.Errorf("ToID: want %q, got %q", module, r.ToID)
	}
	if got := r.Properties["local_name"]; got != wantLocal {
		t.Errorf("Properties[local_name]: want %q, got %q", wantLocal, got)
	}
	if got := r.Properties["source_module"]; got != module {
		t.Errorf("Properties[source_module]: want %q, got %q", module, got)
	}
	if got, ok := r.Properties["imported_name"]; !ok || got != "" {
		t.Errorf("Properties[imported_name]: want \"\" present, got %q (present=%v)", got, ok)
	}
	if got := r.Properties["language"]; got != wantLang {
		t.Errorf("Properties[language]: want %q, got %q", wantLang, got)
	}
}

// --- plain CSS (tree-sitter) ---

func TestCSSExtractor_Imports_Plain(t *testing.T) {
	src := `@import url("foo.css");
@import "bar.css";
@import url('sub/baz.css') screen;
@import "qux.css" print;
body { color: red; }
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("css")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "main.css",
		Content:  []byte(src),
		Language: "css",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cases := []struct {
		module    string
		wantLocal string
	}{
		{"foo.css", "foo.css"},
		{"bar.css", "bar.css"},
		{"sub/baz.css", "baz.css"},
		{"qux.css", "qux.css"},
	}
	for _, c := range cases {
		r := findImportRel(entities, c.module)
		assertImportContract(t, r, "main.css", c.module, c.wantLocal, "css")
	}

	// Each @import should also have produced an import entity.
	importCount := 0
	for _, e := range entities {
		if e.Subtype == "import" {
			importCount++
			if e.Kind != "SCOPE.Component" {
				t.Errorf("import entity %q: Kind=%q, want SCOPE.Component", e.Name, e.Kind)
			}
		}
	}
	if importCount != 4 {
		t.Errorf("expected 4 import entities, got %d", importCount)
	}
}

// --- SCSS (regex) ---

func TestCSSExtractor_Imports_SCSS(t *testing.T) {
	src := `@import "foundation/reset";
@import "vars", "mixins/buttons";
@use "sass:math";
@forward "components/card";
$primary: #007bff;
`
	ext, _ := extractor.Get("css")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "app.scss",
		Content:  []byte(src),
		Language: "scss",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cases := []struct {
		module    string
		wantLocal string
	}{
		{"foundation/reset", "reset"},
		{"vars", "vars"},
		{"mixins/buttons", "buttons"},
		{"sass:math", "sass:math"},
		{"components/card", "card"},
	}
	for _, c := range cases {
		r := findImportRel(entities, c.module)
		assertImportContract(t, r, "app.scss", c.module, c.wantLocal, "scss")
	}
}

// --- Less (regex) ---

func TestCSSExtractor_Imports_Less(t *testing.T) {
	src := `@import "foundation/reset.less";
@import (reference) "mixins.less";
@import (less) "legacy";
@brand: #f00;
`
	ext, _ := extractor.Get("css")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "app.less",
		Content:  []byte(src),
		Language: "less",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cases := []struct {
		module    string
		wantLocal string
	}{
		{"foundation/reset.less", "reset.less"},
		{"mixins.less", "mixins.less"},
		{"legacy", "legacy"},
	}
	for _, c := range cases {
		r := findImportRel(entities, c.module)
		assertImportContract(t, r, "app.less", c.module, c.wantLocal, "less")
	}
}

// --- pinned non-emissions ---

// CSS has no function-call surface to model, so no CALLS edges should be
// emitted by any flavor of the CSS extractor.
func TestCSSExtractor_NoCalls(t *testing.T) {
	cssSrc := `:root { --x: 12px; }
.btn { padding: var(--x); transform: translate(10px, 20px); color: rgb(255, 0, 0); }
`
	tree := parseForTest(t, cssSrc)
	ext, _ := extractor.Get("css")
	entities, _ := ext.Extract(context.Background(), extractor.FileInput{
		Path: "x.css", Content: []byte(cssSrc), Language: "css", Tree: tree,
	})
	for _, e := range entities {
		for _, r := range e.Relationships {
			if r.Kind == "CALLS" {
				t.Errorf("unexpected CALLS edge: %+v", r)
			}
		}
	}
}

// CSS has no parent stylesheet entity to attach selectors to, so no
// CONTAINS edges should be emitted by any flavor of the CSS extractor.
func TestCSSExtractor_NoContains(t *testing.T) {
	srcs := map[string][]byte{
		"a.css": []byte(`@import "x.css"; body { color: red; } .a { color: blue; }`),
		"a.scss": []byte(`@import "vars";
$x: 1;
@mixin button($bg) { background: $bg; }
`),
		"a.less": []byte(`@import "vars.less";
@x: 1;
.button(@bg) { background: @bg; }
`),
	}
	ext, _ := extractor.Get("css")
	for path, src := range srcs {
		var input extractor.FileInput
		input = extractor.FileInput{Path: path, Content: src, Language: "css"}
		if path == "a.css" {
			input.Tree = parseForTest(t, string(src))
		}
		entities, _ := ext.Extract(context.Background(), input)
		for _, e := range entities {
			for _, r := range e.Relationships {
				if r.Kind == "CONTAINS" {
					t.Errorf("[%s] unexpected CONTAINS edge: %+v", path, r)
				}
			}
		}
	}
}
