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
	"os"
	"path/filepath"
	"reflect"
	"sort"
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
// here.
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
	"php":      "Php",
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
			t.Errorf("extractor slug %q is derived from internal/extractors/ but is "+
				"not in languageRoster: it would reach the coverage matrix unreviewed. "+
				"If it is a language, add it to languageRoster (and to "+
				"languageDisplayOverrides if %q is not the label you want). If it is "+
				"a non-language format, add it to extractorNonLanguageFormats. If it "+
				"is a shared utility package, add it to extractorUtilityDirs.",
				s, languageDisplayName(s))
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
// Reintroducing any of those directories is safe: it would fail
// TestSupportedLanguagesMatchesRealTree, which names the table to put it back
// into.
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
