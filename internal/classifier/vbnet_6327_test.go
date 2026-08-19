package classifier

import (
	"context"
	"testing"
)

// Epic #6327, story S2 — classification only.
//
// WHAT S2 DOES AND DOES NOT DO. After these tests pass, a `.vb` file is
// RECOGNISED as VB.NET and still produces ZERO entities: there is no
// internal/extractors/vbnet/ directory, no lexer and no parser. S3–S5 build
// those. Every assertion below is written so that reading it cannot leave the
// impression that VB.NET support exists.

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

// TestVBFileIsSkippedNotExtracted asserts what it can actually observe: a `.vb`
// file is dropped at classification and never enters the pipeline.
//
// It was called TestVBFileStillYieldsNoEntities, which is the DESIGN statement
// for S2 and the BUG for S4 — the same green test means "correct, no extractor
// yet" before S4 and "the extractor exists and is being starved" after it, and
// nothing in the name tells an S4 author which one they are looking at. The
// half-done state is observable, but only from internal/cli, which can import
// internal/extractors; TestLanguagesAwaitingExtractorHaveNoRegisteredExtractor
// there is the paired assertion, and it is the one that will fail on the S4
// commit. This one keeps its narrower job: the skip is a skip, and it does not
// throw away the language.
func TestVBFileIsSkippedNotExtracted(t *testing.T) {
	res := New(nil).Classify(context.Background(), "legacy/Form1.vb")

	if !res.Skip {
		t.Fatalf("a .vb file must still be skipped after S2 — no extractor "+
			"exists for vbnet. Classify = %+v. If S3-S5 have landed, delete "+
			"the languagesAwaitingExtractor entry and replace this test with "+
			"an entity-count assertion.", res)
	}
	if res.SkipReason != SkipReasonUnsupportedLanguage {
		t.Fatalf("SkipReason = %q, want %q so the #6338 report keeps its row",
			res.SkipReason, SkipReasonUnsupportedLanguage)
	}
	if res.Tier != 0 {
		t.Fatalf("Tier = %d, want 0 (skip) — a nonzero tier would route the "+
			"file into extraction", res.Tier)
	}
	// The two facts together, stated as one: known language, no extractor.
	if res.Language != "vbnet" {
		t.Fatalf("Language = %q, want %q — the skip must not throw away what "+
			"we now know the file is", res.Language, "vbnet")
	}
}

// TestVBNetRemainsInUnsupportedReport pins the #6338 interaction.
//
// Classifying `.vb` must NOT silence the "Unsupported languages (no
// extractor)" row that #6338 shipped for dcastro-imp's 672 files. The row is
// what tells them nothing is being extracted; it earns its place until the
// extractor does.
func TestVBNetRemainsInUnsupportedReport(t *testing.T) {
	// SupportedExtension answers for the ROUTER, so after S2 it says true for
	// `.vb` just as it does for `.proto`, `.prisma` and `.toml` — all routed,
	// none extractable. What keeps the row alive is that the display name and
	// the tracking issue survive, and that the tally still counts the skip; the
	// "is it extractable" half is derived from extractors.Get in internal/cli,
	// the only package that can ask (#6327 S2 review).
	if !SupportedExtension(".vb") {
		t.Fatal("SupportedExtension(\".vb\") = false — the router names it vbnet after S2")
	}
	if got := LanguageDisplayName(".vb"); got != "VB.NET" {
		t.Fatalf("LanguageDisplayName(\".vb\") = %q, want %q", got, "VB.NET")
	}
	if got := TrackingIssue(".vb"); got != "#6327" {
		t.Fatalf("TrackingIssue(\".vb\") = %q, want %q", got, "#6327")
	}

	// And the tally still counts it — Observe only folds in skips.
	tally := NewUnsupportedTally()
	c := New(nil)
	for i := 0; i < 672; i++ {
		tally.Observe("legacy/Form.vb", c.Classify(context.Background(), "legacy/Form.vb"))
	}
	if got := tally.Counts()[".vb"]; got != 672 {
		t.Fatalf("tally[.vb] = %d, want 672 — the report row must survive S2", got)
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

// TestAwaitingExtractorDoesNotLeakToOtherLanguages guards the new skip branch:
// it must fire for vbnet and for nothing else. A stray entry here would take a
// working language out of the pipeline silently.
func TestAwaitingExtractorDoesNotLeakToOtherLanguages(t *testing.T) {
	if len(languagesAwaitingExtractor) != 1 || !languagesAwaitingExtractor["vbnet"] {
		t.Fatalf("languagesAwaitingExtractor = %v; adding a language here "+
			"REMOVES it from extraction. Read the map's comment before "+
			"changing this expectation.", languagesAwaitingExtractor)
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
