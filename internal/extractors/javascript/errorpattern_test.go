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

// TestErrorPatternJS_NoTryCatchEmit verifies issue #2282 — the JS/TS
// extractor must no longer emit per-line `error_handling:try_catch:N`
// SCOPE.Pattern entities. Covers both grammars and nested try/finally.
func TestErrorPatternJS_NoTryCatchEmit(t *testing.T) {
	for _, lang := range []string{"javascript", "typescript"} {
		t.Run(lang, func(t *testing.T) {
			src := `function load() {
  try {
    doWork();
  } catch (e) {
    try { cleanup(); } finally { reset(); }
  }
}
`
			for _, r := range extractAll(t, src, lang) {
				if r.Kind == "SCOPE.Pattern" && strings.HasPrefix(r.Name, "error_handling:try_catch:") {
					t.Errorf("regression in %s: %q emitted; #2282 dropped per-line try_catch entities", lang, r.Name)
				}
			}
		})
	}
}
