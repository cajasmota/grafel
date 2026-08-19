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
// internal/extractors/ tree is expected to derive to the human label the
// coverage matrix renders for it. Both directions are enforced:
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
	for _, s := range SupportedLanguages(repoRoot(t)) {
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

// TestRealTreeExtractorDirForSlugResolves asserts the on-disk citation each
// placeholder by-language page prints actually exists. extractorDirForSlug has
// its own hand-written switch, so an alias added without a matching case makes
// the generated page cite a directory that is not there.
func TestRealTreeExtractorDirForSlugResolves(t *testing.T) {
	root := repoRoot(t)
	for _, s := range SupportedLanguages(root) {
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
