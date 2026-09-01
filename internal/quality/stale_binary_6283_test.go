package quality

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These tests drive scripts/quality/run.sh as a program, for the same reason
// the #6273 tests in ungraded_fixture_6273_test.go do: the property under test
// belongs to the RUNNER, not to any Go function or checked-in JSON.
//
// The defect (#6283), as it stood at b3319a3a6: run.sh resolved
// BIN="${GRAFEL_BIN:-$ROOT/build/grafel}" and then built only when that path
// did not exist —
//
//	if [[ ! -x "$BIN" ]]; then ... go build -o build/grafel ./cmd/grafel; fi
//
// so any binary already sitting at build/grafel was graded as-is, whatever
// source it had been built from. build/grafel is gitignored and long-lived on a
// developer machine, which is exactly where this gate ran: at the time,
// .github/workflows/quality.yml was workflow_dispatch-only, and when dispatched
// it built explicitly and passed GRAFEL_BIN — so CI never hit this, only humans
// did.
//
// #6231 has since made quality.yml always-on (pull_request + push to main). CI
// still does not hit the defect, for the unchanged reason: the workflow builds
// into a fresh runner workspace and passes GRAFEL_BIN explicitly, so there is
// never a stale binary to reuse. The dispatch-only property was never a premise
// of anything below — it explained who was exposed, not what is asserted — and
// the tests in this file are unaffected by the trigger change. The paragraph is
// corrected rather than deleted because "only humans hit this" is still the
// reason the defect survived as long as it did.
//
// Why it matters more than the two instrument bugs before it: this repo uses
// mutation testing to detect vacuous tests, and a mutant that "survives" is
// supposed to mean no test covers the mutated line. Grading a binary that
// predates the mutation makes every mutant look dead-or-alive at random. It was
// reported that way: during the #6277 review the benchmark was run at HEAD and
// again with internal/quality/diff.go's isPlaceholderAnchor mutated, and the
// two runs produced byte-identical reports. That episode is reported, not
// re-derived here; what these tests verify is the mechanism, which admits no
// source edit of any size into the graded binary.
//
// The fix is to build unconditionally rather than to detect staleness and
// refuse. Justification is in run.sh's own comment; the short form is that
// `go build` is the only staleness check that cannot go out of date, and a
// no-op rebuild is the same order of cost as the `go list -deps` a staleness
// check would need in order to know what to compare.

func requireGoToolchain(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH; run.sh cannot build")
	}
}

// writeStubModule turns the harness root into a buildable Go module whose
// ./cmd/grafel answers `quality --json <path> <dir>` by writing a one-must-have
// report claiming entityFound of 1 expected entity.
//
// It is a compiled program rather than the shell stub used by the #6273 tests
// because that is the whole point here: the number the runner reports must
// track this SOURCE, and the only way to observe that is to make the runner
// compile it.
func writeStubModule(t *testing.T, h runShHarness, entityFound int) {
	t.Helper()
	gomod := "module grafelstub\n\ngo 1.21\n"
	if err := os.WriteFile(filepath.Join(h.root, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(h.root, "cmd", "grafel")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	main := fmt.Sprintf(`package main

import (
	"fmt"
	"os"
)

// entityFound is the knob these tests turn. Rebuilding is what makes a change
// to it visible in the benchmark's verdict.
const entityFound = %d

func main() {
	out := ""
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		if args[i] == "--json" && i+1 < len(args) {
			out = args[i+1]
			i++
		}
	}
	if out == "" {
		os.Exit(1)
	}
	f, err := os.Create(out)
	if err != nil {
		os.Exit(1)
	}
	defer f.Close()
	fmt.Fprintf(f, `+"`"+`{"fixture":"stub","entity_expected":1,"entity_found":%%d,`+
		`"entity_recall":%%f,"relationship_expected":0,"relationship_found":0,`+
		`"relationship_recall":0.0,"forbidden_hits":0}`+"`"+`,
		entityFound, float64(entityFound))
}
`, entityFound)
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
}

// buildStub compiles the harness module into <root>/build/grafel — the exact
// path run.sh defaults to — the way an operator or CI would, and returns it.
func buildStub(t *testing.T, h runShHarness) string {
	t.Helper()
	bin := filepath.Join(h.root, "build", "grafel")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/grafel")
	cmd.Dir = h.root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building the stub module failed: %v\n%s", err, out)
	}
	return bin
}

// runDefaultBin executes run.sh with GRAFEL_BIN explicitly empty, so the
// default build/grafel path is exercised. os.Environ() is inherited, so an
// operator with GRAFEL_BIN exported in their shell must not be able to change
// what this test measures.
func runDefaultBin(t *testing.T, h runShHarness, args ...string) (int, string) {
	t.Helper()
	cmd := exec.Command("bash", append([]string{filepath.Join(h.root, "scripts", "quality", "run.sh")}, args...)...)
	cmd.Env = append(os.Environ(),
		"GRAFEL_BIN=",
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

// TestRunShGradesTheWorkingTreeNotAPrebuiltBinary_6283 is the defect, driven
// end to end: build a binary, change the source under it, run the benchmark,
// and require the verdict to move.
//
// Both halves are asserted in one test on purpose. The first run is the control
// — it pins that this harness CAN report a held baseline, so the second run's
// failure is attributable to the source edit and not to a harness that fails
// unconditionally. Splitting them would let the second half pass against a
// run.sh that simply always exits 2.
func TestRunShGradesTheWorkingTreeNotAPrebuiltBinary_6283(t *testing.T) {
	requireBash(t)
	requirePython3(t)
	requireGoToolchain(t)

	h := newRunShHarness(t)
	h.addFixture(t, "graded-mini", true)
	h.writeBaseline(t, []string{"graded-mini"})

	// The binary an operator already has on disk: it finds the must-have.
	writeStubModule(t, h, 1)
	bin := buildStub(t, h)

	code, out := runDefaultBin(t, h, "--runs", "1", "--ratchet")
	if code != 0 {
		t.Fatalf("control run: exit = %d, want 0 with a matching binary and baseline\n%s", code, out)
	}

	// Now change the source the binary was built from, the way any code change
	// under test does. Nothing touches build/grafel: it is now stale, and the
	// pre-#6283 runner would grade it and report the baseline held.
	writeStubModule(t, h, 0)
	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("build/grafel should still be on disk: %v", err)
	}

	code, out = runDefaultBin(t, h, "--runs", "1", "--ratchet")
	if code == 0 {
		t.Fatalf("run.sh reported a held baseline after the source stopped finding "+
			"the must-have — it graded the prebuilt binary, not the tree\n%s", out)
	}
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (recall regression)\n%s", code, out)
	}
	if !strings.Contains(out, "entity_found REGRESSED 1 -> 0") {
		t.Errorf("verdict does not report the regression the source edit causes:\n%s", out)
	}
}

// TestRunShAnnouncesTheBuild_6283 pins that the rebuild is stated, not silent.
// An operator reading the log must be able to tell which binary produced the
// numbers; "it rebuilt, probably" is the state that let #6283 stand.
func TestRunShAnnouncesTheBuild_6283(t *testing.T) {
	requireBash(t)
	requirePython3(t)
	requireGoToolchain(t)

	h := newRunShHarness(t)
	h.addFixture(t, "graded-mini", true)
	h.writeBaseline(t, []string{"graded-mini"})
	writeStubModule(t, h, 1)
	buildStub(t, h)

	code, out := runDefaultBin(t, h, "--runs", "1", "--ratchet")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, "building grafel ->") {
		t.Errorf("run.sh did not say it was building the binary it graded:\n%s", out)
	}
}

