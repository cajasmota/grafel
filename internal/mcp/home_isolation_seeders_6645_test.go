package mcp

// home_isolation_seeders_6645_test.go — the observation the #6288 guard cannot make.
//
// # What was wrong
//
// #6288's guard (home_isolation_guard_6288_test.go) is an AST scan keyed on the
// PRESENCE of a Setenv("HOME", …) call, and its own doc comment says what that
// buys and what it does not: it "says nothing about a function that isolates
// nothing at all". #6645 measured the size of that gap. Every one of
// internal/mcp's 1284 top-level tests was run ALONE against a fresh surrogate
// HOME and checked for writes under it: 32 wrote, all of them under .grafel —
// store/<slug>-<hash>/refs/_unknown/graph.fb, and events/*.jsonl.
//
// They were not 32 independent mistakes. Seven on-disk SEEDERS resolved a state
// dir with a bare daemon.StateDirForRepo (which bottoms out in
// $GRAFEL_HOME-or-~/.grafel/store) or drove the event writers (registry.HomeDir)
// with no sandbox in force. On a developer machine `go test ./internal/mcp/`
// therefore mutated the very grafel state the daemon and the MCP tools read.
//
// # Why this file rather than a stronger AST scan
//
// A scan can only ever check that isolation was ASKED FOR. What matters is that
// it HAPPENED — that the path the seeder actually writes to lands inside the
// sandbox. The two come apart in practice: GRAFEL_DAEMON_ROOT out-ranks
// GRAFEL_HOME in daemon.StateDirForRepo, so a seeder can set all three home
// variables correctly and still write outside them.
//
// So this is the #6645 measurement harness reduced to one in-process table:
// point HOME/USERPROFILE/GRAFEL_HOME at a surrogate "real" home, run the seeder,
// and assert the surrogate is still EMPTY. It is an observation of the artefact,
// not of the call — a seeder that stops isolating fails here whatever it says.
//
// A surrogate home rather than the developer's actual one is not squeamishness:
// diffing the real ~/.grafel over a ~2-minute suite would report every write the
// running grafel daemon made during it, which is a guard that fails for reasons
// unrelated to this package. The surrogate makes the check deterministic.
//
// # Bound, stated rather than assumed
//
// This covers the seven seeders it names, not every test. It is a REGRESSION
// PIN, not structural closure. A NEW test that reaches daemon.StateDirForRepo
// inline, without going through sandboxStateDirs, is invisible here — the same
// way it was invisible to #6288 — and nothing else catches it either: the
// seam's escape check only fires for code that went through the seam, so a
// seeder that never calls it is never checked. That is measured, not feared: a
// newly-written seeder in the style of the seven, plus one test calling it,
// leaves this package at exit 0 while writing 6 entries into the surrogate home.
//
// The inline pattern is the normal way to write a seeder here — 18 other
// _test.go files in this package resolve a state dir with a bare
// daemon.StateDirForRepo{,Ref}. None of them leaks today (measured), but the
// count can go 0 -> 1 without this file noticing. Closing that direction needs a
// chokepoint or a vet-style check over those call sites, tracked in #6663.
// Until then: add a row below whenever a seeder is added.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// surrogateHome installs a fresh directory as the "real" home for this test —
// HOME, USERPROFILE and GRAFEL_HOME together, mirroring what registry.HomeDir
// consults — and returns it. A seeder that isolates properly overrides all three
// with its own sandbox and leaves this directory untouched.
//
// GRAFEL_DAEMON_ROOT is cleared for the same reason sandboxStateDirs clears it:
// an exported one would redirect the state dir away from the surrogate and make
// a genuinely leaking seeder look clean.
func surrogateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("GRAFEL_HOME", filepath.Join(home, ".grafel"))
	t.Setenv("GRAFEL_DAEMON_ROOT", "")
	return home
}

