package engine_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/classifier"
	"github.com/cajasmota/grafel/internal/engine"
	"github.com/cajasmota/grafel/internal/extractor"

	// Blank import for its init() side-effects: every language extractor
	// sub-package registers itself with the global extractor registry here.
	// Without it extractor.List() is empty and this whole guard is vacuous —
	// which is why TestLanguageRuleCoverage6537 asserts a floor on the
	// population size before it asserts anything about individual languages.
	_ "github.com/cajasmota/grafel/internal/extractors"
)

// ---------------------------------------------------------------------------
// Issue #6537 (from outside-contributor report #6535) — the guard that would
// have caught VB.NET shipping with no framework rules at all.
//
// v0.3.0 shipped a VB.NET extractor that produced 45,663 entities on a real
// ~1,600-file WinForms codebase and annotated 0.0% of them with a framework.
// The cause was not a bad rule; it was the total absence of a `vbnet` rule
// bucket. Nothing in the tree observed that: the pre-existing alias-map test
// (TestDormant3593_AliasMapWiring) walks the hand-maintained alias map, so it
// can only notice a bucket that is present-but-unwired, never a language with
// no bucket at all. Deleting an entire rule bucket was silent.
//
// This test closes that. It is deliberately NOT a test that VB.NET has rules —
// it does not, and giving it rules is separate work. It is a test that the set
// of languages without rules is a REVIEWED, CHECKED-IN list which cannot grow
// without someone editing knownLanguageRuleGaps in the same commit.
// ---------------------------------------------------------------------------

// extractorBackedRoutedLanguages returns the language keys that are both
//
//	(a) producible by the classifier's router, and
//	(b) backed by a registered extractor,
//
// sorted. This is the population the detector can ever be asked about via a
// real indexed file: the classifier decides file.Language, and an unextractable
// language never reaches Pass 2.5 with content worth scanning.
//
// Both halves are derived, never hand-listed. classifier.RoutedLanguagesForTest
// is the classifier's own enumeration of its extension/basename routing tables;
// extractor.List() is the live registry populated by the blank import above.
// A new language therefore enters this population automatically, which is the
// whole point — it must not be possible to add one and be quietly exempt.
func extractorBackedRoutedLanguages(t *testing.T) []string {
	t.Helper()

	registered := make(map[string]bool)
	for _, lang := range extractor.List() {
		registered[lang] = true
	}

	var out []string
	for _, lang := range classifier.RoutedLanguagesForTest() {
		if registered[lang] {
			out = append(out, lang)
		}
	}
	sort.Strings(out)
	return out
}

