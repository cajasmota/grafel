// languages_realtree_test.go binds the supported-language derivation to the
// REAL internal/extractors/ tree.
//
// Why this file exists (#6332): every other test for SupportedLanguages runs
// against a synthetic t.TempDir() tree, so the derivation was never evaluated
// against the directories it actually ships against. The derivation is not
// purely structural — it filters through four hand-maintained tables
// (extractorUtilityDirs, extractorNonLanguageFormats, extractorDirAliases,
// languageDisplayOverrides) in which *omission is the permissive default*. A
// new extractor directory therefore reaches the coverage matrix silently:
//
//   - internal/extractors/vbnet/ -> slug "vbnet", no alias, no display
//     override -> the matrix renders "Vbnet" via the titleCase fallback.
//   - a new NON-language format extractor -> a standalone language row,
//     because absence from extractorNonLanguageFormats means "language".
//
// The tests below make both cases loud. The roster is deliberately written out
// by hand: adding a language must be an explicit, reviewable edit naming the
// slug and the label a reader will see, not a directory appearing on disk.
//
// The sibling half of #6332 is .github/workflows/coverage-docs.yml, which now
// also triggers on internal/extractors/** so this package's tests and the
// generated-docs diff can actually run on the change that needs them.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// languageRoster maps every canonical language slug the real
// internal/extractors/ tree is expected to derive to its reviewed human label.
//
// That label is NOT uniformly "what the matrix renders", and the header used
// to claim it was (#6332). Two functions consume it and they disagree:
//
//	languageDisplayName(slug) — placeholder by-language pages and the
//	  summary's placeholder table — falls back to titleCase(slug).
//	languageDisplay(slug) — every summary pivot row built from registry
//	  records (generate.go) — falls back to the RAW slug.
//
// For the 12 slugs carrying a languageDisplayOverrides entry the two agree and
// the roster label is literally what ships. For the other 26 the shipped pivot
// renders the bare slug — docs/coverage/summary.md reads "[python]", "[go]",
// "[rust]" — and the roster label is what the placeholder path would render.
// Both are asserted: TestRealTreeLanguageDisplayNamesAreReviewed pins the
// first function, TestRealTreeSummaryPivotLabelsAreReviewed the second, so
// neither renderer can drift from the reviewed label unnoticed.
//
// Both directions of membership are enforced:
//
//	slug on disk but not here  -> a language is reaching the matrix unreviewed
//	here but not on disk       -> dead roster entry
//
// Adding a language: add the extractor directory, add it here with the label
// you want, and add a languageDisplayOverrides entry if the titleCase fallback
// (e.g. "Vbnet") is not the label you want. Adding a NON-language format
// extractor: add it to extractorNonLanguageFormats instead — do NOT add it
// here. Adding a dialect, framework or single-file-component directory that
// belongs under a language already listed here (typescript, vue, svelte,
// astro -> jsts): add it to extractorDirAliases instead — adding it here
// instead ships a standalone matrix row and a by-language page for something
// that is not a separate language. unclassifiedSlugRemedy below is the
// machine-readable form of this paragraph; keep the two in step.
//
// If the label you want IS just titleCase(slug), that is fine — but say so by
// adding the slug to languageLabelDefaultAccepted. Writing the fallback into
// the roster alone is rejected, because that is exactly what #6332 found here
// for "php": indistinguishable from pasting back what the failure printed.
var languageRoster = map[string]string{
	"assembly": "Assembly",
	"bicep":    "Bicep",
	"c-cpp":    "C/C++",
	"clojure":  "Clojure",
	"cobol":    "COBOL",
	"crystal":  "Crystal",
	"csharp":   "C#",
	"dart":     "Dart",
	"elixir":   "Elixir",
	"elm":      "Elm",
	"erlang":   "Erlang",
	"fsharp":   "F#",
	"go":       "Go",
	"groovy":   "Groovy",
	"haskell":  "Haskell",
	"idris":    "Idris",
	"java":     "Java",
	"jcl":      "JCL",
	"jsts":     "JS/TS",
	"kotlin":   "Kotlin",
	"lisp":     "Lisp",
	"lua":      "Lua",
	"nim":      "Nim",
	"ocaml":    "OCaml",
	"php":      "PHP",
	"pony":     "Pony",
	"python":   "Python",
	"reasonml": "ReasonML",
	"rescript": "ReScript",
	"ruby":     "Ruby",
	"rust":     "Rust",
	"scala":    "Scala",
	"sml":      "Standard ML",
	"solidity": "Solidity",
	"swift":    "Swift",
	"vbnet":    "VB.NET",
	"verilog":  "Verilog",
	"vhdl":     "VHDL",
	"zig":      "Zig",
}