// assertHomeUntouched fails with the actual paths written, because "a seeder
// leaked" is not actionable and "it wrote .grafel/store/r1-…/refs/_unknown/graph.fb"
// says exactly which derivation escaped.
func assertHomeUntouched(t *testing.T, home string) {
	t.Helper()
	var written []string
	err := filepath.Walk(home, func(path string, _ os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path != home {
			if rel, rerr := filepath.Rel(home, path); rerr == nil {
				written = append(written, rel)
			} else {
				written = append(written, path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk surrogate home: %v", err)
	}
	if len(written) > 0 {
		t.Fatalf("#6645: the seeder wrote %d entr(ies) into the surrogate HOME — on a developer "+
			"machine these land in the REAL ~/.grafel, which the daemon and the MCP tools read:\n  %v\n"+
			"Route the seeder's state-dir resolution through sandboxStateDirs (or call "+
			"sandboxGrafelHome before the writers run).", len(written), written)
	}
}

// TestSeedersDoNotWriteIntoTheRealGrafelHome runs each on-disk seeder against a
// surrogate home and asserts it wrote nothing there. One row per seeder that
// #6645 measured leaking; the subtest names are the seeders, so a failure names
// the setup path rather than one of its 32 downstream tests.
func TestSeedersDoNotWriteIntoTheRealGrafelHome(t *testing.T) {
	seeders := []struct {
		name string
		run  func(t *testing.T)
	}{
		{"seedRepoOnDisk", func(t *testing.T) {
			seedRepoOnDisk(t, lazyTestDoc())
		}},
		{"seedGroupsOnDisk", func(t *testing.T) {
			seedGroupsOnDisk(t, map[string]*graph.Document{
				"A": lazyTestDoc(),
				"B": lazyTestDoc(),
			})
		}},
		{"writeSegmentSet", func(t *testing.T) {
			writeSegmentSet(t, segFixtureDoc("r", 600))
		}},
		{"writeSingleFileGraph", func(t *testing.T) {
			writeSingleFileGraph(t, segFixtureDoc("s", 40))
		}},
		{"seedTwoGroups", func(t *testing.T) {
			seedTwoGroups(t)
		}},
		{"newMmapFlowServer", func(t *testing.T) {
			newMmapFlowServer(t)
		}},
		{"coreTestServer+grafel_event", func(t *testing.T) {
			// The server alone does not write events; the leak was the event
			// writers under registry.HomeDir(), so the row has to DRIVE them —
			// asserting on a server that never emitted an event would pass
			// against a completely unisolated home.
			srv := coreTestServer(t)
			callBare(t, srv.handleFeedbackEvent, map[string]any{"outcome": "helped"})
			callBare(t, srv.handlePersonaEvent, map[string]any{
				"persona": "architect", "event_type": "invoke",
			})
		}},
	}
	for _, s := range seeders {
		t.Run(s.name, func(t *testing.T) {
			home := surrogateHome(t)
			s.run(t)
			assertHomeUntouched(t, home)
		})
	}
}

// TestSandboxStateDirsNeutralisesDaemonRoot pins the half of the seam the table
// above cannot reach. Each row enters through surrogateHome, which already
// clears GRAFEL_DAEMON_ROOT, so a sandboxStateDirs that forgot to clear it
// SURVIVES the table — measured, not assumed. This test sets the variable and
// then asks the seam for a state dir directly.
//
// It matters because GRAFEL_DAEMON_ROOT is consulted BEFORE GRAFEL_HOME: a
// developer who exports it (plausible in this repo, where isolated daemons are
// a normal debugging move) would otherwise have every seeder write into that
// root instead of the per-test sandbox, silently sharing state between tests
// and with whatever daemon owns the root.
//
// Scored, not assumed. Deleting the seam's t.Setenv("GRAFEL_DAEMON_ROOT", "")
// leaves the whole table above GREEN and fails only here — and it fails through
// the resolver's own escape check, naming the offending path, which is what
// makes that check an observation rather than a comment.
func TestSandboxStateDirsNeutralisesDaemonRoot(t *testing.T) {
	hostile := t.TempDir()
	t.Setenv("GRAFEL_DAEMON_ROOT", hostile)

	stateDirFor := sandboxStateDirs(t)
	repo := t.TempDir()
	dir := stateDirFor(repo)

	home := os.Getenv("HOME")
	if home == "" {
		t.Fatal("sandboxStateDirs did not set HOME")
	}
	want := filepath.Join(home, ".grafel") + string(filepath.Separator)
	if !strings.HasPrefix(dir, want) {
		t.Fatalf("state dir escaped the sandbox: got %s, want a path under %s", dir, want)
	}
	if strings.HasPrefix(dir, hostile+string(filepath.Separator)) {
		t.Fatalf("an exported GRAFEL_DAEMON_ROOT out-ranked the sandbox: %s is under %s", dir, hostile)
	}
}

// escapeProbeEnv gates the child half of TestSandboxStateDirsEscapeCheckIsFatal.
const escapeProbeEnv = "GRAFEL_6662_ESCAPE_PROBE"

// TestSandboxStateDirsEscapeCheckIsFatal observes the seam's escape check
// itself, rather than the path it happens to return.
//
// TestSandboxStateDirsNeutralisesDaemonRoot asserts the RESOLVED PATH, so it
// passes whether the check inside sandboxStateDirs fails the test or merely
// reports: downgrading its t.Fatalf to a t.Logf, or deleting the whole
// `if !strings.HasPrefix(...)` block, left the full package at exit 0. That is
// this milestone's signature defect — a guard that logs instead of failing —
// and it was sitting inside the guard #6645 added. So the check needs a probe
// that puts it in a losing position and requires it to FAIL.
//
// A check that fails the test cannot be observed from inside the same test:
// t.Fatalf marks the running test failed, and a subtest's failure propagates to
// its parent. The observation therefore has to happen in a second process. The
// child re-runs this one test with escapeProbeEnv set, asks the seam for its
// resolver, and only THEN exports a hostile GRAFEL_DAEMON_ROOT — a state the
// seam cannot have neutralised in advance, because it is created after the seam
// ran, exactly as a developer's own exported root behaves relative to a helper
// that ran earlier in the process. The resolver must refuse it.
//
// The parent scores the child on its EXIT CODE, so both shapes of the defect
// die: a t.Logf downgrade and a deleted block both make the child exit 0, and
// the child then hands back a path outside the sandbox that a seeder would
// write a real graph.fb into.
func TestSandboxStateDirsEscapeCheckIsFatal(t *testing.T) {
	if os.Getenv(escapeProbeEnv) == "1" {
		stateDirFor := sandboxStateDirs(t)
		hostile := t.TempDir()
		t.Setenv("GRAFEL_DAEMON_ROOT", hostile)
		dir := stateDirFor(t.TempDir())
		// Only reached if the escape check did not fail the test. Report the
		// escape rather than failing, so the parent's exit-code score is the
		// only thing that decides — a child that fails here for the right
		// reason and one that fails for the wrong reason would be
		// indistinguishable.
		t.Logf("escape check did not fail the test; resolver returned %s (hostile root %s)", dir, hostile)
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestSandboxStateDirsEscapeCheckIsFatal$", "-test.v")
	cmd.Env = append(os.Environ(), escapeProbeEnv+"=1")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("#6645: sandboxStateDirs' escape check did not FAIL the test when the resolved "+
			"state dir landed outside the sandboxed grafel home — the child process exited 0.\n"+
			"A check that only reports lets the seeder write a fixture graph.fb into a REAL "+
			"grafel store; the resolver must call t.Fatalf, not t.Logf, and the block must be "+
			"present.\nchild output:\n%s", out)
	}
	if !strings.Contains(string(out), "resolved OUTSIDE the sandboxed grafel home") {
		t.Fatalf("the child failed, but not through the escape check — its diagnostic is what "+
			"points a developer at the out-ranking environment variable.\nchild output:\n%s", out)
	}
}
