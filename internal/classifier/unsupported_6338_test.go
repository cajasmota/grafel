package classifier_test

import (
	"context"
	"reflect"
	"testing"

	"go.opentelemetry.io/otel/trace/noop"

	"github.com/cajasmota/grafel/internal/classifier"
)

// newTally classifies every path with a real classifier and folds the result
// into an UnsupportedTally, which is exactly what the index walk does.
func tallyOf(t *testing.T, paths ...string) map[string]int {
	t.Helper()
	c := classifier.New(noop.NewTracerProvider().Tracer("test"))
	tal := classifier.NewUnsupportedTally()
	for _, p := range paths {
		tal.Observe(p, c.ClassifyWithSize(context.Background(), p, 10))
	}
	return tal.Counts()
}

// Requirement 1 (issue #6338): a repo containing only supported files must
// report NOTHING. A zero row, or a row for a supported extension, is the
// noise the issue explicitly rules out.
func TestUnsupportedTally_OnlySupportedFiles_ReportsNothing(t *testing.T) {
	got := tallyOf(t,
		"cmd/main.go",
		"internal/a/b.go",
		"web/app.ts",
		"web/app.tsx",
		"svc/handler.py",
		"lib/Thing.java",
	)
	if len(got) != 0 {
		t.Fatalf("supported-only repo must produce an empty tally, got %v", got)
	}
}

// Requirement 2: a mixed repo reports ONLY the unsupported extensions, with
// correct counts, aggregated by extension rather than listed per file.
func TestUnsupportedTally_MixedRepo_ReportsOnlyUnsupportedAggregated(t *testing.T) {
	got := tallyOf(t,
		"cmd/main.go",
		"internal/a/b.go",
		"svc/handler.py",
		"legacy/Form1.vb",
		"legacy/Form2.vb",
		"legacy/sub/Module1.vb",
		"legacy/unit1.pas",
	)
	want := map[string]int{".vb": 3, ".pas": 1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mixed repo tally:\n got  %v\n want %v", got, want)
	}
}

// Requirement 3 — the regression that matters, and the vacuity guard. An
// extension that BECOMES supported must disappear from the tally. `.go` stands
// in for the future state of `.vb`: it is an extension an extractor claims, and
// it must never be tallied no matter how many files carry it.
//
// A tally that simply counted every extension would pass requirement 2's
// ".vb == 3" assertion; it cannot pass this one.
func TestUnsupportedTally_SupportedExtensionNeverTallied(t *testing.T) {
	got := tallyOf(t, "a.go", "b.go", "c.go", "d.go", "e.go")
	if n, ok := got[".go"]; ok {
		t.Fatalf(".go is a supported extension and must never appear in the tally, got count %d", n)
	}
	if !classifier.SupportedExtension(".go") {
		t.Fatal("SupportedExtension(.go) must be true")
	}
	// `.vb` IS routed after #6327 S2, so SupportedExtension — which answers for
	// the router — says true, exactly as it does for the other seven routed
	// languages with no extractor. What keeps the .vb row printing is the tally
	// (Observe only counts skips, and .vb still skips) plus the extractor-derived
	// filter in internal/cli; see the S2-review tests there.
	if !classifier.SupportedExtension(".vb") {
		t.Fatal("SupportedExtension(.vb) must be true — the router names it vbnet (#6327 S2)")
	}
	if got := classifier.LanguageForExtension(".vb"); got != "vbnet" {
		t.Fatalf("LanguageForExtension(.vb) = %q, want vbnet", got)
	}
}

// The tally must distinguish "no extractor claimed this extension" from every
// OTHER skip reason. Vendored, ignored, binary and oversized files are handled
// elsewhere; folding them in here recreates the noise problem the issue is
// about.
func TestUnsupportedTally_OtherSkipReasonsNotCounted(t *testing.T) {
	c := classifier.New(noop.NewTracerProvider().Tracer("test"))

	cases := []struct {
		name string
		path string
		size int64
	}{
		// vendored / universal path skip — the file IS a .go file, so it is
		// dropped for a reason that has nothing to do with extractor coverage.
		{"vendored", "vendor/github.com/x/y/z.go", 10},
		{"git-internal", ".git/hooks/thing.go", 10},
		// binary extension
		{"binary", "assets/logo.png", 10},
		// oversized
		{"too-large", "generated/huge.go", 64 * 1024 * 1024},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cr := c.ClassifyWithSize(context.Background(), tc.path, tc.size)
			if !cr.Skip {
				t.Fatalf("precondition: %q must be skipped, got %+v", tc.path, cr)
			}
			if cr.SkipReason == classifier.SkipReasonUnsupportedLanguage {
				t.Fatalf("precondition: %q must be skipped for a reason OTHER than unsupported_language, got %q", tc.path, cr.SkipReason)
			}
			tal := classifier.NewUnsupportedTally()
			tal.Observe(tc.path, cr)
			if got := tal.Counts(); len(got) != 0 {
				t.Fatalf("skip reason %q must not be tallied as unsupported, got %v", cr.SkipReason, got)
			}
		})
	}
}