// TestRunShAnnouncesThatGrafelBinIsNotRebuilt_6283 pins the announcement on the
// OTHER branch. It matters more than the build-branch one, not less: the
// caller-supplied binary is the only one this script deliberately never
// rebuilds, so it is the only path on which the reported figures can still
// describe source other than the working tree. A reader of the log has to be
// told that, and told it by the runner rather than by documentation — a note
// elsewhere saying "remember GRAFEL_BIN is not rebuilt" is the shape that
// failed in #6283.
//
// Asserting the "not rebuild" wording and not merely the path is deliberate:
// echoing the path alone would satisfy a weaker test while leaving the reader
// to infer the one fact that matters.
func TestRunShAnnouncesThatGrafelBinIsNotRebuilt_6283(t *testing.T) {
	requireBash(t)
	requirePython3(t)

	h := newRunShHarness(t)
	h.addFixture(t, "graded-mini", true)
	h.writeBaseline(t, []string{"graded-mini"})

	bin := h.stubBin(t)
	code, out := h.run(t, bin, "--runs", "1", "--ratchet")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, bin) {
		t.Errorf("run.sh did not name the caller-supplied binary it graded:\n%s", out)
	}
	if !strings.Contains(out, "does not rebuild it") {
		t.Errorf("run.sh did not say the caller-supplied binary is used unrebuilt, so a "+
			"reader cannot tell the figures may describe other source:\n%s", out)
	}
	// The build-branch wording must NOT appear: saying "building grafel" while
	// grading somebody else's prebuilt binary would be worse than saying nothing.
	if strings.Contains(out, "building grafel ->") {
		t.Errorf("run.sh claimed to build while grading a caller-supplied binary:\n%s", out)
	}
}

// TestRunShRefusesUnexecutableGrafelBin_6283 covers the other half of the same
// resolution. GRAFEL_BIN names a binary the operator owns — quality.yml sets it
// to a path it built itself — so run.sh must not overwrite it, which means the
// rebuild cannot cover it. What it must not do is what the old single `if`
// did: build build/grafel and then go on to exec the unusable $GRAFEL_BIN
// anyway.
func TestRunShRefusesUnexecutableGrafelBin_6283(t *testing.T) {
	requireBash(t)
	requirePython3(t)

	h := newRunShHarness(t)
	h.addFixture(t, "graded-mini", true)
	h.writeBaseline(t, []string{"graded-mini"})

	missing := filepath.Join(h.root, "no-such-binary")
	code, out := h.run(t, missing, "--runs", "1", "--ratchet")
	if code != 1 {
		t.Fatalf("exit = %d, want 1 (runner setup error)\n%s", code, out)
	}
	if !strings.Contains(out, "GRAFEL_BIN") || !strings.Contains(out, missing) {
		t.Errorf("refusal does not name GRAFEL_BIN and the path it points at:\n%s", out)
	}
}
