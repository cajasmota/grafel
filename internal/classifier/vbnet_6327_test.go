package classifier

import (
	"context"
	"testing"
)

// Epic #6327 — classification.
//
// S2 shipped these as "recognised but deliberately still skipped": a `.vb` file
// was classified as VB.NET and dropped before extraction, because
// internal/extractors/vbnet did not exist. S5 registered that extractor, so the
// half-done state is over and the assertions below are INVERTED rather than
// deleted — the removal protocol in unsupported.go names exactly these tests as
// the ones that unwind.

// TestVBFileIsRecognisedAsVBNet is the test the epic asks for: a `.vb` file is
// no longer *unclassified*. Both Classify entry points are covered because
// they duplicate the detection ladder rather than sharing it.
func TestVBFileIsRecognisedAsVBNet(t *testing.T) {
	c := New(nil)
	ctx := context.Background()

	for _, path := range []string{
		"legacy/Form1.vb",
		"Services/AccountsSoapClient.vb",
		"UPPER/FORM.VB", // extension matching is case-folded
	} {
		if got := c.Classify(ctx, path).Language; got != "vbnet" {
			t.Fatalf("Classify(%q).Language = %q, want %q", path, got, "vbnet")
		}
		if got := c.ClassifyWithSize(ctx, path, 4096).Language; got != "vbnet" {
			t.Fatalf("ClassifyWithSize(%q).Language = %q, want %q", path, got, "vbnet")
		}
	}
}

// TestVBFileReachesExtraction is the inverted S2 assertion. It was
// TestVBFileIsSkippedNotExtracted, and its own failure message named the
// condition for inverting it: "If S3-S5 have landed, delete the
// languagesAwaitingExtractor entry".
//
// This package cannot import internal/extractors, so it cannot assert that
// entities come out — that half is
// TestLanguagesAwaitingExtractorHaveNoRegisteredExtractor in internal/cli. What
// it CAN assert is the half that starved the extractor: a `.vb` file must reach
// the pipeline at all.
func TestVBFileReachesExtraction(t *testing.T) {
	res := New(nil).Classify(context.Background(), "legacy/Form1.vb")

	if res.Skip {
		t.Fatalf("a .vb file is still skipped although an extractor is registered "+
			"for vbnet (#6327 S5). Classify = %+v — every .vb file would produce "+
			"zero entities, which is #6321 with the extractor already built.", res)
	}
	if res.SkipReason != "" {
		t.Fatalf("SkipReason = %q, want empty", res.SkipReason)
	}
	if res.Tier == 0 {
		t.Fatalf("Tier = %d, want nonzero — tier 0 does not route into extraction", res.Tier)
	}
	if res.Language != "vbnet" {
		t.Fatalf("Language = %q, want %q", res.Language, "vbnet")
	}
}

// TestVBNetLeftTheUnsupportedReport is the other half of the inversion. The
// "Unsupported languages (no extractor)" row #6338 shipped for dcastro-imp's
// 672 files earned its place until the extractor landed; printing it now would
// tell the reporter the opposite of the truth.
func TestVBNetLeftTheUnsupportedReport(t *testing.T) {
	if !SupportedExtension(".vb") {
		t.Fatal("SupportedExtension(\".vb\") = false — the router names it vbnet")
	}
	if got := LanguageDisplayName(".vb"); got != "" {
		t.Fatalf("LanguageDisplayName(\".vb\") = %q, want \"\" — an extension whose "+
			"language has a registered extractor must not name a report row", got)
	}
	if got := TrackingIssue(".vb"); got != "" {
		t.Fatalf("TrackingIssue(\".vb\") = %q, want \"\" — #6327 S5 shipped the extractor", got)
	}

	// Observe only folds in skips, and .vb is no longer skipped, so the tally
	// stays empty however many .vb files it is shown.
	tally := NewUnsupportedTally()
	c := New(nil)
	for i := 0; i < 672; i++ {
		tally.Observe("legacy/Form.vb", c.Classify(context.Background(), "legacy/Form.vb"))
	}
	if got := tally.Counts()[".vb"]; got != 0 {
		t.Fatalf("tally[.vb] = %d, want 0 — .vb is extracted now, not skipped", got)
	}
}

