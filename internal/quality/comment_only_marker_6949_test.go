package quality

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/engine"
)

// A golden fixture's own COMMENT can satisfy a framework gate, and it makes
// every forbidden row in that file silently unfireable.
//
// Found while writing #6927's docker/ansible arm (PR #6949). Four
// gate-direction fixture files were first written with headers explaining what
// they do NOT contain — "declares no `services:` key", "no `become:`, no
// `gather_facts`, no `.j2`" — and `compiledRuleSet.frameworkPresent`
// (detector.go:96) is `strings.Contains` over WHOLE FILE CONTENT, evaluated
// once per (rule set, file) at :402, BEFORE the per-pattern
// `requiresFramework` skip at :409. So each of those headers admitted its own
// file to the very rule set it was written to be declined by, and all four
// gate rows passed for the wrong reason. Nothing failed; the rows were simply
// unreachable. It was caught only by dumping what Detect actually emitted per
// file before writing expected.json.
//
// That is a whole class of silently-unfireable negative control, so it is a
// test rather than a lesson. The scan is deliberately broader than the
// incident: it does not care WHY the marker is on a comment line.
//
// Three limits, stated rather than implied:
//
//  1. It sees COMMENTS only. The same hazard exists for a string literal, a
//     docstring or a heredoc naming a marker, and this test is blind to all
//     three. `strings.Contains` does not care where the bytes are.
//  2. commentPrefixes6949 is a hand-written extension map. A fixture in a
//     language it does not list is skipped, which is a false NEGATIVE — the
//     failure mode is silence, so the map is asserted to cover the corpus
//     below rather than trusted.
//  3. It reports a marker whose occurrences are ALL on comment lines. A file
//     carrying the marker in real code as well is genuinely admitted, and this
//     test has no opinion on it.
func TestGoldenFixtureMarkersAreNotCommentOnly_6949(t *testing.T) {
	rules, err := engine.LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	// One marker string may be declared by several rule sets; the hazard is a
	// property of the STRING, so they are deduplicated and the owners kept for
	// the failure message.
	owners := map[string]map[string]bool{}
	for _, frs := range rules {
		for _, fr := range frs {
			for _, m := range fr.Frameworks.Detection.ImportMarkers {
				if m == "" {
					continue
				}
				if owners[m] == nil {
					owners[m] = map[string]bool{}
				}
				owners[m][fr.Frameworks.Name] = true
			}
		}
	}
	if len(owners) < 200 {
		t.Fatalf("only %d distinct import_markers were loaded; this scan grades nothing at "+
			"that size, so rule loading is what broke", len(owners))
	}

	root := filepath.Join("golden")
	var files []string
	if err := filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return err
		}
		// Only fixture SOURCE is offered to the detector; expected.json is not.
		if strings.Contains(filepath.ToSlash(p), "/src/") {
			files = append(files, p)
		}
		return nil
	}); err != nil {
		t.Fatalf("walk golden/: %v", err)
	}
	if len(files) < 100 {
		t.Fatalf("found %d fixture source files, which is too few for this scan to be "+
			"measuring the corpus", len(files))
	}

	var skipped []string
	var hits []string
	for _, p := range files {
		prefixes, known := commentPrefixes6949[strings.ToLower(filepath.Ext(p))]
		if !known {
			// Extensionless and build-file names (Gemfile, Makefile, …) are
			// keyed by basename instead.
			prefixes, known = commentPrefixes6949[strings.ToLower(filepath.Base(p))]
		}
		if !known {
			skipped = append(skipped, p)
			continue
		}
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		content := string(b)
		// Split once; the marker loop is 1000+ iterations per file.
		var codeLines, commentLines []string
		for _, line := range strings.Split(content, "\n") {
			trimmed := strings.TrimLeft(line, " \t")
			isComment := false
			for _, pre := range prefixes {
				if strings.HasPrefix(trimmed, pre) {
					isComment = true
					break
				}
			}
			if isComment {
				commentLines = append(commentLines, line)
			} else {
				codeLines = append(codeLines, line)
			}
		}
		code := strings.Join(codeLines, "\n")
		comments := strings.Join(commentLines, "\n")
		for m := range owners {
			if !strings.Contains(comments, m) || strings.Contains(code, m) {
				continue
			}
			var names []string
			for n := range owners[m] {
				names = append(names, n)
			}
			sort.Strings(names)
			key := fmt.Sprintf("%s :: %q (declared by %s)", filepath.ToSlash(p), m,
				strings.Join(names, ", "))
			if allowedCommentOnlyMarkers6949[fmt.Sprintf("%s|%s", filepath.ToSlash(p), m)] {
				continue
			}
			hits = append(hits, key)
		}
	}
	sort.Strings(hits)
	for _, h := range hits {
		t.Errorf("a fixture's COMMENT carries a framework import_marker and its code does not:\n"+
			"    %s\n"+
			"  frameworkPresent is strings.Contains over whole file content, so this comment "+
			"admits the file to that rule set. If the file is a negative control for that "+
			"framework, every forbidden row it holds is unfireable — rewrite the comment so it "+
			"does not spell the marker. If the admission is intended, add the pair to "+
			"allowedCommentOnlyMarkers6949 with the reason.", h)
	}
	// A map that stopped covering the corpus turns this test into silence, so
	// the coverage is asserted rather than assumed (limit 2 above).
	if len(skipped) > 0 {
		sort.Strings(skipped)
		t.Errorf("%d fixture source files have an extension commentPrefixes6949 does not know, "+
			"so they were NOT scanned (e.g. %s). Add the extension's comment prefixes; a "+
			"silently unscanned language is the failure mode this test exists to prevent",
			len(skipped), strings.Join(skipped[:min6949(3, len(skipped))], ", "))
	}
}

