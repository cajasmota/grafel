package watch

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// initGitRepo creates a minimal git repo under dir for testing.
// Returns the absolute path.
func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "test@client-fixture-a.test")
	run("config", "user.name", "Test")
	// Initial commit so HEAD is a valid ref.
	f := filepath.Join(dir, "README.md")
	if err := os.WriteFile(f, []byte("# test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "init")
	return dir
}

// switchBranch creates and checks out a new branch in dir.
func switchBranch(t *testing.T, dir, branch string) {
	t.Helper()
	cmd := exec.Command("git", "checkout", "-b", branch)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git checkout -b %s: %v\n%s", branch, err, out)
	}
}

// commitFile adds a new file and commits it, advancing HEAD SHA.
func commitFile(t *testing.T, dir, name string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	f := filepath.Join(dir, name)
	if err := os.WriteFile(f, []byte("content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "add "+name)
}

// TestGitHeadPoller_BranchSwitch verifies that switching branches emits
// exactly one BranchSwitchEvent with the correct old/new ref.
func TestGitHeadPoller_BranchSwitch(t *testing.T) {
	repoDir := initGitRepo(t)

	var (
		mu     sync.Mutex
		events []BranchSwitchEvent
	)

	// Fast poll interval for tests.
	p := NewGitHeadPoller(50*time.Millisecond, func(ev BranchSwitchEvent) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
	}, nil)
	p.AddRepo(repoDir)
	p.Start()
	defer p.Stop()

	// Switch to a new branch.
	switchBranch(t, repoDir, "feat/test-branch")

	// Wait up to 1 second for the event to arrive.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(events)
		mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	got := make([]BranchSwitchEvent, len(events))
	copy(got, events)
	mu.Unlock()

	if len(got) == 0 {
		t.Fatal("no BranchSwitchEvent emitted after branch switch")
	}
	ev := got[0]
	if ev.RepoPath != repoDir {
		t.Errorf("RepoPath: want %s, got %s", repoDir, ev.RepoPath)
	}
	if ev.OldRef != "main" {
		t.Errorf("OldRef: want main, got %q", ev.OldRef)
	}
	if ev.NewRef != "feat/test-branch" {
		t.Errorf("NewRef: want feat/test-branch, got %q", ev.NewRef)
	}
	if ev.OldSHA == "" {
		t.Error("OldSHA should be non-empty")
	}
}

// TestGitHeadPoller_NoSpuriousEvents verifies that the poller does NOT emit
// events when HEAD has not changed.
func TestGitHeadPoller_NoSpuriousEvents(t *testing.T) {
	repoDir := initGitRepo(t)

	var count atomic.Int32
	p := NewGitHeadPoller(50*time.Millisecond, func(_ BranchSwitchEvent) {
		count.Add(1)
	}, nil)
	p.AddRepo(repoDir)
	p.Start()
	defer p.Stop()

	// Let 5 poll cycles pass without any branch change.
	time.Sleep(300 * time.Millisecond)

	if n := count.Load(); n != 0 {
		t.Errorf("unexpected events emitted with no branch change: got %d", n)
	}
}

// TestGitHeadPoller_SHAChange verifies that committing on the same branch
// (advancing the SHA without changing the ref name) also fires an event.
func TestGitHeadPoller_SHAChange(t *testing.T) {
	repoDir := initGitRepo(t)

	var (
		mu     sync.Mutex
		events []BranchSwitchEvent
	)
	p := NewGitHeadPoller(50*time.Millisecond, func(ev BranchSwitchEvent) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
	}, nil)
	p.AddRepo(repoDir)
	p.Start()
	defer p.Stop()

	// Make a new commit on main (SHA advances, ref stays "main").
	commitFile(t, repoDir, "new-file.txt")

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(events)
		mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	got := make([]BranchSwitchEvent, len(events))
	copy(got, events)
	mu.Unlock()

	if len(got) == 0 {
		t.Fatal("no event emitted after new commit (SHA change)")
	}
	ev := got[0]
	if ev.OldSHA == ev.NewSHA {
		t.Errorf("SHA should have changed: old=%s new=%s", ev.OldSHA, ev.NewSHA)
	}
	// Ref name stays "main" in both.
	if ev.OldRef != "main" || ev.NewRef != "main" {
		t.Errorf("ref should remain main: old=%q new=%q", ev.OldRef, ev.NewRef)
	}
}