// A file the classifier did NOT skip must never be tallied, whatever its
// extension. Without this the tally could be driven off the extension alone.
func TestUnsupportedTally_IndexedFileNotCounted(t *testing.T) {
	tal := classifier.NewUnsupportedTally()
	tal.Observe("a/b.go", classifier.ClassifyResult{Language: "go", Skip: false, Tier: 1})
	if got := tal.Counts(); len(got) != 0 {
		t.Fatalf("a non-skipped file must not be tallied, got %v", got)
	}
}

// Extension-less files and dotfiles have no extension to aggregate on. A
// "(no extension)" bucket would collect every LICENSE, Makefile and .gitignore
// in the corpus — precisely the unusable-signal failure the issue rules out.
//
// Review finding F2: the mixed-case entries are load-bearing. The guard
// lowercased the extension but compared it against a RAW base, so ".gitignore"
// was excluded and ".DS_Store" was not.
func TestUnsupportedTally_NoExtensionAndDotfilesExcluded(t *testing.T) {
	got := tallyOf(t, "LICENSE", "Makefile", "bin/run", ".gitignore", ".editorconfig",
		".DS_Store", ".Rprofile", ".GitIgnore")
	if len(got) != 0 {
		t.Fatalf("extension-less files and dotfiles must not be tallied, got %v", got)
	}
}

// Case folding: .VB and .vb are the same extension and must aggregate into one
// row, not two.
func TestUnsupportedTally_ExtensionCaseFolded(t *testing.T) {
	got := tallyOf(t, "a.vb", "b.VB", "c.Vb")
	want := map[string]int{".vb": 3}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("case-folding:\n got  %v\n want %v", got, want)
	}
}

// Counts() must hand back a copy: a caller mutating the returned map (the
// sidecar builder does exactly this when merging the subprocess path) must not
// corrupt the tally.
func TestUnsupportedTally_CountsReturnsCopy(t *testing.T) {
	tal := classifier.NewUnsupportedTally()
	tal.Observe("a.vb", classifier.ClassifyResult{Skip: true, SkipReason: classifier.SkipReasonUnsupportedLanguage})
	first := tal.Counts()
	first[".vb"] = 999
	first[".injected"] = 1
	second := tal.Counts()
	if !reflect.DeepEqual(second, map[string]int{".vb": 1}) {
		t.Fatalf("Counts() must return a copy, tally was mutated: %v", second)
	}
}

// Merge folds a subprocess-path tally (which arrives as a plain map over IPC)
// into an in-process one, and must apply the same supported-extension filter —
// a stale or hostile map must not be able to inject a supported extension.
func TestUnsupportedTally_MergeFiltersSupported(t *testing.T) {
	tal := classifier.NewUnsupportedTally()
	tal.Observe("a.vb", classifier.ClassifyResult{Skip: true, SkipReason: classifier.SkipReasonUnsupportedLanguage})
	tal.Merge(map[string]int{".vb": 2, ".go": 500, ".PAS": 4})
	want := map[string]int{".vb": 3, ".pas": 4}
	if got := tal.Counts(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Merge:\n got  %v\n want %v", got, want)
	}
}

// The language-name lookup is what makes the row actionable: ".vb" alone is
// not, "VB.NET" is. Extensions we have no name for must render bare rather
// than guess.
func TestLanguageDisplayName(t *testing.T) {
	if got := classifier.LanguageDisplayName(".vb"); got != "VB.NET" {
		t.Fatalf("LanguageDisplayName(.vb) = %q, want %q", got, "VB.NET")
	}
	if got := classifier.LanguageDisplayName(".VB"); got != "VB.NET" {
		t.Fatalf("LanguageDisplayName must case-fold, got %q", got)
	}
	if got := classifier.LanguageDisplayName(".pas"); got != "Pascal" {
		t.Fatalf("LanguageDisplayName(.pas) = %q, want %q", got, "Pascal")
	}
	// An extension nobody has a name for stays bare.
	if got := classifier.LanguageDisplayName(".zzqq"); got != "" {
		t.Fatalf("LanguageDisplayName(.zzqq) = %q, want empty", got)
	}
	// A SUPPORTED extension has no business carrying an "unsupported" name.
	if got := classifier.LanguageDisplayName(".go"); got != "" {
		t.Fatalf("LanguageDisplayName(.go) = %q, want empty (it is supported)", got)
	}
}

// The tracking-issue pointer is what turns "not supported" into "not supported
// yet, follow this". It must be absent for extensions with no tracked work,
// and absent for extensions that are already supported.
func TestTrackingIssue(t *testing.T) {
	if got := classifier.TrackingIssue(".vb"); got != "#6327" {
		t.Fatalf("TrackingIssue(.vb) = %q, want %q", got, "#6327")
	}
	if got := classifier.TrackingIssue(".pas"); got != "" {
		t.Fatalf("TrackingIssue(.pas) = %q, want empty (nothing tracks Pascal)", got)
	}
	if got := classifier.TrackingIssue(".go"); got != "" {
		t.Fatalf("TrackingIssue(.go) = %q, want empty (it is supported)", got)
	}
}
