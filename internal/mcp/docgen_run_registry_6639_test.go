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

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// releaseDocgenRuns drains docgenRunsByGroup when the calling test ends.
//
// Every test that reaches handleDocgenStartRun (directly or through
// handleWorkflowDocgen with action=start) must call this, because a started
// run stays in the map until an abort or promote removes it, and the tests
// that deliberately leave a run in flight are the common case rather than the
// exception.
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