func min6949(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// commentPrefixes6949 maps a fixture source extension to the line-comment
// openers of that language. Block comments are NOT modelled: a marker inside
// `/* ... */` reads as code here, which is a false negative in the same
// direction as limit 1 above.
var commentPrefixes6949 = map[string][]string{
	".c": {"//"}, ".cc": {"//"}, ".clj": {";"}, ".cs": {"//"}, ".cpp": {"//"},
	".dart": {"//"}, ".ex": {"#"}, ".exs": {"#"}, ".erl": {"%"}, ".fs": {"//"},
	".go": {"//"}, ".graphql": {"#"}, ".groovy": {"//"}, ".h": {"//"},
	".hrl": {"%"}, ".hs": {"--"}, ".html": {"<!--"}, ".java": {"//"},
	".js": {"//"}, ".json": {}, ".jsx": {"//"}, ".kt": {"//"}, ".lisp": {";"},
	".lua": {"--"}, ".md": {}, ".ml": {}, ".mli": {}, ".nim": {"#"},
	".php": {"//", "#"}, ".pony": {"//"}, ".proto": {"//"}, ".py": {"#"},
	".rb": {"#"}, ".rs": {"//"}, ".sbt": {"//"}, ".scala": {"//"},
	".sol": {"//"}, ".sql": {"--"}, ".swift": {"//"}, ".toml": {"#"},
	".ts": {"//"}, ".tsx": {"//"}, ".txt": {}, ".vb": {"'"}, ".xml": {"<!--"},
	".yaml": {"#"}, ".yml": {"#"}, ".cfg": {"#"}, ".conf": {"#"},
	".gradle": {"//"}, ".properties": {"#"}, ".sh": {"#"}, ".j2": {"#"},
	".mod": {"//"}, ".sum": {}, ".lock": {"#"}, ".csproj": {"<!--"},
	".gitignore": {"#"}, ".env": {"#"}, ".ini": {";", "#"}, ".css": {},
	".scss": {"//"}, ".vue": {"//"}, ".svelte": {"//"}, ".tf": {"#", "//"},
	".edn": {";"}, ".cljs": {";"}, ".pl": {"#"}, ".pm": {"#"}, ".r": {"#"},
	".jl": {"#"}, ".zig": {"//"}, ".v": {"//"}, ".d": {"//"}, ".m": {"//"},
	".mm": {"//"}, ".f90": {"!"}, ".pas": {"//"}, ".ada": {"--"},
	".adb": {"--"}, ".ads": {"--"}, ".cob": {"*"}, ".asm": {";"},
	".ru": {"#"},
	// Play / Revel routes DSL (#6952). `conf/routes` is extensionless and
	// `conf/<module>.routes` carries the `.routes` suffix; both comment with
	// `#`, and a commented-out route is the most ordinary line in one — which
	// is exactly the hazard this scan exists for. Listed in BOTH tables because
	// the two spellings are routed by two different classifier entries.
	".routes": {"#"},
	// Blazor components (#6370). TWO prefixes because a .razor file is two
	// languages stacked: the markup half comments with `@*` … `*@`, and the
	// `@code { }` half is C# and comments with `//`. Listing only one leaves
	// half of every razor fixture graded as code.
	".razor": {"@*", "//"},
	// Keyed by basename, for sources with no extension.
	"gemfile": {"#"}, "rakefile": {"#"}, "makefile": {"#"}, "dockerfile": {"#"},
	"procfile": {"#"},
	"routes":   {"#"},
	// scala-play-mini's negative fixture: a backup of a routes file, byte-for-
	// byte the same DSL. Keyed by basename rather than by a `.bak` EXTENSION
	// entry, because `.bak` names no language and a global entry for it would
	// claim every backup in the corpus comments the same way.
	"routes.bak": {"#"},
}

// allowedCommentOnlyMarkers6949 records "<repo-relative path>|<marker>" pairs
// that are known and harmless. Each needs a reason, because an entry here is a
// negative control someone decided not to have.
var allowedCommentOnlyMarkers6949 = map[string]bool{
	// `stub` is one of lua/frameworks/test_patterns.yaml's markers, and this is
	// a C# file: Detect resolves rule sets by file.Language, so the lua set is
	// never offered this file and the admission cannot happen. Cross-language,
	// therefore inert — but recorded rather than filtered out by a rule, so
	// that a same-language instance still fails.
	"golden/csharp-aspnet-core-mini/src/Services/UserService.cs|stub": true,
}
