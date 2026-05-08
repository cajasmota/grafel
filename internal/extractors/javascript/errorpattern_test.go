package javascript_test

import (
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// extractAll is a small helper wrapping the existing jsExtractorShim
// so the error-pattern tests can pick a language and grammar.
func extractAll(t *testing.T, src, language string) []types.EntityRecord {
	t.Helper()
	content := []byte(src)
	var tree = parseJS(t, content)
	if language == "typescript" {
		tree = parseTS(t, content)
	}
	return extract(t, content, language, tree)
}

// errorPatternsJS filters records to SCOPE.Pattern entities.
func errorPatternsJS(t *testing.T, src, language string) []types.EntityRecord {
	t.Helper()
	var out []types.EntityRecord
	for _, r := range extractAll(t, src, language) {
		if r.Kind == "SCOPE.Pattern" {
			out = append(out, r)
		}
	}
	return out
}

// TestErrorPatternJS_SingleTryCatch verifies a basic JS try/catch emits
// one SCOPE.Pattern entity with the correct Name and line.
func TestErrorPatternJS_SingleTryCatch(t *testing.T) {
	src := `function load() {
  try {
    doWork();
  } catch (e) {
    console.error(e);
  }
}
`
	patterns := errorPatternsJS(t, src, "javascript")
	if len(patterns) != 1 {
		t.Fatalf("expected 1 error pattern, got %d", len(patterns))
	}
	p := patterns[0]
	if p.Kind != "SCOPE.Pattern" {
		t.Errorf("Kind = %q, want SCOPE.Pattern", p.Kind)
	}
	if p.Name != "error_handling:try_catch:2" {
		t.Errorf("Name = %q, want error_handling:try_catch:2", p.Name)
	}
	if p.StartLine != 2 {
		t.Errorf("StartLine = %d, want 2", p.StartLine)
	}
	if p.EndLine != 2 {
		t.Errorf("EndLine = %d, want 2", p.EndLine)
	}
	if p.Language != "javascript" {
		t.Errorf("Language = %q, want javascript", p.Language)
	}
	pt, _ := p.Metadata["pattern_type"].(string)
	if pt != "error_handling" {
		t.Errorf("metadata.pattern_type = %q, want error_handling", pt)
	}
	if p.ID == "" {
		t.Error("ID must be computed for JS pattern records")
	}
}

// TestErrorPatternJS_MultipleTryCatch verifies each try block has its
// own entity keyed by its own line.
func TestErrorPatternJS_MultipleTryCatch(t *testing.T) {
	src := `function a() {
  try { x(); } catch (e) {}
  try { y(); } catch (e) {}
}
`
	patterns := errorPatternsJS(t, src, "javascript")
	if len(patterns) != 2 {
		t.Fatalf("expected 2 patterns, got %d", len(patterns))
	}
	seen := make(map[string]bool, len(patterns))
	for _, p := range patterns {
		if seen[p.Name] {
			t.Errorf("duplicate Name %q", p.Name)
		}
		seen[p.Name] = true
	}
}

// TestErrorPatternJS_TryFinallyNoCatch verifies try-finally without a
// catch clause is still captured (matches Go/Python/Java behaviour).
func TestErrorPatternJS_TryFinallyNoCatch(t *testing.T) {
	src := `function a() {
  try {
    doWork();
  } finally {
    cleanup();
  }
}
`
	patterns := errorPatternsJS(t, src, "javascript")
	if len(patterns) != 1 {
		t.Fatalf("expected 1 pattern for try/finally, got %d", len(patterns))
	}
}

// TestErrorPatternJS_NestedTry verifies nested try/catch blocks each
// produce a separate entity.
func TestErrorPatternJS_NestedTry(t *testing.T) {
	src := `function a() {
  try {
    try {
      x();
    } catch (e) {}
  } catch (e) {}
}
`
	patterns := errorPatternsJS(t, src, "javascript")
	if len(patterns) != 2 {
		t.Fatalf("expected 2 patterns for nested try, got %d", len(patterns))
	}
}

// TestErrorPatternJS_ClassMethod verifies try blocks inside class
// methods are captured — the secondary walker scans the whole tree.
func TestErrorPatternJS_ClassMethod(t *testing.T) {
	src := `class Foo {
  bar() {
    try {
      x();
    } catch (e) {}
  }
}
`
	patterns := errorPatternsJS(t, src, "javascript")
	if len(patterns) != 1 {
		t.Fatalf("expected 1 pattern, got %d", len(patterns))
	}
	if patterns[0].StartLine != 3 {
		t.Errorf("StartLine = %d, want 3", patterns[0].StartLine)
	}
}

// TestErrorPatternJS_NoTry verifies files without try blocks produce
// no pattern records.
func TestErrorPatternJS_NoTry(t *testing.T) {
	src := `function a() { return 1; }
`
	patterns := errorPatternsJS(t, src, "javascript")
	if len(patterns) != 0 {
		t.Fatalf("expected 0 patterns, got %d", len(patterns))
	}
}

// TestErrorPatternJS_EmptyFile verifies empty content produces no
// patterns (the extractor short-circuits on len==0).
func TestErrorPatternJS_EmptyFile(t *testing.T) {
	patterns := errorPatternsJS(t, "", "javascript")
	if len(patterns) != 0 {
		t.Fatalf("expected 0 patterns for empty file, got %d", len(patterns))
	}
}

// TestErrorPatternJS_TypeScriptLanguage verifies a TS try/catch emits
// a pattern whose Language field is "typescript", not "javascript".
func TestErrorPatternJS_TypeScriptLanguage(t *testing.T) {
	src := `function load(): void {
  try {
    doWork();
  } catch (e: unknown) {
    console.error(e);
  }
}
`
	patterns := errorPatternsJS(t, src, "typescript")
	if len(patterns) != 1 {
		t.Fatalf("expected 1 pattern, got %d", len(patterns))
	}
	if patterns[0].Language != "typescript" {
		t.Errorf("Language = %q, want typescript", patterns[0].Language)
	}
	if !strings.HasPrefix(patterns[0].Name, "error_handling:try_catch:") {
		t.Errorf("Name = %q missing prefix", patterns[0].Name)
	}
}

// TestErrorPatternJS_PreservesBaseExtraction verifies the secondary
// pass is additive — function / class / import records must all
// still be present.
func TestErrorPatternJS_PreservesBaseExtraction(t *testing.T) {
	src := `import { foo } from "bar";

class Worker {
  run() {
    try {
      doWork();
    } catch (e) {}
  }
}
`
	recs := extractAll(t, src, "javascript")
	var hasClass, hasMethod, hasImport, hasPattern bool
	for _, r := range recs {
		if r.Kind == "SCOPE.Component" && r.Name == "Worker" {
			hasClass = true
		}
		if r.Kind == "SCOPE.Operation" && r.Name == "run" {
			hasMethod = true
		}
		if r.Kind == "SCOPE.Component" && r.Name == "bar" {
			hasImport = true
		}
		if r.Kind == "SCOPE.Pattern" {
			hasPattern = true
		}
	}
	if !hasClass {
		t.Error("base class extraction missing")
	}
	if !hasMethod {
		t.Error("base method extraction missing")
	}
	if !hasImport {
		t.Error("base import extraction missing")
	}
	if !hasPattern {
		t.Error("error handling pattern missing")
	}
}

// TestErrorPatternJS_ArrowFunctionBody verifies try blocks inside an
// arrow function body are captured (walker scans all nodes).
func TestErrorPatternJS_ArrowFunctionBody(t *testing.T) {
	src := `const run = () => {
  try {
    doWork();
  } catch (e) {}
};
`
	patterns := errorPatternsJS(t, src, "javascript")
	if len(patterns) != 1 {
		t.Fatalf("expected 1 pattern for arrow fn body, got %d", len(patterns))
	}
}
