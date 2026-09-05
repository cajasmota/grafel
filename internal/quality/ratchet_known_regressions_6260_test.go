package quality

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// These tests drive scripts/quality/ratchet.py directly, because the property
// under test is a property of the GENERATOR, not of the checked-in JSON.
//
// known_regressions is the only thing distinguishing "this floor was always
// this low" from "this floor was lowered deliberately and is tracked". Before
// this was wired up, `--update-baseline` rebuilt the document from scratch and
// silently dropped the block — verified by running it — which meant the
// annotation survived exactly until the next re-record and then the drop looked
// like an ordinary floor again. Asserting on the committed baseline.json cannot
// catch that; only running the generator can.

// ratchetScript locates scripts/quality/ratchet.py from this package's dir.
func ratchetScript(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "..", "scripts", "quality", "ratchet.py"))
	if err != nil {
		t.Fatalf("resolve ratchet.py: %v", err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("ratchet.py not found at %s: %v", p, err)
	}
	return p
}

// ratchetFixture lays out the minimum on-disk shape `ratchet.py update` reads:
// a golden dir with one fixture carrying an expected.json, a reports dir with
// one stamped report, and a baseline to carry forward from.
type ratchetFixture struct {
	golden, reports, baseline, stamp string
}

func newRatchetFixture(t *testing.T, entityFound int, priorKnown []map[string]any) ratchetFixture {
	t.Helper()
	root := t.TempDir()
	golden := filepath.Join(root, "golden")
	reports := filepath.Join(root, "reports")
	if err := os.MkdirAll(filepath.Join(golden, "demo-mini"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(reports, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(path string, v any) {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(golden, "demo-mini", "expected.json"), map[string]any{
		"fixture_name": "demo-mini",
		"expected_entities": []map[string]any{
			{"name": "A", "kind": "SCOPE.Component", "must_exist": true},
		},
		// #6490: the key must be DECLARED, even when empty.
		"expected_relationships": []map[string]any{},
	})

	const stamp = "test-stamp-6260"
	write(filepath.Join(reports, "demo-mini.json"), map[string]any{
		"fixture":               "demo-mini",
		"run_stamp":             stamp,
		"entity_found":          entityFound,
		"entity_expected":       10,
		"relationship_found":    0,
		"relationship_expected": 0,
		"forbidden_hits":        0,
		// #6488 arm D: `check` and `update` both read the extracted totals, and
		// a report without them is a refusal, not a pass — so this synthetic
		// report carries what a real one does.
		"entity_extracted_total":       5,
		"relationship_extracted_total": 3,
	})

	baseline := filepath.Join(root, "baseline.json")
	write(baseline, map[string]any{
		"version":           1,
		"regenerate":        "scripts/quality/run.sh --runs 1 --update-baseline",
		"fixtures":          map[string]any{"demo-mini": map[string]any{"entity_found": 4, "entity_expected": 10, "relationship_found": 0, "relationship_expected": 0, "entity_extracted_total": 5, "relationship_extracted_total": 3}},
		"known_regressions": priorKnown,
	})
	return ratchetFixture{golden: golden, reports: reports, baseline: baseline, stamp: stamp}
}

// withPriorKey sets an extra top-level key on the baseline that `update` will
// rebuild from, so a test can assert whether the rebuild preserves it.
func (f ratchetFixture) withPriorKey(t *testing.T, key string, val any) ratchetFixture {
	t.Helper()
	raw, err := os.ReadFile(f.baseline)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	doc[key] = val
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.baseline, out, 0o644); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f ratchetFixture) update(t *testing.T) map[string]any {
	t.Helper()
	cmd := exec.Command("python3", ratchetScript(t), "update", f.reports, f.golden, f.baseline)
	cmd.Env = append(os.Environ(), "QUALITY_RUN_STAMP="+f.stamp)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ratchet.py update failed: %v\n%s", err, out)
	}
	raw, err := os.ReadFile(f.baseline)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse rebuilt baseline: %v", err)
	}
	return doc
}

