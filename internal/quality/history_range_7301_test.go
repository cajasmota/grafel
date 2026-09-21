package quality_test

// history_range_7301_test.go — ReadHistory range-checks every numeric field
// and SKIPS the offending line (#7301).
//
// The trap this file is written around: an unvalidated parser passes an
// absence-assertion identically whether the guard is enforced or unreachable.
// "a well-formed file still reads correctly" passes with no guard at all. So
// every row here plants a VIOLATING line and requires the guard to fire, and
// every row carries a positive control — the same line with an in-range value
// — so a rejection caused by a malformed fixture cannot be mistaken for a
// rejection caused by the range.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/quality"
)

const rangeGroup = "rg"

// goodLine is a fully-populated, entirely in-range history record. Every
// numeric field is present so that overriding exactly one of them isolates
// that field: a fixture leaving a field absent would grade nothing about it.
func goodLine(ts time.Time, entities int) map[string]any {
	return map[string]any{
		"timestamp":       ts.Format(time.RFC3339Nano),
		"group":           rangeGroup,
		"total_entities":  entities,
		"total_flows":     3,
		"total_endpoints": 4,
		"orphan_rate":     12.5,
		"bug_rate":        2.5,
		"health_score":    85.0,
		"coverage_pct":    50.0,
		"recall_pct":      60.0,
		"cycles":          1,
		"auth_uncovered":  2,
		"secrets":         0,
	}
}

func writeLines(t *testing.T, objs ...map[string]any) string {
	t.Helper()
	root := t.TempDir()
	var buf []byte
	for _, o := range objs {
		b, err := json.Marshal(o)
		if err != nil {
			t.Fatalf("marshal fixture: %v", err)
		}
		buf = append(buf, b...)
		buf = append(buf, '\n')
	}
	if err := os.WriteFile(filepath.Join(root, "health-history.jsonl"), buf, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return root
}

// TestReadHistoryRejectsOutOfRangeField plants one out-of-range value per
// field and per bound. Each case is scored twice: the violating value must be
// dropped, and the in-range control on the same field must be kept.
func TestReadHistoryRejectsOutOfRangeField(t *testing.T) {
	cases := []struct {
		name  string
		field string
		bad   any
		// ok is an in-range value for the same field: the positive control
		// that proves the line shape itself is acceptable.
		ok any
	}{
		{"total_entities/lower", "total_entities", -1, 7},
		{"total_flows/lower", "total_flows", -1, 7},
		{"total_endpoints/lower", "total_endpoints", -1, 7},

		{"orphan_rate/lower", "orphan_rate", -0.5, 0.0},
		{"orphan_rate/upper", "orphan_rate", 100.5, 100.0},
		{"bug_rate/lower", "bug_rate", -0.5, 0.0},
		{"bug_rate/upper", "bug_rate", 100.5, 100.0},
		{"health_score/lower", "health_score", -0.5, 0.0},
		{"health_score/upper", "health_score", 100.5, 100.0},
		{"coverage_pct/lower", "coverage_pct", -0.5, 0.0},
		{"coverage_pct/upper", "coverage_pct", 100.5, 100.0},
		{"recall_pct/lower", "recall_pct", -0.5, 0.0},
		{"recall_pct/upper", "recall_pct", 100.5, 100.0},

		{"cycles/lower", "cycles", -1, 7},
		{"auth_uncovered/lower", "auth_uncovered", -1, 7},
		{"secrets/lower", "secrets", -1, 7},
	}

	now := time.Now().UTC()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bad := goodLine(now, 111)
			bad[tc.field] = tc.bad
			root := writeLines(t, bad)
			got, err := quality.ReadHistory(root, rangeGroup, 7)
			if err != nil {
				t.Fatalf("ReadHistory: %v", err)
			}
			if len(got) != 0 {
				t.Fatalf("%s=%v was accepted: got %d entries, want 0 (the guard did not fire)", tc.field, tc.bad, len(got))
			}

			// Positive control: the same line, in range, is read. Without
			// this the case above would also pass if the fixture were
			// simply unparseable.
			ctl := goodLine(now, 111)
			ctl[tc.field] = tc.ok
			root = writeLines(t, ctl)
			got, err = quality.ReadHistory(root, rangeGroup, 7)
			if err != nil {
				t.Fatalf("ReadHistory (control): %v", err)
			}
			if len(got) != 1 {
				t.Fatalf("control %s=%v was rejected: got %d entries, want 1", tc.field, tc.ok, len(got))
			}
		})
	}
}

// TestReadHistoryOutOfRangeSkipDoesNotTruncate pins that the guard `continue`s
// rather than `break`s: a bad line in the MIDDLE of the file must not cost the
// entries after it. A single-bad-line fixture cannot tell the two apart.
func TestReadHistoryOutOfRangeSkipDoesNotTruncate(t *testing.T) {
	now := time.Now().UTC()
	first := goodLine(now.Add(-2*time.Hour), 100)
	bad := goodLine(now.Add(-1*time.Hour), 200)
	bad["orphan_rate"] = -1.0
	last := goodLine(now, 300)

	root := writeLines(t, first, bad, last)
	got, err := quality.ReadHistory(root, rangeGroup, 7)
	if err != nil {
		t.Fatalf("ReadHistory: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2 (good, bad, good — the bad line must be skipped, not end the read)", len(got))
	}
	if got[0].TotalEntities != 100 {
		t.Errorf("first entry TotalEntities = %d, want 100", got[0].TotalEntities)
	}
	if got[1].TotalEntities != 300 {
		t.Errorf("second entry TotalEntities = %d, want 300 (the entry AFTER the bad line was lost)", got[1].TotalEntities)
	}
}

// TestReadHistoryKeepsUnmeasuredPointerFields pins that a nil pointer is "not
// measured", not "out of range" — the distinction #7283/#7287/#7292 exist to
// preserve. Both an absent key and an explicit null must survive.
func TestReadHistoryKeepsUnmeasuredPointerFields(t *testing.T) {
	ptrFields := []string{"bug_rate", "health_score", "coverage_pct", "recall_pct", "cycles", "auth_uncovered", "secrets"}
	now := time.Now().UTC()

	t.Run("absent", func(t *testing.T) {
		e := goodLine(now, 5)
		for _, f := range ptrFields {
			delete(e, f)
		}
		root := writeLines(t, e)
		got, err := quality.ReadHistory(root, rangeGroup, 7)
		if err != nil {
			t.Fatalf("ReadHistory: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("entry with no measured rates was dropped: got %d entries, want 1", len(got))
		}
		if got[0].BugRate != nil || got[0].HealthScore != nil || got[0].CoveragePct != nil ||
			got[0].RecallPct != nil || got[0].Cycles != nil || got[0].AuthUncovered != nil || got[0].Secrets != nil {
			t.Errorf("unmeasured fields did not stay nil: %+v", got[0])
		}
	})

	for _, f := range ptrFields {
		t.Run("null/"+f, func(t *testing.T) {
			e := goodLine(now, 5)
			e[f] = nil
			root := writeLines(t, e)
			got, err := quality.ReadHistory(root, rangeGroup, 7)
			if err != nil {
				t.Fatalf("ReadHistory: %v", err)
			}
			if len(got) != 1 {
				t.Fatalf("explicit null %s was treated as out of range: got %d entries, want 1", f, len(got))
			}
		})
	}
}
