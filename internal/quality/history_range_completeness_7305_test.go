package quality

// history_range_completeness_7305_test.go — the two directions #7301 shipped
// ungraded (#7305).
//
//   - A ceiling can only be graded by ENLARGING past it. Every fixture #7301
//     wrote sits below any plausible cap, so `hi: noUpperBound` could have been
//     any sufficiently large finite number with nothing noticing.
//   - Deleting a numericRanges row is graded by that field's own rows. ADDING a
//     numeric field to HealthEntry with no row was graded by nothing, which is
//     the shape that made #7301 possible: a parser accepting whatever it is
//     handed, with no structural reason anyone would notice.
//
// This file is in package quality because both tests read numericRanges: the
// first so a count field added later is enlarged past automatically rather
// than only the six that exist today, the second because the table is what it
// is comparing the struct against.

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const completenessGroup = "cg"

// inRangeLine is a fully-populated, entirely in-range history record, so that
// overriding exactly one key isolates that key.
func inRangeLine() map[string]any {
	return map[string]any{
		"timestamp":       time.Now().UTC().Format(time.RFC3339Nano),
		"group":           completenessGroup,
		"total_entities":  11,
		"total_flows":     3,
		"total_endpoints": 4,
		"orphan_rate":     12.5,
		"bug_rate":        2.5,
		"health_score":    85.0,
		"coverage_pct":    50.0,
		"recall_pct":      60.0,
		"cycles":          1,
		"auth_uncovered":  2,
		// Every count is non-zero so that this fixture grades the CEILING
		// only: a lower bound moved off zero is scored by the #7301 rows,
		// and a fixture sensitive to both would report one as the other.
		"secrets": 3,
	}
}

func writeHistoryLine(t *testing.T, o map[string]any) string {
	t.Helper()
	root := t.TempDir()
	b, err := json.Marshal(o)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "health-history.jsonl"), append(b, '\n'), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return root
}

// TestReadHistoryAcceptsUnboundedCount grades `hi: noUpperBound` by enlarging
// past it. The value is math.MaxInt — the largest number the Go field can hold
// at all — so the row survives only while its hi is genuinely not a number.
//
// The rows are selected from numericRanges rather than listed, so a count
// field added later is enlarged past by this test on the day it gets a row.
func TestReadHistoryAcceptsUnboundedCount(t *testing.T) {
	unbounded := 0
	for _, r := range numericRanges {
		if !math.IsInf(r.hi, 1) {
			continue
		}
		unbounded++
		t.Run(r.name, func(t *testing.T) {
			line := inRangeLine()
			line[r.name] = math.MaxInt
			root := writeHistoryLine(t, line)
			got, err := ReadHistory(root, completenessGroup, 7)
			if err != nil {
				t.Fatalf("ReadHistory: %v", err)
			}
			if len(got) != 1 {
				t.Fatalf("%s=%d was rejected: got %d entries, want 1 — hi stopped being an absence of a ceiling and became one", r.name, math.MaxInt, len(got))
			}
		})
	}
	if unbounded == 0 {
		t.Fatal("no row in numericRanges has an infinite hi, so this test enlarged past nothing")
	}
}

// numericFieldTags returns the JSON name of every HealthEntry field a history
// line can carry a number into: exported (encoding/json cannot populate the
// others), carrying a json tag, and of an integer or floating-point kind after
// at most one pointer indirection — the "omitted means not measured" encoding
// this struct uses for its optional fields.
//
// The filter is on the kind CLASS, not on the concrete types in use today, so
// a future int64/uint/float32 field is covered without editing it. Strings,
// bools and time.Time are excluded: numericRanges compares a float64 against
// two bounds, which those cannot supply.
func numericFieldTags(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf(HealthEntry{})
	var out []string
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if f.Anonymous {
			t.Fatalf("HealthEntry embeds %s, and this walk does not recurse: a numeric field inside it would escape numericRanges unseen. Extend the walk before embedding.", f.Name)
		}
		if f.PkgPath != "" {
			continue
		}
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if name == "" || name == "-" {
			continue
		}
		ft := f.Type
		if ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		switch ft.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
			reflect.Float32, reflect.Float64:
			out = append(out, name)
		}
	}
	return out
}

// missingRows returns the fields with no row among have, in field order.
func missingRows(fields []string, have map[string]bool) []string {
	var missing []string
	for _, f := range fields {
		if !have[f] {
			missing = append(missing, f)
		}
	}
	return missing
}

func rowNames(rows []numericRange, skip string) map[string]bool {
	have := map[string]bool{}
	for _, r := range rows {
		if r.name != skip {
			have[r.name] = true
		}
	}
	return have
}

// TestNumericRangesCoversEveryNumericField fails when a numeric field is added
// to HealthEntry without a numericRanges row — the direction #7301 left open,
// where the new field silently ships accepting any value ReadHistory is handed.
//
// The subtests are what keep it from passing vacuously. A walk that found no
// fields at all — a kind filter that missed a pointer, a `json:"x,omitempty"`
// tag parsed whole, the unexported skip firing too widely — reports "nothing
// missing" exactly as a healthy walk does. So every row is dropped in turn and
// the SAME comparison must name exactly the dropped field: that is one
// positive control per field, and it fails for any field the walk cannot see.
func TestNumericRangesCoversEveryNumericField(t *testing.T) {
	fields := numericFieldTags(t)

	if missing := missingRows(fields, rowNames(numericRanges, "")); len(missing) > 0 {
		t.Errorf("HealthEntry numeric fields with no numericRanges row: %v — ReadHistory accepts any value for them", missing)
	}

	for _, r := range numericRanges {
		t.Run("without/"+r.name, func(t *testing.T) {
			got := missingRows(fields, rowNames(numericRanges, r.name))
			if len(got) != 1 || got[0] != r.name {
				t.Fatalf("with the %q row dropped the walk reports missing=%v, want exactly [%s] — the walk cannot see that field, so its presence in the table is graded by nothing", r.name, got, r.name)
			}
		})
	}
}
