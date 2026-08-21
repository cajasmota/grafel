package quality

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These tests drive scripts/quality/run.sh and scripts/quality/ratchet.py as
// programs, because the property under test is a property of the RUNNER, not
// of any checked-in JSON.
//
// The defect (#6273), as it stood at 316ecada6 — line numbers below are that
// commit's, not the current file's: run.sh's fixture loop iterated
// internal/quality/golden/*/ unconditionally, and `grafel quality` refuses a
// directory with no expected.json (runQuality in cmd/grafel/quality.go calls
// quality.LoadFixture, which reads expected.json and errors when it is absent,
// before Index is ever started). run.sh swallowed that with `|| true`, the
// median aggregator then found no run*.json and exited 1, and only the strict
// branch looked at that exit — a branch whose own header comment records that
// the strict gate has never been green across the full fixture set, i.e. it is
// never the mode anyone runs. Under --ratchet the per-fixture exit was
// discarded outright, and ratchet.py's check() then classified the same fixture
// as an approved `expectations_missing` entry. Two of twenty directories were
// therefore never measured and never mentioned.
//
// Everything asserted here is asserted with absolute counts and exact exit
// codes. "ratchet check exits 0" is not evidence: --update-baseline re-records
// whatever it observes and --ratchet then agrees with it.

// exitStatusUngraded mirrors the code run.sh reserves for "a fixture directory
// yielded no measurement". It is deliberately distinct from 2 (a fixture ran
// and regressed) and 1 (runner/build error).
const exitStatusUngraded = 3

// runShHarness is a hermetic copy of the tree run.sh navigates. run.sh derives
// its ROOT as dirname($0)/../.., so laying the scripts out under
// <tmp>/scripts/quality/ makes <tmp> the root and <tmp>/internal/quality/golden
// the fixture set it iterates — no reference to the real golden fixtures, and
// no indexing.
type runShHarness struct {
	root, golden, reports string
}

