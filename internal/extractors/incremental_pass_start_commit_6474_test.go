package extractors_test

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractors"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/indexer/diff"
)

// #6474 — the two INCREMENTAL manifest saves stamped git_commit from LIVE HEAD
// at save time (diff.SaveManifest) rather than from the commit whose bytes the
// pass actually read. If HEAD moves DURING the pass, the manifest labels a
// graph built at commit A as being at commit B.
//
// The one consumer this breaks functionally is the #5710 head-advance detector
// (incremental.go's `manifest.GitCommit != currentHead` gate): an over-advanced
// label makes the NEXT pass see HEAD == manifest, find zero changes, and return
// Done=true over a graph built from an earlier commit. That is self-concealing —
// nothing re-requests the work. The remaining consumers (statuswriter → status
// line/dashboard, daemon/index_commit → RPC status and grafel_index_status) are
// reporting surfaces; no automatic reindex is keyed on them.
//
// FIXTURE PROPERTIES, each load-bearing (copied from the #5822 reject test,
// whose header explains why — fixture vacuity is this repo's recurring failure):
//
//   - the repo is a REAL git repo. Against a plain t.TempDir() `git rev-parse`
//     fails, the stamp is unconditionally "" on BOTH the fixed and the
//     defective code, and these assertions would pass against the defect.
//   - HEAD GENUINELY MOVES (c1 -> c2), asserted explicitly. With one commit,
//     "stamped the commit it read" and "stamped live HEAD" are the same value.
//   - the branch under test is asserted to have FIRED — Done=true with
//     ChangedFiles==0 for the zero-change reconcile, ChangedFiles>0 for Step 9.
//     The two sites are DIFFERENT call sites; a test that reaches only one
//     leaves the other unpinned, so there is one test per site.
//
// THE SEAM. The defect's window is "HEAD moves between the byte read and the
// save", which no test can schedule reliably. extractors.SwapPassStartCommitHook
// hands it over: the callback fires immediately after tryIncremental captures
// the pass-start commit, commits c2, and returns — so the rest of the pass runs
// with live HEAD genuinely one commit ahead of what it read.
//
// KILLING MUTANT for both tests: revert the site to
// `diff.SaveManifest(stateDir, absRepo, manifest)`. It yields c2; these assert
// c1.

// commitOnceAt returns a hook that runs fn on its FIRST invocation only.
// TryIncremental may be called more than once per test, and a hook that
// commits on every pass would keep moving HEAD out from under later passes.
func commitOnceAt(fn func()) func(short, full string) {
	done := false
	return func(short, full string) {
		if done {
			return
		}
		done = true
		fn()
	}
}

// TestIncremental_ZeroChangeSave_StampsPassStartCommit pins the zero-change
// reconcile site. HEAD advances DURING the pass; the pass compared the walked
// source set against the PASS-START commit, so that is the commit the manifest
// must name. c2's contents were never examined by anything in this pass.
//
// This does NOT contradict TestIncremental_ZeroChangePassDoesAdvanceIndexedCommit
// (#5822 ②), which pins that the zero-change path DOES advance when HEAD moved
// BEFORE the pass — there the range diff genuinely confirmed the new commit.
func TestIncremental_ZeroChangeSave_StampsPassStartCommit(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()
	initGitRepo(t, repo)

	writeFile(t, repo, "a.go", "package p\n\nfunc F() {}\n")
	writeFile(t, repo, "node_modules/pkg/index.js", "module.exports = 1\n")
	c1Short := gitCommitAll(t, repo, "c1")
	c1Full := gitRevParse5822(t, repo, "HEAD")

	buildMinimalGraph(t, stateDir, []graph.Entity{
		{ID: graph.EntityID("test-repo", "SCOPE.Operation", "F", "a.go"),
			Name: "F", Kind: "SCOPE.Operation", SourceFile: "a.go", Language: "go"},
	}, nil)
	m := diff.LoadManifest(stateDir)
	diff.UpdateManifest(repo, []string{"a.go"}, m) // walked set only
	if err := diff.SaveManifest(stateDir, repo, m); err != nil {
		t.Fatalf("seed manifest: %v", err)
	}
	if seeded := diff.LoadManifest(stateDir); seeded.GitCommit != c1Short {
		t.Fatalf("fixture: seeded manifest GitCommit = %q, want %q — the fixture repo is not being seen as a git repo, so this test would assert nothing", seeded.GitCommit, c1Short)
	}

	// HEAD moves DURING the pass, right after the pass-start capture. Only a
	// hard-excluded path is touched, so the walked source set is still
	// unchanged and the pass stays on the zero-change branch.
	var c2Short, c2Full string
	restore := extractors.SwapPassStartCommitHook(commitOnceAt(func() {
		writeFile(t, repo, "node_modules/pkg/index.js", "module.exports = 2\n")
		c2Short = gitCommitAll(t, repo, "c2")
		c2Full = gitRevParse5822(t, repo, "HEAD")
	}))
	defer restore()

	res := extractors.TryIncremental(context.Background(), repo, stateDir, nil, nil)

	if c2Short == "" {
		t.Fatalf("fixture: the pass-start hook never fired — the capture seam is not on the path under test")
	}
	if c1Short == c2Short || c1Full == c2Full {
		t.Fatalf("fixture: HEAD did not move (c1=%s/%s c2=%s/%s) — 'stamped what it read' and 'stamped live HEAD' would be indistinguishable", c1Short, c1Full, c2Short, c2Full)
	}
	if !res.Done {
		t.Fatalf("expected the zero-change no-op, got fallback %q — this test is not exercising the zero-change save", res.FallbackReason)
	}
	if res.ChangedFiles != 0 {
		t.Fatalf("expected the ZERO-change branch, but %d file(s) were re-extracted — this pass took the incremental SUCCESS path, whose manifest write is a DIFFERENT call site", res.ChangedFiles)
	}

	got := diff.LoadManifest(stateDir)
	if got.GitCommit != c1Short {
		t.Errorf("zero-change save: manifest GitCommit = %q, want %q (the pass-start commit). %q landed mid-pass and its tree was never compared against anything; labelling the graph with it disarms the #5710 head-advance detector on the next pass.", got.GitCommit, c1Short, c2Short)
	}
	if got.GitCommitFull != c1Full {
		t.Errorf("zero-change save: manifest GitCommitFull = %q, want %q", got.GitCommitFull, c1Full)
	}
}

