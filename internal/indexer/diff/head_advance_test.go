package diff

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// FilterWithGit trusts `git diff --name-only HEAD` — working tree vs the
// CURRENT HEAD — and never hashes a file git did not report. That is only
// sound while the manifest was written at the SAME commit the repo is on now.
//
// #5964 (worktree graph seeding) breaks that premise on purpose: a worktree's
// state dir is seeded with its PARENT ref's manifest, so the manifest's commit
// is the parent's while the child's HEAD has already advanced. Every file that
// differs by a COMMIT then looks clean to `git diff HEAD` and is never hashed
// — its stale entities survive into the child's graph, invisibly.
//
// The same hole exists for any HEAD advance (fetch+reset / checkout / pull)
// that leaves a clean working tree; internal/extractors.TryIncremental papers
// over it with its own range diff (#5710), but Index()+WithIncremental does
// not. These tests pin the fix at the source.

func gitT(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestFilterWithGit_CatchesAFileChangedByACommitSinceTheManifest(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	repo := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.go", "package p\nfunc A() {}\n")
	write("b.go", "package p\nfunc B() {}\n")
	gitT(t, repo, "init", "-q", "-b", "trunk")
	gitT(t, repo, "add", "-A")
	gitT(t, repo, "commit", "-q", "-m", "one")

	// Manifest pinned at commit one, describing both files exactly.
	m := newManifest()
	files := []string{"a.go", "b.go"}
	UpdateManifest(repo, files, m)
	m.GitCommit = HeadCommit(repo)
	if m.GitCommit == "" {
		t.Fatal("no HEAD commit recorded")
	}

	// a.go changes and is COMMITTED. The working tree is now clean against
	// the new HEAD, so `git diff --name-only HEAD` reports nothing.
	write("a.go", "package p\nfunc A() {}\nfunc ANew() {}\n")
	gitT(t, repo, "add", "-A")
	gitT(t, repo, "commit", "-q", "-m", "two")
	if wt, err := GitChangedFiles(repo); err != nil || len(wt) != 0 {
		t.Fatalf("fixture is inert: working-tree diff must be empty, got %v (err %v)", wt, err)
	}

	changed, unchanged := FilterWithGit(repo, files, m)

	if !contains(changed, "a.go") {
		t.Errorf("changed=%v — a.go changed by a commit since the manifest and was NOT re-extracted; its stale entities would survive", changed)
	}
	if !contains(unchanged, "b.go") {
		t.Errorf("unchanged=%v — b.go did not change and must stay cached", unchanged)
	}
}

func TestFilterWithGit_KeepsEverythingCachedWhenHeadHasNotMoved(t *testing.T) {
	// The common case must not regress into a full hash sweep.
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	repo := t.TempDir()
	for _, n := range []string{"a.go", "b.go"} {
		if err := os.WriteFile(filepath.Join(repo, n), []byte("package p\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitT(t, repo, "init", "-q", "-b", "trunk")
	gitT(t, repo, "add", "-A")
	gitT(t, repo, "commit", "-q", "-m", "one")

	files := []string{"a.go", "b.go"}
	m := newManifest()
	UpdateManifest(repo, files, m)
	m.GitCommit = HeadCommit(repo)

	changed, unchanged := FilterWithGit(repo, files, m)
	if len(changed) != 0 {
		t.Errorf("changed=%v want none", changed)
	}
	if len(unchanged) != 2 {
		t.Errorf("unchanged=%v want both files", unchanged)
	}
}

func TestFilterWithGit_StillSeesUncommittedAndUntrackedWork(t *testing.T) {
	// The union must not lose what the working-tree diff already caught —
	// an agent's in-progress edits are the most common thing it asks about.
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	repo := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.go", "package p\nfunc A() {}\n")
	write("b.go", "package p\nfunc B() {}\n")
	gitT(t, repo, "init", "-q", "-b", "trunk")
	gitT(t, repo, "add", "-A")
	gitT(t, repo, "commit", "-q", "-m", "one")

	m := newManifest()
	UpdateManifest(repo, []string{"a.go", "b.go"}, m)
	m.GitCommit = HeadCommit(repo)

	// Committed change to a.go …
	write("a.go", "package p\nfunc A() {}\nfunc ACommitted() {}\n")
	gitT(t, repo, "add", "-A")
	gitT(t, repo, "commit", "-q", "-m", "two")
	// … uncommitted change to b.go …
	write("b.go", "package p\nfunc B() {}\nfunc BDirty() {}\n")
	// … and an untracked file.
	write("c.go", "package p\nfunc C() {}\n")

	files := []string{"a.go", "b.go", "c.go"}
	changed, _ := FilterWithGit(repo, files, m)
	for _, want := range files {
		if !contains(changed, want) {
			t.Errorf("changed=%v is missing %s", changed, want)
		}
	}
}

func TestFilterWithGit_FallsBackToHashesWhenTheManifestCommitIsUnreachable(t *testing.T) {
	// A manifest commit that git can no longer resolve (gc, shallow clone,
	// history rewrite) must NOT be read as "nothing changed since then".
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	repo := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.go", "package p\nfunc A() {}\n")
	gitT(t, repo, "init", "-q", "-b", "trunk")
	gitT(t, repo, "add", "-A")
	gitT(t, repo, "commit", "-q", "-m", "one")

	m := newManifest()
	UpdateManifest(repo, []string{"a.go"}, m)
	m.GitCommit = "0123456789abcdef0123456789abcdef01234567" // unreachable

	// a.go really did change, but only in a way a commit would have carried.
	write("a.go", "package p\nfunc A() {}\nfunc ANew() {}\n")
	gitT(t, repo, "add", "-A")
	gitT(t, repo, "commit", "-q", "-m", "two")

	changed, _ := FilterWithGit(repo, []string{"a.go"}, m)
	if !contains(changed, "a.go") {
		t.Errorf("changed=%v — an unresolvable manifest commit was treated as 'nothing changed'", changed)
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
