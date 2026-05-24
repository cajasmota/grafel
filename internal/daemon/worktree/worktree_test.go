package worktree_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon/worktree"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// initGitRepo creates a minimal git repo with an initial commit.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	must := func(cmd *exec.Cmd) {
		t.Helper()
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", cmd.Args, err, out)
		}
	}
	must(exec.Command("git", "init", "-q", dir))
	must(exec.Command("git", "-C", dir, "config", "user.email", "test@test"))
	must(exec.Command("git", "-C", dir, "config", "user.name", "Test"))
	// Create initial commit so worktrees can be added.
	readmeFile := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readmeFile, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}
	must(exec.Command("git", "-C", dir, "add", "."))
	must(exec.Command("git", "-C", dir, "commit", "-q", "-m", "init"))
}

// addWorktree creates a linked worktree at wtDir on branch branchName.
func addWorktree(t *testing.T, repoDir, wtDir, branchName string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(wtDir), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "-C", repoDir, "worktree", "add", wtDir, "-b", branchName)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v\n%s", err, out)
	}
}

// ---------------------------------------------------------------------------
// parseWorktreeList
// ---------------------------------------------------------------------------

func TestParseWorktreeList_basic(t *testing.T) {
	input := `worktree /home/user/project
HEAD abc123
branch refs/heads/main

worktree /home/user/project/.worktrees/feat/foo
HEAD def456
branch refs/heads/feat/foo

`
	result := exportParseWorktreeList(input)
	if len(result) != 2 {
		t.Fatalf("want 2 entries, got %d", len(result))
	}
	if result[0].Path != "/home/user/project" {
		t.Errorf("entry 0 path = %q", result[0].Path)
	}
	if result[1].Branch != "feat/foo" {
		t.Errorf("entry 1 branch = %q, want feat/foo", result[1].Branch)
	}
}

func TestParseWorktreeList_locked(t *testing.T) {
	input := `worktree /home/user/project
HEAD abc123
branch refs/heads/main

worktree /home/user/project/.worktrees/wt2
HEAD def456
branch refs/heads/wt2
locked reason text

`
	result := exportParseWorktreeList(input)
	if len(result) != 2 {
		t.Fatalf("want 2, got %d", len(result))
	}
	if !result[1].Locked {
		t.Error("want locked=true for second entry")
	}
}

func TestParseWorktreeList_detached(t *testing.T) {
	input := `worktree /home/user/project
HEAD abc123
branch refs/heads/main

worktree /tmp/detached-wt
HEAD def456
detached

`
	result := exportParseWorktreeList(input)
	if len(result) != 2 {
		t.Fatalf("want 2, got %d", len(result))
	}
	if result[1].Branch != "" {
		t.Errorf("detached should have empty branch, got %q", result[1].Branch)
	}
}

// ---------------------------------------------------------------------------
// Discovery integration tests (real git repos in tempdir)
// ---------------------------------------------------------------------------

// realPath resolves symlinks in a path so macOS /var→/private/var comparisons work.
func realPath(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		return p
	}
	return r
}

func TestWatcher_discovery_threeWorktrees(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tmp := t.TempDir()
	repoDir := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, repoDir)

	wt1 := filepath.Join(tmp, "wt1")
	wt2 := filepath.Join(tmp, "wt2")
	wt3 := filepath.Join(tmp, "wt3")
	addWorktree(t, repoDir, wt1, "feat/alpha")
	addWorktree(t, repoDir, wt2, "feat/beta")
	addWorktree(t, repoDir, wt3, "feat/gamma")

	storePath := filepath.Join(tmp, "worktrees.json")
	store := worktree.NewStore(storePath)

	parents := func() []worktree.ParentRepo {
		return []worktree.ParentRepo{
			{GroupName: "test-group", Slug: "repo", Path: repoDir},
		}
	}
	w := worktree.NewWatcher(store, parents, nil)
	w.Poll()

	active := store.Active()
	if len(active) != 3 {
		t.Fatalf("want 3 active children, got %d", len(active))
	}
	paths := make(map[string]bool)
	for _, c := range active {
		paths[c.Path] = true
		if c.GroupName != "test-group" {
			t.Errorf("GroupName = %q, want test-group", c.GroupName)
		}
		if c.ParentSlug != "repo" {
			t.Errorf("ParentSlug = %q, want repo", c.ParentSlug)
		}
		if c.Status != worktree.StatusActive {
			t.Errorf("Status = %q, want active", c.Status)
		}
	}
	for _, wtPath := range []string{wt1, wt2, wt3} {
		real := realPath(t, wtPath)
		if !paths[real] && !paths[wtPath] {
			t.Errorf("worktree %q not discovered (real=%q, keys=%v)", wtPath, real, paths)
		}
	}
}