// TestIncremental_Step9Save_StampsPassStartCommit pins the OTHER site: the
// successful incremental's Step 9 write. Its manifest also carries the per-file
// walkStamps taken from the bytes the extraction loop read (#6212), so a
// save-time HEAD stamp puts the commit label and the file stamps at different
// commits within one manifest.
func TestIncremental_Step9Save_StampsPassStartCommit(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()
	initGitRepo(t, repo)

	writeFile(t, repo, "a.go", "package p\n\nfunc F() {}\n")
	writeFile(t, repo, "node_modules/pkg/index.js", "module.exports = 1\n")
	c0Short := gitCommitAll(t, repo, "c0")

	buildMinimalGraph(t, stateDir, []graph.Entity{
		{ID: graph.EntityID("test-repo", "SCOPE.Operation", "F", "a.go"),
			Name: "F", Kind: "SCOPE.Operation", SourceFile: "a.go", Language: "go"},
	}, nil)
	// Stamps for a.go AS IT IS AT c0, pinned at c0.
	m := diff.LoadManifest(stateDir)
	diff.UpdateManifest(repo, []string{"a.go"}, m)
	if err := diff.SaveManifest(stateDir, repo, m); err != nil {
		t.Fatalf("seed manifest: %v", err)
	}

	// c1: a.go genuinely changes, so the pass has real work and reaches Step 9.
	writeFile(t, repo, "a.go", "package p\n\nfunc F() {}\n\nfunc G() {}\n")
	c1Short := gitCommitAll(t, repo, "c1")
	c1Full := gitRevParse5822(t, repo, "HEAD")
	if c0Short == c1Short {
		t.Fatalf("fixture: c0 == c1")
	}

	var c2Short, c2Full string
	restore := extractors.SwapPassStartCommitHook(commitOnceAt(func() {
		writeFile(t, repo, "node_modules/pkg/index.js", "module.exports = 2\n")
		c2Short = gitCommitAll(t, repo, "c2")
		c2Full = gitRevParse5822(t, repo, "HEAD")
	}))
	defer restore()

	res := extractors.TryIncremental(context.Background(), repo, stateDir, nil, nil)

	if c2Short == "" {
		t.Fatalf("fixture: the pass-start hook never fired — the capture seam is not on the path under test")
	}
	if c1Short == c2Short || c1Full == c2Full {
		t.Fatalf("fixture: HEAD did not move during the pass (c1=%s/%s c2=%s/%s)", c1Short, c1Full, c2Short, c2Full)
	}
	if !res.Done {
		t.Fatalf("expected the incremental SUCCESS path, got fallback %q — Step 9's save never ran", res.FallbackReason)
	}
	if res.ChangedFiles == 0 {
		t.Fatalf("expected the SUCCESS path (ChangedFiles>0), got 0 — this pass took the zero-change branch, whose manifest write is a DIFFERENT call site")
	}

	got := diff.LoadManifest(stateDir)
	if got.GitCommit != c1Short {
		t.Errorf("Step 9 save: manifest GitCommit = %q, want %q (the commit the extraction loop's bytes came from). %q landed mid-pass; none of its content is in the graph this save just persisted.", got.GitCommit, c1Short, c2Short)
	}
	if got.GitCommitFull != c1Full {
		t.Errorf("Step 9 save: manifest GitCommitFull = %q, want %q", got.GitCommitFull, c1Full)
	}
}
