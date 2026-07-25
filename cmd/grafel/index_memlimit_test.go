package main

import (
	"math"
	"strings"
	"testing"
)

const (
	giB = int64(1) << 30
	miB = int64(1) << 20
)

// TestResolveIndexMemLimit pins the adaptive policy (#5954):
//
//	unset                          if available == 0 or available < 4GiB
//	max(3GiB, available*3/4)       otherwise
func TestResolveIndexMemLimit(t *testing.T) {
	cases := []struct {
		name      string
		available uint64
		want      int64
	}{
		{"unknown ram -> unset", 0, memLimitUnset},
		{"tiny host 1GiB -> unset (do not attempt to bound)", uint64(giB), memLimitUnset},
		{"small host 3GiB -> unset", uint64(3 * giB), memLimitUnset},
		{"just under the 4GiB attempt threshold -> unset", uint64(4*giB) - 1, memLimitUnset},
		// NB: these two are the 0.75*available path, not the floor path. 0.75 of
		// 4GiB is already 3GiB, so the two coincide here.
		{"exactly at the 4GiB threshold -> 0.75*available = 3GiB", uint64(4 * giB), 3 * giB},
		{"just over the threshold -> 0.75*available, still 3GiB", uint64(4*giB) + 1, 3 * giB},
		// Regression pin for the REQUEST-CHANGES bug: the real value this host
		// reports. The old max(2GiB, available/2) policy returned 2280MB here.
		// That was justified at the time by a "~2.5-2.7GB live heap" reading
		// which was really heap_inuse/heap_alloc — see measuredLiveHeapPeakMB
		// for the corrected figure (1714MB), against which 2280MB is 1.33x
		// live: tight rather than fatal. The 3/4 policy stands regardless; only
		// the justification for it was wrong.
		{"real host 4560MB -> 3420MB (was 2280MB, only 1.33x live)", uint64(4560 * miB), 3420 * miB},
		{"6GiB -> 4.5GiB", uint64(6 * giB), 6 * giB * 3 / 4},
		{"large host 64GiB -> 48GiB (non-binding, as intended)", uint64(64 * giB), 48 * giB},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveIndexMemLimit(tc.available); got != tc.want {
				t.Errorf("resolveIndexMemLimit(%d) = %d, want %d", tc.available, got, tc.want)
			}
		})
	}
}

// TestResolveIndexMemLimitSafeFloorInvariant is the anti-thrash guarantee.
//
// GOMEMLIMIT is SOFT: set BELOW the workload's live heap the runtime cannot
// honor it and instead runs repeated full-heap mark cycles. Go's 50% GC CPU
// limiter does NOT bound that damage — it throttles assists, but the mark
// cycles themselves are cache/TLB/page-fault bound and are not accounted to
// GC CPU. madvdontneed=1 COMPOUNDS it (freed pages go back to the OS and are
// immediately re-faulted), and the tree-sitter CGo arenas are invisible to
// GOMEMLIMIT, so the runtime GCs toward a target that non-Go memory
// guarantees it can never reach. Measured: 1200MiB against a 1714MB live heap
// (0.70x live — see measuredLiveHeapPeakMB) ran >4x slower (>16 min vs 235 s)
// and never completed.
//
// So the trade is asymmetric: a too-low limit costs unbounded, non-terminating
// time; no limit at all costs a bounded +823MB. The invariant therefore is
// EITHER unset OR >= the safe floor — never a value in between.
//
// This test restates the implementation constant on purpose: its subject is
// the "never in between" SHAPE, not the value. The value has to be pinned
// against the measured hazard instead — an earlier form of this test asserted
// ">= 2GiB" while the harmful value was 2280MB, so it passed on the bug.
func TestResolveIndexMemLimitSafeFloorInvariant(t *testing.T) {
	for avail := uint64(0); avail <= uint64(96*giB); avail += uint64(64 * miB) {
		got := resolveIndexMemLimit(avail)
		if got == memLimitUnset {
			continue
		}
		if got < indexMemLimitSafeFloorBytes {
			t.Fatalf("resolveIndexMemLimit(%d) = %d — a limit was returned below the %d-byte safe floor (thrash regime)",
				avail, got, indexMemLimitSafeFloorBytes)
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
		{"no env -> adaptive", "", uint64(8 * giB), 6 * giB, "adaptive"},
		{"env wins over adaptive", "2500MiB", uint64(64 * giB), 2500 * miB, "env"},
		{"env disables even with ram known", "off", uint64(64 * giB), memLimitUnset, "env"},
		{"malformed env -> adaptive", "banana", uint64(8 * giB), 6 * giB, "adaptive"},
		{"malformed env, unknown ram -> unset", "banana", 0, memLimitUnset, "adaptive"},
		{"no env, low ram -> unset", "", uint64(2 * giB), memLimitUnset, "adaptive"},
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

// TestGCThrashWarning covers the post-index diagnostic (#5954). It is
// DIAGNOSIS ONLY — it never adjusts the limit. A sustained GC CPU fraction
// above the threshold alongside an applied soft limit is the fingerprint of
// the thrash regime the safe floor exists to prevent, so it must be visible in
// a support bundle without a profiler.
func TestGCThrashWarning(t *testing.T) {
	const noLimit = int64(math.MaxInt64)
	cases := []struct {
		name     string
		limit    int64
		fraction float64
		wantWarn bool
	}{
		{"healthy run under a limit", 3 * giB, 0.04, false},
		{"just under threshold", 3 * giB, 0.29, false},
		{"over threshold with a limit -> warn", 3 * giB, 0.55, true},
		{"way over threshold -> warn", 3 * giB, 0.92, true},
		// No limit applied: high GC CPU is not attributable to our tuning, so
		// we stay quiet rather than emit a misleading warning.
		{"no limit applied, high gc -> quiet", noLimit, 0.92, false},
		{"no limit applied, low gc -> quiet", noLimit, 0.01, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := gcThrashWarning(tc.limit, tc.fraction)
			if (got != "") != tc.wantWarn {
				t.Errorf("gcThrashWarning(%d, %v) = %q, wantWarn=%v", tc.limit, tc.fraction, got, tc.wantWarn)
			}
			if tc.wantWarn {
				// Both knobs, because either can be the cause — and the
				// GC-percent one is the likelier since the GOGC cap landed.
				for _, knob := range []string{"GRAFEL_INDEX_GOGC", "GRAFEL_INDEX_MEMLIMIT"} {
					if !strings.Contains(got, knob) {
						t.Errorf("warning should name the %s escape hatch, got %q", knob, got)
					}
				}
			}
		})
	}
}
