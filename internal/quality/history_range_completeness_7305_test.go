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
	"slices"
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

// unboundedCountFields lists, by name, every numericRanges row whose hi is
// noUpperBound. It is spelled out rather than derived so that a row LOSING its
// unbounded hi is a failure instead of one fewer subtest: selecting the rows
// dynamically and only checking that some were found proves something was
// enlarged past, not that everything unbounded was — which is #7305's own
// defect one level down.
//
// Adding a count field means adding its name here. That is the intended cost:
// it asks whoever adds it to say out loud that the field is a cardinality with
// no ceiling this package can justify, rather than inheriting that by copying a
// neighbouring row.
var unboundedCountFields = []string{
	"total_entities", "total_flows", "total_endpoints", "cycles", "auth_uncovered", "secrets",
}

// TestReadHistoryAcceptsUnboundedCount grades `hi: noUpperBound`.
//
// The exact-set assertion is what grades the sentinel, not the magnitude the
// subtests plant. Replace noUpperBound with any finite number and no row is
// selected, so the set comparison fails naming all six; give one row a finite
// hi and the set fails naming that row. The math.MaxInt below kills neither of
// those — a row with a finite hi is simply not selected, so nothing ever
// compares MaxInt against it. It is the behavioural read-through: ReadHistory
// does return an entry carrying the largest count the field can hold, which is
// what "no ceiling" has to mean at the only place it is observable.
func TestReadHistoryAcceptsUnboundedCount(t *testing.T) {
	var unbounded []string
	for _, r := range numericRanges {
		if math.IsInf(r.hi, 1) {
			unbounded = append(unbounded, r.name)
		}
	}
	got, want := slices.Clone(unbounded), slices.Clone(unboundedCountFields)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("rows with an unbounded hi = %v, want %v — a row that lost its unbounded hi has a ceiling nothing enlarges past, and a row that gained one is not enlarged past by this test until it is listed in unboundedCountFields", got, want)
	}

	for _, name := range unboundedCountFields {
		t.Run(name, func(t *testing.T) {
			line := inRangeLine()
			line[name] = math.MaxInt
			root := writeHistoryLine(t, line)
			got, err := ReadHistory(root, completenessGroup, 7)
			if err != nil {
				t.Fatalf("ReadHistory: %v", err)
			}
			if len(got) != 1 {
				t.Fatalf("%s=%d was rejected: got %d entries, want 1 — a count the field can hold did not survive the read", name, math.MaxInt, len(got))
			}
		})
	}
}

// numericFieldTags returns the name a history line can carry a number into for
// every HealthEntry field that can carry one: exported (encoding/json cannot
// populate the others) and of an integer or floating-point kind after at most
// one pointer indirection — the "omitted means not measured" encoding this
// struct uses for its optional fields.
//
// The name is the json tag, or the GO FIELD NAME when there is no tag:
// encoding/json falls back to the field name, so an UNTAGGED exported field is
// decodable and would ship unvalidated, not unreachable. Only `json:"-"` is
// genuinely unreachable, and only that is skipped.
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
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name := strings.Split(tag, ",")[0]
		if name == "" {
			name = f.Name
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

// TestDecoderPopulatesUntaggedField pins the premise numericFieldTags rests
// on. encoding/json matches a field with no tag by its Go name, so an untagged
// exported numeric field on HealthEntry would decode from a history line and
// ship unvalidated — which is why the walk names such a field rather than
// skipping it. Checked rather than assumed: a wrong justification is worse than
// none, because it stops the next reader looking.
func TestDecoderPopulatesUntaggedField(t *testing.T) {
	var v struct {
		Group    string `json:"group"`
		NewCount int
	}
	if err := json.Unmarshal([]byte(`{"group":"g","NewCount":-999999}`), &v); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if v.NewCount != -999999 {
		t.Fatalf("untagged NewCount = %d, want -999999 — if an untagged field cannot be populated, numericFieldTags need not name one", v.NewCount)
	}
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
