package quality

// history_range_internal_7301_test.go — the range predicate itself, for the
// values no JSONL fixture can carry.
//
// NaN and ±Inf have no JSON literal, so they are unreachable through
// ReadHistory's decoder; the cases below reach outOfRangeField directly. The
// decoder's own handling of an overflowing literal is pinned here too rather
// than assumed.

import (
	"encoding/json"
	"math"
	"testing"
)

func TestOutOfRangeFieldRejectsNaNAndInf(t *testing.T) {
	cases := []struct {
		name string
		v    float64
	}{
		{"NaN", math.NaN()},
		{"+Inf", math.Inf(1)},
		{"-Inf", math.Inf(-1)},
	}
	for _, tc := range cases {
		t.Run("orphan_rate/"+tc.name, func(t *testing.T) {
			e := HealthEntry{OrphanRate: tc.v}
			if got := outOfRangeField(&e); got != "orphan_rate" {
				t.Fatalf("outOfRangeField(orphan_rate=%v) = %q, want %q", tc.v, got, "orphan_rate")
			}
		})
		t.Run("bug_rate/"+tc.name, func(t *testing.T) {
			v := tc.v
			e := HealthEntry{BugRate: &v}
			if got := outOfRangeField(&e); got != "bug_rate" {
				t.Fatalf("outOfRangeField(bug_rate=%v) = %q, want %q", tc.v, got, "bug_rate")
			}
		})
	}
}

// TestOutOfRangeFieldAcceptsInRangeEntry is the control for the cases above:
// the predicate is not simply rejecting everything.
func TestOutOfRangeFieldAcceptsInRangeEntry(t *testing.T) {
	bug, score := 0.0, 100.0
	e := HealthEntry{OrphanRate: 100, BugRate: &bug, HealthScore: &score, TotalEntities: 0}
	if got := outOfRangeField(&e); got != "" {
		t.Fatalf("outOfRangeField(in-range entry) = %q, want \"\"", got)
	}
}

// TestDecoderRejectsOverflowingFloatLiteral pins where ±Inf would otherwise
// enter: encoding/json fails the whole line on a literal that overflows
// float64, so ReadHistory's pre-existing unmarshal-error skip already covers
// it. Checked rather than assumed (#7301).
func TestDecoderRejectsOverflowingFloatLiteral(t *testing.T) {
	for _, lit := range []string{"1e999", "-1e999"} {
		var e HealthEntry
		if err := json.Unmarshal([]byte(`{"group":"g","orphan_rate":`+lit+`}`), &e); err == nil {
			t.Fatalf("json.Unmarshal accepted orphan_rate:%s, decoding to %v", lit, e.OrphanRate)
		}
	}
	// NaN/Infinity have no JSON literal at all.
	for _, lit := range []string{"NaN", "Infinity", "-Infinity"} {
		var e HealthEntry
		if err := json.Unmarshal([]byte(`{"group":"g","orphan_rate":`+lit+`}`), &e); err == nil {
			t.Fatalf("json.Unmarshal accepted orphan_rate:%s", lit)
		}
	}
}
