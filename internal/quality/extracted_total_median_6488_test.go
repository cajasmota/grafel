package quality

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// #6488 arm D. The extracted totals are gate metrics, so scripts/quality/run.sh
// must MEDIAN them across the repeat runs like every other gated scalar,
// instead of letting them ride along in `merged = dict(base)` from reports[-1].
//
// Why this is not a stylistic point: indexing is not deterministic run to run
// (a parallel measurement saw an entity total of 5461/5462/5463 across three
// indexes of identical trees). quality.yml is always-on and sets no
// QUALITY_RUNS, so CI grades at the default RUNS=5 against a baseline recorded
// with --runs 1. A high last-run outlier is then a spurious red, and a low one
// records a ceiling looser than the truth at --update-baseline time — which is
// the worse direction, because the gate goes on to pass what it exists to catch.
//
// The stub varies its totals per invocation so that the MEDIAN and the LAST run
// disagree. That disagreement is the whole test: a run.sh that inherits the
// last run's figure, and one that medians, are indistinguishable on a stub that
// answers the same thing every time.

// varyingStubBin writes a fake `grafel` whose per-run entity/relationship
// extracted totals are drawn in order from the given lists, counted through a
// file on disk. Recall figures are held constant at 1/1 so the fixture passes
// and nothing but the aggregation of the totals is under test.
//
// withTotals=false omits both keys entirely, which is how a binary predating
// the field behaves.
func varyingStubBin(t *testing.T, h runShHarness, entityTotals, relTotals []int, withTotals bool) string {
	t.Helper()
	if len(entityTotals) != len(relTotals) {
		t.Fatalf("test bug: %d entity totals but %d relationship totals",
			len(entityTotals), len(relTotals))
	}
	counter := filepath.Join(h.root, "stub-run-counter")
	var cases string
	for i := range entityTotals {
		cases += fmt.Sprintf("  %d) ent=%d rel=%d ;;\n", i+1, entityTotals[i], relTotals[i])
	}
	// A run beyond the supplied list is a test-bug, not a case to interpolate:
	// fail loudly rather than silently reusing the last pair.
	cases += fmt.Sprintf("  *) echo \"stub: run $n beyond the %d supplied\" >&2; exit 1 ;;\n",
		len(entityTotals))

	totals := ""
	if withTotals {
		totals = `,"entity_extracted_total":$ent,"relationship_extracted_total":$rel`
	}
	script := `#!/usr/bin/env bash
out=""
shift
while [[ $# -gt 0 ]]; do
  case "$1" in
    --json) out="$2"; shift 2 ;;
    *) shift ;;
  esac
done
n=$(cat "` + counter + `" 2>/dev/null || echo 0)
n=$((n + 1))
echo "$n" > "` + counter + `"
case "$n" in
` + cases + `esac
cat > "$out" <<JSON
{"fixture":"stub","entity_expected":1,"entity_found":1,"entity_recall":1.0,
 "relationship_expected":0,"relationship_found":0,"relationship_recall":0.0,
 "forbidden_hits":0` + totals + `}
JSON
`
	p := filepath.Join(h.root, "varying-stub-grafel")
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// mergedReport reads the per-fixture report run.sh wrote for `name`, decoded so
// that an absent key is distinguishable from a zero one.
func mergedReport(t *testing.T, h runShHarness, name string) map[string]json.RawMessage {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(h.reports, name+".json"))
	if err != nil {
		t.Fatalf("read merged report for %s: %v", name, err)
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("parse merged report for %s: %v", name, err)
	}
	return out
}

func mergedInt(t *testing.T, rep map[string]json.RawMessage, key string) int {
	t.Helper()
	raw, ok := rep[key]
	if !ok {
		t.Fatalf("merged report carries no %s", key)
	}
	var n int
	if err := json.Unmarshal(raw, &n); err != nil {
		t.Fatalf("merged report %s is not an integer: %v", key, err)
	}
	return n
}

// TestRunShMediansTheExtractedTotals_6488 runs three times over one fixture and
// asserts both totals equal the median of the three, not the last.
func TestRunShMediansTheExtractedTotals_6488(t *testing.T) {
	requireBash(t)
	requirePython3(t)
	h := newRunShHarness(t)
	h.addFixture(t, "graded-mini", true)

	// Deliberately ordered so median != last AND median != first: an
	// implementation that took either end scores differently from one that
	// medians. Entity median is 9 (of 5, 9, 90); relationship median is 4.
	bin := varyingStubBin(t, h, []int{5, 9, 90}, []int{3, 4, 40}, true)

	// Strict mode, not --ratchet: the recall figures are a constant 1/1, so the
	// fixture passes, and no baseline is consulted. What is under test is what
	// run.sh WROTE, so the gate's verdict would only be a second way to be
	// wrong about it.
	code, out := h.run(t, bin, "--runs", "3")
	if code != 0 {
		t.Fatalf("run.sh exit = %d, want 0 with a 1/1 stub\n%s", code, out)
	}
	rep := mergedReport(t, h, "graded-mini")
	if got := mergedInt(t, rep, "runs_executed"); got != 3 {
		t.Fatalf("runs_executed = %d, want 3 — the premise (three differing runs) "+
			"does not hold, so what follows grades nothing\n%s", got, out)
	}
	if got := mergedInt(t, rep, "entity_extracted_total"); got != 9 {
		t.Errorf("entity_extracted_total = %d, want the median 9 of {5,9,90} "+
			"(90 is the last run, which `merged = dict(base)` would inherit)", got)
	}
	if got := mergedInt(t, rep, "relationship_extracted_total"); got != 4 {
		t.Errorf("relationship_extracted_total = %d, want the median 4 of {3,4,40} "+
			"(40 is the last run)", got)
	}
}

// TestRunShOmitsExtractedTotalsItNeverMeasured_6488 pins the other half: when
// the binary emits no totals, the merged report must carry none either.
//
// The alternative — defaulting to 0 through med_int's default — would be worse
// than the absence: ratchet.py refuses a report with no totals and says why,
// while a fabricated 0 is a ceiling no real extraction can hold, so the gate
// would go red on the next honest run and get re-recorded away.
func TestRunShOmitsExtractedTotalsItNeverMeasured_6488(t *testing.T) {
	requireBash(t)
	requirePython3(t)
	h := newRunShHarness(t)
	h.addFixture(t, "graded-mini", true)
	bin := varyingStubBin(t, h, []int{0, 0}, []int{0, 0}, false)

	code, out := h.run(t, bin, "--runs", "2")
	if code != 0 {
		t.Fatalf("run.sh exit = %d, want 0 with a 1/1 stub\n%s", code, out)
	}
	rep := mergedReport(t, h, "graded-mini")
	for _, key := range []string{"entity_extracted_total", "relationship_extracted_total"} {
		if raw, ok := rep[key]; ok {
			t.Errorf("merged report carries %s = %s from a binary that emitted "+
				"neither total", key, raw)
		}
	}
}
