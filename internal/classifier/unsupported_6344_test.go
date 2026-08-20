package classifier

// #6344 option 1 — the allow-list in unsupported.go is the whole policy: a
// language absent from it is silent, which is #6321's blind spot reproduced
// once per missing language. This file pins the batch of languages probed for
// #6344, and — just as importantly — pins that the table is still an
// ALLOW-list. A change that "fixed" the silence by reporting every unknown
// extension would satisfy the first half of this file and fail the second.

import (
	"context"
	"testing"
)

// newlyNamedLanguages is the #6344 batch: extension → the name the report must
// print. Every entry must ALSO satisfy the registry invariant enforced by
// TestUnsupportedLanguageRegistryHasNoRegisteredExtractor in internal/cli.
var newlyNamedLanguages = map[string]string{
	".trigger": "Apex",
	".pks":     "PL/SQL",
	".pkb":     "PL/SQL",
	".plb":     "PL/SQL",
	".xsl":     "XSLT",
	".xslt":    "XSLT",
	".ino":     "Arduino",
	".pde":     "Processing",
	".dtsx":    "SSIS package",
	".4gl":     "Informix 4GL",
	".nut":     "Squirrel",
	".moon":    "MoonScript",
}

// stillUnnamedJunk is the other direction. These are real extensions off real
// repo inventories (the #6338 report's own long tail) that name no language.
// They must stay OUT of the report; if they appear, the allow-list has become
// a deny-list and #6338's noise problem is back.
var stillUnnamedJunk = []string{
	".835", ".native", ".1", ".jrxml", ".ktr", ".properties", ".fbs",
}

// Direction 1: each newly named extension is skipped as an unsupported
// LANGUAGE and carries the display name the report prints.
func TestNewlyNamedLanguages_ReportedWithALanguageName(t *testing.T) {
	cls := New(nil)
	for ext, want := range newlyNamedLanguages {
		if got := LanguageDisplayName(ext); got != want {
			t.Errorf("LanguageDisplayName(%q) = %q, want %q", ext, got, want)
		}
		res := cls.ClassifyWithSize(context.Background(), "legacy/probe"+ext, 512)
		if !res.Skip {
			t.Errorf("%s: Skip = false, want true", ext)
			continue
		}
		if res.SkipReason != SkipReasonUnsupportedLanguage {
			t.Errorf("%s: SkipReason = %q, want %q", ext, res.SkipReason, SkipReasonUnsupportedLanguage)
		}
		tal := NewUnsupportedTally()
		tal.Observe("legacy/probe"+ext, res)
		if tal.Counts()[ext] != 1 {
			t.Errorf("%s: not tallied, counts = %v", ext, tal.Counts())
		}
	}
}

// Direction 2: genuinely unknown junk is still NOT reported. This is the half
// that fails if the allow-list is turned into a deny-list.
func TestUnknownJunkExtensions_StillNotReported(t *testing.T) {
	cls := New(nil)
	for _, ext := range stillUnnamedJunk {
		if got := LanguageDisplayName(ext); got != "" {
			t.Errorf("LanguageDisplayName(%q) = %q, want \"\" — junk must name no language", ext, got)
		}
		p := "junk/probe" + ext
		res := cls.ClassifyWithSize(context.Background(), p, 512)
		if res.Skip && res.SkipReason == SkipReasonUnsupportedLanguage {
			t.Errorf("%s: reported as an unsupported LANGUAGE; want unsupported_extension or a real language", ext)
		}
		tal := NewUnsupportedTally()
		tal.Observe(p, res)
		if n := len(tal.Counts()); n != 0 {
			t.Errorf("%s: tallied %v, want nothing", ext, tal.Counts())
		}
	}
}

// The registry invariant, checked from this side too: nothing added for #6344
// may be an extension the ROUTER already claims. A routed extension is not
// skipped at all, so an entry for one is dead weight that the report can never
// print — and it is the shape that precedes an extractor landing and the
// internal/cli invariant test going red.
func TestNewlyNamedLanguages_AreNotRoutedExtensions(t *testing.T) {
	for ext := range newlyNamedLanguages {
		if lang := LanguageForExtension(ext); lang != "" {
			t.Errorf("%s routes to %q — it is a SUPPORTED extension and must not be listed as an unsupported language", ext, lang)
		}
	}
}
