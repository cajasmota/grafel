package cli

// #6327 S2 review — the classifier surface used to CLAIM a fact it could not
// COMPUTE, and enforced it with hand-maintained lists.
//
// SupportedExtension's doc comment said "'supported' on this surface means an
// extractor will produce entities". It asked detectLanguage and then subtracted
// one hand-written map (languagesAwaitingExtractor) holding exactly one entry.
// Enumerated against extractors.Get, EIGHT of the classifier's routed languages
// have no registered extractor — vbnet plus nginx, objective_c, perl, prisma,
// protobuf, r and toml. Seven of the eight answered "supported: true", so the
// claim was false for seven languages and the map was a carve-out for one.
//
// internal/classifier cannot ask extractors.Get: internal/extractors imports
// internal/classifier (incremental.go), so the edge only goes one way. This
// package imports BOTH, which is why the derived check lives here and why these
// tests do.

import (
	"testing"

	"github.com/cajasmota/grafel/internal/classifier"
	"github.com/cajasmota/grafel/internal/extractors"
)

// hasRegisteredExtractor is the fact the classifier surface used to assert by
// hand. It is a function, not a map.
func hasRegisteredExtractor(lang string) bool {
	_, ok := extractors.Get(lang)
	return ok
}

// SupportedExtension answers for the ROUTER, uniformly. Before this change
// `.vb` was the one routed extension that answered false, which is the shape of
// a special case rather than a rule.
func TestSupportedExtensionIsUniformAcrossRoutedLanguages(t *testing.T) {
	// Every one of these extensions is routed by the classifier and has NO
	// registered extractor. They must not disagree with each other.
	for _, ext := range []string{".vb", ".proto", ".prisma", ".toml", ".pm", ".r"} {
		lang := classifier.LanguageForExtension(ext)
		if lang == "" {
			t.Fatalf("LanguageForExtension(%q) = \"\" — this test's premise is that "+
				"the classifier routes it; re-derive the list if routing changed", ext)
		}
		if hasRegisteredExtractor(lang) {
			t.Logf("%s (%s) has acquired an extractor — it is no longer a "+
				"no-extractor language, drop it from this list", ext, lang)
			continue
		}
		if !classifier.SupportedExtension(ext) {
			t.Errorf("SupportedExtension(%q) = false but the router names it %q. "+
				"This surface answers for the router; whether an extractor exists "+
				"is derived from extractors.Get by the caller, not carved out per "+
				"language in internal/classifier.", ext, lang)
		}
	}
}

// The row for a no-extractor language survives — that is the #6338 guarantee
// #6327 S2 must not break — and it survives by DERIVATION, not by a map entry.
func TestUnsupportedRows_KeepsRoutedLanguageWithNoExtractor(t *testing.T) {
	if hasRegisteredExtractor("vbnet") {
		// S4 has landed. Dropping the row is now the CORRECT behaviour, and the
		// two tests that must fail on that commit are the removal-protocol pair
		// in unsupported_s4_protocol_6327_test.go. Failing here as well would
		// point an S4 author at the wrong file.
		t.Skip("vbnet is extractable — this pins the pre-S4 state only")
	}
	rows := UnsupportedRows(map[string]int{".vb": 672}, DoctorUnsupportedMinFiles)
	if len(rows) != 1 || rows[0].Ext != ".vb" || rows[0].Language != "VB.NET" || rows[0].Issue != "#6327" {
		t.Fatalf("rows = %+v, want one .vb/VB.NET/#6327 row — the #6321 report "+
			"must survive classification (#6327 S2)", rows)
	}
}

// ...and it disappears the moment an extractor is registered for its language,
// with no map edited anywhere. The seam is injected because the global
// extractor registry has no unregister, so the landed-extractor state cannot be
// simulated in-process against the real one without leaking into every other
// test in this package.
func TestUnsupportedRows_DropsRowOnceAnExtractorIsRegistered(t *testing.T) {
	counts := map[string]int{".vb": 672, ".pas": 14}

	before := unsupportedRows(counts, DoctorUnsupportedMinFiles, func(string) bool { return false })
	if len(before) != 2 {
		t.Fatalf("with no extractors: rows = %+v, want .vb and .pas", before)
	}

	after := unsupportedRows(counts, DoctorUnsupportedMinFiles, func(lang string) bool {
		return lang == "vbnet"
	})
	if len(after) != 1 || after[0].Ext != ".pas" {
		t.Fatalf("with a vbnet extractor registered: rows = %+v, want .pas only — "+
			"the row must drop from a MONTHS-OLD sidecar count with no reindex "+
			"and no map edit", after)
	}
}
