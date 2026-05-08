package css_test

import (
	"context"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	tscss "github.com/smacker/go-tree-sitter/css"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/css"
)

func parseForTest(t *testing.T, src string) *sitter.Tree {
	t.Helper()
	parser := sitter.NewParser()
	parser.SetLanguage(tscss.GetLanguage())
	tree, err := parser.ParseCtx(context.Background(), nil, []byte(src))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	return tree
}

func TestCSSExtractor_Registered(t *testing.T) {
	_, ok := extractor.Get("css")
	if !ok {
		t.Fatal("css extractor not registered")
	}
}

func TestCSSExtractor_Selectors(t *testing.T) {
	src := `body {
    margin: 0;
    padding: 0;
}

.container {
    max-width: 1200px;
}

#header {
    background: #fff;
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("css")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "styles.css",
		Content:  []byte(src),
		Language: "css",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entities) < 3 {
		t.Fatalf("expected at least 3 entities, got %d", len(entities))
	}
	for _, e := range entities {
		if e.Subtype == "selector" {
			if e.Kind != "SCOPE.Stylesheet" {
				t.Errorf("entity %q: expected Kind=SCOPE.Stylesheet, got %q", e.Name, e.Kind)
			}
			if e.Language != "css" {
				t.Errorf("entity %q: expected Language=css, got %q", e.Name, e.Language)
			}
		}
	}
}

func TestCSSExtractor_CSSVariables(t *testing.T) {
	src := `:root {
    --primary-color: #007bff;
    --secondary-color: #6c757d;
    --font-size: 16px;
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("css")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "vars.css",
		Content:  []byte(src),
		Language: "css",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	varNames := make(map[string]bool)
	for _, e := range entities {
		if e.Subtype == "variable" {
			varNames[e.Name] = true
		}
	}
	for _, want := range []string{"--primary-color", "--secondary-color", "--font-size"} {
		if !varNames[want] {
			t.Errorf("expected CSS variable %q to be extracted", want)
		}
	}
}

func TestCSSExtractor_Keyframes(t *testing.T) {
	src := `@keyframes fadeIn {
    from { opacity: 0; }
    to { opacity: 1; }
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("css")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "anim.css",
		Content:  []byte(src),
		Language: "css",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, e := range entities {
		if e.Subtype == "keyframe" && e.Name == "fadeIn" {
			found = true
		}
	}
	if !found {
		t.Error("expected keyframe 'fadeIn' to be extracted")
	}
}

func TestCSSExtractor_EmptyInput(t *testing.T) {
	ext, _ := extractor.Get("css")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "empty.css",
		Content:  []byte{},
		Language: "css",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entities) != 0 {
		t.Errorf("expected 0 entities for empty input, got %d", len(entities))
	}
}
