// whoami_sha_6079_test.go — issue #6079 at its HEADLINE symptom.
//
// groupIndexedRefSHA is what grafel_whoami reports as `indexed_sha` when the
// caller passes an explicit group=. It is the field a user actually looks at to
// answer "which commit is the graph I am querying built from", so a stale SHA
// here is the observable form of this defect — and it was the one surface with
// no test: reverting either of its two gitmeta call sites to the
// HEAD-pointer-keyed CaptureCached left the whole internal/mcp suite green.
//
// Both call sites are covered below, because they are reached under different
// preconditions and a fix to one does not exercise the other.
package mcp

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/gitmeta"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// commitOnBranch adds a commit to repoDir's CURRENT branch (no checkout, no
// branch creation) and returns the resulting abbreviated SHA. This is the event
// the HEAD-pointer cache key cannot see.
func commitOnBranch(t *testing.T, repoDir, content string) string {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(repoDir, "f.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", content)
	return gitmeta.Capture(repoDir).SHA
}

// assertHeadPointerUnmoved is the non-vacuity guard shared by both cases: it
// captures the HEAD pointer's exact bytes BEFORE the commit and returns a
// checker to run after, so "the commit did not rewrite HEAD" is asserted about
// the commit itself rather than about some later interval. Without this the test
// could pass for the wrong reason — a branch change would invalidate the old key
// on its own and prove nothing about #6079.
func assertHeadPointerUnmoved(t *testing.T, repoDir string) func() {
	t.Helper()
	headPath := filepath.Join(repoDir, ".git", "HEAD")
	before, err := os.ReadFile(headPath)
	if err != nil {
		t.Fatalf("read HEAD: %v", err)
	}
	fiBefore, err := os.Stat(headPath)
	if err != nil {
		t.Fatalf("stat HEAD: %v", err)
	}
	return func() {
		t.Helper()
		after, err := os.ReadFile(headPath)
		if err != nil {
			t.Fatalf("read HEAD after: %v", err)
		}
		fiAfter, err := os.Stat(headPath)
		if err != nil {
			t.Fatalf("stat HEAD after: %v", err)
		}
		if string(after) != string(before) {
			t.Fatalf("vacuous: the commit rewrote HEAD (%q -> %q) — the pre-existing key would have caught that", before, after)
		}
		if !fiAfter.ModTime().Equal(fiBefore.ModTime()) || fiAfter.Size() != fiBefore.Size() {
			t.Fatalf("vacuous: the commit moved the HEAD pointer's mtime/size (%v/%d -> %v/%d)",
				fiBefore.ModTime(), fiBefore.Size(), fiAfter.ModTime(), fiAfter.Size())
		}
	}
}

// TestWhoamiIndexedSHA_FreshAfterSameBranchCommit_LoadedRepo covers
// tools.go's RESIDENT-GRAPH path: a repo whose graph predates the IndexedSHA
// field (Doc.IndexedSHA == "") falls through to gitmeta, which is where the
// stale key bit.
func TestWhoamiIndexedSHA_FreshAfterSameBranchCommit_LoadedRepo(t *testing.T) {
	t.Setenv(daemon.EnvRoot, t.TempDir())
	// #6101: never read the developer's real state directory.
	t.Setenv("GRAFEL_HOME", t.TempDir())

	repoDir := gitRepoForDiscovery(t)
	// genDocWithMarker leaves IndexedRef/IndexedSHA empty, which is exactly the
	// pre-#2088 graph shape that routes groupIndexedRefSHA to gitmeta.
	if _, err := fbwriter.WriteGraphGen(daemon.StateDirForRepoRef(repoDir, "main"), genDocWithMarker("e")); err != nil {
		t.Fatalf("publish graph: %v", err)
	}

	reg := &Registry{Groups: map[string]RegistryGroup{
		"test": {Repos: map[string]RegistryRepo{"r": {Path: repoDir}}},
	}}
	st := NewState(reg)
	t.Cleanup(st.Close)
	if _, _, err := st.reloadLocked(); err != nil {
		t.Fatalf("initial reload: %v", err)
	}

	lr := st.Group("test").Repos["r"]
	if lr == nil || lr.Doc == nil {
		t.Fatal("fixture degenerate: repo not loaded")
	}
	if lr.Doc.IndexedSHA != "" {
		t.Fatalf("fixture degenerate: Doc.IndexedSHA=%q, so the gitmeta path is never reached", lr.Doc.IndexedSHA)
	}

	_, firstSHA := groupIndexedRefSHA(st, "test")
	if firstSHA == "" {
		t.Fatal("fixture degenerate: no SHA reported before the commit")
	}

	checkHead := assertHeadPointerUnmoved(t, repoDir)
	wantSHA := commitOnBranch(t, repoDir, "second")
	checkHead()
	if wantSHA == "" || wantSHA == firstSHA {
		t.Fatalf("fixture degenerate: SHA did not advance (%q -> %q)", firstSHA, wantSHA)
	}

	gotRef, gotSHA := groupIndexedRefSHA(st, "test")
	if gotRef != "main" {
		t.Fatalf("vacuous: ref moved to %q — the branch was supposed to stay put", gotRef)
	}
	if gotSHA != wantSHA {
		t.Errorf("grafel_whoami would report a stale indexed_sha after a same-branch commit:\n got  %q\n want %q",
			gotSHA, wantSHA)
	}
}

// TestWhoamiIndexedSHA_FreshAfterSameBranchCommit_RegistryFallback covers
// tools.go's REGISTRY-FALLBACK path, taken when the group has no repo with a
// resident Doc (e.g. the first whoami before any graph is read into memory).
// It is a separate gitmeta call site and was equally unpinned.
func TestWhoamiIndexedSHA_FreshAfterSameBranchCommit_RegistryFallback(t *testing.T) {
	t.Setenv(daemon.EnvRoot, t.TempDir())
	t.Setenv("GRAFEL_HOME", t.TempDir())

	repoDir := gitRepoForDiscovery(t)
	// Deliberately NO graph on disk: every repo stays Doc==nil, so
	// groupIndexedRefSHA falls past the resident-graph loop to the registry.
	reg := &Registry{Groups: map[string]RegistryGroup{
		"test": {Repos: map[string]RegistryRepo{"r": {Path: repoDir}}},
	}}
	st := NewState(reg)
	t.Cleanup(st.Close)

	if lg := st.Group("test"); lg != nil {
		for slug, lr := range lg.Repos {
			if lr != nil && lr.Doc != nil {
				t.Fatalf("fixture degenerate: repo %q has a resident Doc, so the registry fallback is not reached", slug)
			}
		}
	}

	_, firstSHA := groupIndexedRefSHA(st, "test")
	if firstSHA == "" {
		t.Fatal("fixture degenerate: no SHA reported before the commit")
	}

	checkHead := assertHeadPointerUnmoved(t, repoDir)
	wantSHA := commitOnBranch(t, repoDir, "second")
	checkHead()
	if wantSHA == firstSHA {
		t.Fatalf("fixture degenerate: SHA did not advance (%q)", firstSHA)
	}

	gotRef, gotSHA := groupIndexedRefSHA(st, "test")
	if gotRef != "main" {
		t.Fatalf("vacuous: ref moved to %q", gotRef)
	}
	if gotSHA != wantSHA {
		t.Errorf("grafel_whoami registry fallback would report a stale indexed_sha:\n got  %q\n want %q",
			gotSHA, wantSHA)
	}
}
