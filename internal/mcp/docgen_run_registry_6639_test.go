package mcp

// docgen_run_registry_6639_test.go — #6639.
//
// # What failed
//
//	go test -count=1 -shuffle=1724578000 ./internal/mcp/
//
// failed in TestWorkflowDocgenDispatch (meta_merges_test.go:53): action=start
// answered `"resumed":true` with a staging_path under the repo root instead of
// the fresh run the test asserts. Nothing in that test is wrong. Under that
// ordering TestElapsedMSCoverageAllTools ran first, called
// grafel_docgen_start_run with {"group":"g"}, and left an entry under key "g"
// in the package-level docgenRunsByGroup (docgen.go:52-53). The next test to
// use group "g" — the same literal, by coincidence rather than by design —
// took the resume branch (docgen.go:223-234) and reported the polluter's run.
//
// # Why the registry, and not the directory
//
// The original diagnosis was "a test escapes sandboxGrafelHome and writes to
// <repo>/.grafel/staging". The write is real, but it is a SIDE EFFECT, not the
// failure: the staging root is anchored on the PROJECT root
// (stagingRootPath, docgen.go:99-101), which is derived from git and has
// nothing to do with HOME. Passing a t.TempDir() as cwd moves the directory
// and the seed still fails, because the resume branch never consults the disk
// — it consults this map. The production code is correct; project-anchored
// staging is what makes cross-process resume work at all, and it must not move.
//
// # Drain per test AND assert globally — they do different jobs
//
// newDocgenServer already cleared the map, but on the way IN. Clearing at
// setup protects the test doing the clearing from its predecessors; it does
// nothing for its successors, which is exactly the direction this failure
// travels. So the fix is to drain on the way OUT, via t.Cleanup
// (releaseDocgenRuns): a test's effect on package state then ends when the
// test does, and no ordering can couple two tests through it.
//
// Draining alone would fix today's failure and leave the property unobserved —
// the next test to call start_run without a cleanup reintroduces it silently.
// So TestMain also asserts the map is empty after m.Run(). That assertion
// cannot fix anything (it runs last, by construction); its job is to make
// "no test leaks docgen run state" a checked property rather than a
// convention. Asserting the map rather than statting the staging directory is
// deliberate: the map is the coupling, the directory is only its artefact, and
// a test can fail this package through the map without touching disk at all.
//
// # What these guards do NOT cover
//
// The staging guard watches <repo>/.grafel/staging and nothing else, which is
// narrower than #6639 item 3 asked for ("any test writes under the repo root's
// .grafel/"): a leak into .grafel/store or .grafel/events escapes it, and so
// does a staging directory that a test creates and removes within the run.
//
// Neither guard survives a PANIC. Both report after m.Run(), and a panic kills
// the test binary before os.Exit is reached — the pre-existing home-isolation
// guard runs BEFORE m.Run() precisely so its diagnosis outlives one (see the
// header of home_isolation_guard_6288_test.go). These two cannot: what they
// assert is a property of the FINISHED run, so there is nothing to report
// until it has finished. A panicking suite is already failing loudly, so the
// asymmetry costs a diagnosis rather than a detection.

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// releaseDocgenRuns drains docgenRunsByGroup when the calling test ends.
//
// Every test that reaches handleDocgenStartRun (directly or through
// handleWorkflowDocgen with action=start) must call this, because a started
// run stays in the map until an abort or promote removes it, and the tests
// that deliberately leave a run in flight are the common case rather than the
// exception.
//
// It drains the WHOLE map, not just the caller's groups, matching what
// newDocgenServer already does on entry. Kept that way deliberately: a caller
// does not reliably know which group names its own table produced (the #6639
// polluter's "g" came from a table shared with 30 other tools). The cost is
// that no test holding a docgen run may call t.Parallel() — none does today,
// and the alternative is a per-group API that silently under-drains.
func releaseDocgenRuns(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		docgenMu.Lock()
		defer docgenMu.Unlock()
		for k := range docgenRunsByGroup {
			delete(docgenRunsByGroup, k)
		}
	})
}

