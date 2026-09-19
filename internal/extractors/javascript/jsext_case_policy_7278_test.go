package javascript

import "testing"

// dottedModuleFromPath is the extractor end of the JS/TS case split
// recorded in #7278. It is case-SENSITIVE, and it does not fail the way
// the resolver does: the resolver registers NOTHING for an
// uppercase-extension path, while this function strips no extension and
// emits a dotted module with the extension embedded — `src/App.TS`
// becomes `src.App.TS`. That asymmetry is why widening the resolver
// alone would turn a no-edge into a wrong-edge. This row pins today's
// answer in both directions so a one-sided widening cannot land quietly.
func TestDottedModuleFromPathIsCaseSensitive_7278(t *testing.T) {
	if got := dottedModuleFromPath("src/App.TS"); got != "src.App.TS" {
		t.Fatalf("dottedModuleFromPath(%q) = %q, want %q: no arm of jsImportExtensions matches an uppercase extension, so nothing is stripped and the extension lands inside the module name (#7278)", "src/App.TS", got, "src.App.TS")
	}
	if got := dottedModuleFromPath("src/app.ts"); got != "src.app" {
		t.Fatalf("dottedModuleFromPath(%q) = %q, want %q: the lowercase twin must still have its extension stripped, otherwise the row above is satisfied by a helper that strips nothing at all (#7278)", "src/app.ts", got, "src.app")
	}
}
