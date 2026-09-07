package classifier

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// #6985 — an oversized file is dropped from the graph; these pin the
// user-visible report that names it.
//
// Every assertion below observes the EMITTED REPORT (the artefact a user
// reads), never a counter the classifier keeps about itself. The tests share
// package-level state (the dedup map and the output writer), so none of them
// calls t.Parallel.

// oversizedBytes is comfortably over the gate; the exact value is asserted in
// the report line, so it doubles as the "the size is named" fixture.
const oversizedBytes = maxIndexableBytes + 1024

// captureOversizedReport runs fn with the report redirected and returns
// everything it wrote.
func captureOversizedReport(t *testing.T, fn func(c *Classifier)) string {
	t.Helper()
	var buf bytes.Buffer
	restore := setOversizedReportOutput(&buf)
	defer restore()
	fn(New(nil))
	return buf.String()
}

// The report names the file, its size and the limit — not just "a file was
// skipped".
func TestOversizedSkipReportNamesTheFile(t *testing.T) {
	var res ClassifyResult
	out := captureOversizedReport(t, func(c *Classifier) {
		res = c.ClassifyWithSize(context.Background(), "packages/next/src/compiled/webpack/bundle5.js", oversizedBytes)
	})

	if !strings.Contains(out, "packages/next/src/compiled/webpack/bundle5.js") {
		t.Errorf("report does not name the oversized file; got %q", out)
	}
	if !strings.Contains(out, fmt.Sprintf("%d bytes", oversizedBytes)) {
		t.Errorf("report does not carry the file's size %d; got %q", oversizedBytes, out)
	}
	if !strings.Contains(out, fmt.Sprintf("%d-byte limit", maxIndexableBytes)) {
		t.Errorf("report does not carry the limit %d; got %q", maxIndexableBytes, out)
	}
	if !strings.Contains(out, "NOT indexed") {
		t.Errorf("report does not state the consequence; got %q", out)
	}
	if !res.Skip || res.SkipReason != "too_large" {
		t.Errorf("classification changed: got Skip=%v reason=%q, want true/too_large", res.Skip, res.SkipReason)
	}
}

// A file exactly at the limit is indexed and must produce no report at all —
// otherwise the report fires for files that were never dropped.
func TestOversizedReportSilentAtAndUnderTheLimit(t *testing.T) {
	var atLimit, underLimit ClassifyResult
	out := captureOversizedReport(t, func(c *Classifier) {
		atLimit = c.ClassifyWithSize(context.Background(), "src/at_limit.js", maxIndexableBytes)
		underLimit = c.ClassifyWithSize(context.Background(), "src/under_limit.js", 1024)
	})

	if out != "" {
		t.Errorf("report fired for a file that was not dropped for size: %q", out)
	}
	if atLimit.Skip || underLimit.Skip {
		t.Fatalf("fixture degenerate: at-limit skip=%v under-limit skip=%v, want both indexed",
			atLimit.Skip, underLimit.Skip)
	}
}

// Two DISTINCT oversized files are two lines. The dedup map must key on the
// path; a map that suppresses the second file reports a repo of bundles as one
// bundle.
func TestOversizedReportNamesEachDistinctFile(t *testing.T) {
	out := captureOversizedReport(t, func(c *Classifier) {
		c.ClassifyWithSize(context.Background(), "a/first_bundle.js", oversizedBytes)
		c.ClassifyWithSize(context.Background(), "b/second_bundle.js", oversizedBytes)
	})

	for _, want := range []string{"a/first_bundle.js", "b/second_bundle.js"} {
		if !strings.Contains(out, want) {
			t.Errorf("report omits distinct oversized file %q; got %q", want, out)
		}
	}
}

