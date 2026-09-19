package javascript

import (
	"testing"

	"github.com/cajasmota/grafel/internal/classifier"
)

// TestJSImportExtensionsAgreeWithClassifier_7272 is the extractor-side half of
// the #7272 extension-set guard (the resolver-side half is
// TestJSExtensionSetsAgreeWithClassifier_7272 in internal/resolve).
//
// jsImportExtensions and internal/resolve's jsExtensions must agree:
// dottedModuleFromPath strips from this list while the resolver's
// modulesForJSFile strips from that one, and the two dotted forms have to
// match for an IMPORTS edge to bind. #7272 round 2 added ".mts"/".cts" to the
// resolver list only, which made dottedModuleFromPath("src/x.mts") return
// "src.x.mts" against the resolver's "src.x" — a silent non-binding.
//
// Both slices are derived from the classifier, the authority on a file's
// language, so neither package can drift without this failing.
func TestJSImportExtensionsAgreeWithClassifier_7272(t *testing.T) {
	authority := classifier.ExtensionsForLanguagesForTest("javascript", "typescript")
	if len(authority) == 0 {
		t.Fatal("classifier reported no javascript/typescript extensions — the guard would be " +
			"vacuous, so this is a failure rather than a pass")
	}
	inList := func(list []string, ext string) bool {
		for _, e := range list {
			if e == ext {
				return true
			}
		}
		return false
	}
	for _, ext := range authority {
		if !inList(jsImportExtensions, ext) {
			t.Errorf("classifier types %q as javascript/typescript but jsImportExtensions omits "+
				"it — dottedModuleFromPath leaves the extension on the dotted module and the "+
				"IMPORTS edge cannot bind to the resolver's form", ext)
		}
	}
	for _, ext := range jsImportExtensions {
		if !inList(authority, ext) {
			t.Errorf("jsImportExtensions lists %q but the classifier does not type it as "+
				"javascript/typescript (it says %q)", ext, classifier.LanguageForExtension(ext))
		}
	}
}

// TestMtsCtsDottedFormAndResolution_7272 pins the two consumers of
// jsImportExtensions that the divergence broke, by observed value rather than
// by asserting list membership.
func TestMtsCtsDottedFormAndResolution_7272(t *testing.T) {
	for _, ext := range []string{".mts", ".cts"} {
		if got, want := dottedModuleFromPath("src/x"+ext), "src.x"; got != want {
			t.Errorf("dottedModuleFromPath(%q) = %q, want %q — the extension must be stripped "+
				"so this agrees with the resolver's modulesForJSFile", "src/x"+ext, got, want)
		}
		// A specifier that already carries the extension must be used
		// verbatim, not have ".ts" appended onto it.
		if got, want := resolveRelativeImport("src/app.ts", "./x"+ext), "src/x"+ext; got != want {
			t.Errorf("resolveRelativeImport(%q, %q) = %q, want %q — an unrecognised extension "+
				"gets a spurious \".ts\" appended", "src/app.ts", "./x"+ext, got, want)
		}
	}
	// Negative control: a genuinely unknown extension still takes the
	// default ".ts" append, so the assertions above are not passing merely
	// because the function returns its input unchanged.
	if got, want := resolveRelativeImport("src/app.ts", "./x.weird"), "src/x.weird.ts"; got != want {
		t.Errorf("resolveRelativeImport(%q, %q) = %q, want %q", "src/app.ts", "./x.weird", got, want)
	}
}
