package extractors_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractors"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/indexer/diff"
)

// gitRevParse5822 runs `git -C dir rev-parse <args...>` and returns the trimmed
// stdout, failing the test on error.
func gitRevParse5822(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir, "rev-parse"}, args...)...).Output()
	if err != nil {
		t.Fatalf("git rev-parse %v: %v", args, err)
	}
	return string(bytes.TrimSpace(out))
}

// diskSHA5822 is the hex SHA-256 of the file's CURRENT bytes on disk — the
// value a correctly-refreshed manifest stamp must carry.
func diskSHA5822(t *testing.T, abs string) string {
	t.Helper()
	b, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read %s: %v", abs, err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// TestIncremental_RejectPathDoesNotAdvanceIndexedCommit is the RED test for
// #5822 part ②: the too-many-changed REJECT path stamped the manifest's
// git_commit from LIVE HEAD at save time, not from the commit the graph was
// actually built at.
//
// The reject path indexes NOTHING — it reconciles the manifest and returns a
// full-reindex request. Stamping live HEAD there makes the manifest claim
// "indexed at HEAD" for a pass that touched no entity, and every consumer of
// that field (grafel status / the dashboard / the statusline, via
// daemon.indexedCommitShortNoGit and daemon.IndexedCommitForRepo, both of
// which prefer the manifest over the graph.fb header) then reports a commit
// the graph has never seen. If the requested full reindex is cancelled,
// watchdog-killed, or merely queued behind other work, the label stays
// advanced over an older graph INDEFINITELY — and worse, it disarms the #5710
// HEAD-advance detector, which re-requests the reindex precisely BECAUSE
// manifest.GitCommit trails HEAD. Once the manifest lies about being at HEAD,
// nothing ever asks again.
//
// FIXTURE, and why each property is load-bearing (fixture vacuity has been
// this repo's recurring test failure — a git-dependent path tested against a
// non-git tmpdir asserts nothing):
//
//   - repo is a REAL git repo. diff.HeadCommit/headCommit shell out to
//     `git rev-parse`; against a plain t.TempDir() they return "" and
//     SaveManifest's stamp is unconditionally "" — the assertion below would
//     pass against the defective code because there is no commit to advance
//     TO. TestIncremental_FallbackRefreshesStampsAndReconciles (#5668) is
//     exactly that shape, which is why it never caught this.
//   - HEAD GENUINELY MOVES (c1 -> c2), asserted explicitly. A repo with one
//     commit makes "did not advance" and "advanced" the same value.
//   - the working tree is CLEAN at c2. `git diff --name-only HEAD` is then
//     empty, so the ONLY thing that puts the five files into the changed set
//     is the #5710 commit-RANGE diff off manifest.GitCommit. That is the
//     post-fetch/checkout/pull shape the reject path actually meets in
//     production, and it also proves the range detector ran.
//   - the reject is asserted to have FIRED (Done=false, reason
//     too-many-changed). Without that the manifest assertions would hold
//     trivially on any code path that never reached the write.
func TestIncremental_RejectPathDoesNotAdvanceIndexedCommit(t *testing.T) {
	t.Setenv("GRAFEL_INCREMENTAL_MAX_FILES", "2") // 5 changed > 2 → reject

	repo := t.TempDir()
	stateDir := t.TempDir()
	initGitRepo(t, repo)

	files := []string{"a.go", "b.go", "c.go", "d.go", "e.go"}
	for i, rel := range files {
		writeFile(t, repo, rel, fmt.Sprintf("package p\n\nfunc F%d() {}\n", i))
	}
	c1Short := gitCommitAll(t, repo, "c1")
	c1Full := gitRevParse5822(t, repo, "HEAD")

	// A materialized, non-empty graph so the #5710 absent-graph guard cannot
	// be what forces the fallback (it would fire before the trigger limit and
	// take a DIFFERENT path, one that deliberately writes no manifest).
	buildMinimalGraph(t, stateDir, []graph.Entity{
		{ID: graph.EntityID("test-repo", "SCOPE.Operation", "F0", "a.go"),
			Name: "F0", Kind: "SCOPE.Operation", SourceFile: "a.go", Language: "go"},
	}, nil)

	// Baseline manifest: correct stamps for every file, pinned at c1. This is
	// the honest record of a graph built from c1.
	seedManifest(t, repo, stateDir)
	seeded := diff.LoadManifest(stateDir)
	if seeded.GitCommit != c1Short {
		t.Fatalf("fixture: seeded manifest GitCommit = %q, want %q — the fixture repo is not being seen as a git repo, so this test would assert nothing", seeded.GitCommit, c1Short)
	}
	if seeded.GitCommitFull != c1Full {
		t.Fatalf("fixture: seeded manifest GitCommitFull = %q, want %q", seeded.GitCommitFull, c1Full)
	}

	// Advance HEAD: rewrite every file and commit. Working tree ends CLEAN.
	for i, rel := range files {
		writeFile(t, repo, rel, fmt.Sprintf("package p\n\nfunc F%d() { _ = %d }\n", i, i+100))
	}
	c2Short := gitCommitAll(t, repo, "c2")
	c2Full := gitRevParse5822(t, repo, "HEAD")

	if c1Short == c2Short || c1Full == c2Full {
		t.Fatalf("fixture: HEAD did not move (c1=%s/%s c2=%s/%s) — 'did not advance' and 'advanced' would be indistinguishable", c1Short, c1Full, c2Short, c2Full)
	}
	if got := diff.HeadCommit(repo); got != c2Short {
		t.Fatalf("fixture: diff.HeadCommit(repo) = %q, want %q — git is not resolving in this fixture", got, c2Short)
	}

	// ── Pass 1: the reject ────────────────────────────────────────────────
	res := extractors.TryIncremental(context.Background(), repo, stateDir, nil, nil)
	if res.Done {
		t.Fatalf("pass 1: expected the too-many-changed reject, got Done=true (reason=%q) — the reject path under test never ran", res.FallbackReason)
	}
	if !strings.Contains(res.FallbackReason, "too-many-changed") {
		t.Fatalf("pass 1: fell back for the WRONG reason %q — this test only pins the too-many-changed reject; another path reaching here means the fixture stopped exercising it", res.FallbackReason)
	}

	after1 := diff.LoadManifest(stateDir)

	// THE DEFECT. Nothing was indexed, so the graph is still the c1 graph and
	// the label must still say c1.
	if after1.GitCommit != c1Short {
		t.Errorf("pass 1: manifest GitCommit = %q, want %q (the commit the graph was actually built at). The reject indexed nothing; %q is live HEAD at save time, which the graph has never seen.", after1.GitCommit, c1Short, c2Short)
	}
	if after1.GitCommitFull != c1Full {
		t.Errorf("pass 1: manifest GitCommitFull = %q, want %q", after1.GitCommitFull, c1Full)
	}

	// THE GUARD THAT FORBIDS "just don't save on reject". #6201 established
	// that the reject MUST still persist a reconciled manifest: #5668's stamp
	// refresh (or the changed files re-surface next pass and re-trip the same
	// reject forever) and #5667's prune. Removing the write to fix the commit
	// label would trade a wrong label for a permanent reject loop.
	for _, rel := range files {
		e, ok := after1.Files[rel]
		if !ok {
			t.Fatalf("pass 1: %s missing from the manifest — the reject path's write was dropped, re-entering #5667", rel)
		}
		if want := diskSHA5822(t, filepath.Join(repo, rel)); e.SHA256 != want {
			t.Errorf("pass 1: %s stamp = %q, want the on-disk hash %q — the reject path stopped refreshing stamps, re-entering the #5668 reject loop", rel, e.SHA256, want)
		}
	}

	// ── Pass 2: NO full reindex happened in between ───────────────────────
	// The graph is still the c1 graph. The pass must therefore still ask for
	// the reindex, and must still label the graph c1. Against the defective
	// code pass 1 wrote GitCommit=c2, so this pass sees HEAD==manifest, no
	// head-advance, matching stamps, zero changes — and reports Done=true
	// over a graph that was never rebuilt. That is the self-conceal.
	res2 := extractors.TryIncremental(context.Background(), repo, stateDir, nil, nil)
	if res2.Done {
		t.Errorf("pass 2: Done=true with no reindex in between — the advanced label concealed the stale graph and the reindex is never requested again")
	}

	after2 := diff.LoadManifest(stateDir)
	if after2.GitCommit != c1Short {
		t.Errorf("pass 2: manifest GitCommit = %q, want %q — the label must keep describing the graph until a reindex actually rebuilds it", after2.GitCommit, c1Short)
	}
	if after2.GitCommitFull != c1Full {
		t.Errorf("pass 2: manifest GitCommitFull = %q, want %q", after2.GitCommitFull, c1Full)
	}
}

// TestIncremental_ZeroChangePassDoesAdvanceIndexedCommit pins the OTHER half of
// the #5822 ② decision, so a later reader does not "simplify" the fix into
// "never stamp the commit from HEAD".
//
// On the zero-change pass the graph genuinely DOES reflect the current HEAD:
// the head-advance detector ran, the commit-range diff was computed and
// CONFIRMED (unconfirmed takes the earlier fallback), and it produced no
// walked source file. A commit that touched only README/CI/.gitignore is
// exactly this. Refusing to advance there would leave manifest.GitCommit
// permanently behind HEAD, and the range diff would be recomputed on every
// single poll for the life of the repo.
//
// Fixture note, and it took a correction to get right: the second commit must
// touch ONLY a file the source walk does not surface, so the range diff is
// non-empty while the changed-SOURCE set stays empty. A first draft advanced
// HEAD by editing README.md — the walk surfaces markdown, so the pass took the
// ordinary INCREMENTAL SUCCESS path (ChangedFiles=1) and the zero-change branch
// this test names was never reached, while the assertion passed anyway.
// node_modules/ is in the walker's hard-exclude set (internal/daemon/walk),
// and the manifest is seeded with the walked set only, so the excluded file is
// not reported deleted either. res.ChangedFiles==0 is asserted for exactly
// that reason: it is what distinguishes this branch from the success path.
func TestIncremental_ZeroChangePassDoesAdvanceIndexedCommit(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()
	initGitRepo(t, repo)

	writeFile(t, repo, "a.go", "package p\n\nfunc F() {}\n")
	writeFile(t, repo, "node_modules/pkg/index.js", "module.exports = 1\n")
	c1Short := gitCommitAll(t, repo, "c1")

	buildMinimalGraph(t, stateDir, []graph.Entity{
		{ID: graph.EntityID("test-repo", "SCOPE.Operation", "F", "a.go"),
			Name: "F", Kind: "SCOPE.Operation", SourceFile: "a.go", Language: "go"},
	}, nil)
	m := diff.LoadManifest(stateDir)
	diff.UpdateManifest(repo, []string{"a.go"}, m) // walked set only
	if err := diff.SaveManifest(stateDir, repo, m); err != nil {
		t.Fatalf("seed manifest: %v", err)
	}

	// Advance HEAD without touching any file the source walk surfaces.
	writeFile(t, repo, "node_modules/pkg/index.js", "module.exports = 2\n")
	c2Short := gitCommitAll(t, repo, "c2")
	if c1Short == c2Short {
		t.Fatalf("fixture: HEAD did not move")
	}

	res := extractors.TryIncremental(context.Background(), repo, stateDir, nil, nil)
	if !res.Done {
		t.Fatalf("expected the zero-change no-op, got fallback %q — this test is not exercising the zero-change branch", res.FallbackReason)
	}
	if res.ChangedFiles != 0 {
		t.Fatalf("expected the ZERO-change branch, but %d file(s) were re-extracted — this pass took the incremental SUCCESS path, whose manifest write is a different call site", res.ChangedFiles)
	}

	m = diff.LoadManifest(stateDir)
	if m.GitCommit != c2Short {
		t.Errorf("zero-change pass: manifest GitCommit = %q, want %q. This pass CONFIRMED that no walked source file differs between the two commits, so the existing graph does describe HEAD; pinning it at %q would re-run the range diff on every poll forever.", m.GitCommit, c2Short, c1Short)
	}
}