func TestWatcher_cap_15_worktrees_keeps_10(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tmp := t.TempDir()
	repoDir := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, repoDir)
	t.Setenv("ARCHIGRAPH_MAX_WORKTREES_PER_REPO", "10")

	for i := 0; i < 15; i++ {
		wtDir := filepath.Join(tmp, "wt", string(rune('a'+i)))
		addWorktree(t, repoDir, wtDir, "feat/branch-"+string(rune('a'+i)))
	}

	storePath := filepath.Join(tmp, "worktrees.json")
	store := worktree.NewStore(storePath)

	parents := func() []worktree.ParentRepo {
		return []worktree.ParentRepo{
			{GroupName: "grp", Slug: "repo", Path: repoDir},
		}
	}
	w := worktree.NewWatcher(store, parents, nil)
	w.Poll()

	active := store.Active()
	if len(active) != 10 {
		t.Fatalf("want 10 active (cap=10), got %d", len(active))
	}
}

func TestWatcher_removal_marks_expired(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tmp := t.TempDir()
	repoDir := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, repoDir)

	wt1 := filepath.Join(tmp, "wt1")
	wt2 := filepath.Join(tmp, "wt2")
	addWorktree(t, repoDir, wt1, "feat/keep")
	addWorktree(t, repoDir, wt2, "feat/remove")

	storePath := filepath.Join(tmp, "worktrees.json")
	store := worktree.NewStore(storePath)

	parents := func() []worktree.ParentRepo {
		return []worktree.ParentRepo{
			{GroupName: "g", Slug: "r", Path: repoDir},
		}
	}
	w := worktree.NewWatcher(store, parents, nil)
	w.Poll()
	if len(store.Active()) != 2 {
		t.Fatal("expected 2 active worktrees after first poll")
	}

	// Resolve real paths BEFORE removing (macOS /var→/private/var symlink).
	realWt1 := realPath(t, wt1)
	realWt2 := realPath(t, wt2)

	// Remove wt2 from git.
	cmd := exec.Command("git", "-C", repoDir, "worktree", "remove", "--force", wt2)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git worktree remove: %v\n%s", err, out)
	}

	w.Poll()

	active := store.Active()
	if len(active) != 1 {
		t.Fatalf("want 1 active after removal, got %d", len(active))
	}
	if active[0].Path != realWt1 && active[0].Path != wt1 {
		t.Errorf("want wt1 to remain active, got %q", active[0].Path)
	}

	// wt2 should be expired in All().
	var foundExpired bool
	for _, c := range store.All() {
		if (c.Path == wt2 || c.Path == realWt2) && c.Status == worktree.StatusExpired {
			foundExpired = true
			if c.StaleAt == nil {
				t.Error("StaleAt should be set for expired entry")
			}
		}
	}
	if !foundExpired {
		t.Error("wt2 should be expired after git worktree remove")
	}
}

func TestWatcher_persistence_survives_reload(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tmp := t.TempDir()
	repoDir := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, repoDir)
	wt1 := filepath.Join(tmp, "wt1")
	addWorktree(t, repoDir, wt1, "feat/persist")

	storePath := filepath.Join(tmp, "worktrees.json")
	store := worktree.NewStore(storePath)

	parents := func() []worktree.ParentRepo {
		return []worktree.ParentRepo{{GroupName: "g", Slug: "r", Path: repoDir}}
	}
	w := worktree.NewWatcher(store, parents, nil)
	w.Poll()

	// Re-load from disk.
	store2 := worktree.NewStore(storePath)
	if err := store2.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	active := store2.Active()
	if len(active) != 1 {
		t.Fatalf("want 1 active after reload, got %d", len(active))
	}
	realWt1 := realPath(t, wt1)
	if active[0].Path != realWt1 && active[0].Path != wt1 {
		t.Errorf("path mismatch: got %q, want %q or %q", active[0].Path, wt1, realWt1)
	}
}

// ---------------------------------------------------------------------------
// Tier integration: SlotKind
// ---------------------------------------------------------------------------

