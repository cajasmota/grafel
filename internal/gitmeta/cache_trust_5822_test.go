package gitmeta

// cache_trust_5822_test.go — #5822 defect D at the capture layer.
//
// #6181 stopped MEMOIZING an untrusted capture, but CaptureCached still RETURNS
// it, so callers keep receiving Ref == "" — the value that becomes the
// refs/_unknown sentinel. Two things follow from that:
//
//   - a caller that CAN say "unknown" needs the trust flag (CaptureCachedTrusted);
//   - a long-lived process that has already seen this repo's ref should serve
//     the last KNOWN ref rather than a sentinel it knows to be wrong.

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestCaptureCachedTrusted_ReportsDistrust is the flag itself.
func TestCaptureCachedTrusted_ReportsDistrust(t *testing.T) {
	generousDeadline(t)
	resetCaptureCacheForTest()
	dir := initGitRepo(t)

	if info, ok := CaptureCachedTrusted(dir); !ok || info.Ref != "main" {
		t.Fatalf("healthy git: got (%+v, %v), want (Ref=main, true)", info, ok)
	}

	resetCaptureCacheForTest() // no last-known value to fall back on
	calls := withGitUnavailable(t)
	info, ok := CaptureCachedTrusted(dir)
	if *calls == 0 {
		t.Fatal("seam was never exercised — the test is not testing anything")
	}
	if ok {
		t.Fatalf("un-runnable git reported as trusted: %+v — Ref==%q then becomes the "+
			"refs/_unknown sentinel with no way for the caller to notice (#5822 D)", info, info.Ref)
	}
}

// TestCaptureCachedTrusted_ReusesLastKnownRef covers the long-lived-process
// half. The memo is keyed on the HEAD pointer's (path, mtime, size), so a HEAD
// REWRITE invalidates it — and if git happens to be un-runnable at that exact
// moment, the caller used to be handed "" even though the process had a
// perfectly good ref for this repo a moment earlier. A stale-but-real ref beats
// a sentinel that is certainly wrong.
func TestCaptureCachedTrusted_ReusesLastKnownRef(t *testing.T) {
	generousDeadline(t)
	resetCaptureCacheForTest()
	dir := initGitRepo(t)

	if got := CaptureCached(dir); got.Ref != "main" {
		t.Fatalf("fixture broken: first capture Ref=%q, want main", got.Ref)
	}

	// Invalidate the memo the way production does — by moving HEAD's mtime —
	// so the un-runnable capture below is actually REACHED rather than served
	// from cache (which would make this test prove nothing).
	head := filepath.Join(dir, ".git", "HEAD")
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(head, past, past); err != nil {
		t.Fatal(err)
	}
	before, okKey := headPointerKey(dir)
	if !okKey {
		t.Fatal("fixture broken: HEAD pointer no longer resolvable")
	}
	_ = before

	withGitUnavailable(t)
	info, ok := CaptureCachedTrusted(dir)
	if info.Ref != "main" || !ok {
		t.Fatalf("got (Ref=%q, %v), want (main, true): the process already knew this "+
			"repo's ref and threw it away in favour of the _unknown sentinel (#5822 D)",
			info.Ref, ok)
	}
}