// unclassifiedSlugRemedy is the failure text for an extractor directory that
// reached the derivation without being classified. It enumerates ALL FOUR
// tables a contributor may need, because a remedy that omits the applicable
// table is worse than no remedy: following it faithfully then reproduces the
// defect the gate exists to catch. See
// TestUnclassifiedSlugRemedyNamesEveryTable.
func unclassifiedSlugRemedy(slug string) string {
	return fmt.Sprintf("extractor slug %q is derived from internal/extractors/ but is not in "+
		"languageRoster: it would reach the coverage matrix unreviewed. Pick the "+
		"table that fits what %q actually is:\n"+
		"  - a genuinely new LANGUAGE: add it to languageRoster, and add a "+
		"languageDisplayOverrides entry if %q (the titleCase fallback) is not the "+
		"label you want — do not just paste that fallback into the roster;\n"+
		"  - a DIALECT, FRAMEWORK or single-file-component format that belongs "+
		"under a language already in the roster (typescript, vue, svelte and astro "+
		"all fold into jsts this way): add it to extractorDirAliases as "+
		"%q -> \"<existing slug>\". Do NOT add it to languageRoster; that ships a "+
		"standalone matrix row and its own by-language page, which is exactly what "+
		"the alias exists to prevent;\n"+
		"  - a NON-LANGUAGE format (build files, config, markup): add it to "+
		"extractorNonLanguageFormats;\n"+
		"  - a SHARED UTILITY package with no per-language extraction: add it to "+
		"extractorUtilityDirs.",
		slug, slug, titleCase(slug), slug)
}

// languageLabelDefaultAccepted names the slugs whose roster label is
// deliberately just titleCase(slug) — "Python", "Go", "Rust". It exists so
// that accepting the fallback is a visible act rather than the quiet path.
//
// Why (#6332): the roster shipped "php": "Php". There is no
// languageDisplayOverrides["php"], so that string was verbatim the fallback
// the failure message prints in its parenthetical. Pasting it back made the
// gate go green and certified the wrong label as reviewed — and then made the
// gate fail the correction. A gate whose cheapest remedy reproduces the defect
// it prevents is worse than no gate.
//
// This cannot force judgement — a contributor can still add a slug here
// without thinking. What it removes is the case where writing the fallback
// leaves NO trace at all: the acceptance now appears as its own line in the
// diff, next to the label, where a reviewer can ask "is 'Vbnet' really what we
// want to publish?".
var languageLabelDefaultAccepted = map[string]bool{
	"assembly": true,
	"bicep":    true,
	"clojure":  true,
	"crystal":  true,
	"dart":     true,
	"elixir":   true,
	"elm":      true,
	"erlang":   true,
	"go":       true,
	"groovy":   true,
	"haskell":  true,
	"idris":    true,
	"java":     true,
	"kotlin":   true,
	"lisp":     true,
	"lua":      true,
	"nim":      true,
	"pony":     true,
	"python":   true,
	"ruby":     true,
	"rust":     true,
	"scala":    true,
	"solidity": true,
	"swift":    true,
	"verilog":  true,
	"zig":      true,
}