// reportLeakedDocgenRuns writes one line per group still holding an in-flight
// run and returns the number of them. Called from TestMain after m.Run().
//
// The group NAMES are the diagnosis: they are the only attribution available
// once the run is over, and in this defect the colliding name ("g") was the
// whole story.
func reportLeakedDocgenRuns(w io.Writer) int {
	docgenMu.Lock()
	defer docgenMu.Unlock()
	if len(docgenRunsByGroup) == 0 {
		return 0
	}
	groups := make([]string, 0, len(docgenRunsByGroup))
	for g := range docgenRunsByGroup {
		groups = append(groups, g)
	}
	sort.Strings(groups)
	fmt.Fprintf(w, "mcp: %d docgen run(s) left in docgenRunsByGroup after the suite (#6639).\n", len(groups))
	fmt.Fprintf(w, "mcp: a test called grafel_docgen_start_run without releaseDocgenRuns(t); the next\n")
	fmt.Fprintf(w, "mcp: test using one of these group names will silently take the resume branch.\n")
	for _, g := range groups {
		info := docgenRunsByGroup[g]
		fmt.Fprintf(w, "mcp:   group %q run_id=%s staging_path=%s\n", g, info.RunID, info.StagingPath)
	}
	return len(groups)
}

// snapshotRepoStaging records which run directories exist under the CHECKOUT's
// staging root before the suite runs, so a leak can be told apart from residue
// left by an earlier run (this checkout carried two such directories, dated
// 28 and 31 July, when #6639 was filed — .grafel/ is gitignored, so nothing
// ever surfaced them).
//
// The root is derived with the production helpers the polluter itself went
// through — projectRootFromCWD on os.Getwd(), which under `go test` is this
// package's directory, inside the checkout. If that derivation fails (no git,
// no cwd) the check disables itself rather than guessing: a missing guard is
// better than one that fails somewhere other than where the defect is.
func snapshotRepoStaging() (string, map[string]bool) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", nil
	}
	projectRoot, err := projectRootFromCWD(cwd, false)
	if err != nil {
		return "", nil
	}
	root := stagingRootPath(projectRoot)
	return root, stagingEntries(root)
}

func stagingEntries(root string) map[string]bool {
	out := map[string]bool{}
	if root == "" {
		return out
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return out
	}
	for _, e := range entries {
		out[e.Name()] = true
	}
	return out
}

// reportNewRepoStaging fails the run for staging directories the suite itself
// created inside the checkout, and returns how many. This pins the other half
// of the fix — passing an explicit cwd so start_run anchors on a t.TempDir()
// instead of on whatever working tree the suite happens to run from.
func reportNewRepoStaging(w io.Writer, root string, before map[string]bool) int {
	if root == "" {
		return 0
	}
	var added []string
	for name := range stagingEntries(root) {
		if !before[name] {
			added = append(added, name)
		}
	}
	if len(added) == 0 {
		return 0
	}
	sort.Strings(added)
	fmt.Fprintf(w, "mcp: %d staging dir(s) written into the CHECKOUT during this run (#6639).\n", len(added))
	fmt.Fprintf(w, "mcp: a test called grafel_docgen_start_run without an explicit cwd, so the\n")
	fmt.Fprintf(w, "mcp: project root resolved to this working tree. Pass cwd: t.TempDir().\n")
	for _, name := range added {
		fmt.Fprintf(w, "mcp:   %s\n", filepath.Join(root, name))
	}
	return len(added)
}

