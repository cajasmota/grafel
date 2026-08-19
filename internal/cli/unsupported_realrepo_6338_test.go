package cli

// #6338 review finding F1 — every fixture in the first cut of this feature was
// a hand-picked list of source files. No package.json, no go.sum, no
// README.txt, no .DS_Store. That is why the suite was green and the feature was
// unusable: driven over a real 31,820-file monorepo it produced 60 rows in
// doctor and 20 above the status floor, and not one of them carried a language
// name — `.json 1938`, `.xml 1531`, `.ktr 1107`, `.txt 149`, `.log 67`,
// `.ds_store 11`, `.1 8`, `.835 6`.
//
// These tests drive the production chain — classifier → tally → UnsupportedRows
// → PrintUnsupportedLanguages — over a REALISTIC repo inventory rather than a
// curated one, which is what the earlier fixtures could not do.

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/classifier"
)

// realisticInventory is what a walker actually hands the classifier: source in
// several languages, and a great deal that is not source at all. Every entry
// here survives the walker's deny-list of binary/media types and reaches
// classification, which is precisely why the report saw them.
func realisticInventory() []string {
	var files []string
	add := func(n int, pattern string) {
		for i := 0; i < n; i++ {
			files = append(files, fmt.Sprintf(pattern, i))
		}
	}
	// Supported source — the bulk of a real repo.
	add(400, "internal/pkg%d/service.go")
	add(120, "web/src/component%d.tsx")
	add(60, "svc/handler%d.py")
	// Config, data, docs, lockfiles, logs, artifacts. NONE of these are
	// languages, and all of them reached the tally before this fix.
	add(120, "config/settings%d.json")
	add(80, "resources/report%d.xml")
	add(40, "docs/notes%d.txt")
	add(30, "data/export%d.csv")
	add(25, "logs/run%d.log")
	add(20, "etc/app%d.properties")
	add(12, "man/page%d.1")
	add(9, "claims/file%d.835")
	files = append(files,
		"package.json", "tsconfig.json", "go.mod", "go.sum", "yarn.lock",
		"README.txt", "LICENSE", "Makefile", "CHANGELOG.md",
		".gitignore", ".editorconfig", ".DS_Store", ".Rprofile",
		"Dockerfile.native", "schema/graph.fbs",
	)
	// The silent gap that matters.
	add(672, "legacy/Form%d.vb")
	add(14, "legacy/unit%d.pas")
	return files
}

// tallyInventory runs the production classification path over paths.
func tallyInventory(t *testing.T, paths []string) map[string]int {
	t.Helper()
	cls := classifier.New(nil)
	tal := classifier.NewUnsupportedTally()
	for _, p := range paths {
		tal.Observe(p, cls.ClassifyWithSize(context.Background(), p, 512))
	}
	return tal.Counts()
}

// F1: on a realistic inventory the report contains ONLY the language rows, and
// every row it does contain names a language.
func TestReport_RealisticInventory_OnlyNamedLanguages(t *testing.T) {
	counts := tallyInventory(t, realisticInventory())

	rows := UnsupportedRows(counts, DoctorUnsupportedMinFiles)
	if len(rows) != 2 {
		t.Fatalf("want exactly 2 rows (.vb, .pas), got %d: %+v", len(rows), rows)
	}
	for _, r := range rows {
		if r.Language == "" {
			t.Fatalf("row %q carries no language name — this is the F1 defect", r.Ext)
		}
	}
	if rows[0].Ext != ".vb" || rows[0].Count != 672 {
		t.Fatalf("headline row = %+v, want .vb 672", rows[0])
	}
	if rows[1].Ext != ".pas" || rows[1].Count != 14 {
		t.Fatalf("second row = %+v, want .pas 14", rows[1])
	}

	// Named, exhaustively, so this cannot pass by counting rows alone.
	for _, junk := range []string{
		".json", ".xml", ".txt", ".csv", ".log", ".properties", ".1", ".835",
		".mod", ".sum", ".lock", ".md", ".ds_store", ".rprofile", ".native", ".fbs",
	} {
		if n, ok := counts[junk]; ok {
			t.Errorf("%s is not a language and must not be tallied (got %d)", junk, n)
		}
	}
}

// The rendered block a user actually reads. Asserted whole: a substring check
// would not have caught 58 extra rows above the one that matters.
func TestReport_RealisticInventory_RenderedOutput(t *testing.T) {
	counts := tallyInventory(t, realisticInventory())
	var buf bytes.Buffer
	PrintUnsupportedLanguages(&buf, "  ", UnsupportedRows(counts, DoctorUnsupportedMinFiles))

	want := "  Unsupported languages (no extractor):\n" +
		"    .vb   672 files  (VB.NET — not supported, see #6327)\n" +
		"    .pas   14 files  (Pascal — not supported)\n"
	if buf.String() != want {
		t.Fatalf("rendered report:\n--- got ---\n%s\n--- want ---\n%s", buf.String(), want)
	}
}