// TestRosterFallbackLabelsAreDeliberate separates "a human chose this label"
// from "a human accepted the fallback". Both are legitimate; only the second
// used to be indistinguishable from not having looked.
func TestRosterFallbackLabelsAreDeliberate(t *testing.T) {
	if len(languageRoster) == 0 {
		t.Fatal("languageRoster is empty, so this test is vacuous")
	}
	for _, s := range sortedKeysString(languageRoster) {
		_, overridden := languageDisplayOverrides[s]
		isFallback := !overridden && languageRoster[s] == titleCase(s)
		switch {
		case isFallback && !languageLabelDefaultAccepted[s]:
			t.Errorf("languageRoster[%q] = %q is exactly titleCase(%q) — the fallback, "+
				"not a chosen label, and indistinguishable from pasting back what the "+
				"failure message printed. Decide, then record the decision: if %q is "+
				"genuinely the label docs/coverage should publish, add %q to "+
				"languageLabelDefaultAccepted; if it is not (php -> \"PHP\", vbnet -> "+
				"\"VB.NET\"), add languageDisplayOverrides[%q] and put the real label "+
				"in the roster.", s, languageRoster[s], s, languageRoster[s], s, s)
		case !isFallback && languageLabelDefaultAccepted[s]:
			t.Errorf("languageLabelDefaultAccepted lists %q, but its roster label %q is "+
				"not the titleCase fallback — remove the stale acceptance", s, languageRoster[s])
		}
	}
	for _, s := range sortedKeysBool(languageLabelDefaultAccepted) {
		if _, ok := languageRoster[s]; !ok {
			t.Errorf("languageLabelDefaultAccepted lists %q but languageRoster does not — "+
				"dead entry, remove it", s)
		}
	}
}

// TestSupportedLanguagesMatchesRealTree runs SupportedLanguages against the
// actual repository root — not a fixture — and pins the result to the roster
// above in both directions.
func TestSupportedLanguagesMatchesRealTree(t *testing.T) {
	root := repoRoot(t)
	got := SupportedLanguages(root)
	if len(got) == 0 {
		t.Fatalf("SupportedLanguages(%q) returned nothing: the real "+
			"internal/extractors/ tree was not read, so this test is vacuous", root)
	}

	derived := make(map[string]bool, len(got))
	for _, s := range got {
		derived[s] = true
	}

	for _, s := range got {
		if _, ok := languageRoster[s]; !ok {
			t.Errorf("%s", unclassifiedSlugRemedy(s))
		}
	}
	for s := range languageRoster {
		if !derived[s] {
			t.Errorf("languageRoster lists %q but the real internal/extractors/ tree "+
				"no longer derives it — dead roster entry, remove it", s)
		}
	}
}

// TestRealTreeLanguageDisplayNamesAreReviewed asserts every derived slug
// renders the exact label recorded in the roster. A new directory cannot reach
// the matrix "unnamed": either it already renders the reviewed label, or a
// human has to write the label down here to make this test pass.
func TestRealTreeLanguageDisplayNamesAreReviewed(t *testing.T) {
	langs := SupportedLanguages(repoRoot(t))
	if len(langs) == 0 {
		t.Fatal("SupportedLanguages returned nothing: the real internal/extractors/ " +
			"tree was not read, so this test is vacuous")
	}
	for _, s := range langs {
		want, ok := languageRoster[s]
		if !ok {
			continue // reported by TestSupportedLanguagesMatchesRealTree
		}
		if got := languageDisplayName(s); got != want {
			t.Errorf("languageDisplayName(%q) = %q, roster says %q — the coverage "+
				"matrix would render a label nobody signed off on", s, got, want)
		}
	}
}