// ---------------------------------------------------------------------------
// The guards' own coverage (#6644 review)
// ---------------------------------------------------------------------------
//
// Both reporters above are called from exactly one place — TestMain — and
// nothing captures that stderr, so until these tests existed the checkers were
// themselves unchecked. Two non-equivalent mutants survived the full package:
// inverting reportNewRepoStaging's `!before[name]`, which turns the residue
// filter into a residue ACCUSER (live on any checkout carrying an aged staging
// dir, as the one this was filed from does), and short-circuiting the map
// guard to `if true { return 0 }`, which deletes this change's central
// observer with the suite still green.
//
// Both helpers take their input as parameters — an io.Writer, and
// (root, before) — so this costs two table tests rather than a redesign.

// TestReportLeakedDocgenRunsNamesTheGroups pins the map guard: the count it
// returns AND the text it writes, since the group names are the only
// attribution available once the run is over.
//
// Seeding the package global mid-suite is deterministic here: the only reader
// of docgenRunsByGroup is handleDocgenStartRun, and no test that reaches it
// calls t.Parallel() (docgen_test.go and meta_merges_test.go have none, and
// server_test.go's single t.Parallel is in TestMCPInstructionsOrientationMap).
func TestReportLeakedDocgenRunsNamesTheGroups(t *testing.T) {
	tests := []struct {
		name     string
		seed     map[string]*docgenRunInfo
		want     int
		contains []string
	}{
		{name: "clean run reports nothing", seed: map[string]*docgenRunInfo{}, want: 0},
		{
			name: "one leaked group is counted and named",
			seed: map[string]*docgenRunInfo{
				"g": {RunID: "2026-05-26-testid01", StagingPath: "/tmp/x/.grafel/staging/2026-05-26-testid01", Group: "g"},
			},
			want:     1,
			contains: []string{`group "g"`, "2026-05-26-testid01", "/tmp/x/.grafel/staging/2026-05-26-testid01"},
		},
		{
			name: "two leaked groups are both named",
			seed: map[string]*docgenRunInfo{
				"zeta":  {RunID: "run-z", StagingPath: "/tmp/z", Group: "zeta"},
				"alpha": {RunID: "run-a", StagingPath: "/tmp/a", Group: "alpha"},
			},
			want:     2,
			contains: []string{`group "alpha"`, `group "zeta"`},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			swapDocgenRuns(t, tc.seed)

			var buf bytes.Buffer
			got := reportLeakedDocgenRuns(&buf)

			if got != tc.want {
				t.Errorf("reportLeakedDocgenRuns() = %d, want %d", got, tc.want)
			}
			for _, want := range tc.contains {
				if !strings.Contains(buf.String(), want) {
					t.Errorf("report does not mention %q; got:\n%s", want, buf.String())
				}
			}
			if tc.want == 0 && buf.Len() != 0 {
				t.Errorf("clean run wrote a report:\n%s", buf.String())
			}
		})
	}
}

// TestReportLeakedDocgenRunsSortsGroups pins the sort.Strings ordering, which
// the count assertions above cannot see. An unsorted report is a
// non-deterministic diagnosis, and a non-deterministic diagnosis is how an
// order-dependent defect gets reseeded away rather than fixed.
//
// Six groups over eight reports, not one report of three: Go randomises map
// iteration, so deleting the sort leaves a 1-in-6 chance of coming out ordered
// anyway with three keys. Scoring that mutant on a single draw would be
// scheduling luck rather than a kill. Six keys make one accidental ordering
// 1-in-720, and eight independent reports put the survival probability of an
// unsorted implementation below 1e-23.
func TestReportLeakedDocgenRunsSortsGroups(t *testing.T) {
	groups := []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot"}
	seed := map[string]*docgenRunInfo{}
	for _, g := range groups {
		seed[g] = &docgenRunInfo{RunID: "run-" + g, StagingPath: "/tmp/" + g, Group: g}
	}
	swapDocgenRuns(t, seed)

	for attempt := range 8 {
		var buf bytes.Buffer
		reportLeakedDocgenRuns(&buf)
		out := buf.String()

		prev := -1
		for _, g := range groups {
			at := strings.Index(out, fmt.Sprintf("group %q", g))
			if at < 0 {
				t.Fatalf("attempt %d: report is missing group %q:\n%s", attempt, g, out)
			}
			if at < prev {
				t.Fatalf("attempt %d: group %q is out of sorted order:\n%s", attempt, g, out)
			}
			prev = at
		}
	}
}