// TestVBNetExcludedExtensions pins the three extensions the epic listed and
// this story deliberately did NOT claim, each with its reason, so a later
// story cannot add them back by symmetry without reading why.
func TestVBNetExcludedExtensions(t *testing.T) {
	c := New(nil)
	ctx := context.Background()

	cases := []struct {
		path   string
		reason string
	}{
		{
			"src/App.vbproj",
			"MSBuild XML project file, not VB source. .csproj is likewise " +
				"absent from the classifier and is claimed by the " +
				"cross/manifest NuGet parser instead; .vbproj belongs there.",
		},
		{
			"legacy/Module1.bas",
			"VB6/VBA standard module. VB.NET has no .bas form. Reported as " +
				"the honestly-vague \"BASIC\" by #6338 instead.",
		},
		{
			"legacy/Class1.cls",
			"VB6/VBA class module, but also Salesforce Apex and LaTeX " +
				"document classes. Claiming it for vbnet would misclassify " +
				"every LaTeX .cls. Needs content sniffing; not in S2.",
		},
	}

	for _, tc := range cases {
		if got := c.Classify(ctx, tc.path).Language; got == "vbnet" {
			t.Errorf("Classify(%q) claimed vbnet, but it is excluded: %s",
				tc.path, tc.reason)
		}
	}

	// .cls in particular must stay unnamed in the #6338 report too — the
	// three-owner ambiguity is why it prints nothing.
	if got := LanguageDisplayName(".cls"); got != "" {
		t.Errorf("LanguageDisplayName(\".cls\") = %q, want \"\" — .cls has "+
			"three plausible owners (Apex / VBA / LaTeX)", got)
	}
	// .bas keeps its deliberately generic name; it must not become "VB.NET".
	if got := LanguageDisplayName(".bas"); got != "BASIC" {
		t.Errorf("LanguageDisplayName(\".bas\") = %q, want \"BASIC\"", got)
	}
}

// TestAwaitingExtractorDoesNotLeakToOtherLanguages guards the skip branch: a
// stray entry takes a working language out of the pipeline silently.
//
// The map is EMPTY after #6327 S5 removed its only entry, so the assertion is
// about vbnet's ABSENCE — the thing that would regress — while the branch
// itself stays covered by the loop below.
func TestAwaitingExtractorDoesNotLeakToOtherLanguages(t *testing.T) {
	if languagesAwaitingExtractor["vbnet"] {
		t.Fatalf("languagesAwaitingExtractor still lists vbnet although "+
			"internal/extractors/vbnet is registered (#6327 S5): every .vb file "+
			"would be skipped before extraction. Map = %v", languagesAwaitingExtractor)
	}

	c := New(nil)
	ctx := context.Background()
	for _, path := range []string{"a.cs", "a.go", "a.fs", "a.razor", "a.py"} {
		res := c.Classify(ctx, path)
		if res.Skip {
			t.Errorf("Classify(%q) = %+v, want Skip=false", path, res)
		}
	}
}

// TestTrackingIssuePositivePath keeps TrackingIssue's lookup non-vacuous.
//
// unsupportedTrackingIssues went EMPTY when #6327 S5 removed its only entry
// (`.vb` -> "#6327"), so the external test in unsupported_6338_test.go can now
// only assert absences — and an all-absent test passes just as happily against
// a function that returns "" unconditionally. This one seeds the map in-package
// and restores it, so the positive branch stays covered until some other
// language is classified ahead of its extractor.
func TestTrackingIssuePositivePath(t *testing.T) {
	if len(unsupportedTrackingIssues) != 0 {
		t.Fatalf("unsupportedTrackingIssues = %v; this test seeds the map because "+
			"it is expected to be empty. If a real entry has landed, assert THAT "+
			"instead of a synthetic one.", unsupportedTrackingIssues)
	}
	unsupportedTrackingIssues[".zzsynthetic"] = "#0000"
	t.Cleanup(func() { delete(unsupportedTrackingIssues, ".zzsynthetic") })

	if got := TrackingIssue(".zzsynthetic"); got != "#0000" {
		t.Errorf("TrackingIssue(.zzsynthetic) = %q, want %q", got, "#0000")
	}
	if got := TrackingIssue(".ZZSYNTHETIC"); got != "#0000" {
		t.Errorf("TrackingIssue must case-fold, got %q", got)
	}
}