// The same path classified twice is one line — but BOTH calls still return a
// skip, so the `skipped=N` aggregate keeps counting every skip and a named
// file is not double-counted or dropped from the count.
func TestOversizedReportDedupsRepeatsWithoutChangingTheSkip(t *testing.T) {
	var first, second ClassifyResult
	out := captureOversizedReport(t, func(c *Classifier) {
		first = c.ClassifyWithSize(context.Background(), "a/bundle.js", oversizedBytes)
		second = c.ClassifyWithSize(context.Background(), "a/bundle.js", oversizedBytes)
	})

	if got := strings.Count(out, "a/bundle.js"); got != 1 {
		t.Errorf("same path reported %d times, want 1; got %q", got, out)
	}
	if !first.Skip || !second.Skip {
		t.Errorf("a repeated classification stopped being a skip: first=%v second=%v", first.Skip, second.Skip)
	}
	if first.SkipReason != "too_large" || second.SkipReason != "too_large" {
		t.Errorf("skip reason changed: %q / %q", first.SkipReason, second.SkipReason)
	}
}

// The output is bounded. maxOversizedReports files are named, the next is not,
// and the suppression line says so.
func TestOversizedReportIsCapped(t *testing.T) {
	total := maxOversizedReports + 1
	out := captureOversizedReport(t, func(c *Classifier) {
		for i := range total {
			c.ClassifyWithSize(context.Background(), fmt.Sprintf("vendor/bundle_%d.js", i), oversizedBytes)
		}
	})

	for i := range maxOversizedReports {
		want := fmt.Sprintf("vendor/bundle_%d.js", i)
		if !strings.Contains(out, want) {
			t.Errorf("file %q under the cap was not reported; got %q", want, out)
		}
	}
	beyond := fmt.Sprintf("vendor/bundle_%d.js", maxOversizedReports)
	if strings.Contains(out, beyond) {
		t.Errorf("file %q past the cap was reported; got %q", beyond, out)
	}
	if !strings.Contains(out, fmt.Sprintf("suppressed after %d", maxOversizedReports)) {
		t.Errorf("no suppression line once the cap was reached; got %q", out)
	}
}

// The UnsupportedTally must count exactly what it counted before #6985.
//
// `.pas` is in unsupportedLanguageNames, so an UNDER-limit .pas file is the
// positive control that the tally still works at all; the same .pas file over
// the limit is skipped as too_large and must not appear, and neither must an
// oversized recognised-language file.
func TestOversizedSkipLeavesUnsupportedTallyUnchanged(t *testing.T) {
	c := New(nil)
	restore := setOversizedReportOutput(&bytes.Buffer{})
	defer restore()

	tally := NewUnsupportedTally()
	observe := func(p string, size int64) {
		tally.Observe(p, c.ClassifyWithSize(context.Background(), p, size))
	}

	observe("legacy/Report.pas", 4096)
	control := tally.Counts()
	if !reflect.DeepEqual(control, map[string]int{".pas": 1}) {
		t.Fatalf("fixture degenerate: control tally is %v, want {.pas:1}", control)
	}

	observe("legacy/Huge.pas", oversizedBytes)
	observe("vendor/bundle.js", oversizedBytes)
	observe("assets/data.json", oversizedBytes)

	if got := tally.Counts(); !reflect.DeepEqual(got, control) {
		t.Errorf("oversized files changed the unsupported-extension report: got %v, want %v", got, control)
	}
}

// The oversized skip is still classified as too_large and never re-labelled as
// an unsupported-language skip — the report is additive to the classification,
// not a re-routing of it.
func TestOversizedSkipReasonIsUnchangedForAnUnsupportedLanguageExtension(t *testing.T) {
	c := New(nil)
	restore := setOversizedReportOutput(&bytes.Buffer{})
	defer restore()

	res := c.ClassifyWithSize(context.Background(), "legacy/Huge.pas", oversizedBytes)
	if res.SkipReason != "too_large" {
		t.Errorf("oversized .pas skip reason is %q, want too_large", res.SkipReason)
	}
	if res.Language != "" {
		t.Errorf("oversized skip now carries a language %q; the size gate runs before language detection", res.Language)
	}
}
