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

// TestUnmeasuredFieldSurvivesRangeExcludingZero pins the contingency that
// makes the `!measured` skip in outOfRangeField look redundant today.
//
// Every row in numericRanges currently has lo 0, so a nil pointer decoded as a
// plain 0 would be accepted anyway and dropping the skip changes nothing. That
// is a property of the table, not of the nil semantics — and nothing else
// asserts it. This installs two synthetic rows whose lo excludes zero, one per
// pointer helper, so the two helpers are scored as a pair rather than one of
// them standing in for the other.
func TestUnmeasuredFieldSurvivesRangeExcludingZero(t *testing.T) {
	saved := numericRanges
	defer func() { numericRanges = saved }()
	numericRanges = append(append([]numericRange{}, saved...),
		numericRange{name: "synthetic_float", lo: 1, hi: 100, value: func(e *HealthEntry) (float64, bool) {
			return fromFloatPtr(e.RecallPct)
		}},
		numericRange{name: "synthetic_int", lo: 1, hi: noUpperBound, value: func(e *HealthEntry) (float64, bool) {
			return fromIntPtr(e.Secrets)
		}},
	)

	// RecallPct and Secrets are nil: not measured, so no range applies to
	// them — not even one that excludes the zero value they decode to.
	e := HealthEntry{OrphanRate: 5}
	if got := outOfRangeField(&e); got != "" {
		t.Fatalf("outOfRangeField(entry with unmeasured %s) = %q, want \"\" — a nil pointer was treated as a measured 0", got, got)
	}

	// Control: the same rows do reject a MEASURED zero, so the rows are
	// live and the assertion above is not passing because lo 1 is inert.
	zero := 0.0
	e.RecallPct = &zero
	if got := outOfRangeField(&e); got != "synthetic_float" {
		t.Fatalf("outOfRangeField(recall_pct=0 under lo 1) = %q, want %q", got, "synthetic_float")
	}
	e.RecallPct = nil
	zeroInt := 0
	e.Secrets = &zeroInt
	if got := outOfRangeField(&e); got != "synthetic_int" {
		t.Fatalf("outOfRangeField(secrets=0 under lo 1) = %q, want %q", got, "synthetic_int")
	}
}
