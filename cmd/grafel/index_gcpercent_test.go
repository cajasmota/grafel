package main

import (
	"runtime/debug"
	"strings"
	"testing"
)

// The measured corpus figures these tests derive their thresholds from, so
// that a change to the implementation's own constants cannot silently move the
// bar (#5954). Source: .claude/dev/perf/runs — 8 full `index-internal` runs
// (runs/latest/mt-{unlimited,productized}-{1,2,3} plus the earlier
// runs/mt-unlimited and runs/mt-productized), field `next_gc` in child.ndjson.
//
// LIVE HEAP is the number that matters, and it is NOT heap_inuse/heap_alloc —
// those include uncollected garbage. Go sets next_gc = live * (1 + GOGC/100)
// at the end of every mark cycle, so with the child running at the GOGC=100
// default, live = next_gc/2 exactly. Empirically corroborated: across the 881
// samples of runs/latest/mt-unlimited-1 there is not one sample where
// heap_alloc < next_gc/2, and over the 109 first-post-GC samples the minimum
// heap_alloc/(next_gc/2) ratio is 1.003. The harness's orthogonal measure
// (pprof -inuse_space at `materializing`) reads 1173-1539MB across the six
// runs/latest runs — same order, and nowhere near 2.7GB.
//
// Per-run live peaks, all in the `materializing` phase:
// 1571 / 1611 / 1674 / 1674 / 1677 / 1682 / 1707 / 1714 MB. We take the worst
// over ALL runs, 1714MB (runs/mt-unlimited).
//
// For contrast, the same runs report heap_inuse up to 3449MB and a post-GC
// heap_alloc up to 2285MB. Reading a "~2.7GB live heap" off those is a
// category error; it is what set the memlimit safe floor too high.
const (
	// measuredLiveHeapPeakMB is the worst live-heap peak over all 8 runs.
	measuredLiveHeapPeakMB = int64(1714)
	// measuredUnlimitedHeapSysMB is the SMALLEST heap_sys peak observed at
	// GOGC=100 (they range 3679-3783MB). Smallest, deliberately: a bound has
	// to be below even the most favourable unbounded run to bound anything.
	measuredUnlimitedHeapSysMB = int64(3679)
	// measuredThrashLimitMB is the GOMEMLIMIT that made a real run >4x slower
	// (>16 min vs 235 s) and never complete — 0.70x the live peak.
	measuredThrashLimitMB = int64(1200)
)

// TestIndexGCPercentDecision pins the GC-pacing policy (#5954).
//
// Precedence, highest first:
//
//  1. the command is not the background index child -> NEVER capped
//  2. GRAFEL_INDEX_GOGC (well-formed)                -> operator wins
//  3. GOGC set explicitly by the operator            -> leave the runtime alone
//  4. otherwise                                       -> indexGCPercentDefault
func TestIndexGCPercentDecision(t *testing.T) {
	const bg = backgroundIndexCommand
	cases := []struct {
		name        string
		command     string
		rawEnv      string
		rawGOGC     string
		wantPercent int
		wantSource  string
	}{
		{"background child, no env -> policy default", bg, "", "", indexGCPercentDefault, "policy"},
		{"operator override wins", bg, "35", "", 35, "GRAFEL_INDEX_GOGC"},
		{"operator override wins over an explicit GOGC", bg, "35", "200", 35, "GRAFEL_INDEX_GOGC"},
		{"operator override can disable the cap", bg, "off", "", gcPercentUnset, "GRAFEL_INDEX_GOGC"},
		{"explicit GOGC is respected", bg, "", "200", gcPercentUnset, "GOGC"},
		{"malformed override falls back to policy", bg, "banana", "", indexGCPercentDefault, "policy"},

		// The standing product decision: background indexing is capped,
		// interactive/foreground indexing is NOT. The cap must not leak onto any
		// other command, and not even an explicit GRAFEL_INDEX_GOGC may turn it
		// on there — the knob tunes the background cap, it does not create one.
		{"interactive index is not capped", "index", "", "", gcPercentUnset, "interactive"},
		{"interactive index ignores the override", "index", "35", "", gcPercentUnset, "interactive"},
		{"daemon is not capped here", "daemon", "", "", gcPercentUnset, "interactive"},
		{"empty command is not capped", "", "", "", gcPercentUnset, "interactive"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotPercent, gotSource := indexGCPercentDecision(tc.command, tc.rawEnv, tc.rawGOGC)
			if gotPercent != tc.wantPercent {
				t.Errorf("indexGCPercentDecision(%q, %q, %q) percent = %d, want %d",
					tc.command, tc.rawEnv, tc.rawGOGC, gotPercent, tc.wantPercent)
			}
			if !strings.Contains(gotSource, tc.wantSource) {
				t.Errorf("indexGCPercentDecision(%q, %q, %q) source = %q, want it to mention %q",
					tc.command, tc.rawEnv, tc.rawGOGC, gotSource, tc.wantSource)
			}
		})
	}
}