func TestLanguageRuleCoverage6537(t *testing.T) {
	rules, err := engine.LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	d := engine.New(rules)

	langs := extractorBackedRoutedLanguages(t)

	// --- vacuity guards -----------------------------------------------------
	// Every assertion below is of the form "for each language ...". If the
	// population collapses (blank import dropped, routing table renamed, rule
	// FS not embedded) the loop body never runs and the test passes while
	// observing nothing. These three checks make that collapse loud.
	if len(langs) < 50 {
		t.Fatalf("only %d classifier-routed languages have a registered extractor (%v); "+
			"expected at least 50 — the enumeration is broken, not the ruleset",
			len(langs), langs)
	}
	if n := d.RuleCount(); n < 100 {
		t.Fatalf("detector loaded only %d rules; expected at least 100 — the embedded "+
			"rule FS is not reaching this test", n)
	}
	// Anchors: languages whose rule coverage is substantial and long-standing.
	// If CompiledRuleCount ever reports zero for these, the accessor is broken
	// and every "gap" it reports below is noise.
	for _, anchor := range []string{"python", "java", "csharp", "go", "typescript", "ruby", "php"} {
		if !contains(langs, anchor) {
			t.Errorf("anchor language %q is missing from the routed+extractable population; "+
				"the enumeration no longer covers what it used to", anchor)
			continue
		}
		if n := d.CompiledRuleCount(anchor); n == 0 {
			t.Errorf("anchor language %q compiles 0 actionable rules; CompiledRuleCount "+
				"is broken, so the gap list below cannot be trusted", anchor)
		}
	}

	// --- the allowlist itself ----------------------------------------------
	allow := make(map[string]languageRuleGap, len(knownLanguageRuleGaps))
	for _, gap := range knownLanguageRuleGaps {
		if gap.Language == "" {
			t.Errorf("knownLanguageRuleGaps contains an entry with an empty Language")
			continue
		}
		if strings.TrimSpace(gap.Reason) == "" {
			t.Errorf("knownLanguageRuleGaps[%q] has no Reason; a bare language string is "+
				"exactly the un-reviewed state this list exists to prevent", gap.Language)
		}
		if _, dup := allow[gap.Language]; dup {
			t.Errorf("knownLanguageRuleGaps lists %q twice", gap.Language)
		}
		allow[gap.Language] = gap
	}

	// --- the guard ----------------------------------------------------------
	var uncovered []string
	for _, lang := range langs {
		n := d.CompiledRuleCount(lang)
		_, allowed := allow[lang]

		switch {
		case n == 0 && !allowed:
			// A language the indexer can produce, extracts, and for which no
			// YAML rule can ever fire — and nobody signed off on that.
			uncovered = append(uncovered, lang)
		case n > 0 && allowed:
			// The gap closed. Stale allowlist entries are how a reviewed list
			// rots back into an un-reviewed one.
			t.Errorf("language %q is listed in knownLanguageRuleGaps but now compiles %d "+
				"actionable rules — delete its allowlist entry", lang, n)
		}
	}
	if len(uncovered) > 0 {
		t.Errorf("%d classifier-routed, extractor-backed language(s) resolve to an empty "+
			"compiled rule set and are not in knownLanguageRuleGaps:\n  %s\n\n"+
			"Framework detection cannot fire on any file of these languages. Either add a "+
			"rule bucket under internal/engine/rules/<language>/, or add each language to "+
			"knownLanguageRuleGaps with a reason — in this commit, not later.",
			len(uncovered), strings.Join(uncovered, "\n  "))
	}

	// --- the allowlist may not outlive its subjects --------------------------
	for _, gap := range knownLanguageRuleGaps {
		if !contains(langs, gap.Language) {
			t.Errorf("knownLanguageRuleGaps[%q] is not a classifier-routed language with a "+
				"registered extractor; it exempts nothing and should be deleted", gap.Language)
		}
	}
}

// TestLanguageRuleCoverage6537_ListIsCurrent pins the size of the reviewed gap
// list. The count is the headline number from #6537 and moves only when someone
// closes a gap or opens one; either way it should be a deliberate line in a
// diff rather than a drift nobody reads.
func TestLanguageRuleCoverage6537_ListIsCurrent(t *testing.T) {
	rules, err := engine.LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	d := engine.New(rules)

	var stillOpen []string
	for _, gap := range knownLanguageRuleGaps {
		if d.CompiledRuleCount(gap.Language) == 0 {
			stillOpen = append(stillOpen, gap.Language)
		}
	}
	sort.Strings(stillOpen)

	if len(stillOpen) != len(knownLanguageRuleGaps) {
		t.Errorf("knownLanguageRuleGaps has %d entries but only %d are still real gaps: %v",
			len(knownLanguageRuleGaps), len(stillOpen), stillOpen)
	}

	// vbnet is the reported instance (#6535) and the reason this guard exists.
	// When Arm B lands a `vbnet` bucket this assertion fails and the entry gets
	// deleted — which is the intended, visible handshake between the two arms.
	if !contains(stillOpen, "vbnet") {
		t.Errorf("vbnet is no longer an open rule gap; remove it from knownLanguageRuleGaps "+
			"(and update this assertion) — open gaps are now: %v", stillOpen)
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