// TestRealTreeSummaryPivotLabelsAreReviewed pins the OTHER renderer.
//
// languageDisplayName, guarded above, is reached only from the placeholder
// paths in generate.go — extractor-supported languages with zero registry
// records, today just {bicep}. Every other row of the shipped matrix goes
// through languageDisplay, whose fallback is the raw slug rather than
// titleCase. Guarding only the first function left 37 of 38 slugs' rendered
// labels unasserted, while the roster header claimed to describe them.
//
// The expectation therefore forks on the override table, which is the one
// thing both functions consult: with an override, both render the roster
// label; without one, the pivot renders the bare slug. Fixing a label means
// adding a languageDisplayOverrides entry — which is what makes the label
// reach BOTH renderers, and what the php fix in this series did.
func TestRealTreeSummaryPivotLabelsAreReviewed(t *testing.T) {
	langs := SupportedLanguages(repoRoot(t))
	if len(langs) == 0 {
		t.Fatal("SupportedLanguages returned nothing: the real internal/extractors/ " +
			"tree was not read, so this test is vacuous")
	}
	for _, s := range langs {
		want, ok := languageRoster[s]
		if !ok {
			continue // reported by TestSupportedLanguagesMatchesRealTree
		}
		if _, overridden := languageDisplayOverrides[s]; !overridden {
			want = s // languageDisplay falls back to the raw slug, not titleCase
			if got := languageDisplay(s); got != want {
				t.Errorf("languageDisplay(%q) = %q, want the bare slug %q — the summary "+
					"pivot's fallback changed, so 26 rows now render a label this "+
					"roster never reviewed", s, got, want)
			}
			continue
		}
		if got := languageDisplay(s); got != want {
			t.Errorf("languageDisplay(%q) = %q, roster says %q — the summary pivot "+
				"would render a label nobody signed off on", s, got, want)
		}
	}
}

// TestRealTreeExtractorDirForSlugResolves asserts the on-disk citation each
// placeholder by-language page prints actually exists. extractorDirForSlug has
// its own hand-written switch, so an alias added without a matching case makes
// the generated page cite a directory that is not there.
func TestRealTreeExtractorDirForSlugResolves(t *testing.T) {
	root := repoRoot(t)
	langs := SupportedLanguages(root)
	if len(langs) == 0 {
		t.Fatal("SupportedLanguages returned nothing: the real internal/extractors/ " +
			"tree was not read, so this test is vacuous")
	}
	for _, s := range langs {
		dir := extractorDirForSlug(s)
		full := filepath.Join(root, "internal", "extractors", dir)
		if fi, err := os.Stat(full); err != nil || !fi.IsDir() {
			t.Errorf("extractorDirForSlug(%q) = %q, but internal/extractors/%s is not "+
				"a directory (%v) — the generated by-language page would cite a "+
				"path that does not exist", s, dir, dir, err)
		}
	}
}

