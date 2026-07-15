package repolock

import (
	"sync"
	"testing"
	"time"
)

// A background claim must yield (fail) while a foreground claim is held, and
// succeed again once it is released.
func TestBackgroundYieldsToForeground(t *testing.T) {
	r := New()

	relFg := r.ClaimForeground("/repo/a")
	if _, ok := r.TryClaimBackground("/repo/a"); ok {
		t.Fatal("background claim must fail while a foreground claim is held")
	}
	// A different repo is unaffected.
	if rel, ok := r.TryClaimBackground("/repo/b"); !ok {
		t.Fatal("background claim for an unrelated repo must succeed")
	} else {
		rel()
	}
	relFg()
	if rel, ok := r.TryClaimBackground("/repo/a"); !ok {
		t.Fatal("background claim must succeed after the foreground claim releases")
	} else {
		rel()
	}
}

// A background claim also excludes a second background claim (mutual exclusion),
// and a foreground claim blocks until the background one releases.
func TestForegroundBlocksOnRunningBackground(t *testing.T) {
	r := New()

	relBg, ok := r.TryClaimBackground("/repo/a")
	if !ok {
		t.Fatal("first background claim must succeed")
	}
	if _, ok := r.TryClaimBackground("/repo/a"); ok {
		t.Fatal("a second concurrent background claim must fail")
	}

	acquired := make(chan struct{})
	go func() {
		rel := r.ClaimForeground("/repo/a") // blocks until relBg fires
		close(acquired)
		rel()
	}()

	select {
	case <-acquired:
		t.Fatal("foreground claim acquired while a background index was still running")
	case <-time.After(50 * time.Millisecond):
	}
	relBg()
	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("foreground claim did not acquire after the background release")
	}
}

// Release must be idempotent — the orphaned-rebuild path may invoke it more
// than once, and a double release must not corrupt the ledger.
func TestReleaseIsIdempotent(t *testing.T) {
	r := New()
	rel := r.ClaimForeground("/repo/a")
	rel()
	rel() // must not panic nor wrongly free a fresh claim

	relBg, ok := r.TryClaimBackground("/repo/a")
	if !ok {
		t.Fatal("claim must be free after release")
	}
	// A stale second call to the FIRST release must not clear this new claim.
	rel()
	if _, ok := r.TryClaimBackground("/repo/a"); ok {
		t.Fatal("stale release wrongly freed a live claim")
	}
	relBg()
}

// HasForegroundClaim must report false before a claim is acquired, true while
// it is held (including the brief pre-hold "intended" window guarded by
// fgWant), and false again after release. This is the read-only seam the
// status-plane writer uses to detect a foreground rebuild that the scheduler
// itself cannot see (#5729 follow-up: foreground-indexing status-plane gap).
func TestHasForegroundClaim(t *testing.T) {
	r := New()

	if r.HasForegroundClaim("/repo/a") {
		t.Fatal("must be false before any claim is acquired")
	}

	rel := r.ClaimForeground("/repo/a")
	if !r.HasForegroundClaim("/repo/a") {
		t.Fatal("must be true while a foreground claim is held")
	}
	// An unrelated key is unaffected.
	if r.HasForegroundClaim("/repo/b") {
		t.Fatal("must be false for an unrelated key")
	}

	rel()
	if r.HasForegroundClaim("/repo/a") {
		t.Fatal("must be false after the claim releases")
	}
}

// HasForegroundClaim is a pure in-memory read safe to call concurrently with
// ClaimForeground/TryClaimBackground under -race.
func TestHasForegroundClaimConcurrentRace(t *testing.T) {
	r := New()
	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			rel := r.ClaimForeground("/repo/a")
			time.Sleep(time.Millisecond)
			rel()
		}
		close(stop)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				r.HasForegroundClaim("/repo/a")
			}
		}
	}()

	wg.Wait()
}

// Concurrent foreground claims for the same repo are serialised (never both
// hold at once), exercised under -race.
func TestForegroundClaimsSerialise(t *testing.T) {
	r := New()
	var active, max int32
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rel := r.ClaimForeground("/repo/a")
			mu.Lock()
			active++
			if active > max {
				max = active
			}
			mu.Unlock()
			time.Sleep(2 * time.Millisecond)
			mu.Lock()
			active--
			mu.Unlock()
			rel()
		}()
	}
	wg.Wait()
	if max != 1 {
		t.Fatalf("foreground claims overlapped: max concurrent = %d, want 1", max)
	}
}
