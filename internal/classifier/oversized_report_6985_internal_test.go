package classifier

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// #6985 — an oversized file is dropped from the graph; these pin the
// user-visible report that names it.
//
// Every assertion below observes the EMITTED REPORT (the artefact a user
// reads), never a counter the classifier keeps about itself. The report state
// is per-Classifier, so each test constructs its own and there is no shared
// state to reset — a fresh Classifier IS the starting state.

// oversizedBytes is comfortably over the gate; the exact value is asserted in
// the report line, so it doubles as the "the size is named" fixture.
const oversizedBytes = maxIndexableBytes + 1024

// newReportingClassifier returns a classifier whose report is captured.
func newReportingClassifier(w io.Writer) *Classifier {
	c := New(nil)
	setOversizedReportOutput(c, w)
	return c
}

// The report names the file, its size and the limit — not just "a file was
// skipped".
func TestOversizedSkipReportNamesTheFile(t *testing.T) {
	var buf bytes.Buffer
	c := newReportingClassifier(&buf)

	res := c.ClassifyWithSize(context.Background(), "packages/next/src/compiled/webpack/bundle5.js", oversizedBytes)

	out := buf.String()
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
	var buf bytes.Buffer
	c := newReportingClassifier(&buf)

	atLimit := c.ClassifyWithSize(context.Background(), "src/at_limit.js", maxIndexableBytes)
	underLimit := c.ClassifyWithSize(context.Background(), "src/under_limit.js", 1024)
	c.ReportOversizedSummary()

	if out := buf.String(); out != "" {
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
	var buf bytes.Buffer
	c := newReportingClassifier(&buf)

	c.ClassifyWithSize(context.Background(), "a/first_bundle.js", oversizedBytes)
	c.ClassifyWithSize(context.Background(), "b/second_bundle.js", oversizedBytes)

	out := buf.String()
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
	var buf bytes.Buffer
	c := newReportingClassifier(&buf)

	first := c.ClassifyWithSize(context.Background(), "a/bundle.js", oversizedBytes)
	second := c.ClassifyWithSize(context.Background(), "a/bundle.js", oversizedBytes)

	out := buf.String()
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

// The output is bounded: maxOversizedReports files are named, the next is not,
// the "not named" notice appears EXACTLY ONCE (printing it per suppressed file
// makes the output unbounded again, which is the whole point of the cap), and
// the total line count is exactly the cap plus that one notice.
func TestOversizedReportIsCapped(t *testing.T) {
	var buf bytes.Buffer
	c := newReportingClassifier(&buf)
	if buf.Len() != 0 {
		t.Fatalf("fixture degenerate: a fresh classifier already emitted %q", buf.String())
	}

	total := maxOversizedReports + 1
	for i := range total {
		c.ClassifyWithSize(context.Background(), fmt.Sprintf("vendor/bundle_%d.js", i), oversizedBytes)
	}

	out := buf.String()
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
	if got := strings.Count(out, "further oversized-file skips are not named"); got != 1 {
		t.Errorf("the cap notice was printed %d times, want exactly 1; got %q", got, out)
	}
	if got := strings.Count(out, "grafel:"); got != maxOversizedReports+1 {
		t.Errorf("emitted %d lines, want %d (%d named + 1 notice); got %q",
			got, maxOversizedReports+1, maxOversizedReports, out)
	}
}

// The end-of-index summary says HOW MUCH the cap hid: "8 of 40" must not read
// like "8 of 9".
func TestOversizedSummaryCountsWhatTheCapHid(t *testing.T) {
	var buf bytes.Buffer
	c := newReportingClassifier(&buf)

	const total = 40
	for i := range total {
		c.ClassifyWithSize(context.Background(), fmt.Sprintf("vendor/bundle_%d.js", i), oversizedBytes)
	}
	beforeSummary := buf.String()
	c.ReportOversizedSummary()
	summary := strings.TrimPrefix(buf.String(), beforeSummary)

	hidden := total - maxOversizedReports
	for _, want := range []string{
		fmt.Sprintf("%d further oversized-file skips were not named", hidden),
		fmt.Sprintf("%d named", maxOversizedReports),
		fmt.Sprintf("%d over the %d-byte limit in total", total, maxIndexableBytes),
	} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary omits %q; got %q", want, summary)
		}
	}
	if got := strings.Count(summary, "grafel:"); got != 1 {
		t.Errorf("summary emitted %d lines, want exactly 1; got %q", got, summary)
	}
}