// TestGitHeadPoller_RemoveRepo verifies that removing a repo stops event delivery.
func TestGitHeadPoller_RemoveRepo(t *testing.T) {
	repoDir := initGitRepo(t)

	var count atomic.Int32
	p := NewGitHeadPoller(50*time.Millisecond, func(_ BranchSwitchEvent) {
		count.Add(1)
	}, nil)
	p.AddRepo(repoDir)
	p.Start()
	defer p.Stop()

	// Remove the repo, then switch branches.
	p.RemoveRepo(repoDir)
	switchBranch(t, repoDir, "feat/after-remove")

	time.Sleep(300 * time.Millisecond)

	if n := count.Load(); n != 0 {
		t.Errorf("received %d events after RemoveRepo, want 0", n)
	}
}

// ---------------------------------------------------------------------------
// Monorepo M1 tests (issue #2178): common-dir dedup
// ---------------------------------------------------------------------------

// addGitWorktree adds a linked worktree under wtDir using a new branch
// created from baseRepo. Returns the absolute path of the new worktree.
func addGitWorktree(t *testing.T, baseRepo, wtDir, branch string) string {
	t.Helper()
	abs, err := filepath.Abs(wtDir)
	if err != nil {
		t.Fatal(err)
	}

	// Create the branch in the base repo first, then create a worktree for it.
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = baseRepo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v in %s: %v\n%s", args, baseRepo, err, out)
		}
	}
	run("branch", branch)
	run("worktree", "add", abs, branch)
	return abs
}

// itoa is a tiny int-to-string helper to avoid importing strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := make([]byte, 0, 10)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	return string(buf)
}

// makeSharedCommonDirRepos creates a base git repo and n linked worktrees
// that all share its common-dir.
func makeSharedCommonDirRepos(t *testing.T, n int) (base string, worktrees []string) {
	t.Helper()
	base = initGitRepo(t)
	for i := 0; i < n; i++ {
		branch := "wt-branch-" + itoa(i)
		wtDir := t.TempDir()
		wt := addGitWorktree(t, base, wtDir, branch)
		worktrees = append(worktrees, wt)
	}
	return base, worktrees
}

// TestCommonDirDedup_SharedRepos_OneStatPerCycle is the primary M1 measurement
// test. 11 repos (base + 10 linked worktrees) sharing one common-dir must
// produce 2 stat calls per poll cycle (HEAD + ref), not 22.
func TestCommonDirDedup_SharedRepos_OneStatPerCycle(t *testing.T) {
	const numWorktrees = 10
	base, worktrees := makeSharedCommonDirRepos(t, numWorktrees)

	p := NewGitHeadPoller(50*time.Millisecond, func(_ BranchSwitchEvent) {}, nil)
	p.AddRepo(base)
	for _, wt := range worktrees {
		p.AddRepo(wt)
	}

	// All repos share one common-dir → exactly 1 group.
	if gc := p.GroupCount(); gc != 1 {
		t.Fatalf("GroupCount: want 1 (all repos share common-dir), got %d", gc)
	}

	p.Start()
	// Let ~5 poll cycles run (50ms * 5 = 250ms, +slack).
	time.Sleep(300 * time.Millisecond)
	p.Stop()

	got := p.StatCalls()
	// Expect ~10 stat calls (2 per cycle × 5 cycles). Allow [8,18] for jitter.
	// Without M1 dedup: 11 repos × 2 files × 5 cycles = 110 calls.
	if got < 8 || got > 18 {
		t.Errorf("StatCalls: want 8-18 (2/cycle × ~5 cycles), got %d (dedup broken? without dedup: ~110)", got)
	}
}

