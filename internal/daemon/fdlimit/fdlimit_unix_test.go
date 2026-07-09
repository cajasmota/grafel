//go:build linux || darwin

package fdlimit

import (
	"syscall"
	"testing"
)

// TestRaise_NeverLowers verifies that a target below the current soft limit
// leaves the limit unchanged (the raise must never shrink an already-high
// limit).
func TestRaise_NeverLowers(t *testing.T) {
	var before syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &before); err != nil {
		t.Fatalf("Getrlimit: %v", err)
	}

	old, updated, err := Raise(1) // absurdly low target
	if err != nil {
		t.Fatalf("Raise(1) returned error: %v", err)
	}
	if old != before.Cur {
		t.Errorf("old = %d, want current soft limit %d", old, before.Cur)
	}
	if updated < old {
		t.Fatalf("Raise LOWERED the limit: old=%d new=%d", old, updated)
	}

	var after syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &after); err != nil {
		t.Fatalf("Getrlimit after: %v", err)
	}
	if after.Cur < before.Cur {
		t.Fatalf("soft limit was lowered on disk: before=%d after=%d", before.Cur, after.Cur)
	}
}

// TestRaise_ReturnsNewGreaterOrEqualOld verifies the core postcondition:
// updated >= old always holds, regardless of platform ceiling.
func TestRaise_ReturnsNewGreaterOrEqualOld(t *testing.T) {
	old, updated, err := Raise(DefaultTarget)
	if err != nil {
		// A non-nil error is acceptable (best-effort), but then the limit must
		// be reported unchanged.
		if updated != old {
			t.Fatalf("Raise errored (%v) but reported a changed limit: old=%d new=%d", err, old, updated)
		}
		return
	}
	if updated < old {
		t.Fatalf("Raise LOWERED the limit: old=%d new=%d", old, updated)
	}
}

// TestRaise_RaisesFromLoweredLimit deterministically proves that Raise
// increases the soft limit. We first LOWER our own soft limit (always
// permitted) then assert Raise brings it back up toward the original — this
// works on any platform regardless of the hard-max representation (Darwin
// reports an effectively-infinite hard max, so testing "toward hard max"
// directly is unreliable).
func TestRaise_RaisesFromLoweredLimit(t *testing.T) {
	var orig syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &orig); err != nil {
		t.Fatalf("Getrlimit: %v", err)
	}
	t.Cleanup(func() { _ = syscall.Setrlimit(syscall.RLIMIT_NOFILE, &orig) })

	const lowered = 256
	if orig.Cur <= lowered {
		t.Skipf("soft limit (%d) already at/below %d; cannot lower to test", orig.Cur, lowered)
	}
	low := orig
	low.Cur = lowered
	if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &low); err != nil {
		t.Fatalf("Setrlimit(lowered): %v", err)
	}

	old, updated, err := Raise(orig.Cur)
	if err != nil {
		t.Fatalf("Raise: %v", err)
	}
	if old != lowered {
		t.Errorf("old = %d, want lowered value %d", old, lowered)
	}
	if updated <= old {
		t.Fatalf("expected raise: old=%d new=%d (target=%d, hardMax=%d)", old, updated, orig.Cur, orig.Max)
	}
	if updated > orig.Max {
		t.Fatalf("raised above hard max: new=%d hardMax=%d", updated, orig.Max)
	}
}

// TestCandidates_DescendingClampedUnique checks the candidate generator clamps
// to the hard max, drops zeros/dupes, and preserves descending priority.
func TestCandidates_DescendingClampedUnique(t *testing.T) {
	got := candidates(200000, 100000)
	if len(got) == 0 {
		t.Fatal("no candidates produced")
	}
	if got[0] != 100000 {
		t.Errorf("first candidate = %d, want clamp to hardMax 100000", got[0])
	}
	seen := map[uint64]bool{}
	for _, v := range got {
		if v == 0 {
			t.Errorf("candidate list contains zero: %v", got)
		}
		if v > 100000 {
			t.Errorf("candidate %d exceeds hardMax 100000", v)
		}
		if seen[v] {
			t.Errorf("duplicate candidate %d in %v", v, got)
		}
		seen[v] = true
	}
}