func newRunShHarness(t *testing.T) runShHarness {
	t.Helper()
	root := t.TempDir()
	scripts := filepath.Join(root, "scripts", "quality")
	golden := filepath.Join(root, "internal", "quality", "golden")
	reports := filepath.Join(root, "reports", "quality")
	for _, d := range []string{scripts, golden, reports} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"run.sh", "ratchet.py"} {
		src, err := filepath.Abs(filepath.Join("..", "..", "scripts", "quality", name))
		if err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(src)
		if err != nil {
			t.Fatalf("read %s: %v", src, err)
		}
		if err := os.WriteFile(filepath.Join(scripts, name), raw, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return runShHarness{root: root, golden: golden, reports: reports}
}

// addFixture creates <golden>/<name>/src/ and, when withExpectations, an
// expected.json declaring one must-have entity.
func (h runShHarness) addFixture(t *testing.T, name string, withExpectations bool) {
	t.Helper()
	dir := filepath.Join(h.golden, name)
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !withExpectations {
		return
	}
	raw, err := json.Marshal(map[string]any{
		"fixture_name": name,
		"expected_entities": []map[string]any{
			{"name": "A", "kind": "SCOPE.Component", "must_exist": true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "expected.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeBaseline records every fixture named in `gated` at 1/1 entity recall,
// and every fixture named in `excused` as {"expectations_missing": true}.
//
// The `excused` half is what makes these tests non-vacuous. ratchet.py check()
// already failed a fixture that has a numeric baseline entry but no
// expected.json (the "expectations file was deleted" branch), and it already
// failed one with no baseline entry at all — so a test that simply omitted the
// ungraded fixture from the baseline would go red on the pre-existing
// behaviour, and prove nothing about this change.
// `expectations_missing: true` is the state the gate accepted, and it is the
// state the golden baseline actually recorded for both real fixtures.
func (h runShHarness) writeBaseline(t *testing.T, gated []string, excused ...string) {
	t.Helper()
	fixtures := map[string]any{}
	for _, n := range gated {
		fixtures[n] = map[string]any{
			"entity_found": 1, "entity_expected": 1,
			"relationship_found": 0, "relationship_expected": 0,
		}
	}
	for _, n := range excused {
		fixtures[n] = map[string]any{"expectations_missing": true}
	}
	raw, err := json.Marshal(map[string]any{
		"version":    1,
		"regenerate": "scripts/quality/run.sh --runs 1 --update-baseline",
		"fixtures":   fixtures,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.golden, "baseline.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

// stubBin writes a fake `grafel` that answers `quality --json <path> <dir>` by
// emitting a 1/1 report — except for fixtures named in silent, for which it
// writes nothing and exits 1. That reproduces the second shape of the defect: a
// fixture that HAS expected.json but whose run produced no measurement.
//
// The stub is a shell script; run.sh only ever execs "$BIN" as
// `"$BIN" quality --json "$rjson" "$fix"`, so nothing else about the real
// binary is needed.
func (h runShHarness) stubBin(t *testing.T, silent ...string) string {
	t.Helper()
	var guards strings.Builder
	for _, s := range silent {
		fmt.Fprintf(&guards, "case \"$fixdir\" in *%s*) exit 1 ;; esac\n", s)
	}
	script := `#!/usr/bin/env bash
out=""
fixdir=""
shift # drop the "quality" subcommand
while [[ $# -gt 0 ]]; do
  case "$1" in
    --json) out="$2"; shift 2 ;;
    *) fixdir="$1"; shift ;;
  esac
done
` + guards.String() + `
cat > "$out" <<EOF
{"fixture":"stub","entity_expected":1,"entity_found":1,"entity_recall":1.0,
 "relationship_expected":0,"relationship_found":0,"relationship_recall":0.0,
 "forbidden_hits":0}
EOF
`
	p := filepath.Join(h.root, "stub-grafel")
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// run executes run.sh and returns (exit code, combined output).
func (h runShHarness) run(t *testing.T, bin string, args ...string) (int, string) {
	t.Helper()
	cmd := exec.Command("bash", append([]string{filepath.Join(h.root, "scripts", "quality", "run.sh")}, args...)...)
	cmd.Env = append(os.Environ(),
		"GRAFEL_BIN="+bin,
		"QUALITY_OUT_DIR="+h.reports,
	)
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("run.sh could not be executed: %v\n%s", err, out)
	}
	return code, string(out)
}

func requireBash(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH; run.sh cannot be exercised")
	}
}

// TestRunShFailsOnFixtureWithoutExpectations_6273 is the defect itself. A
// fixture directory carrying src/ and no expected.json must not pass through
// the runner as if it had been graded.
//
// The mode under test is --ratchet, not strict, deliberately: --ratchet is the
// gate run.sh's own header describes as the one that can actually be enforced,
// and it is the mode in which the per-fixture exit was discarded entirely.
func TestRunShFailsOnFixtureWithoutExpectations_6273(t *testing.T) {
	requireBash(t)
	requirePython3(t)
	h := newRunShHarness(t)
	h.addFixture(t, "graded-mini", true)
	h.addFixture(t, "ungraded-mini", false)
	h.writeBaseline(t, []string{"graded-mini"}, "ungraded-mini")

	code, out := h.run(t, h.stubBin(t), "--runs", "1", "--ratchet")
	if code != exitStatusUngraded {
		t.Fatalf("exit = %d, want %d (ungraded fixture)\n%s", code, exitStatusUngraded, out)
	}
	if !strings.Contains(out, "ungraded-mini") {
		t.Errorf("output does not name the ungraded fixture:\n%s", out)
	}
	if strings.Contains(out, "quality ratchet OK") {
		t.Errorf("the ratchet reported a verdict on an incomplete run:\n%s", out)
	}
}

// TestRunShFailsOnFixtureThatProducesNoReport_6273 covers the other way a
// directory can yield no measurement: it has expected.json, but the run over it
// produced nothing. Before this change that was visible only in strict mode,
// where it was also indistinguishable from a recall regression.
func TestRunShFailsOnFixtureThatProducesNoReport_6273(t *testing.T) {
	requireBash(t)
	requirePython3(t)
	h := newRunShHarness(t)
	h.addFixture(t, "graded-mini", true)
	h.addFixture(t, "broken-mini", true)
	h.writeBaseline(t, []string{"graded-mini", "broken-mini"})

	code, out := h.run(t, h.stubBin(t, "broken-mini"), "--runs", "1", "--ratchet")
	if code != exitStatusUngraded {
		t.Fatalf("exit = %d, want %d (fixture produced no report)\n%s", code, exitStatusUngraded, out)
	}
	if !strings.Contains(out, "broken-mini") {
		t.Errorf("output does not name the unmeasured fixture:\n%s", out)
	}
}

// TestRunShPassesWhenEveryFixtureIsGraded_6273 is the control. A guard that
// fires on every run is not a guard, and would make the two tests above pass
// for the wrong reason.
func TestRunShPassesWhenEveryFixtureIsGraded_6273(t *testing.T) {
	requireBash(t)
	requirePython3(t)
	h := newRunShHarness(t)
	h.addFixture(t, "graded-mini", true)
	h.addFixture(t, "other-mini", true)
	h.writeBaseline(t, []string{"graded-mini", "other-mini"})

	code, out := h.run(t, h.stubBin(t), "--runs", "1", "--ratchet")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 when every fixture is graded\n%s", code, out)
	}
	if !strings.Contains(out, "2 gated fixtures held their baseline") {
		t.Errorf("ratchet did not report the expected gated count:\n%s", out)
	}
}

// TestRunShRefusesUpdateBaselineOnIncompleteRun_6273 closes the route by which
// the silence became permanent: --update-baseline re-recorded the ungraded
// state as an approved `expectations_missing` entry (ratchet.py build()), after
// which --ratchet agreed with it forever.
func TestRunShRefusesUpdateBaselineOnIncompleteRun_6273(t *testing.T) {
	requireBash(t)
	requirePython3(t)
	h := newRunShHarness(t)
	h.addFixture(t, "graded-mini", true)
	h.addFixture(t, "ungraded-mini", false)
	h.writeBaseline(t, []string{"graded-mini"}, "ungraded-mini")

	before, err := os.ReadFile(filepath.Join(h.golden, "baseline.json"))
	if err != nil {
		t.Fatal(err)
	}
	code, out := h.run(t, h.stubBin(t), "--runs", "1", "--update-baseline")
	if code != exitStatusUngraded {
		t.Fatalf("exit = %d, want %d\n%s", code, exitStatusUngraded, out)
	}
	after, err := os.ReadFile(filepath.Join(h.golden, "baseline.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("baseline was rewritten from an incomplete run:\nbefore %s\nafter  %s", before, after)
	}
}

// TestRatchetCheckFailsOnFixtureWithoutExpectations_6273 pins the same rule one
// level down. run.sh is not the only caller — ratchet.py is invoked directly by
// the tests in this package — and before this change check() actively excused a
// missing expected.json (the branch that read `expectations_missing` from the
// baseline and continued) instead of failing on it.
func TestRatchetCheckFailsOnFixtureWithoutExpectations_6273(t *testing.T) {
	requirePython3(t)
	h := newRunShHarness(t)
	h.addFixture(t, "ungraded-mini", false)
	h.writeBaseline(t, nil, "ungraded-mini")

	cmd := exec.Command("python3", ratchetScript(t), "check", h.reports,
		h.golden, filepath.Join(h.golden, "baseline.json"))
	cmd.Env = append(os.Environ(), "QUALITY_RUN_STAMP=test-stamp-6273")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("ratchet check passed on a fixture with no expected.json:\n%s", out)
	}
	if !strings.Contains(string(out), "expected.json") {
		t.Errorf("failure does not name the cause:\n%s", out)
	}
}

// TestRatchetUpdateRefusesFixtureWithoutExpectations_6273 covers build(), which
// is a SEPARATE guard from the check() one above and was reachable by no test at
// all when this file was first written.
//
// The gap was real and was found by mutation: reverting build()'s SystemExit
// back to `fixtures[name] = {"expectations_missing": True}; continue` left
// `go test ./internal/quality` fully green.
// TestRunShRefusesUpdateBaselineOnIncompleteRun_6273 does not reach it — run.sh
// exits 3 before `ratchet.py update` is ever invoked — so the only way to
// exercise build() is to call it directly, which is what this does.
//
// build() matters independently of check(): it is the half that WRITES the
// baseline, and recording {"expectations_missing": true} is precisely how the
// two directories survived every re-record for as long as they did.
func TestRatchetUpdateRefusesFixtureWithoutExpectations_6273(t *testing.T) {
	requirePython3(t)
	h := newRunShHarness(t)
	h.addFixture(t, "ungraded-mini", false)
	h.writeBaseline(t, nil, "ungraded-mini")
	baseline := filepath.Join(h.golden, "baseline.json")

	before, err := os.ReadFile(baseline)
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("python3", ratchetScript(t), "update", h.reports, h.golden, baseline)
	cmd.Env = append(os.Environ(), "QUALITY_RUN_STAMP=test-stamp-6273")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("ratchet update recorded a baseline for a fixture with no expected.json:\n%s", out)
	}
	if !strings.Contains(string(out), "expected.json") {
		t.Errorf("failure does not name the cause:\n%s", out)
	}

	after, err := os.ReadFile(baseline)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("baseline was rewritten despite the refusal:\nbefore %s\nafter  %s", before, after)
	}
}

// TestGoldenSetIsFullyGraded_6273 asserts the absolute figure the benchmark's
// denominators are quoted from. All three halves matter: golden/ holds exactly
// wantFixtures directories, every one carries expected.json, AND the baseline
// gates exactly that many. A test that only checked "no directory is ungraded"
// would pass on an empty golden/, and one that only counted baseline entries
// would pass on a baseline full of expectations_missing stubs.
//
// This subsumes ungradedFixtures + TestUngradedFixturesAreTheKnownTwo, which
// #6260 added and #6273 deleted. That set existed to make demotion to ungraded a
// reviewable act; with the category abolished, demotion is not reviewable but
// impossible, which is strictly stronger. It was verified subsumed by mutation
// before deletion: deleting a fixture's expected.json, flipping its baseline
// entry to {"expectations_missing": true}, AND adding its name to the set —
// the exact edit the set was built to catch — left its own test green and was
// caught here, on all three assertions.
func TestGoldenSetIsFullyGraded_6273(t *testing.T) {
	// 21 since #6327 S6 added vbnet-mini; 22 since #6424 added solidity-mini.
	//
	// solidity-mini is the first fixture that is graded and DELIBERATELY RED:
	// its expected.json records what the extractor should produce, and the six
	// recall defects of #6423 are encoded as must-haves that currently miss.
	// That is not a special case for this test — the recorded floor in
	// baseline.json is the ordinary mechanism (csharp-aspnet-core-mini has sat
	// at 4/5 entities since before this file existed), and the assertions
	// below are exactly the ones that stop the gap being closed by deleting
	// the fixture instead of fixing the extractor.
	//
	// 23 since #6433 added angular-http-mini — the first fixture that grades
	// OUTBOUND http client calls at all. Before it, nothing in golden/ asserted
	// a single SCOPE.ExternalAPI or consumer-side http_endpoint_call, so the
	// frontend half of cross-stack linking could sit at zero indefinitely
	// without any fixture noticing (it had).
	//
	// 24 since #6453 added proto-mini — the first fixture for the proto
	// extractor at all. Three proto defects (#6359, #6419, #6422) had been
	// fixed against a package with no golden, so every one of them could
	// regress silently. proto-mini carries the rpc/message name collision
	// #6422 turns on; see its NOTICE.md for the one shape it still cannot
	// grade and why.
	const wantFixtures = 24

	dirs := fixtureDirs(t)
	if len(dirs) != wantFixtures {
		t.Fatalf("golden/ holds %d fixture directories, want %d — if a fixture was "+
			"added or removed on purpose, update this number so the change is reviewed",
			len(dirs), wantFixtures)
	}
	for _, name := range dirs {
		if _, err := os.Stat(filepath.Join(goldenDir, name, "expected.json")); err != nil {
			t.Errorf("fixture %q has no expected.json — it would produce no report and "+
				"be graded by nothing", name)
		}
	}

	doc := loadBaseline(t)
	gated := 0
	for name, base := range doc.Fixtures {
		if base.ExpectationsMissing {
			t.Errorf("baseline records fixture %q as ungraded; golden/ has no ungraded "+
				"category any more", name)
			continue
		}
		gated++
		if base.EntityExpected == 0 && base.RelExpected == 0 {
			t.Errorf("fixture %q is gated on nothing", name)
		}
	}
	if gated != wantFixtures {
		t.Errorf("baseline gates %d fixtures, want %d", gated, wantFixtures)
	}
}