// F2: the dotfile guard compared a lowercased extension against a raw base, so
// it excluded ".gitignore" and let ".DS_Store" through. Both cases, both cases.
func TestReport_DotfilesExcludedRegardlessOfCase(t *testing.T) {
	cls := classifier.New(nil)
	for _, f := range []string{
		".gitignore", ".editorconfig", ".rprofile", ".bashrc",
		".DS_Store", ".Rprofile", ".BASHRC", ".GitIgnore",
	} {
		if got := classifier.ReportableExtensionForTest(f); got != "" {
			t.Errorf("dotfile %q yielded extension %q, want none", f, got)
		}
		tal := classifier.NewUnsupportedTally()
		tal.Observe(f, cls.ClassifyWithSize(context.Background(), f, 10))
		if n := len(tal.Counts()); n != 0 {
			t.Errorf("dotfile %q was tallied: %v", f, tal.Counts())
		}
	}
}

// F3: a trailing dot-segment that is not an extension (manpage.1, claim.835,
// Dockerfile.native) must not become a row. The allow-list makes this
// structural rather than a special case.
func TestReport_NonExtensionSegmentsNotReported(t *testing.T) {
	counts := tallyInventory(t, []string{
		"man/grafel.1", "claims/x.835", "build/Dockerfile.native",
		"a.zzq", "b.qqz",
	})
	if len(counts) != 0 {
		t.Fatalf("non-language dot-segments must not be tallied, got %v", counts)
	}
}

// F1 addendum: the row cap. Sixty rows is unreadable however clean each row is.
func TestReport_CapsRowsWithRemainderSummary(t *testing.T) {
	counts := map[string]int{}
	for _, ext := range []string{
		".vb", ".pas", ".f90", ".ada", ".jl", ".tcl", ".hx", ".coffee",
		".abap", ".rpg", ".ps1", ".bat",
	} {
		counts[ext] = 100
	}
	counts[".vb"] = 9000 // the headline must survive the cap

	var buf bytes.Buffer
	rows := UnsupportedRows(counts, DoctorUnsupportedMinFiles)
	PrintUnsupportedLanguages(&buf, "  ", rows)
	out := buf.String()

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	// header + UnsupportedMaxRows + one remainder line
	if len(lines) != 1+UnsupportedMaxRows+1 {
		t.Fatalf("want %d lines (header + %d rows + remainder), got %d:\n%s",
			1+UnsupportedMaxRows+1, UnsupportedMaxRows, len(lines), out)
	}
	if !strings.Contains(out, "9,000 files") {
		t.Fatalf("the headline row must survive the cap:\n%s", out)
	}
	if !strings.Contains(out, "… and 4 more (400 files)") {
		t.Fatalf("the remainder must be summarised, not dropped:\n%s", out)
	}
}

// F5: the report's self-correction guarantee must not depend on HOW an
// extractor is wired up. SupportedExtension asks detectLanguage, which is the
// router of record, and this invariant makes a landed extractor force the
// registry entry out — so a stale `.vb` row cannot survive #6327 by any route.
func TestUnsupportedLanguageRegistryHasNoSupportedExtension(t *testing.T) {
	for _, ext := range classifier.UnsupportedLanguageExtensionsForTest() {
		if classifier.SupportedExtension(ext) {
			t.Errorf("%s is now a SUPPORTED extension but is still listed as an "+
				"unsupported language — remove its entry from unsupportedLanguageNames "+
				"in internal/classifier/unsupported.go so the report stops printing "+
				"the row (#6338)", ext)
		}
		if classifier.LanguageDisplayName(ext) == "" {
			t.Errorf("%s is in the registry but yields no display name", ext)
		}
	}
}

// F6 as reported did not reproduce, but the property is worth pinning: both
// walk paths pass a real size, so an oversized file is `too_large` on both and
// is never counted as an unsupported language on either.
func TestReport_OversizedFileNotCountedOnEitherPath(t *testing.T) {
	cls := classifier.New(nil)
	const huge = 64 * 1024 * 1024
	cr := cls.ClassifyWithSize(context.Background(), "legacy/Big.vb", huge)
	if cr.SkipReason != "too_large" {
		t.Fatalf("oversized .vb: SkipReason = %q, want too_large", cr.SkipReason)
	}
	tal := classifier.NewUnsupportedTally()
	tal.Observe("legacy/Big.vb", cr)
	if n := len(tal.Counts()); n != 0 {
		t.Fatalf("oversized file must not be tallied, got %v", tal.Counts())
	}
}