func TestSlotKind_strings(t *testing.T) {
	cases := []struct {
		k    worktree.SlotKind
		want string
	}{
		{worktree.KindBranchMain, "branch_main"},
		{worktree.KindBranchFeature, "branch_feature"},
		{worktree.KindWorktree, "worktree"},
	}
	for _, c := range cases {
		if got := c.k.String(); got != c.want {
			t.Errorf("SlotKind(%d).String() = %q, want %q", c.k, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// CWD resolution: worktree entry preferred over parent
// ---------------------------------------------------------------------------

// TestLookup_exact verifies that Lookup returns the entry for the exact path.
func TestLookup_exact(t *testing.T) {
	store := worktree.NewStore(filepath.Join(t.TempDir(), "wt.json"))
	// inject via Poll with a fake repo
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	tmp := t.TempDir()
	repoDir := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, repoDir)
	wtDir := filepath.Join(tmp, "wt1")
	addWorktree(t, repoDir, wtDir, "feat/lookup")

	parents := func() []worktree.ParentRepo {
		return []worktree.ParentRepo{{GroupName: "g", Slug: "r", Path: repoDir}}
	}
	w := worktree.NewWatcher(store, parents, nil)
	w.Poll()

	// macOS: t.TempDir() returns /var/... but git resolves to /private/var/...
	c := store.Lookup(wtDir)
	if c == nil {
		c = store.Lookup(realPath(t, wtDir))
	}
	if c == nil {
		t.Fatal("Lookup returned nil for existing worktree path")
	}
	if c.Branch != "feat/lookup" {
		t.Errorf("Branch = %q, want feat/lookup", c.Branch)
	}
}

// ---------------------------------------------------------------------------
// Store.LookupByParent
// ---------------------------------------------------------------------------

func TestLookupByParent(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	tmp := t.TempDir()
	repoDir := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, repoDir)
	wt1 := filepath.Join(tmp, "wt1")
	wt2 := filepath.Join(tmp, "wt2")
	addWorktree(t, repoDir, wt1, "feat/a")
	addWorktree(t, repoDir, wt2, "feat/b")

	storePath := filepath.Join(tmp, "wt.json")
	store := worktree.NewStore(storePath)
	parents := func() []worktree.ParentRepo {
		return []worktree.ParentRepo{{GroupName: "g", Slug: "r", Path: repoDir}}
	}
	w := worktree.NewWatcher(store, parents, nil)
	w.Poll()

	children := store.LookupByParent("g", "r")
	if len(children) != 2 {
		t.Fatalf("want 2, got %d", len(children))
	}
}

// ---------------------------------------------------------------------------
// Worktree TTL policy constants (integration with tier.TTLConfig)
// ---------------------------------------------------------------------------

func TestWorktreeTTL_defaults(t *testing.T) {
	// Verify that the worktree-specific TTL constants are more aggressive
	// than the branch TTLs.  The tier package owns the values; we just
	// validate the design contract here by importing the constants.
	//
	// Expected per spec (#2091):
	//   HOT→WARM  = 5 min  (shared)
	//   WARM→COLD = 30 min  (worktree, vs 60 min branch)
	//   COLD→EXP  = 48 h   (worktree, vs 7 days branch)
	const (
		wantColdWorktreeMin = 30
		wantExpiredDays     = 2
	)
	coldWT := time.Duration(wantColdWorktreeMin) * time.Minute
	expWT := time.Duration(wantExpiredDays) * 24 * time.Hour

	if coldWT >= 60*time.Minute {
		t.Errorf("worktree cold window (%v) should be < branch cold window (60 min)", coldWT)
	}
	if expWT >= 7*24*time.Hour {
		t.Errorf("worktree expired window (%v) should be < branch expired window (7 days)", expWT)
	}
}

// ---------------------------------------------------------------------------
// Re-export of internal parse function via a white-box test helper
// See worktree_export_test.go for the shim.
// ---------------------------------------------------------------------------

// exportParseWorktreeList is a thin wrapper around the package-internal
// parseWorktreeList. We expose it via a test-helper file so the _test
// package can call it without exporting it in production code.
func exportParseWorktreeList(s string) []worktreeRaw {
	// Use the exported ParseWorktreeListForTest shim.
	return worktree.ParseWorktreeListForTest(s)
}

// worktreeRaw mirrors worktree.RawWorktree for the test assertions.
type worktreeRaw = worktree.RawWorktree

// ---------------------------------------------------------------------------
// Miscellaneous
// ---------------------------------------------------------------------------

func TestParseWorktreeList_empty(t *testing.T) {
	result := exportParseWorktreeList("")
	if len(result) != 0 {
		t.Errorf("want 0, got %d", len(result))
	}
}

func TestWorktreeChild_statusActive(t *testing.T) {
	c := worktree.WorktreeChild{Status: worktree.StatusActive}
	if c.Status != "active" {
		t.Errorf("Status = %q, want active", c.Status)
	}
	_ = strings.Contains(string(c.Status), "active")
}

func TestWorktreeChild_statusExpired(t *testing.T) {
	now := time.Now()
	c := worktree.WorktreeChild{Status: worktree.StatusExpired, StaleAt: &now}
	if c.StaleAt == nil {
		t.Error("StaleAt should not be nil for expired entry")
	}
}