// Nothing was hidden → no summary line. A "0 further" row is the zero row the
// rest of this package's reporting refuses to print.
func TestOversizedSummarySilentWhenNothingWasSuppressed(t *testing.T) {
	var buf bytes.Buffer
	c := newReportingClassifier(&buf)

	c.ClassifyWithSize(context.Background(), "vendor/only_bundle.js", oversizedBytes)
	named := buf.String()
	c.ReportOversizedSummary()

	if got := strings.TrimPrefix(buf.String(), named); got != "" {
		t.Errorf("summary printed with nothing suppressed: %q", got)
	}
	if !strings.Contains(named, "vendor/only_bundle.js") {
		t.Fatalf("fixture degenerate: the one oversized file was not named; got %q", named)
	}
}

// THE LIFETIME. A second index in the same process must report again — even
// after a first index saturated the cap, and even for a repo whose paths were
// never seen. The first cut of this file kept the state process-global, so the
// second run printed NOTHING AT ALL: a user who fixed nothing and re-ran saw a
// clean report.
//
// One writer across both passes, and no reset between them, because production
// has no reset to call.
func TestOversizedReportIsPerIndexNotPerProcess(t *testing.T) {
	var buf bytes.Buffer

	firstIndex := newReportingClassifier(&buf)
	for i := range maxOversizedReports {
		firstIndex.ClassifyWithSize(context.Background(), fmt.Sprintf("repo-a/bundle_%d.js", i), oversizedBytes)
	}
	firstIndex.ReportOversizedSummary()
	firstOut := buf.String()
	if !strings.Contains(firstOut, "repo-a/bundle_0.js") {
		t.Fatalf("fixture degenerate: the first index reported nothing; got %q", firstOut)
	}

	secondIndex := newReportingClassifier(&buf)
	secondIndex.ClassifyWithSize(context.Background(), "repo-b/other_bundle.js", oversizedBytes)
	secondIndex.ClassifyWithSize(context.Background(), "repo-a/bundle_0.js", oversizedBytes)
	secondOut := strings.TrimPrefix(buf.String(), firstOut)

	if !strings.Contains(secondOut, "repo-b/other_bundle.js") {
		t.Errorf("a second index reported nothing for a path never seen before; got %q", secondOut)
	}
	if !strings.Contains(secondOut, "repo-a/bundle_0.js") {
		t.Errorf("a re-indexed file was silent on the second index; got %q", secondOut)
	}
}

// lockingWriter serialises the writes themselves, so a failure below is the
// reporter's own race and not the buffer's.
type lockingWriter struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *lockingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *lockingWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// THE MUTEX. ClassifyWithSize runs inside the indexer's worker pool, so the
// reporter's map is written concurrently. Unguarded that is a concurrent map
// write — a Go runtime fatal, not a warning — and under -race it is a reported
// data race. Both are failures; the dedup assertion below is the third.
func TestOversizedReportIsSafeUnderConcurrentClassification(t *testing.T) {
	w := &lockingWriter{}
	c := newReportingClassifier(w)

	paths := []string{"vendor/a.js", "vendor/b.js", "vendor/c.js", "vendor/d.js"}
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c.ClassifyWithSize(context.Background(), paths[i%len(paths)], oversizedBytes)
		}(i)
	}
	wg.Wait()
	c.ReportOversizedSummary()

	out := w.String()
	for _, p := range paths {
		if got := strings.Count(out, p); got != 1 {
			t.Errorf("path %q named %d times under concurrency, want exactly 1; got %q", p, got, out)
		}
	}
	if got := strings.Count(out, "grafel:"); got != len(paths) {
		t.Errorf("emitted %d lines under concurrency, want %d; got %q", got, len(paths), out)
	}
}

// The UnsupportedTally must count exactly what it counted before #6985.
//
// `.pas` is in unsupportedLanguageNames, so an UNDER-limit .pas file is the
// positive control that the tally still works at all; the same .pas file over
// the limit is skipped as too_large and must not appear, and neither must an
// oversized recognised-language file.
func TestOversizedSkipLeavesUnsupportedTallyUnchanged(t *testing.T) {
	c := newReportingClassifier(&bytes.Buffer{})

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
	c := newReportingClassifier(&bytes.Buffer{})

	res := c.ClassifyWithSize(context.Background(), "legacy/Huge.pas", oversizedBytes)
	if res.SkipReason != "too_large" {
		t.Errorf("oversized .pas skip reason is %q, want too_large", res.SkipReason)
	}
	if res.Language != "" {
		t.Errorf("oversized skip now carries a language %q; the size gate runs before language detection", res.Language)
	}
}