// TestExtractorClassificationTablesAreLive is the reverse guard: an entry in
// any of the hand-maintained tables that no longer corresponds to anything on
// disk is dead config. It was not hypothetical — extractorUtilityDirs kept
// "complexity" and "references" long after #3653 / #3650 deleted those
// packages, and extractorDirAliases kept a "typescript" key for a directory
// that does not exist (the javascript/ extractor handles .ts).
//
// Reintroducing any of those directories fails
// TestSupportedLanguagesMatchesRealTree. Note what that does and does not buy
// you: the failure names the four candidate tables, but it cannot know which
// one is right. For internal/extractors/typescript/ the correct answer is
// extractorDirAliases (-> jsts); putting it in languageRoster instead compiles,
// passes, and ships the standalone Typescript row this PR deleted. The remedy
// text spells that trade-off out — see unclassifiedSlugRemedy and
// TestUnclassifiedSlugRemedyNamesEveryTable — but the judgement is still the
// contributor's.
func TestExtractorClassificationTablesAreLive(t *testing.T) {
	root := repoRoot(t)
	onDisk := map[string]bool{}
	entries, err := os.ReadDir(filepath.Join(root, "internal", "extractors"))
	if err != nil {
		t.Fatalf("read internal/extractors: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			onDisk[e.Name()] = true
		}
	}

	for _, tab := range []struct {
		name string
		keys map[string]bool
	}{
		{"extractorUtilityDirs", extractorUtilityDirs},
		{"extractorNonLanguageFormats", extractorNonLanguageFormats},
	} {
		for _, k := range sortedKeysBool(tab.keys) {
			if !onDisk[k] {
				t.Errorf("%s has entry %q but internal/extractors/%s does not exist — "+
					"dead config, remove it", tab.name, k, k)
			}
		}
	}
	for _, k := range sortedKeysString(extractorDirAliases) {
		if !onDisk[k] {
			t.Errorf("extractorDirAliases has key %q but internal/extractors/%s does "+
				"not exist — dead config, remove it", k, k)
		}
	}

	derived := map[string]bool{}
	for _, s := range SupportedLanguages(root) {
		derived[s] = true
	}
	for _, k := range sortedKeysString(extractorDirAliases) {
		if v := extractorDirAliases[k]; !derived[v] {
			t.Errorf("extractorDirAliases[%q] = %q but %q is not a derived language "+
				"slug — the alias target is unreachable", k, v, v)
		}
	}
	for _, k := range sortedKeysString(languageDisplayOverrides) {
		if !derived[k] {
			t.Errorf("languageDisplayOverrides has entry %q but no extractor "+
				"directory derives that slug — dead config, remove it", k)
		}
	}
}

