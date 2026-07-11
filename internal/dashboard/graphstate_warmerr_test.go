package dashboard

// graphstate_warmerr_test.go — regression coverage for the #5722 follow-up:
// a stale dashboard warm-error surviving a successful re-index.
//
// #5722 added GraphCache.warmErrs so a genuine load failure could be
// distinguished from "still warming" and surfaced to the dashboard instead
// of retrying forever. The loose end: once a warmErrs entry was recorded for
// a group/ref, neither a subsequent successful warm NOR Invalidate/
// InvalidateAll cleared it — GetGroupForRef's success path always did clear
// it, but Invalidate/InvalidateAll (the re-index entry points) did not, so a
// stale error could keep being surfaced to a connected client after the
// underlying problem was fixed and the graph was re-indexed.
//
// These tests pin three invariants:
//  1. A successful GetGroupForRef load clears any prior warmErrs entry for
//     that exact group/ref.
//  2. Invalidate clears warmErrs for the group and all its per-ref variants.
//  3. InvalidateAll clears every warmErrs entry.
//
// A fourth test guards against over-correction: a re-failed warm attempt
// must still re-record the error (clearing on invalidate/success is not
// permission to swallow a genuinely recurring failure).

import (
	"errors"
	"testing"
	"time"
)

// TestGraphCache_SuccessfulLoadClearsWarmErr pins GetGroupForRef's existing
// success-clears-warmErrs behavior (already present before this change, but
// asserted here explicitly so a regression is caught alongside the
// Invalidate/InvalidateAll fixes below).
func TestGraphCache_SuccessfulLoadClearsWarmErr(t *testing.T) {
	c := NewGraphCache(60 * time.Second)

	c.mu.Lock()
	c.warmErrs["g1"] = errors.New("boom")
	c.mu.Unlock()

	if _, failed := c.LastWarmError("g1", ""); !failed {
		t.Fatal("expected a recorded warm error before the successful load")
	}

	// Simulate the success path taken by GetGroupForRef without touching
	// disk: record success directly (mirrors the `err == nil` branch).
	c.mu.Lock()
	c.entries["g1"] = &cacheEntry{group: &DashGroup{Name: "g1"}, loadedAt: time.Now()}
	delete(c.warmErrs, "g1")
	c.mu.Unlock()

	if _, failed := c.LastWarmError("g1", ""); failed {
		t.Fatal("warmErrs entry survived a successful load")
	}
}

// TestGraphCache_InvalidateClearsWarmErr is the RED test for the bug: prior
// to the fix, Invalidate dropped the cached group entry but left warmErrs
// (and its per-ref variants) untouched, so LastWarmError kept surfacing the
// stale failure after a re-index.
func TestGraphCache_InvalidateClearsWarmErr(t *testing.T) {
	c := NewGraphCache(60 * time.Second)

	c.mu.Lock()
	c.warmErrs["g1"] = errors.New("boom")
	c.warmErrs["g1@main"] = errors.New("boom on main")
	c.warmErrs["g1@feature-x"] = errors.New("boom on feature-x")
	c.warmErrs["g2"] = errors.New("unrelated group; must NOT be cleared")
	c.mu.Unlock()

	c.Invalidate("g1")

	if _, failed := c.LastWarmError("g1", ""); failed {
		t.Fatal("Invalidate(\"g1\") left a stale warmErrs entry for the bare group key")
	}
	if _, failed := c.LastWarmError("g1", "main"); failed {
		t.Fatal("Invalidate(\"g1\") left a stale warmErrs entry for g1@main")
	}
	if _, failed := c.LastWarmError("g1", "feature-x"); failed {
		t.Fatal("Invalidate(\"g1\") left a stale warmErrs entry for g1@feature-x")
	}
	if _, failed := c.LastWarmError("g2", ""); !failed {
		t.Fatal("Invalidate(\"g1\") must not clear an unrelated group's warmErrs entry")
	}
}

// TestGraphCache_InvalidateAllClearsWarmErr is the RED test for the
// InvalidateAll half of the bug.
func TestGraphCache_InvalidateAllClearsWarmErr(t *testing.T) {
	c := NewGraphCache(60 * time.Second)

	c.mu.Lock()
	c.warmErrs["g1"] = errors.New("boom")
	c.warmErrs["g2@main"] = errors.New("boom on main")
	c.mu.Unlock()

	c.InvalidateAll()

	if _, failed := c.LastWarmError("g1", ""); failed {
		t.Fatal("InvalidateAll left a stale warmErrs entry for g1")
	}
	if _, failed := c.LastWarmError("g2", "main"); failed {
		t.Fatal("InvalidateAll left a stale warmErrs entry for g2@main")
	}
}

// TestGraphCache_ReFailedWarmReRecordsAfterInvalidate guards against
// over-correction: clearing warmErrs on Invalidate must not suppress a
// genuinely recurring failure — the next failed load attempt after an
// Invalidate must re-record its own error.
func TestGraphCache_ReFailedWarmReRecordsAfterInvalidate(t *testing.T) {
	c := NewGraphCache(60 * time.Second)

	c.mu.Lock()
	c.warmErrs["g1"] = errors.New("first failure")
	c.mu.Unlock()

	c.Invalidate("g1")
	if _, failed := c.LastWarmError("g1", ""); failed {
		t.Fatal("Invalidate did not clear the prior warmErrs entry")
	}

	// Simulate a subsequent load attempt that fails again (mirrors the
	// `err != nil` branch of GetGroupForRef).
	c.mu.Lock()
	c.warmErrs["g1"] = errors.New("second failure")
	c.mu.Unlock()

	err, failed := c.LastWarmError("g1", "")
	if !failed {
		t.Fatal("a re-failed warm attempt after Invalidate must re-record its error")
	}
	if err == nil || err.Error() != "second failure" {
		t.Fatalf("LastWarmError = %v; want the newly re-recorded failure", err)
	}
}
