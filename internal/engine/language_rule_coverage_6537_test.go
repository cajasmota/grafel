package engine_test

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/classifier"
	"github.com/cajasmota/grafel/internal/engine"
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

// extraProducedLanguages are language keys the classifier can produce that
// classifier.RoutedLanguagesForTest() does not enumerate — it unions only
// extensionLanguageMap and basenameLanguageMap. These come from
// exactBasenameLanguageMap and from the compound-suffix branches inside
// detectLanguage. Each is hand-listed here but NOT taken on trust: the guard
// runs the real classifier over the probe path and fails if the key it
// produces has changed, so a stale entry cannot survive.
var extraProducedLanguages = []struct {
	Language string
	Probe    string
	Source   string
}{
	{Language: "swift_package", Probe: "Package.swift", Source: "exactBasenameLanguageMap"},
	{Language: "json", Probe: "openapi.json", Source: "detectLanguage .json branch (OpenAPI/Ocelot/Debezium routing)"},
	{Language: "jsonschema", Probe: "api/user.schema.json", Source: "detectLanguage *.schema.json branch"},
}

// producibleLanguages returns every language key an indexed file can carry into
// the Pass 2.5 detector, sorted.
//
// THE POPULATION IS EVERY ROUTED LANGUAGE, NOT ONLY THE EXTRACTABLE ONES.
// cmd/grafel/index.go builds its classifiedFile list from any file with a
// non-empty cr.Language (index.go:3981) and calls i.detector.Detect on every
// one of them (index.go:~4300) with no consultation of the extractor registry.
// The classifier says so itself for nginx/caddy (classifier.go:648-652: "These
// carry no language extractor; they still reach the Pass 2.5 detector").
// Filtering by extractor.List() would therefore exempt nine languages that
// Detect really is called with — including objective_c, perl and r, whose rule
// buckets load 11/4/4 rule sets that emit nothing.
//
// The routed half is derived, never hand-listed: classifier.RoutedLanguagesForTest
// is the classifier's own enumeration of its routing tables, so a new extension
// enters this population automatically.
func producibleLanguages(t *testing.T) []string {
	t.Helper()

	seen := make(map[string]bool)
	for _, lang := range classifier.RoutedLanguagesForTest() {
		seen[lang] = true
	}

	c := classifier.New(nil)
	ctx := context.Background()
	for _, extra := range extraProducedLanguages {
		got := c.Classify(ctx, extra.Probe).Language
		if got != extra.Language {
			t.Errorf("extraProducedLanguages: %q classifies as %q, not %q — the %s routing "+
				"changed and this entry is stale", extra.Probe, got, extra.Language, extra.Source)
			continue
		}
		seen[extra.Language] = true
	}

	out := make([]string, 0, len(seen))
	for lang := range seen {
		out = append(out, lang)
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

	langs := producibleLanguages(t)

	// --- vacuity guards -----------------------------------------------------
	// Every assertion below is of the form "for each language ...". If the
	// population collapses (routing table renamed, rule FS not embedded) the
	// loop body never runs and the test passes while observing nothing. These
	// checks make that collapse loud.
	if len(langs) < 60 {
		t.Fatalf("only %d producible languages enumerated (%v); expected at least 60 — "+
			"the enumeration is broken, not the ruleset", len(langs), langs)
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
			t.Errorf("anchor language %q is missing from the producible population; "+
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
			// A language the indexer can produce and hand to Detect, for which
			// no YAML rule can ever fire — and nobody signed off on that.
			uncovered = append(uncovered, lang)
		case n > 0 && allowed:
			// The gap closed. Stale allowlist entries are how a reviewed list
			// rots back into an un-reviewed one.
			t.Errorf("language %q is listed in knownLanguageRuleGaps but now compiles %d "+
				"actionable rules — delete its allowlist entry", lang, n)
		}
	}
	if len(uncovered) > 0 {
		t.Errorf("%d producible language(s) resolve to an empty compiled rule set and are "+
			"not in knownLanguageRuleGaps:\n  %s\n\n"+
			"Framework detection cannot fire on any file of these languages. Either add a "+
			"rule bucket under internal/engine/rules/<language>/, or add each language to "+
			"knownLanguageRuleGaps with a reason — in this commit, not later.",
			len(uncovered), strings.Join(uncovered, "\n  "))
	}

	// --- the allowlist may not outlive its subjects --------------------------
	for _, gap := range knownLanguageRuleGaps {
		if !contains(langs, gap.Language) {
			t.Errorf("knownLanguageRuleGaps[%q] is not a language the classifier can "+
				"produce; it exempts nothing and should be deleted", gap.Language)
		}
	}
}

// TestLanguageRuleCoverage6537_CountIsTheSumOfItsTerms pins CompiledRuleCount to
// its three terms.
//
// Without this, dropping `relationshipRules` or `fileConventions` from the sum
// is invisible: every gap the guard reports is a language with zero of all
// three, so a count built from source patterns alone produces an identical gap
// list. The accessor's doc comment claims the sum answers "can any YAML rule
// fire" — this is what makes that claim observed rather than asserted.
func TestLanguageRuleCoverage6537_CountIsTheSumOfItsTerms(t *testing.T) {
	rules, err := engine.LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	d := engine.New(rules)

	langs := producibleLanguages(t)
	var withRelationshipRules, withFileConventions, withSourcePatterns []string

	for _, lang := range langs {
		sp, rr, fc := d.CompiledRuleBreakdown(lang)
		if got, want := d.CompiledRuleCount(lang), sp+rr+fc; got != want {
			t.Errorf("CompiledRuleCount(%q) = %d, want %d (source_patterns=%d + "+
				"relationship_rules=%d + file_conventions=%d) — a term is missing from the sum",
				lang, got, want, sp, rr, fc)
		}
		if sp > 0 {
			withSourcePatterns = append(withSourcePatterns, lang)
		}
		if rr > 0 {
			withRelationshipRules = append(withRelationshipRules, lang)
		}
		if fc > 0 {
			withFileConventions = append(withFileConventions, lang)
		}
	}

	// Non-vacuity: the equality above only constrains a term that is non-zero
	// somewhere. If a term is dead across the entire ruleset, dropping it from
	// the sum would be undetectable and this test would be theatre.
	if len(withSourcePatterns) == 0 {
		t.Errorf("no language compiles any source_patterns; the source_patterns term is unobserved")
	}
	if len(withRelationshipRules) == 0 {
		t.Errorf("no language compiles any relationship_rules; the relationship_rules term is unobserved")
	}
	if len(withFileConventions) == 0 {
		t.Errorf("no language compiles any file_conventions; the file_conventions term is unobserved")
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
