package python_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/python"
	"github.com/cajasmota/archigraph/internal/types"
)

// extractPy is a typed helper that parses src and runs the extractor.
func extractPy(t *testing.T, src, path string) []types.EntityRecord {
	t.Helper()
	tree := parse(t, []byte(src))
	ext, ok := extractor.Get("python")
	if !ok {
		t.Fatal("python extractor not registered")
	}
	recs, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "python",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return recs
}

// TestErrorPatternPy_NoTryCatchEmit verifies issue #2282 — the Python
// extractor must no longer emit per-line `error_handling:try_catch:N`
// SCOPE.Pattern entities. A file dense in try/except is exercised so a
// regression in the secondary pass would surface here.
func TestErrorPatternPy_NoTryCatchEmit(t *testing.T) {
	src := `class Worker:
    def run(self):
        try:
            do_a()
        except ValueError:
            pass
        try:
            do_b()
        except Exception:
            try:
                cleanup()
            finally:
                pass
`
	recs := extractPy(t, src, "test.py")
	for _, r := range recs {
		if r.Kind == "SCOPE.Pattern" && strings.HasPrefix(r.Name, "error_handling:try_catch:") {
			t.Errorf("regression: %q emitted; #2282 dropped per-line try_catch entities", r.Name)
		}
	}
}

// TestErrorPatternPy_PreservesBaseExtraction guarantees the primary
// walker (class + method extraction) still works even though the
// secondary error-handling pass is now a no-op.
func TestErrorPatternPy_PreservesBaseExtraction(t *testing.T) {
	src := `class Worker:
    def run(self):
        try:
            do()
        except:
            pass
`
	recs := extractPy(t, src, "test.py")
	var hasClass, hasMethod bool
	for _, r := range recs {
		if r.Kind == "SCOPE.Component" && r.Name == "Worker" {
			hasClass = true
		}
		// Issue #45: methods are emitted with class-qualified Name.
		if r.Kind == "SCOPE.Operation" && r.Name == "Worker.run" {
			hasMethod = true
		}
	}
	if !hasClass {
		t.Error("base class extraction missing")
	}
	if !hasMethod {
		t.Error("base method extraction missing")
	}
}
