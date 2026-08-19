package cli

// #6327 S2 review, Finding 2 — the removal protocol has to be able to FIRE.
//
// The protocol documented on languagesAwaitingExtractor pointed at
// TestUnsupportedLanguageRegistryHasNoRegisteredExtractor, which triggers on the
// REGISTRY (unsupportedLanguageNames). That test cannot force the removal of
// languagesAwaitingExtractor itself: nothing about registering an extractor
// touches that map, and a language left in it is SKIPPED BY THE CLASSIFIER —
// so a fully built internal/extractors/vbnet/ would receive zero files while
// `doctor` and `status` went on printing "VB.NET — not supported, see #6327".
// That is #6321 reproduced with the extractor already written.
//
// Simulated before this test existed: a stub internal/extractors/vbnet/
// registered in init(), with the map entry left in place, left
// internal/classifier, internal/extractors and internal/cli all GREEN.
//
// The two tests below are the two halves of the unwind, and each names the
// exact map to edit.

import (
	"testing"

	"github.com/cajasmota/grafel/internal/classifier"
	"github.com/cajasmota/grafel/internal/extractors"
)

// Half one: the classifier must stop SKIPPING a language the moment an
// extractor for it is registered. Fires on the S4 commit that adds
// internal/extractors/vbnet/.
func TestLanguagesAwaitingExtractorHaveNoRegisteredExtractor(t *testing.T) {
	for _, lang := range classifier.LanguagesAwaitingExtractorForTest() {
		if !hasRegisteredExtractor(lang) {
			continue
		}
		t.Errorf("an extractor is registered for %q, but %q is still listed in "+
			"languagesAwaitingExtractor (internal/classifier/unsupported.go). "+
			"Every file of that language is being SKIPPED before it reaches the "+
			"extractor — it will produce zero entities and the report will keep "+
			"saying it is unsupported. Delete the %q entry from "+
			"languagesAwaitingExtractor; TestUnsupportedLanguageRegistryHasNoRegisteredExtractor "+
			"names the other two maps to unwind.", lang, lang, lang)
	}
}

// Half two, stated as a property rather than a fixture: nothing in
// languagesAwaitingExtractor may already be extractable, and everything in it
// must still be routed — an entry for a language the router does not produce is
// dead weight that silences nothing and skips nothing.
func TestLanguagesAwaitingExtractorAreRoutedAndSkipped(t *testing.T) {
	langs := classifier.LanguagesAwaitingExtractorForTest()
	if len(langs) == 0 {
		t.Skip("no languages awaiting an extractor — S3-S5 have landed")
	}
	routed := map[string]bool{}
	for _, l := range classifier.RoutedLanguagesForTest() {
		routed[l] = true
	}
	for _, lang := range langs {
		if !routed[lang] {
			t.Errorf("%q is in languagesAwaitingExtractor but the classifier never "+
				"produces that token — the entry does nothing; remove it", lang)
		}
		if _, ok := extractors.Get(lang); ok {
			t.Errorf("%q is in languagesAwaitingExtractor and extractable", lang)
		}
	}
}