// TestParseIndexGCPercentEnv covers the operator escape hatch parser.
func TestParseIndexGCPercentEnv(t *testing.T) {
	cases := []struct {
		name        string
		raw         string
		wantPercent int
		wantDecided bool
	}{
		{"empty -> undecided", "", 0, false},
		{"plain percent", "40", 40, true},
		{"spaces trimmed", "  75  ", 75, true},
		{"trailing percent sign", "60%", 60, true},
		{"off disables the cap", "off", gcPercentUnset, true},
		{"OFF disables the cap", "OFF", gcPercentUnset, true},
		{"disabled disables the cap", "disabled", gcPercentUnset, true},
		{"none disables the cap", "none", gcPercentUnset, true},
		{"zero disables the cap", "0", gcPercentUnset, true},
		// Negative would mean "turn GC off entirely" — far outside the intent of
		// a knob whose job is to LOWER peak RSS. Treat it as malformed so the
		// policy applies rather than silently shipping a no-GC indexer.
		{"negative -> malformed", "-1", 0, false},
		{"garbage -> malformed", "banana", 0, false},
		{"float -> malformed", "50.5", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotPercent, gotDecided := parseIndexGCPercentEnv(tc.raw)
			if gotPercent != tc.wantPercent || gotDecided != tc.wantDecided {
				t.Errorf("parseIndexGCPercentEnv(%q) = (%d, %v), want (%d, %v)",
					tc.raw, gotPercent, gotDecided, tc.wantPercent, tc.wantDecided)
			}
		})
	}
}

// TestApplyIndexGCPercentActuallyApplies asserts the decision reaches the Go
// runtime — that the GC percent in force after applyIndexGCPercent equals the
// policy value, not merely that a pure function returned it.
//
// debug.SetGCPercent returns the PREVIOUS value, so a single call both reads
// the current setting and restores the one we want.
func TestApplyIndexGCPercentActuallyApplies(t *testing.T) {
	orig := debug.SetGCPercent(100)
	t.Cleanup(func() { debug.SetGCPercent(orig) })

	t.Run("background child is capped at the policy value", func(t *testing.T) {
		debug.SetGCPercent(100)
		applyIndexGCPercent(backgroundIndexCommand, "", "")
		if got := debug.SetGCPercent(100); got != indexGCPercentDefault {
			t.Fatalf("GOGC in force after applyIndexGCPercent = %d, want %d", got, indexGCPercentDefault)
		}
	})

	t.Run("operator override is what actually reaches the runtime", func(t *testing.T) {
		debug.SetGCPercent(100)
		applyIndexGCPercent(backgroundIndexCommand, "35", "")
		if got := debug.SetGCPercent(100); got != 35 {
			t.Fatalf("GOGC in force after applyIndexGCPercent = %d, want 35", got)
		}
	})

	t.Run("interactive path is left untouched", func(t *testing.T) {
		debug.SetGCPercent(100)
		applyIndexGCPercent("index", "", "")
		if got := debug.SetGCPercent(100); got != 100 {
			t.Fatalf("interactive path changed GOGC to %d, want it left at 100", got)
		}
	})

	t.Run("explicit GOGC is left untouched", func(t *testing.T) {
		debug.SetGCPercent(100)
		applyIndexGCPercent(backgroundIndexCommand, "", "200")
		if got := debug.SetGCPercent(100); got != 100 {
			t.Fatalf("explicit GOGC was overridden to %d, want it left at 100", got)
		}
	})
}

// TestIndexGCPercentDefaultBoundsTheMeasuredLiveHeap derives the default from
// the hazard rather than restating the constant.
//
// GOGC is a RELATIVE target: next_gc = live * (1 + GOGC/100). Two bounds
// follow from the measured corpus figures (see measuredLiveHeapPeakMB):
//
//   - It must be low enough to actually reduce peak RSS. At GOGC=100 the
//     measured heap_sys was ~3679MB against a ~1707MB live peak. Anything
//     that does not bring the steady-state target meaningfully below that is
//     a no-op change.
//   - It must be high enough that the collector is not running continuously.
//     Below ~25% the mark cycle re-runs before the mutator has produced a
//     working set worth collecting.
func TestIndexGCPercentDefaultBoundsTheMeasuredLiveHeap(t *testing.T) {
	targetMB := measuredLiveHeapPeakMB * (100 + indexGCPercentDefault) / 100
	if targetMB >= measuredUnlimitedHeapSysMB {
		t.Fatalf("GOGC=%d gives a steady-state target of %dMB against a measured unlimited heap_sys of %dMB — no reduction",
			indexGCPercentDefault, targetMB, measuredUnlimitedHeapSysMB)
	}
	// Require at least a 20% reduction; below that the added GC CPU is not
	// worth the change.
	if targetMB > measuredUnlimitedHeapSysMB*80/100 {
		t.Fatalf("GOGC=%d gives %dMB, less than a 20%% reduction on the measured %dMB",
			indexGCPercentDefault, targetMB, measuredUnlimitedHeapSysMB)
	}
	if indexGCPercentDefault < 25 {
		t.Fatalf("GOGC=%d is below the continuous-collection floor of 25", indexGCPercentDefault)
	}
}