// swapDocgenRuns substitutes the package global for the supplied seed and puts
// the real map back when the test ends, so these tests cannot themselves
// become the leak they exist to detect.
func swapDocgenRuns(t *testing.T, seed map[string]*docgenRunInfo) {
	t.Helper()
	docgenMu.Lock()
	saved := docgenRunsByGroup
	docgenRunsByGroup = seed
	docgenMu.Unlock()
	t.Cleanup(func() {
		docgenMu.Lock()
		docgenRunsByGroup = saved
		docgenMu.Unlock()
	})
}

// TestReportNewRepoStagingSeparatesResidueFromLeaks pins the comparison that
// makes the staging guard usable on a developer machine at all: residue is
// ignored and only directories created DURING the run are reported.
func TestReportNewRepoStagingSeparatesResidueFromLeaks(t *testing.T) {
	const residue = "2026-01-01-preexist"
	const fresh = "2026-08-26-brandnew"

	tests := []struct {
		name      string
		createNew []string
		want      int
		contains  []string
		unwanted  []string
	}{
		{
			name:     "residue alone is not a leak",
			want:     0,
			unwanted: []string{residue},
		},
		{
			name:      "a dir created during the run is reported by name",
			createNew: []string{fresh},
			want:      1,
			contains:  []string{fresh},
			unwanted:  []string{residue},
		},
		{
			name:      "every new dir is counted",
			createNew: []string{fresh, "2026-08-26-second"},
			want:      2,
			contains:  []string{fresh, "2026-08-26-second"},
			unwanted:  []string{residue},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), ".grafel", "staging")
			if err := os.MkdirAll(filepath.Join(root, residue), 0o755); err != nil {
				t.Fatal(err)
			}
			before := stagingEntries(root)
			if !before[residue] {
				t.Fatalf("snapshot missed the pre-existing dir; got %v", before)
			}
			for _, name := range tc.createNew {
				if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
					t.Fatal(err)
				}
			}

			var buf bytes.Buffer
			got := reportNewRepoStaging(&buf, root, before)

			if got != tc.want {
				t.Errorf("reportNewRepoStaging() = %d, want %d; report:\n%s", got, tc.want, buf.String())
			}
			for _, want := range tc.contains {
				if !strings.Contains(buf.String(), want) {
					t.Errorf("report does not name %q; got:\n%s", want, buf.String())
				}
			}
			for _, no := range tc.unwanted {
				if strings.Contains(buf.String(), no) {
					t.Errorf("report names %q, which existed before the run:\n%s", no, buf.String())
				}
			}
			if tc.want == 0 && buf.Len() != 0 {
				t.Errorf("a run that created nothing wrote a report:\n%s", buf.String())
			}
		})
	}
}

// TestReportNewRepoStagingDisablesItselfWithoutARoot pins the escape hatch:
// when the project root cannot be derived, snapshotRepoStaging returns "" and
// the guard must stay silent rather than guess.
func TestReportNewRepoStagingDisablesItselfWithoutARoot(t *testing.T) {
	var buf bytes.Buffer
	if got := reportNewRepoStaging(&buf, "", nil); got != 0 {
		t.Errorf("reportNewRepoStaging with no root = %d, want 0", got)
	}
	if buf.Len() != 0 {
		t.Errorf("disabled guard still wrote:\n%s", buf.String())
	}
	if entries := stagingEntries(""); len(entries) != 0 {
		t.Errorf("stagingEntries with no root = %v, want empty", entries)
	}
}