func knownList(t *testing.T, doc map[string]any) []map[string]any {
	t.Helper()
	raw, ok := doc["known_regressions"]
	if !ok {
		t.Fatalf("rebuilt baseline has no known_regressions key at all: %v", doc)
	}
	items, _ := raw.([]any)
	out := make([]map[string]any, 0, len(items))
	for _, it := range items {
		m, _ := it.(map[string]any)
		out = append(out, m)
	}
	return out
}

func requirePython3(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not on PATH; ratchet.py cannot be exercised")
	}
}

// A tracked regression must survive a re-record. This is the mutation that
// matters: without the carry-forward, --update-baseline emits a baseline with
// no known_regressions and the drop reverts to an unexplained number.
func TestRatchetUpdateCarriesKnownRegressions_6260(t *testing.T) {
	requirePython3(t)
	// Observed 4, historic high 9 — still regressed.
	f := newRatchetFixture(t, 4, []map[string]any{{
		"fixture": "demo-mini", "metric": "entity_found",
		"was": 9, "now": 4, "issue": "#1", "note": "n",
	}})
	got := knownList(t, f.update(t))
	if len(got) != 1 {
		t.Fatalf("known_regressions = %v, want the one entry carried forward", got)
	}
	if got[0]["fixture"] != "demo-mini" || got[0]["metric"] != "entity_found" {
		t.Errorf("carried entry lost its identity: %v", got[0])
	}
	if int(got[0]["was"].(float64)) != 9 {
		t.Errorf("was = %v, want 9 preserved — the historic high is the fact the bare floor loses", got[0]["was"])
	}
	if got[0]["issue"] != "#1" || got[0]["note"] != "n" {
		t.Errorf("carried entry lost its issue/note: %v", got[0])
	}
}

// A regression that has been fixed must stop being annotated, or the block
// rots into prose that outlives the defect it describes.
func TestRatchetUpdateDropsRecoveredRegression_6260(t *testing.T) {
	requirePython3(t)
	// Observed 9, historic high 9 — recovered.
	f := newRatchetFixture(t, 9, []map[string]any{{
		"fixture": "demo-mini", "metric": "entity_found",
		"was": 9, "now": 4, "issue": "#1", "note": "n",
	}})
	if got := knownList(t, f.update(t)); len(got) != 0 {
		t.Fatalf("known_regressions = %v, want empty once the figure reached its recorded high", got)
	}
}

// measured_on_note documents what the measured_on sha means — that it is the
// BASE commit whose behaviour the figures describe, which for a change that
// alters extraction is not the same as the commit the file lands in.
//
// It was lost once already: `build()` rewrites the document from a fixed key
// set, so the note silently vanished on the first --update-baseline of this
// change and the field went unexplained at the same moment its value became
// non-obvious. Carry it, or it will go again.
func TestRatchetUpdateCarriesMeasuredOnNote_6260(t *testing.T) {
	requirePython3(t)
	const note = "base commit; figures include the change under test"
	f := newRatchetFixture(t, 4, nil).withPriorKey(t, "measured_on_note", note)
	doc := f.update(t)
	if got := doc["measured_on_note"]; got != note {
		t.Fatalf("measured_on_note = %v, want %q carried across the rebuild", got, note)
	}
}

// An entry whose figure moved again is restated from the observation, so the
// block can never disagree with the numbers three lines above it.
func TestRatchetUpdateRestatesMovedRegression_6260(t *testing.T) {
	requirePython3(t)
	// Observed 4, but the entry still claims now=6.
	f := newRatchetFixture(t, 4, []map[string]any{{
		"fixture": "demo-mini", "metric": "entity_found",
		"was": 9, "now": 6, "issue": "#1", "note": "n",
	}})
	got := knownList(t, f.update(t))
	if len(got) != 1 {
		t.Fatalf("known_regressions = %v, want one entry", got)
	}
	if n := int(got[0]["now"].(float64)); n != 4 {
		t.Errorf("now = %d, want 4 restated from the observed report", n)
	}
}
