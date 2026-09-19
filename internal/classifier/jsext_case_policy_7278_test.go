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
	for _, p := range []string{"src/app.ts", "src/app.tsx", "src/app.mts", "src/app.cts"} {
		if got := detectLanguage(p); got != "typescript" {
			t.Errorf("detectLanguage(%q) = %q, want %q (#7278)", p, got, "typescript")
		}
	}
	// Negative twin: the rows above must not be satisfiable by a router
	// that answers "typescript" for anything.
	if got := detectLanguage("src/App.TXT"); got == "typescript" {
		t.Errorf("detectLanguage(%q) = %q, want anything but typescript (#7278)", "src/App.TXT", got)
	}
}

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