func sortedKeysBool(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedKeysString(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestToolReservedDirsNeverBecomeLanguages is the regression case for the
// defect this file's real-tree guard caught on main (#6332): #6349 added
// internal/extractors/testdata/incrfixture/main.go, and because "testdata"
// was in none of the three classification maps it took the permissive default
// and shipped a "Testdata" row plus a by-language/testdata.md placeholder into
// the coverage matrix.
//
// Synthetic tree on purpose: the real tree can only ever exhibit whichever of
// these names happens to exist today (currently just testdata/), so pinning
// the rule needs a tree we control. The real-tree guard above is the other
// half — it is what noticed the defect in the first place.
func TestToolReservedDirsNeverBecomeLanguages(t *testing.T) {
	root := t.TempDir()
	extractorsRoot := filepath.Join(root, "internal", "extractors")
	reserved := []string{"testdata", "vendor", "_scratch", ".cache", "_"}
	for _, d := range append([]string{"python", "golang"}, reserved...) {
		if err := os.MkdirAll(filepath.Join(extractorsRoot, d), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	got := SupportedLanguages(root)
	want := []string{"go", "python"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SupportedLanguages leaked a Go-tool-reserved directory into the "+
			"coverage matrix:\nwant %v\n got %v", want, got)
	}
	for _, d := range reserved {
		if !isToolReservedDir(d) {
			t.Errorf("isToolReservedDir(%q) = false, want true", d)
		}
	}
	for _, d := range []string{"python", "golang", "vbnet", "testdatabase", "cpp"} {
		if isToolReservedDir(d) {
			t.Errorf("isToolReservedDir(%q) = true, want false — a real extractor "+
				"directory would be silently dropped from the matrix", d)
		}
	}
}

// TestDiscoverSkipsToolReservedDirs pins the sibling exposure: discover's
// extractorDirLister reads the same internal/extractors/ tree and would
// otherwise propose a "lang.testdata" registry candidate (#6332).
func TestDiscoverSkipsToolReservedDirs(t *testing.T) {
	root := t.TempDir()
	extractorsRoot := filepath.Join(root, "internal", "extractors")
	for _, d := range []string{"python", "testdata", "vendor", "_scratch"} {
		if err := os.MkdirAll(filepath.Join(extractorsRoot, d), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	cands := map[string]*Candidate{}
	extractorDirLister(root, cands)
	if _, ok := cands["lang.python"]; !ok {
		t.Fatalf("extractorDirLister dropped a real language; got %v", sortedCandIDs(cands))
	}
	for _, id := range []string{"lang.testdata", "lang.vendor", "lang.scratch", "lang._scratch"} {
		if _, ok := cands[id]; ok {
			t.Errorf("extractorDirLister proposed %q from a Go-tool-reserved "+
				"directory; got %v", id, sortedCandIDs(cands))
		}
	}
}

func sortedCandIDs(m map[string]*Candidate) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestUnclassifiedSlugRemedyNamesEveryTable guards the gate's own guidance.
//
// A gate is only as good as the remedy it hands you: if the message omits the
// table that is actually correct for the case in hand, following it faithfully
// reintroduces the defect. That is not hypothetical — the first version of this
// message offered languageRoster, extractorNonLanguageFormats and
// extractorUtilityDirs but not extractorDirAliases, so recreating
// internal/extractors/typescript/ (whose alias to "jsts" #6332 removed as dead)
// and doing exactly what the failure said produced a standalone "Typescript"
// row plus a by-language/typescript.md page: precisely what the alias existed
// to prevent, and what any future SFC/framework directory would hit.
//
// Synthetic tree on purpose — the real tree cannot host a deliberately
// unclassified directory without failing every other test in this file.
func TestUnclassifiedSlugRemedyNamesEveryTable(t *testing.T) {
	root := t.TempDir()
	extractorsRoot := filepath.Join(root, "internal", "extractors")
	for _, d := range []string{"golang", "typescript"} {
		if err := os.MkdirAll(filepath.Join(extractorsRoot, d), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	var unclassified []string
	for _, s := range SupportedLanguages(root) {
		if _, ok := languageRoster[s]; !ok {
			unclassified = append(unclassified, s)
		}
	}
	if !reflect.DeepEqual(unclassified, []string{"typescript"}) {
		t.Fatalf("setup: want exactly [typescript] unclassified, got %v", unclassified)
	}

	msg := unclassifiedSlugRemedy("typescript")
	for _, table := range []string{
		"languageRoster",
		"languageDisplayOverrides",
		"extractorNonLanguageFormats",
		"extractorUtilityDirs",
		"extractorDirAliases",
	} {
		if !strings.Contains(msg, table) {
			t.Errorf("the remedy for an unclassified extractor directory never names %s;\n"+
				"a contributor following this message cannot reach that table:\n%s", table, msg)
		}
	}
}

// ---------------------------------------------------------------------------
// #6353: the OTHER half of the language axis — registry `language` tags.
//
// The coverage matrix's language rows are the UNION of two sources: slugs
// derived from internal/extractors/<lang>/ (guarded above, #6351) and the
// `language` tags on records in docs/coverage/registry.json (guarded here).
// Until #6353 the second source was completely unguarded: adding a record
// with a novel `language` value shipped a row into the published matrix with
// nothing checking it, and the alias table that exists precisely to collapse
// javascript/typescript into jsts was bypassed by not going through a
// directory at all. That is not hypothetical — one record tagged
// language:"javascript" (against 84 tagged "jsts") rendered a spurious
// "[javascript](by-language/javascript.md) | 0 | 0 | 0 | 1" row directly
// below the real "[JS/TS]" row in the shipped docs/coverage/summary.md.
//
// THE EXCEPTION LIST, AND HOW IT WAS DERIVED.
//
// #6353's thread hand-listed five registry `language` values as "legitimately
// not derived directory slugs": multi (126), c-cpp (55), lisp (1), idris (1),
// jcl (1). Enumerating the set mechanically — SupportedLanguages(repoRoot)
// diffed against the distinct tags in registry.json, which is exactly what
// this test does — shows four of those five are wrong:
//
//	c-cpp  IS a derived slug: internal/extractors/cpp/ + extractorDirAliases.
//	lisp   IS a derived slug: internal/extractors/lisp/ exists.
//	idris  IS a derived slug: internal/extractors/idris/ exists.
//	jcl    IS a derived slug: internal/extractors/jcl/ exists.
//
// Had that list been pasted in as the exception list, four dead entries would
// have shipped on day one — entries that then have to be actively defended
// forever, because nothing distinguishes a justified exception from a stale
// one. Hence: the enumeration is mechanical, and the list below is what
// survived it. That is also why registry exceptions get a staleness check of
// their own (second half of this test): a hand-maintained list that only
// fails on unlisted items and never on entries that have gone stale is
// #6382's defect class, and #6382 was closed, so it is written inline here.
//
// The mechanical diff leaves exactly ONE non-slug value: "multi".
//
// "multi" is not a language and deliberately has no extractor directory. It
// is the cross-cutting bucket for build/CI/infra records that apply to every
// language (Docker, Terraform, GitHub Actions, ...). It cannot reach the
// language pivot: generate.go skips language=="multi" (and "") when building
// the by-language rows and folds those records into the cross-cutting section
// and the headline totals instead. Verified against the shipped
// docs/coverage/summary.md — there is no "[multi]" row, while "[C/C++]" IS a
// row (derived, as above). So "multi" is a genuine exception, not a retag:
// retagging it to any real language would be a lie about what those records
// cover.
//
// Every other non-derived value is a RETAG, not an exception. "javascript" is
// the worked example: it folds into "jsts" via extractorDirAliases, the same
// collapse the directory path already performs.
//
// (vbnet already carries a registry record and IS a derived slug —
// internal/extractors/vbnet/ — so the VB.NET work now landing flows through
// this guard rather than around it.)
var registryLanguageExceptions = map[string]string{
	"multi": "cross-cutting build/CI/infra records that apply to every language; " +
		"generate.go excludes language==\"multi\" from the by-language pivot and " +
		"routes them to the cross-cutting section, so this tag ships no matrix row",
}

// registryLanguageRemedy is the failure text for a registry `language` tag
// that is neither a derived slug nor a recorded exception. Like
// unclassifiedSlugRemedy it enumerates every option a contributor may need —
// including the alias/retag option, whose omission from #6351's first draft
// had to be corrected — because a remedy that omits the applicable one gets
// followed faithfully and reproduces the defect the gate exists to catch.
func registryLanguageRemedy(lang string, n int) string {
	return fmt.Sprintf("docs/coverage/registry.json tags %d record(s) with language %q, which is "+
		"neither a slug derived from internal/extractors/ nor an entry in "+
		"registryLanguageExceptions: it ships an unreviewed row into the coverage "+
		"matrix. Pick the option that fits what %q actually is:\n"+
		"  - a DIALECT or ALTERNATE SPELLING of a language already derived "+
		"(javascript/typescript -> jsts, cpp -> c-cpp, golang -> go): RETAG those "+
		"records to the canonical slug. Do NOT add it to registryLanguageExceptions "+
		"— that ships the standalone row extractorDirAliases exists to prevent;\n"+
		"  - a genuinely new LANGUAGE grafel extracts: add internal/extractors/%s/ "+
		"and its languageRoster entry, so the slug is derived rather than excepted;\n"+
		"  - a CROSS-CUTTING or extractor-less tag that must never render a language "+
		"row (the \"multi\" case): add it to registryLanguageExceptions with a "+
		"justification saying why no row is correct — and confirm generate.go "+
		"actually excludes it from the pivot before you do.",
		n, lang, lang, lang)
}

// TestRegistryLanguageTagsAreDerivedSlugsOrJustifiedExceptions binds the
// SECOND source of matrix language rows to the first. Both directions are
// enforced:
//
//	tag in registry.json, neither derived nor excepted -> unreviewed row
//	exception listed but no longer tagged in registry.json -> dead config
//
// The second half is the staleness check #6353 asks for explicitly. Without
// it the list rots the way the 34 dead config entries deleted across #6330
// and #6351 rotted: silently, because a one-directional gate never mentions
// the entries that stopped mattering.
func TestRegistryLanguageTagsAreDerivedSlugsOrJustifiedExceptions(t *testing.T) {
	root := repoRoot(t)

	reg, err := loadRegistry(filepath.Join(root, "docs", "coverage", "registry.json"))
	if err != nil {
		t.Fatalf("load docs/coverage/registry.json: %v", err)
	}
	if len(reg.Records) == 0 {
		t.Fatalf("docs/coverage/registry.json has no records — the guard would " +
			"assert nothing; refusing to pass vacuously")
	}

	derived := map[string]bool{}
	for _, s := range SupportedLanguages(root) {
		derived[s] = true
	}
	if len(derived) == 0 {
		t.Fatalf("SupportedLanguages(%s) derived nothing — the guard would then "+
			"reject every registry tag rather than check it; refusing to run "+
			"against an empty derivation", root)
	}

	// Mechanical enumeration of the distinct tags. Counting matters: a
	// single-record tag is what shipped the javascript defect and what made
	// jcl get missed by eye in #6353's own thread.
	tagged := map[string]int{}
	for _, r := range reg.Records {
		tagged[r.Language]++
	}

	langs := make([]string, 0, len(tagged))
	for l := range tagged {
		langs = append(langs, l)
	}
	sort.Strings(langs)

	for _, l := range langs {
		_, excepted := registryLanguageExceptions[l]
		if derived[l] {
			if excepted {
				t.Errorf("registryLanguageExceptions has entry %q but %q IS a slug "+
					"derived from internal/extractors/ — the exception is dead and "+
					"misleading, remove it", l, l)
			}
			continue
		}
		if excepted {
			continue
		}
		t.Errorf("%s", registryLanguageRemedy(l, tagged[l]))
	}

	// Staleness: an exception that no longer describes anything in the
	// registry is dead config and must be removed, not carried.
	for _, k := range sortedKeysString(registryLanguageExceptions) {
		if tagged[k] == 0 {
			t.Errorf("registryLanguageExceptions has entry %q but no record in "+
				"docs/coverage/registry.json is tagged with that language — dead "+
				"config, remove it", k)
		}
	}

	// Every exception must carry a real justification, not an empty string:
	// the list's whole value is that a reader can tell a decision from a
	// paste.
	for _, k := range sortedKeysString(registryLanguageExceptions) {
		if strings.TrimSpace(registryLanguageExceptions[k]) == "" {
			t.Errorf("registryLanguageExceptions[%q] has an empty justification — "+
				"say why this tag must never render a language row", k)
		}
	}

	// The claim every exception rests on is "this tag ships no matrix row".
	// Assert it against the SHIPPED summary rather than trusting the comment:
	// the moment generate.go stops filtering an excepted tag out of the pivot,
	// the exception silently becomes a licence to publish an unreviewed row —
	// which is the exact permissive-default failure #6353 is about.
	summaryPath := filepath.Join(root, "docs", "coverage", "summary.md")
	summary, err := os.ReadFile(summaryPath)
	if err != nil {
		t.Fatalf("read docs/coverage/summary.md: %v", err)
	}
	if !strings.Contains(string(summary), "](by-language/jsts.md)") {
		t.Fatalf("docs/coverage/summary.md has no by-language/jsts.md row — the " +
			"pivot's shape changed and the row check below would assert nothing; " +
			"refusing to pass vacuously")
	}
	for _, k := range sortedKeysString(registryLanguageExceptions) {
		row := fmt.Sprintf("](by-language/%s.md)", k)
		if strings.Contains(string(summary), row) {
			t.Errorf("registryLanguageExceptions[%q] is justified as %q, but "+
				"docs/coverage/summary.md renders a %s row for it — the exception no "+
				"longer holds. Either generate.go must exclude %q from the "+
				"by-language pivot again, or %q is a real language and must be "+
				"derived from internal/extractors/ instead of excepted.",
				k, registryLanguageExceptions[k], row, k, k)
		}
	}
}
