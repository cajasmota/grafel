package resolve

import "testing"

// The JS/TS path carries five case decisions and they do not agree: the
// classifier lowercases before routing, resolve.isJSImportSource
// lowercases, and resolve.hasJSExtension, resolve.looksLikeSourceFilePath
// and the JS extractor's dottedModuleFromPath are case-SENSITIVE. #7278
// records that split; mutant M3 on hasJSExtension was ALIVE against the
// whole package, i.e. the case policy could be flipped in either
// direction and nothing observed it.
//
// These rows pin today's answer at each resolve-side site, in BOTH
// directions: an uppercase-extension path must get the case-sensitive
// answer AND its lowercase twin must get the other one, so a predicate
// that answers the same for both cannot satisfy the row.
//
// Every twin pair varies EXTENSION case only and holds the stem fixed
// (`src/App.TS` vs `src/App.ts`, never `src/app.ts`). Stem case is inert
// in all three resolve-side predicates today — they are pure trailing-
// suffix tests — but varying it would leave the neighbouring axis open
// the day one of them grows a basename rule, which classifier's
// detectLanguage already has. They assert
// behaviour as it is; they are not an endorsement of it. Widening any
// one site alone turns a no-edge into a wrong-edge (the extractor emits
// the dotted module `src.App.TS` for the same input), so a widening has
// to move the resolver and the extractor together — see #7278.

func TestHasJSExtensionIsCaseSensitive_7278(t *testing.T) {
	if hasJSExtension("src/App.TS") {
		t.Fatalf("hasJSExtension(%q) = true, want false: this arm is case-SENSITIVE; if it was widened to lowercase, modulesForFile now derives dotted modules for uppercase-extension files while the JS extractor still emits src.App.TS for them (#7278)", "src/App.TS")
	}
	if !hasJSExtension("src/App.ts") {
		t.Fatalf("hasJSExtension(%q) = false, want true: the lowercase-EXTENSION twin must still match, otherwise the row above is satisfied by a predicate that rejects everything (#7278)", "src/App.ts")
	}
	// Uppercase rejection holds across the whole extension table, not
	// just the `.ts` arm it was noticed on.
	for _, up := range []string{"src/App.TSX", "src/App.JS", "src/App.JSX", "src/App.MJS", "src/App.CJS", "src/App.MTS", "src/App.CTS"} {
		if hasJSExtension(up) {
			t.Errorf("hasJSExtension(%q) = true, want false (#7278)", up)
		}
	}
	for _, low := range []string{"src/App.tsx", "src/App.js", "src/App.jsx", "src/App.mjs", "src/App.cjs", "src/App.mts", "src/App.cts"} {
		if !hasJSExtension(low) {
			t.Errorf("hasJSExtension(%q) = false, want true (#7278)", low)
		}
	}
}

func TestModulesForFileUppercaseJSExtensionYieldsNoModule_7278(t *testing.T) {
	if got := modulesForFile("src/App.TS"); len(got) != 0 {
		t.Fatalf("modulesForFile(%q) = %v, want none: the JS arm is reached through the case-sensitive hasJSExtension, so an uppercase-extension file registers NO dotted binding today (#7278)", "src/App.TS", got)
	}
	got := modulesForFile("src/App.ts")
	if len(got) == 0 {
		t.Fatalf("modulesForFile(%q) = none, want dotted modules: without this the row above is satisfied by a dispatch that derives nothing at all (#7278)", "src/App.ts")
	}
	var found bool
	for _, m := range got {
		if m == "App" {
			found = true
		}
	}
	if !found {
		t.Fatalf("modulesForFile(%q) = %v, want it to contain %q (#7278)", "src/App.ts", got, "App")
	}
}

func TestIsJSImportSourceLowercases_7278(t *testing.T) {
	if !isJSImportSource("src/App.TS") {
		t.Fatalf("isJSImportSource(%q) = false, want true: this site LOWERCASES, unlike hasJSExtension in the same file; dropping the fold makes the JS default-export fallback stop firing for uppercase-extension sources (#7278)", "src/App.TS")
	}
	if !isJSImportSource("src/App.ts") {
		t.Fatalf("isJSImportSource(%q) = false, want true (#7278)", "src/App.ts")
	}
	// Negative twin: the row above must not be satisfiable by a
	// predicate that accepts every path.
	if isJSImportSource("src/App.TXT") {
		t.Fatalf("isJSImportSource(%q) = true, want false: a non-JS extension must be rejected in either case policy (#7278)", "src/App.TXT")
	}
	if isJSImportSource("src/App.txt") {
		t.Fatalf("isJSImportSource(%q) = true, want false (#7278)", "src/App.txt")
	}
}

func TestLooksLikeSourceFilePathIsCaseSensitive_7278(t *testing.T) {
	if looksLikeSourceFilePath("src/App.TS") {
		t.Fatalf("looksLikeSourceFilePath(%q) = true, want false: this allowlist is case-SENSITIVE, which is why an uppercase-extension TypeScript file's IMPORTS FromID falls through to DispositionBugExtractor today (#7278)", "src/App.TS")
	}
	if !looksLikeSourceFilePath("src/App.ts") {
		t.Fatalf("looksLikeSourceFilePath(%q) = false, want true: the lowercase-EXTENSION twin must still be accepted, otherwise the row above grades nothing (#7278)", "src/App.ts")
	}
}