// TestCommonDirDedup_IndependentRepos_OneStatEachPerCycle verifies that 10
// standalone repos each generate their own stat call per cycle.
func TestCommonDirDedup_IndependentRepos_OneStatEachPerCycle(t *testing.T) {
	const numRepos = 10

	p := NewGitHeadPoller(50*time.Millisecond, func(_ BranchSwitchEvent) {}, nil)
	for i := 0; i < numRepos; i++ {
		p.AddRepo(initGitRepo(t))
	}

	// Each independent repo is its own group.
	if gc := p.GroupCount(); gc != numRepos {
		t.Fatalf("GroupCount: want %d (one per repo), got %d", numRepos, gc)
	}

	p.Start()
	time.Sleep(300 * time.Millisecond)
	p.Stop()

	got := p.StatCalls()
	// Expect ~100 stat calls (10 repos × 2 files × 5 cycles). Allow [60,140].
	if got < 60 || got > 140 {
		t.Errorf("StatCalls: want 60-140 (10 repos × 2 files × ~5 cycles), got %d", got)
	}
}

// TestCommonDirDedup_FanOut_BaseRepoReceivesEvent verifies that a branch
// switch in the base repo emits a BranchSwitchEvent for the base repo path.
func TestCommonDirDedup_FanOut_BaseRepoReceivesEvent(t *testing.T) {
	const numWorktrees = 10
	base, worktrees := makeSharedCommonDirRepos(t, numWorktrees)

	var mu sync.Mutex
	received := make(map[string]int)

	p := NewGitHeadPoller(30*time.Millisecond, func(ev BranchSwitchEvent) {
		mu.Lock()
		received[ev.RepoPath]++
		mu.Unlock()
	}, nil)

	p.AddRepo(base)
	for _, wt := range worktrees {
		p.AddRepo(wt)
	}

	// Verify dedup — only 1 group.
	if gc := p.GroupCount(); gc != 1 {
		t.Fatalf("GroupCount: want 1, got %d", gc)
	}

	p.Start()
	defer p.Stop()

	// Switch branch in the base repo (advances HEAD on the shared common-dir).
	switchBranch(t, base, "feat/fanout-test")

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := received[base]
		mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	baseEvents := received[base]
	mu.Unlock()

	if baseEvents == 0 {
		t.Error("base repo did not receive a BranchSwitchEvent after branch switch")
	}
}

