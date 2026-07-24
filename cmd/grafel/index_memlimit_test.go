package main

import (
	"strings"
	"testing"
)

const (
	giB = int64(1) << 30
	miB = int64(1) << 20
)

// TestResolveIndexMemLimit pins the adaptive policy: max(2GiB, 0.5*available),
// and "unset" when available RAM cannot be determined (#5954).
func TestResolveIndexMemLimit(t *testing.T) {
	cases := []struct {
		name      string
		available uint64
		want      int64
	}{
		{"unknown ram -> unset", 0, memLimitUnset},
		{"tiny host 1GiB -> floor", uint64(giB), 2 * giB},
		{"small host 3GiB -> floor (half is 1.5GiB)", uint64(3 * giB), 2 * giB},
		{"exactly at floor boundary 4GiB -> 2GiB", uint64(4 * giB), 2 * giB},
		{"dev machine ~6GiB -> 3GiB", uint64(6 * giB), 3 * giB},
		{"large host 64GiB -> 32GiB", uint64(64 * giB), 32 * giB},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveIndexMemLimit(tc.available); got != tc.want {
				t.Errorf("resolveIndexMemLimit(%d) = %d, want %d", tc.available, got, tc.want)
			}
		})
	}
}

// TestResolveIndexMemLimitFloorInvariant is the anti-thrash guarantee: a
// GOMEMLIMIT BELOW the live heap sends the runtime into a GC death spiral
// (measured: 1200MiB against a ~2.7GB live heap ran >4x slower and never
// finished). So whenever the policy returns a limit at all, it is >= 2GiB.
func TestResolveIndexMemLimitFloorInvariant(t *testing.T) {
	for avail := uint64(0); avail <= uint64(96*giB); avail += uint64(64 * miB) {
		got := resolveIndexMemLimit(avail)
		if got == memLimitUnset {
			continue
		}
		if got < 2*giB {
			t.Fatalf("resolveIndexMemLimit(%d) = %d, below the 2GiB anti-thrash floor", avail, got)
		}
	}
}

// TestParseIndexMemLimitEnv covers the operator escape hatch.
func TestParseIndexMemLimitEnv(t *testing.T) {
	cases := []struct {
		name        string
		raw         string
		wantLimit   int64
		wantDecided bool
	}{
		{"empty -> undecided", "", 0, false},
		{"plain bytes", "3221225472", 3 * giB, true},
		{"MiB suffix", "2500MiB", 2500 * miB, true},
		{"GiB suffix", "4GiB", 4 * giB, true},
		{"lowercase gib", "4gib", 4 * giB, true},
		{"spaces trimmed", "  2GiB  ", 2 * giB, true},
		{"zero disables", "0", memLimitUnset, true},
		{"off disables", "off", memLimitUnset, true},
		{"OFF disables", "OFF", memLimitUnset, true},
		{"disabled disables", "disabled", memLimitUnset, true},
		{"negative -> malformed", "-5", 0, false},
		{"garbage -> malformed", "banana", 0, false},
		{"bad suffix -> malformed", "12PiB", 0, false},
		{"empty number -> malformed", "GiB", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotLimit, gotDecided := parseIndexMemLimitEnv(tc.raw)
			if gotLimit != tc.wantLimit || gotDecided != tc.wantDecided {
				t.Errorf("parseIndexMemLimitEnv(%q) = (%d, %v), want (%d, %v)",
					tc.raw, gotLimit, gotDecided, tc.wantLimit, tc.wantDecided)
			}
		})
	}
}

// TestIndexMemLimitDecision asserts precedence: a well-formed env override
// always wins; a malformed one falls back to the adaptive policy (and never
// panics).
func TestIndexMemLimitDecision(t *testing.T) {
	cases := []struct {
		name       string
		raw        string
		available  uint64
		wantLimit  int64
		wantSource string
	}{
		{"no env -> adaptive", "", uint64(6 * giB), 3 * giB, "adaptive"},
		{"env wins over adaptive", "2500MiB", uint64(64 * giB), 2500 * miB, "env"},
		{"env disables even with ram known", "off", uint64(64 * giB), memLimitUnset, "env"},
		{"malformed env -> adaptive", "banana", uint64(6 * giB), 3 * giB, "adaptive"},
		{"malformed env, unknown ram -> unset", "banana", 0, memLimitUnset, "adaptive"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotLimit, gotSource := indexMemLimitDecision(tc.raw, tc.available)
			if gotLimit != tc.wantLimit {
				t.Errorf("indexMemLimitDecision(%q, %d) limit = %d, want %d",
					tc.raw, tc.available, gotLimit, tc.wantLimit)
			}
			if !strings.Contains(gotSource, tc.wantSource) {
				t.Errorf("indexMemLimitDecision(%q, %d) source = %q, want it to mention %q",
					tc.raw, tc.available, gotSource, tc.wantSource)
			}
		})
	}
}
