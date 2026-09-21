package classifier

import "testing"

// The classifier is the LOWERCASING end of the JS/TS case split recorded
// in #7278: `App.TS` is routed to "typescript" and gets a file carrier,
// and then the case-sensitive resolver consumers (hasJSExtension,
// looksLikeSourceFilePath) and the JS extractor's dottedModuleFromPath
// disagree with that answer. These rows pin the routing side in both
// directions so the disagreement cannot be narrowed away silently at
// this end either.
func TestDetectLanguageLowercasesExtension_7278(t *testing.T) {
	for _, p := range []string{"src/App.TS", "src/App.TSX", "src/App.MTS", "src/App.CTS"} {
		if got := detectLanguage(p); got != "typescript" {
			t.Errorf("detectLanguage(%q) = %q, want %q: the routing table is consulted on a LOWERCASED extension, so an uppercase extension is classified exactly like its lowercase twin (#7278)", p, got, "typescript")
		}
	}
	for _, p := range []string{"src/App.ts", "src/App.tsx", "src/App.mts", "src/App.cts"} {
		if got := detectLanguage(p); got != "typescript" {
			t.Errorf("detectLanguage(%q) = %q, want %q (#7278)", p, got, "typescript")
		}
	}
	// Negative twin: the rows above must not be satisfiable by a router
	// that answers "typescript" for anything.
	for _, p := range []string{"src/App.TXT", "src/App.txt"} {
		if got := detectLanguage(p); got == "typescript" {
			t.Errorf("detectLanguage(%q) = %q, want anything but typescript (#7278)", p, got)
		}
	}
}

// LanguageForExtension is the second lowercasing site. Its own fold is
// NOT observable through an extension-shaped input: it builds the probe
// path `grafelextprobe<ext>` and hands it to detectLanguage, which folds
// the extension again — so for `.TS` vs `.ts` the two folds coincide and
// the rows below pin delegation, not this site's case policy.
//
// I previously recorded that as "equivalent, unkillable at any level".
// That was WRONG. detectLanguage derives `base := path.Base(norm)`
// (classifier.go:846) and consults exactBasenameLanguageMap,
// basenameLanguageMap and containerVariantLanguage against it
// CASE-SENSITIVELY. When ext contains a separator, path.Base discards
// the probe stem and those basename lookups become reachable — so this
// site's fold does change the answer. An enumeration over every key of
// all three routing maps, in original / lower / upper spelling, bare and
// separator-prefixed (1001 inputs), found 15 inputs that distinguish
// dropping the fold and 17 that distinguish folding the other way.
//
// TestLanguageForExtensionFoldIsReachableViaBasenameRoute_7278 carries
// three of them, chosen to kill BOTH directions. They are deliberately
// absurd as "extensions": no caller supplies them. The sole production
// caller (internal/cli/unsupported_report.go:93) passes reportableExtension
// keys, which are already lowercased and separator-free, so the fold is
// inert on the production path — but inert-for-today's-callers is not
// equivalent, and the difference is exactly what an equivalence verdict
// would have retired permanently.
func TestLanguageForExtensionLowercases_7278(t *testing.T) {
	for _, ext := range []string{".TS", ".TSX", ".MTS", ".CTS"} {
		if got := LanguageForExtension(ext); got != "typescript" {
			t.Errorf("LanguageForExtension(%q) = %q, want %q (#7278)", ext, got, "typescript")
		}
	}
	for _, ext := range []string{".ts", ".tsx", ".mts", ".cts"} {
		if got := LanguageForExtension(ext); got != "typescript" {
			t.Errorf("LanguageForExtension(%q) = %q, want %q (#7278)", ext, got, "typescript")
		}
	}
	if got := LanguageForExtension(".TXT"); got == "typescript" {
		t.Errorf("LanguageForExtension(%q) = %q, want anything but typescript (#7278)", ".TXT", got)
	}
}

func TestLanguageForExtensionFoldIsReachableViaBasenameRoute_7278(t *testing.T) {
	cases := []struct {
		ext  string
		want string
		why  string
	}{
		{
			// Without the fold the base is `Package.swift`, which
			// exactBasenameLanguageMap routes to the MORE specific
			// "swift_package". The fold lowercases it out of that map.
			ext:  ".x/Package.swift",
			want: "swift",
			why:  "dropping the fold answers \"swift_package\" — the case-sensitive exactBasenameLanguageMap is reachable here",
		},
		{
			// Routed solely by exactBasenameLanguageMap["elm.json"],
			// which only the lowercase spelling of the base matches.
			// extensionLanguageMap has no ".json" key at all (153 keys,
			// none json-bearing), so when the basename lookup misses
			// there is no extension route to fall back on — which is
			// why BOTH flips land on "" rather than some generic JSON
			// answer.
			ext:  ".x/ELM.JSON",
			want: "elm",
			why:  "both dropping the fold and folding to upper answer \"\" — this row kills the site in both directions",
		},
		{
			// `routes` is a basenameLanguageMap key; upper-folding it
			// to `ROUTES` misses the map entirely.
			ext:  ".x/routes",
			want: "scala",
			why:  "folding to upper answers \"\" — the permissive-in-the-other-direction flip",
		},
	}
	for _, c := range cases {
		if got := LanguageForExtension(c.ext); got != c.want {
			t.Errorf("LanguageForExtension(%q) = %q, want %q: %s (#7278)", c.ext, got, c.want, c.why)
		}
	}
}