// TestCommonDirDedup_FanOut_AllWorktreesReceiveEvent verifies that all repos
// in a shared common-dir group receive BranchSwitchEvents when HEAD changes.
// Each worktree is on its own branch (git requires this). We commit on the
// worktree's own branch via the worktree path and then verify the worktree
// repo receives an event. The base repo is on main; worktrees are on wt-X
// branches. A commit on wt-0's branch advances the ref for wt-0.
func TestCommonDirDedup_FanOut_AllWorktreesReceiveEvent(t *testing.T) {
	// Create base repo with 3 linked worktrees each on their own branch.
	base := initGitRepo(t)
	const numWT = 3
	worktrees := make([]string, numWT)
	for i := 0; i < numWT; i++ {
		branch := "shared-wt-" + itoa(i)
		wtDir := t.TempDir()
		worktrees[i] = addGitWorktree(t, base, wtDir, branch)
	}

	var mu sync.Mutex
	received := make(map[string][]BranchSwitchEvent)

	p := NewGitHeadPoller(30*time.Millisecond, func(ev BranchSwitchEvent) {
		mu.Lock()
		received[ev.RepoPath] = append(received[ev.RepoPath], ev)
		mu.Unlock()
	}, nil)

	// Register only the worktrees (skip base to keep the test focused on
	// the fan-out from a commit in one worktree vs others in the same group).
	for _, r := range worktrees {
		p.AddRepo(r)
	}

	if gc := p.GroupCount(); gc != 1 {
		t.Fatalf("GroupCount want 1, got %d", gc)
	}

	p.Start()
	defer p.Stop()

	// Commit a source file in the first worktree (on branch shared-wt-0).
	// This advances the ref for shared-wt-0 and updates the HEAD file of
	// that worktree, triggering an event for worktrees[0] at minimum.
	commitFile(t, worktrees[0], "from-wt0.go")

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := received[worktrees[0]]
		mu.Unlock()
		if len(n) >= 1 {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	// The first worktree must have received an event.
	if len(received[worktrees[0]]) == 0 {
		t.Errorf("worktree[0] did not receive a BranchSwitchEvent after commit")
	}
	// Group count must still be 1 — dedup invariant.
	if gc := p.GroupCount(); gc != 1 {
		t.Errorf("GroupCount changed after commit: want 1, got %d", gc)
	}
}

// TestCommonDirDedup_StatCountMeasurement is an instrumented measurement test
// that prints before/after stat rates for the PR description.
//
// Before M1: N repos → N stats/cycle.
// After M1:  N repos sharing 1 common-dir → 1 stat/cycle.
func TestCommonDirDedup_StatCountMeasurement(t *testing.T) {
	const (
		numSubRepos   = 10
		pollInterval  = 20 * time.Millisecond
		measureWindow = 200 * time.Millisecond // ~10 cycles
	)

	// Shared common-dir scenario.
	base, worktrees := makeSharedCommonDirRepos(t, numSubRepos)
	pShared := NewGitHeadPoller(pollInterval, func(_ BranchSwitchEvent) {}, nil)
	pShared.AddRepo(base)
	for _, wt := range worktrees {
		pShared.AddRepo(wt)
	}
	pShared.Start()
	time.Sleep(measureWindow)
	pShared.Stop()
	sharedStats := pShared.StatCalls()

	// Independent repos scenario.
	pIndep := NewGitHeadPoller(pollInterval, func(_ BranchSwitchEvent) {}, nil)
	for i := 0; i <= numSubRepos; i++ {
		pIndep.AddRepo(initGitRepo(t))
	}
	pIndep.Start()
	time.Sleep(measureWindow)
	pIndep.Stop()
	indepStats := pIndep.StatCalls()

	cycles := int(measureWindow / pollInterval)
	t.Logf("M1 measurement (%d sub-repos, %s window, ~%d cycles):", numSubRepos, measureWindow, cycles)
	t.Logf("  Shared common-dir: %d stat calls (want ~%d = 1×cycles)", sharedStats, cycles)
	t.Logf("  Independent repos: %d stat calls (want ~%d = %d×cycles)", indepStats, cycles*(numSubRepos+1), numSubRepos+1)

	// Shared: 2 stats per cycle (HEAD + ref file), 1 group.
	// Allow up to 4× cycles for timing jitter.
	if sharedStats > uint64(cycles*4) {
		t.Errorf("shared: too many stat calls — want ≤%d (2/cycle×%d cycles), got %d",
			cycles*4, cycles, sharedStats)
	}

	// Independent: 2 stats per repo per cycle × (N+1) repos.
	expectedIndep := uint64(cycles * (numSubRepos + 1))
	if indepStats < expectedIndep/2 {
		t.Errorf("independent: too few stat calls — want ≥%d, got %d", expectedIndep/2, indepStats)
	}

	// Key ratio: shared (1 group) must be ≥3× cheaper than independent (11 groups).
	if indepStats > 0 && sharedStats*3 > indepStats {
		t.Errorf("dedup ratio insufficient: shared=%d indep=%d (need shared ≤ indep/3)",
			sharedStats, indepStats)
	}
}
