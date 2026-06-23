package javascript_test

// TestClassArrow_Measurement — corpus measurement for issue #771.
// Counts SCOPE.Operation entities emitted from class-field arrow methods
// across private fixtures. Only runs when GRAFEL_771_MEASURE=1.
//
//	GRAFEL_771_MEASURE=1 go test ./internal/extractors/javascript/... -run TestClassArrow_Measurement -v

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/treesitter/ts"
	tssmacker "github.com/cajasmota/grafel/internal/treesitter/ts/smacker"
	tsjavascript "github.com/smacker/go-tree-sitter/javascript"
	tstypescript "github.com/smacker/go-tree-sitter/typescript/typescript"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/extractors/javascript"
)

// findClientFixture searches for a client fixture directory.
// Checks environment variable GRAFEL_FIXTURES first, then common developer paths.
// Returns "" if not found.
func findClientFixture(fixtureName string) string {
	// Check environment variable first.
	if env := os.Getenv("GRAFEL_FIXTURES"); env != "" {
		path := filepath.Join(env, fixtureName)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	// Check common developer paths.
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, "private/grafel-fixtures", fixtureName),
		filepath.Join(home, "Documents/Projects/grafel-fixtures", fixtureName),
		"/tmp/grafel-fixtures/" + fixtureName,
	}

	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return ""
}

func TestClassArrow_Measurement(t *testing.T) {
	if os.Getenv("GRAFEL_771_MEASURE") != "1" {
		t.Skip("set GRAFEL_771_MEASURE=1 to run #771 corpus measurement")
	}
	cases := []struct {
		fixture string
		root    string
		minWant int
	}{
		// fixture-e: AngularJS-style service classes with class-field arrows;
		// expect at least 5 new SCOPE.Operation entities (actual: 51 found).
		{"client-fixture-e", findClientFixture("client-fixture-e"), 5},
		// fixture-b and fixture-c: React/functional code — no class-field
		// arrows in practice; minWant=0 so measurement reports without failing.
		{"client-fixture-b", findClientFixture("client-fixture-b"), 0},
		{"client-fixture-c", findClientFixture("client-fixture-c"), 0},
	}

	ext := javascript.New()
	jstsExts := map[string]bool{
		".js": true, ".jsx": true, ".ts": true, ".tsx": true,
	}

	for _, c := range cases {
		c := c
		t.Run(c.fixture, func(t *testing.T) {
			if _, err := os.Stat(c.root); err != nil {
				t.Skipf("fixture %s not present: %v", c.root, err)
			}
			var arrowMethods int
			var samples []string
			_ = filepath.WalkDir(c.root, func(p string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return nil
				}
				e := strings.ToLower(filepath.Ext(p))
				if !jstsExts[e] {
					return nil
				}
				lang := "javascript"
				if e == ".ts" || e == ".tsx" {
					lang = "typescript"
				}
				body, err := os.ReadFile(p)
				if err != nil {
					return nil
				}
				var grammar ts.Language
				if lang == "typescript" {
					grammar = tssmacker.WrapLanguage(tstypescript.GetLanguage())
				} else {
					grammar = tssmacker.WrapLanguage(tsjavascript.GetLanguage())
				}
				parser, parserErr := tssmacker.New().NewParser(grammar)
				if parserErr != nil {
					return nil
				}
				defer parser.Close()
				tree, parseErr := parser.Parse(body)
				if parseErr != nil || tree == nil {
					return nil
				}
				entities, extractErr := ext.Extract(context.Background(), extreg.FileInput{
					Path:     p,
					Content:  body,
					Language: lang,
					TSTree:   tree,
				})
				if extractErr != nil {
					return nil
				}
				for _, ent := range entities {
					if ent.Kind == "SCOPE.Operation" && ent.Subtype == "method" &&
						strings.Contains(ent.Signature, "= (...) =>") {
						arrowMethods++
						if len(samples) < 10 {
							relPath, _ := filepath.Rel(c.root, p)
							samples = append(samples, "  "+ent.Name+" ("+relPath+")")
						}
					}
				}
				return nil
			})
			t.Logf("[771-measure] fixture=%s arrowMethods=%d", c.fixture, arrowMethods)
			for _, s := range samples {
				t.Log(s)
			}
			if arrowMethods < c.minWant {
				t.Errorf("fixture=%s: got %d arrow-method Operations, want >= %d",
					c.fixture, arrowMethods, c.minWant)
			}
		})
	}
}
