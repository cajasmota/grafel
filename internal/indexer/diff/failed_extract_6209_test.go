package diff

// #6209 — a stamp says "the graph was built from these bytes". For a file whose
// EXTRACTION failed that is false, and false in the one direction the next pass
// cannot detect: the hash matches, the file is classified unchanged, and its
// entities stay missing from the graph with nothing anywhere recording why.
//
// ApplyStampsAndFailures keeps the stamp and qualifies it with a consecutive-
// failure count; retryDue spends that count over the next MaxExtractRetries
// passes. These are unit tests on the two halves the cmd/grafel end-to-end
// fixture cannot reach: the non-git Filter path (its repo is always a git repo)
// and the content-change reset (four more real index passes to observe).

import (
	"os"
	"path/filepath"
	"testing"
)

// writeAndStamp writes body to dir/rel and returns the stamp the walk would
// have produced for it — the real hash of the real bytes, so Filter's own
// stat-and-hash check agrees with the manifest unless something else forces the
// file dirty.
func writeAndStamp(t *testing.T, dir, rel, body string) FileEntry {
	t.Helper()
	abs := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	e, err := HashFile(abs)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func changedSet(t *testing.T, dir string, rels []string, m *Manifest) map[string]bool {
	t.Helper()
	changed, _ := Filter(dir, rels, m)
	got := make(map[string]bool, len(changed))
	for _, c := range changed {
		got[c] = true
	}
	return got
}

// TestFailedExtraction_IsRetriedThenGoesQuiet walks the whole budget in one
// place: the file comes back for exactly MaxExtractRetries extra passes and then
// stops, with the bytes never changing.
//
// The bytes not changing is the point. Every pass here would classify the file
// UNCHANGED on the hash alone — that is precisely how the defect hid.
func TestFailedExtraction_IsRetriedThenGoesQuiet(t *testing.T) {
	dir := t.TempDir()
	stamp := writeAndStamp(t, dir, "victim.go", "package v\n")
	other := writeAndStamp(t, dir, "clean.go", "package v\n\nfunc C() {}\n")
	walk := []string{"victim.go", "clean.go"}

	m := newManifest()
	stamps := map[string]FileEntry{"victim.go": stamp, "clean.go": other}
	failed := map[string]bool{"victim.go": true}

	// Pass 1: both stamped, one failed.
	ApplyStampsAndFailures(stamps, walk, failed, m)
	if got := m.Files["victim.go"].Failures; got != 1 {
		t.Fatalf("after pass 1 victim.go carries %d failure(s), want 1 — an unqualified stamp is "+
			"indistinguishable from a successful index (#6209)", got)
	}
	if got := m.Files["clean.go"].Failures; got != 0 {
		t.Fatalf("clean.go carries %d failure(s) and never failed, want 0 — the mark must name the "+
			"file, not the walk", got)
	}
	if got := m.Files["victim.go"].SHA256; got != stamp.SHA256 {
		t.Fatalf("victim.go's stamp is %q, want the walk's %q — the failure mark qualifies the stamp, "+
			"it does not replace it", got, stamp.SHA256)
	}

	// Passes 2..1+MaxExtractRetries: the file is due, and re-fails.
	for n := 2; n <= 1+MaxExtractRetries; n++ {
		got := changedSet(t, dir, walk, m)
		if !got["victim.go"] {
			t.Fatalf("pass %d did not re-present victim.go. Its bytes are unchanged BY CONSTRUCTION — "+
				"a failed extraction does not edit the file — so the hash can only ever say "+
				"\"unchanged\" and the missing entities are never retried (#6209)", n)
		}
		if got["clean.go"] {
			t.Fatalf("pass %d also re-presented clean.go, which extracted fine; the retry must not "+
				"widen into a full reindex", n)
		}
		ApplyStampsAndFailures(stamps, walk, failed, m)
		if f := m.Files["victim.go"].Failures; f != n {
			t.Fatalf("after pass %d victim.go carries %d failure(s), want %d — the count must be "+
				"CONSECUTIVE and durable, or the retry cannot be bounded", n, f, n)
		}
	}

	// The budget is spent.
	if got := changedSet(t, dir, walk, m); got["victim.go"] {
		t.Fatalf("victim.go is still re-presenting after %d consecutive deterministic failures. "+
			"Unbounded retry means a genuinely unparseable file is re-read and re-parsed on every "+
			"pass for the life of the repo — a silent gap traded for a permanent tax (#6209)",
			1+MaxExtractRetries)
	}
}

// TestFailedExtraction_ContentChangeRestartsTheBudget pins the reset.
//
// Once the budget is spent the file is quiet, which is only defensible while
// nothing has changed. New bytes are the one piece of new information that could
// plausibly change the outcome — a syntax error fixed, a generated file
// regenerated — so they buy a fresh budget rather than inheriting a spent one.
// Without this a file that failed three times in its lifetime could never be
// retried again no matter how it was edited.
func TestFailedExtraction_ContentChangeRestartsTheBudget(t *testing.T) {
	dir := t.TempDir()
	walk := []string{"victim.go"}
	m := newManifest()

	first := writeAndStamp(t, dir, "victim.go", "package v\n")
	failed := map[string]bool{"victim.go": true}
	for n := 1; n <= 1+MaxExtractRetries; n++ {
		ApplyStampsAndFailures(map[string]FileEntry{"victim.go": first}, walk, failed, m)
	}
	if got := changedSet(t, dir, walk, m); got["victim.go"] {
		t.Fatal("fixture is inert: the budget was not spent, so the reset below proves nothing")
	}

	// The developer edits the file and it fails again — at DIFFERENT bytes.
	second := writeAndStamp(t, dir, "victim.go", "package v\n\nfunc Fixed() {}\n")
	if second.SHA256 == first.SHA256 {
		t.Fatal("fixture is inert: the edit produced identical bytes")
	}
	ApplyStampsAndFailures(map[string]FileEntry{"victim.go": second}, walk, failed, m)

	if got := m.Files["victim.go"].Failures; got != 1 {
		t.Fatalf("victim.go carries %d failure(s) after failing at NEW content, want 1 — inheriting a "+
			"spent budget means an edited file is never retried again (#6209)", got)
	}
	if !changedSet(t, dir, walk, m)["victim.go"] {
		t.Fatal("victim.go is not due for retry after failing at new content, so the fresh budget is " +
			"not actually spendable")
	}
}

// TestSuccessfulExtraction_ClearsTheFailureHistory pins the count as
// CONSECUTIVE rather than lifetime: one success wipes it, so a file that fails
// transiently and then succeeds is back to an ordinary entry with no residual
// retry pressure.
func TestSuccessfulExtraction_ClearsTheFailureHistory(t *testing.T) {
	dir := t.TempDir()
	walk := []string{"victim.go"}
	stamp := writeAndStamp(t, dir, "victim.go", "package v\n")
	m := newManifest()

	ApplyStampsAndFailures(map[string]FileEntry{"victim.go": stamp}, walk,
		map[string]bool{"victim.go": true}, m)
	if m.Files["victim.go"].Failures != 1 {
		t.Fatalf("fixture is inert: no failure was recorded")
	}

	// Same bytes, same stamp — this time extraction succeeded, so the file is
	// not in the failed set.
	ApplyStampsAndFailures(map[string]FileEntry{"victim.go": stamp}, walk, nil, m)

	if got := m.Files["victim.go"].Failures; got != 0 {
		t.Fatalf("victim.go still carries %d failure(s) after a SUCCESSFUL extraction, want 0 — the "+
			"count is consecutive, and stale pressure re-extracts a healthy file for no reason", got)
	}
	if changedSet(t, dir, walk, m)["victim.go"] {
		t.Fatal("victim.go is still due for retry after a successful extraction")
	}
}

// TestApplyStampsAndFailures_ReportsTheBudgetCrossingOnce pins the only signal a
// human gets. The count is durable but nothing outside this package reads it, so
// the crossing — the instant a file stops being retried and its entities become
// permanently absent — is returned to the caller to log. It must fire exactly
// once, on the pass that spends the last attempt: a crossing reported early is a
// false alarm, and one reported on every subsequent pass is noise that trains
// the reader to ignore it.
func TestApplyStampsAndFailures_ReportsTheBudgetCrossingOnce(t *testing.T) {
	dir := t.TempDir()
	walk := []string{"victim.go"}
	stamp := writeAndStamp(t, dir, "victim.go", "package v\n")
	failed := map[string]bool{"victim.go": true}
	m := newManifest()

	for n := 1; n <= MaxExtractRetries; n++ {
		if got := ApplyStampsAndFailures(map[string]FileEntry{"victim.go": stamp}, walk, failed, m); len(got) != 0 {
			t.Fatalf("pass %d reported %v as out of retries, but %d attempt(s) remain — a premature "+
				"crossing tells the reader a file went dark while it is still being retried", n, got, 1+MaxExtractRetries-n)
		}
	}

	got := ApplyStampsAndFailures(map[string]FileEntry{"victim.go": stamp}, walk, failed, m)
	if len(got) != 1 || got[0] != "victim.go" {
		t.Fatalf("the pass that spent the last attempt reported %v, want [victim.go] — this is the "+
			"only moment anything announces that the file's entities are now permanently missing (#6209)", got)
	}

	if again := ApplyStampsAndFailures(map[string]FileEntry{"victim.go": stamp}, walk, failed, m); len(again) != 0 {
		t.Fatalf("a later pass reported the crossing again (%v); it must fire once per file, not once "+
			"per pass forever", again)
	}
}

// TestUpdateManifestScoped_PreservesTheFailureBudget pins a coupling that is
// easy to miss and silent when broken.
//
// UpdateManifestScoped re-hashes files off disk and writes fresh FileEntry
// values, whose Failures is zero. The too-many-changed reject in
// internal/extractors.TryIncremental calls it with exactly the changed set — and
// a retry-due file is IN that set by construction, because the retry union put
// it there. Zeroing the count there restarts the budget on every reject, which
// is the unbounded retry this design deliberately rejected, reached by accident
// rather than by decision.
func TestUpdateManifestScoped_PreservesTheFailureBudget(t *testing.T) {
	dir := t.TempDir()
	stamp := writeAndStamp(t, dir, "victim.go", "package v\n")
	m := newManifest()
	m.Files["victim.go"] = FileEntry{SHA256: stamp.SHA256, Size: stamp.Size, Mtime: stamp.Mtime, Failures: 2}

	UpdateManifestScoped(dir, []string{"victim.go"}, []string{"victim.go"}, m)

	if got := m.Files["victim.go"].Failures; got != 2 {
		t.Fatalf("a re-hash reset victim.go's failure count to %d, want 2 preserved. The bytes did not "+
			"change, so nothing about the file's prospects changed either; restarting its budget on "+
			"every too-many-changed reject makes the retry unbounded (#6209)", got)
	}
	if RetryDue(m.Files["victim.go"]) != true {
		t.Fatal("victim.go is no longer retry-due after a re-hash that changed nothing")
	}
}

// TestUpdateManifestScoped_NewContentClearsTheFailureBudget is the other side of
// that rule: a re-hash that finds DIFFERENT bytes writes a fresh entry with no
// count, because the file is being re-extracted on its own merits anyway and the
// old budget described bytes that no longer exist.
func TestUpdateManifestScoped_NewContentClearsTheFailureBudget(t *testing.T) {
	dir := t.TempDir()
	m := newManifest()
	m.Files["victim.go"] = FileEntry{SHA256: "stamp-of-the-old-bytes", Size: 1, Mtime: 1, Failures: 2}
	writeAndStamp(t, dir, "victim.go", "package v\n\nfunc Edited() {}\n")

	UpdateManifestScoped(dir, []string{"victim.go"}, []string{"victim.go"}, m)

	if got := m.Files["victim.go"].Failures; got != 0 {
		t.Fatalf("victim.go kept %d failure(s) from bytes that no longer exist, want 0 — an edited "+
			"file must not inherit a budget spent against different content (#6209)", got)
	}
}

// TestApplyStampsAndFailures_SkipsFilesWithNoManifestEntry covers the file that
// failed and was never stamped at all — the unreadable-file case, and anything
// the membership prune drops. There is nothing to qualify, and an absent entry
// already re-presents as new, which is the stronger signal. Marking must not
// invent an entry with no hash: that would be a manifest row claiming a file
// with no content, which Filter reads as unchanged-if-you-squint rather than as
// absent.
func TestApplyStampsAndFailures_SkipsFilesWithNoManifestEntry(t *testing.T) {
	m := newManifest()
	ApplyStampsAndFailures(nil, []string{"kept.go"}, map[string]bool{"never-stamped.go": true}, m)

	if _, ok := m.Files["never-stamped.go"]; ok {
		t.Fatalf("a failed file with no stamp was given a manifest entry %+v — an unstamped file must "+
			"stay absent so it reads as NEW next pass (#6209)", m.Files["never-stamped.go"])
	}
}
